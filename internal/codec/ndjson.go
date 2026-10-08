package codec

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/touno-io/go-tsunami-cli/internal/value"
)

// ReadNDJSON decodes newline-delimited JSON into an array with one element
// per non-blank line.
func ReadNDJSON(src Source, opts ReadOptions) (value.Value, error) {
	stream := value.NewStream(tagReadErrors(ndjsonStream(src, opts.ctx())), src.Restartable)
	if opts.Stream {
		return stream, nil
	}
	if _, err := stream.Array().Items(); err != nil {
		return value.Null, err
	}
	return stream, nil
}

func ndjsonStream(src Source, ctx context.Context) value.IterFunc {
	return func(yield func(value.Value) error) error {
		r, err := src.Open()
		if err != nil {
			return err
		}
		lr := &lineReader{br: bufio.NewReaderSize(r, readChunk)}
		return lr.each(ctx, yield)
	}
}

// each decodes every line and passes it to yield.
func (l *lineReader) each(ctx context.Context, yield func(value.Value) error) error {
	for line := 1; ; line++ {
		if err := checkContext(ctx, line); err != nil {
			return err
		}
		b, err := l.next()
		if err != nil && err != io.EOF {
			return err
		}
		if perr := yieldLine(b, line, yield); perr != nil || err == io.EOF {
			return perr
		}
	}
}

// yieldLine decodes a non-blank line and passes it to yield.
func yieldLine(b []byte, line int, yield func(value.Value) error) error {
	t := bytes.TrimSpace(b)
	if len(t) == 0 {
		return nil
	}
	v, err := ParseJSON(t)
	if err != nil {
		return fmt.Errorf("ndjson line %d: %w", line, err)
	}
	return yield(v)
}

// lineReader returns lines of any length, reusing its buffers.
type lineReader struct {
	br   *bufio.Reader
	long []byte
}

// next returns the next line including its terminator. The slice is only
// valid until the following call.
func (l *lineReader) next() ([]byte, error) {
	b, err := l.br.ReadSlice('\n')
	if err != bufio.ErrBufferFull {
		return b, err
	}
	l.long = append(l.long[:0], b...)
	for err == bufio.ErrBufferFull {
		b, err = l.br.ReadSlice('\n')
		l.long = append(l.long, b...)
	}
	return l.long, err
}
