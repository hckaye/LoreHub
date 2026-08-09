package runner

import (
	"bytes"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
)

const defaultMaskingLineBytes = 1 << 20

var errMaskingLineTooLong = errors.New("Actions log line exceeds the masking limit")

type maskingLogWriter struct {
	mu          sync.Mutex
	writer      io.Writer
	secrets     []string
	pending     []byte
	maxLineSize int
	onError     func(error)
	err         error
}

func newMaskingLogWriter(writer io.Writer, secrets map[string]string) *maskingLogWriter {
	return newMaskingLogWriterWithLimit(writer, secrets, defaultMaskingLineBytes, nil)
}

func newMaskingLogWriterWithLimit(
	writer io.Writer,
	secrets map[string]string,
	maxLineSize int,
	onError func(error),
) *maskingLogWriter {
	if maxLineSize <= 0 {
		maxLineSize = defaultMaskingLineBytes
	}
	masked := &maskingLogWriter{
		writer:      writer,
		maxLineSize: maxLineSize,
		onError:     onError,
	}
	for _, value := range secrets {
		masked.addSecret(value)
	}
	return masked
}

func (writer *maskingLogWriter) Write(contents []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.err != nil {
		return 0, writer.err
	}
	writer.pending = append(writer.pending, contents...)
	for {
		lineEnd := bytes.IndexByte(writer.pending, '\n')
		if lineEnd < 0 {
			break
		}
		lineLength := lineEnd + 1
		if lineLength > writer.maxLineSize {
			return 0, writer.failLocked(errMaskingLineTooLong)
		}
		line := append([]byte(nil), writer.pending[:lineLength]...)
		writer.pending = writer.pending[lineLength:]
		if err := writer.writeLine(line); err != nil {
			return 0, writer.failLocked(err)
		}
	}
	if len(writer.pending) > writer.maxLineSize {
		return 0, writer.failLocked(errMaskingLineTooLong)
	}
	return len(contents), nil
}

func (writer *maskingLogWriter) Flush() error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.err != nil {
		return writer.err
	}
	if len(writer.pending) == 0 {
		return nil
	}
	if len(writer.pending) > writer.maxLineSize {
		return writer.failLocked(errMaskingLineTooLong)
	}
	line := append([]byte(nil), writer.pending...)
	writer.pending = nil
	if err := writer.writeLine(line); err != nil {
		return writer.failLocked(err)
	}
	return nil
}

func (writer *maskingLogWriter) Err() error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return writer.err
}

func (writer *maskingLogWriter) failLocked(err error) error {
	if writer.err != nil {
		return writer.err
	}
	writer.err = err
	writer.pending = nil
	if writer.onError != nil {
		writer.onError(err)
	}
	return err
}

func (writer *maskingLogWriter) writeLine(line []byte) error {
	body, ending := splitLineEnding(line)
	if bytes.HasPrefix(body, []byte("::add-mask::")) {
		value := strings.TrimSuffix(string(body[len("::add-mask::"):]), "\r")
		if value != "" {
			writer.addSecret(value)
		}
		return writer.writeBytes(append([]byte("::add-mask::***"), ending...))
	}
	return writer.writeBytes(append(writer.redact(body), ending...))
}

func splitLineEnding(line []byte) ([]byte, []byte) {
	if bytes.HasSuffix(line, []byte("\n")) {
		body := line[:len(line)-1]
		if bytes.HasSuffix(body, []byte("\r")) {
			return body[:len(body)-1], []byte("\r\n")
		}
		return body, []byte("\n")
	}
	return line, nil
}

func (writer *maskingLogWriter) writeBytes(contents []byte) error {
	_, err := writer.writer.Write(contents)
	return err
}

func (writer *maskingLogWriter) redact(contents []byte) []byte {
	redacted := contents
	for _, secret := range writer.secrets {
		redacted = bytes.ReplaceAll(redacted, []byte(secret), []byte("***"))
	}
	return redacted
}

func (writer *maskingLogWriter) addSecret(value string) {
	for _, representation := range secretRepresentations(value) {
		if representation == "" || containsString(writer.secrets, representation) {
			continue
		}
		writer.secrets = append(writer.secrets, representation)
	}
	sort.Slice(writer.secrets, func(left, right int) bool {
		return len(writer.secrets[left]) > len(writer.secrets[right])
	})
}

func secretRepresentations(value string) []string {
	if value == "" {
		return nil
	}
	representations := []string{value}
	if strings.Contains(value, "\r\n") {
		representations = append(representations, strings.ReplaceAll(value, "\r\n", "\n"))
	}
	if strings.ContainsAny(value, "\r\n") {
		for _, line := range strings.FieldsFunc(value, func(r rune) bool { return r == '\r' || r == '\n' }) {
			representations = append(representations, line)
		}
	}
	return representations
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
