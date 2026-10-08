// Package tsunami transforms data with DataWeave-compatible scripts.
//
// A script (.tsu or .dwl) has an optional header and a body separated by
// "---":
//
//	%dw 2.0
//	output application/json
//	---
//	payload map (row) -> { id: row.ID as Number, name: upper(row.NAME) }
//
// Inputs are read with streaming readers and outputs are written with
// streaming writers, so record-oriented transformations of large CSV, JSON
// array or NDJSON files run in constant memory.
package tsunami

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/touno-io/go-tsunami-cli/internal/codec"
	"github.com/touno-io/go-tsunami-cli/internal/interp"
	"github.com/touno-io/go-tsunami-cli/internal/value"
)

// File extensions recognized as scripts.
const (
	ExtFile      = ".tsu"
	ExtDataWeave = ".dwl"
)

const (
	defaultOutputType = "application/json"
	defaultInputType  = "application/json"
	// streamThreshold is the file size above which a payload that the script
	// reads more than once is re-read from disk instead of held in memory.
	streamThreshold = 8 << 20
)

// Options configure how scripts are compiled and run.
type Options struct {
	// ModulePaths are directories searched for imported modules.
	ModulePaths []string
	// ResourcePaths are directories searched by readUrl("classpath://...").
	ResourcePaths []string
	// Properties are returned by p(name).
	Properties map[string]string
	// Log receives the output of log(); it defaults to standard error.
	Log io.Writer
}

// Script is a compiled transformation. A Script is safe for concurrent use.
type Script struct {
	prog        *interp.Program
	outputMime  string
	outputProps map[string]string
}

// Compile compiles a script. A string ending in .tsu or .dwl is read as a
// file; anything else is compiled as inline syntax.
func Compile(syntax string, opts Options) (*Script, error) {
	if isScriptPath(syntax) {
		return CompileFile(syntax, opts)
	}
	return compile(syntax, "", opts)
}

// CompileFile compiles the script stored at path. Modules and resources are
// resolved relative to the script's directory.
func CompileFile(path string, opts Options) (*Script, error) {
	src, err := readScriptFile(path)
	if err != nil {
		return nil, err
	}
	s, err := compile(string(src), filepath.Dir(path), opts)
	if err != nil {
		return nil, fmt.Errorf("%s:%w", filepath.Base(path), err)
	}
	return s, nil
}

func compile(src, dir string, opts Options) (*Script, error) {
	prog, err := interp.Compile(src, interp.Options{
		Dir:           dir,
		ModulePaths:   opts.ModulePaths,
		ResourcePaths: opts.ResourcePaths,
		Properties:    opts.Properties,
		Log:           opts.Log,
	})
	if err != nil {
		return nil, err
	}
	s := &Script{prog: prog, outputMime: defaultOutputType, outputProps: map[string]string{}}
	if out := prog.Output(); out != nil {
		s.outputMime = out.Mime
		for k, v := range codec.DefaultProps(out.Mime) {
			s.outputProps[k] = v
		}
		for k, v := range out.Props {
			s.outputProps[k] = v
		}
	}
	return s, nil
}

// OutputMime returns the output media type, application/json by default.
func (s *Script) OutputMime() string { return s.outputMime }

// WithOutput returns a copy of the script that writes mime with the given
// writer properties merged over the script's own.
func (s *Script) WithOutput(mime string, props map[string]string) *Script {
	c := *s
	if mime != "" && codec.Normalize(mime) != codec.Normalize(s.outputMime) {
		c.outputMime = mime
		c.outputProps = map[string]string{}
		for k, v := range codec.DefaultProps(mime) {
			c.outputProps[k] = v
		}
	} else {
		c.outputProps = make(map[string]string, len(s.outputProps)+len(props))
		for k, v := range s.outputProps {
			c.outputProps[k] = v
		}
	}
	for k, v := range props {
		c.outputProps[k] = v
	}
	return &c
}

// Input is a named input document such as payload, vars or attributes.
// Exactly one of Path, Data or Reader should be set. Path inputs can be
// re-read, which lets large files stream even when a script uses the
// payload more than once.
type Input struct {
	Name     string
	MimeType string
	Props    map[string]string
	Path     string
	Data     []byte
	Reader   io.Reader
}

// Run evaluates the script and writes the result to w.
func (s *Script) Run(ctx context.Context, w io.Writer, inputs ...Input) error {
	if ctx == nil {
		ctx = context.Background()
	}
	values := make(map[string]value.Value, len(inputs))
	var files []*os.File
	track := func(f *os.File) { files = append(files, f) }
	defer func() {
		for _, f := range files {
			_ = f.Close()
		}
	}()
	for _, in := range inputs {
		v, err := s.readInput(ctx, in, track)
		if err != nil {
			return fmt.Errorf("reading %s: %w", in.Name, err)
		}
		values[in.Name] = v
	}
	result, err := s.prog.Eval(values)
	if err != nil {
		return inputError(err)
	}
	f, err := codec.Lookup(s.outputMime)
	if err != nil {
		return err
	}
	if err := f.Write(w, result, s.outputProps); err != nil {
		var ie *interp.Error
		var re *codec.ReadError
		if errors.As(err, &ie) || errors.As(err, &re) || errors.Is(err, context.Canceled) {
			return inputError(err)
		}
		return fmt.Errorf("writing %s: %w", codec.Normalize(s.outputMime), err)
	}
	return nil
}

// inputError labels malformed streamed input, which surfaces while the
// result is evaluated or written.
func inputError(err error) error {
	var re *codec.ReadError
	if errors.As(err, &re) {
		return fmt.Errorf("reading payload: %w", re.Err)
	}
	return err
}

// Transform runs the script on an in-memory payload and returns the output.
func (s *Script) Transform(payload []byte, mime string) ([]byte, error) {
	var buf bytes.Buffer
	err := s.Run(context.Background(), &buf, Input{Name: "payload", MimeType: mime, Data: payload})
	return buf.Bytes(), err
}

func (s *Script) readInput(ctx context.Context, in Input, track func(*os.File)) (value.Value, error) {
	if in.Name == "" {
		in.Name = "payload"
	}
	mime, props := s.inputFormat(in)
	src, size, err := inputSource(in, track)
	if err != nil || src.Open == nil {
		return value.Null, err
	}
	stream := in.Name == "payload" && (!s.prog.PayloadReusable() || src.Restartable && size > streamThreshold)
	return interp.ReadValue(src, mime, codec.ReadOptions{Props: props, Context: ctx, Stream: stream})
}

// inputFormat resolves the media type and reader properties of an input
// from, in increasing priority, format defaults, the script's input
// directive and the input itself.
func (s *Script) inputFormat(in Input) (string, map[string]string) {
	mime := in.MimeType
	d, declared := s.prog.Input(in.Name)
	if mime == "" && declared {
		mime = d.Mime
	}
	if mime == "" && in.Path != "" {
		mime = codec.MimeForPath(in.Path)
	}
	if mime == "" {
		mime = defaultInputType
	}
	props := map[string]string{}
	for _, layer := range []map[string]string{codec.DefaultProps(mime), d.Props, in.Props} {
		for k, v := range layer {
			props[k] = v
		}
	}
	return mime, props
}

// inputSource opens the document behind an input and reports its size when
// known. A zero Source means the input is empty.
func inputSource(in Input, track func(*os.File)) (codec.Source, int64, error) {
	switch {
	case in.Path != "":
		st, err := os.Stat(in.Path)
		if err != nil {
			return codec.Source{}, 0, err
		}
		path := in.Path
		return codec.Source{Restartable: true, Open: func() (io.Reader, error) {
			f, err := os.Open(path)
			if err != nil {
				return nil, err
			}
			track(f)
			return f, nil
		}}, st.Size(), nil
	case in.Data != nil:
		return codec.BytesSource(in.Data), int64(len(in.Data)), nil
	case in.Reader != nil:
		return codec.ReaderSource(in.Reader), -1, nil
	}
	return codec.Source{}, 0, nil
}
