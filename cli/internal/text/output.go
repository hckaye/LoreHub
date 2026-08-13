package text

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"golang.org/x/term"
)

type Writer struct {
	out io.Writer
	tty bool
}

func NewWriter(out io.Writer) Writer {
	return Writer{out: out, tty: isTTY(out)}
}

func (w Writer) IsTTY() bool {
	return w.tty
}

func (w Writer) JSON(value any) error {
	encoder := json.NewEncoder(w.out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func (w Writer) Table(headers []string, rows [][]string) error {
	if w.tty {
		return w.table(headers, rows)
	}
	return w.tsv(headers, rows)
}

func (w Writer) table(headers []string, rows [][]string) error {
	writer := tabwriter.NewWriter(w.out, 0, 4, 2, ' ', 0)
	if err := writeRow(writer, headers); err != nil {
		return err
	}
	for _, row := range rows {
		if err := writeRow(writer, row); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func (w Writer) tsv(headers []string, rows [][]string) error {
	if err := writeTSVRow(w.out, headers); err != nil {
		return err
	}
	for _, row := range rows {
		if err := writeTSVRow(w.out, row); err != nil {
			return err
		}
	}
	return nil
}

func writeRow(writer io.Writer, values []string) error {
	_, err := fmt.Fprintln(writer, strings.Join(values, "\t"))
	return err
}

func writeTSVRow(writer io.Writer, values []string) error {
	_, err := fmt.Fprintln(writer, strings.Join(values, "\t"))
	return err
}

func isTTY(value io.Writer) bool {
	file, ok := value.(*os.File)
	if !ok {
		return false
	}
	fileDescriptor := int(file.Fd())
	return term.IsTerminal(fileDescriptor)
}
