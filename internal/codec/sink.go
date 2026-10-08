package codec

import (
	"bufio"
	"io"
)

// sink is a buffered writer whose first error is sticky: bufio.Writer keeps
// returning it, and flush reports it, so individual writes need no checks.
type sink struct {
	w *bufio.Writer
}

func newSink(w io.Writer) sink {
	return sink{w: bufio.NewWriterSize(w, writeBuffer)}
}

func (s sink) put(c byte) { _ = s.w.WriteByte(c) }

func (s sink) puts(str string) { _, _ = s.w.WriteString(str) }

func (s sink) flush() error { return s.w.Flush() }
