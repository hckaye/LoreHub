package runner

import (
	"bytes"
	"io"
	"sort"
	"strings"
)

type maskingLogWriter struct {
	writer  io.Writer
	secrets []string
	pending []byte
}

func newMaskingLogWriter(writer io.Writer, secrets map[string]string) *maskingLogWriter {
	values := make([]string, 0, len(secrets))
	for _, value := range secrets {
		if value != "" {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(left, right int) bool { return len(values[left]) > len(values[right]) })
	return &maskingLogWriter{writer: writer, secrets: values}
}

func (writer *maskingLogWriter) Write(contents []byte) (int, error) {
	originalLength := len(contents)
	writer.pending = append(writer.pending, contents...)
	for {
		lineEnd := bytes.IndexByte(writer.pending, '\n')
		if lineEnd < 0 {
			break
		}
		line := writer.pending[:lineEnd]
		writer.pending = writer.pending[lineEnd+1:]
		if err := writer.writeLine(line); err != nil {
			return originalLength, err
		}
	}
	limit := writer.pendingLimit()
	if len(writer.pending) > limit {
		flushLength := len(writer.pending) - limit
		if err := writer.writeBytes(writer.pending[:flushLength]); err != nil {
			return originalLength, err
		}
		writer.pending = writer.pending[flushLength:]
	}
	return originalLength, nil
}

func (writer *maskingLogWriter) Flush() error {
	if len(writer.pending) == 0 {
		return nil
	}
	if err := writer.writeLine(writer.pending); err != nil {
		return err
	}
	writer.pending = nil
	return nil
}

func (writer *maskingLogWriter) pendingLimit() int {
	limit := 4096
	for _, secret := range writer.secrets {
		if len(secret)+32 > limit {
			limit = len(secret) + 32
		}
	}
	return limit
}

func (writer *maskingLogWriter) writeLine(line []byte) error {
	if bytes.HasPrefix(line, []byte("::add-mask::")) {
		value := strings.TrimSuffix(string(line[len("::add-mask::"):]), "\r")
		if value != "" {
			writer.addSecret(value)
		}
		return writer.writeBytes([]byte("::add-mask::***\n"))
	}
	if err := writer.writeBytes(writer.redact(line)); err != nil {
		return err
	}
	return writer.writeBytes([]byte("\n"))
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
	for _, existing := range writer.secrets {
		if existing == value {
			return
		}
	}
	writer.secrets = append(writer.secrets, value)
	sort.Slice(writer.secrets, func(left, right int) bool {
		return len(writer.secrets[left]) > len(writer.secrets[right])
	})
}
