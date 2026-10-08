// Command tsunami transforms data files with DataWeave-compatible scripts.
//
// Usage:
//
//	tsunami [flags] [input ...]
//
// Examples:
//
//	tsunami -to ndjson data.csv                      # convert formats
//	tsunami -t transform.dwl -o out.json data.csv    # run a script
//	tsunami -e 'payload map $.id' data.json          # inline script
//	cat data.json | tsunami -e 'sizeOf(payload)'     # read standard input
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"time"

	tsunami "github.com/touno-io/go-tsunami-cli"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
	errPrefix = "tsunami:"
)

const usageHeader = `Usage: tsunami [flags] [input ...]

Transforms data with DataWeave-compatible scripts. Inputs stream from disk,
so large CSV, JSON array and NDJSON files use constant memory.

Flags:
`

const usageFooter = `
Examples:
  tsunami -to ndjson data.csv
  tsunami -t transform.dwl -o out.json data.csv
  tsunami -e 'payload map { id: $.ID as Number }' data.csv
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// multiFlag collects repeated flag values.
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }

func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

type config struct {
	script   string
	expr     string
	to       string
	from     string
	output   string
	inputs   multiFlag
	vars     multiFlag
	inOpts   multiFlag
	outOpts  multiFlag
	modules  multiFlag
	stats    bool
	cpuProf  string
	memLimit string
	memProf  string
	version  bool
	files    []string
	stdinTTY bool
}

func parseFlags(args []string, stderr io.Writer) (*config, error) {
	cfg := &config{}
	fs := flag.NewFlagSet("tsunami", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&cfg.script, "t", "", "transformation script (.tsu or .dwl); defaults to <input>.tsu or <input>.dwl")
	fs.StringVar(&cfg.script, "script", "", "alias of -t")
	fs.StringVar(&cfg.expr, "e", "", "inline transformation script")
	fs.StringVar(&cfg.expr, "expr", "", "alias of -e")
	fs.StringVar(&cfg.to, "to", "", "output media type: json, ndjson, csv, tsv, xml, yaml, text, dw (overrides the script)")
	fs.StringVar(&cfg.from, "from", "", "payload media type (default: from the file extension)")
	fs.StringVar(&cfg.output, "o", "", "output file, or directory when several inputs are given (default: stdout)")
	fs.StringVar(&cfg.output, "output", "", "alias of -o")
	fs.Var(&cfg.inputs, "i", "named input `name=path` (repeatable), e.g. -i vars=vars.json")
	fs.Var(&cfg.vars, "var", "set `key=value` in vars (repeatable)")
	fs.Var(&cfg.inOpts, "in-opt", "reader property `key=value` (repeatable), e.g. -in-opt separator=;")
	fs.Var(&cfg.outOpts, "out-opt", "writer property `key=value` (repeatable), e.g. -out-opt indent=false")
	fs.Var(&cfg.modules, "m", "module and resource search `dir` (repeatable)")
	fs.BoolVar(&cfg.stats, "stats", false, "print elapsed time and memory statistics to stderr")
	fs.StringVar(&cfg.memLimit, "memlimit", "", "soft memory `limit` such as 256MiB or 2GiB; 0 disables it (default 512MiB or $GOMEMLIMIT)")
	fs.StringVar(&cfg.cpuProf, "cpuprofile", "", "write a CPU profile to `file`")
	fs.StringVar(&cfg.memProf, "memprofile", "", "write a heap profile to `file` when the run ends")
	fs.BoolVar(&cfg.version, "version", false, "print the version and exit")
	fs.Usage = func() {
		_, _ = io.WriteString(stderr, usageHeader)
		fs.PrintDefaults()
		_, _ = io.WriteString(stderr, usageFooter)
	}
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	cfg.files = fs.Args()
	return cfg, nil
}

// report prints err and returns code.
func report(w io.Writer, err error, code int) int {
	_, _ = fmt.Fprintln(w, errPrefix, err)
	return code
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	cfg, err := parseFlags(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if cfg.version {
		_, _ = fmt.Fprintln(stdout, "tsunami", version)
		return exitOK
	}
	if err := tuneRuntime(cfg.memLimit); err != nil {
		return report(stderr, err, exitUsage)
	}
	cfg.stdinTTY = isTerminal(stdin)
	stopProfile, err := startProfiles(cfg)
	if err != nil {
		return report(stderr, err, exitError)
	}
	start := time.Now()
	err = execute(ctx, cfg, stdin, stdout)
	if perr := stopProfile(); err == nil {
		err = perr
	}
	if err != nil {
		return report(stderr, err, exitError)
	}
	if cfg.stats {
		printStats(stderr, time.Since(start))
	}
	return exitOK
}

// isTerminal reports whether r is an interactive terminal, in which case
// it is not read as the payload.
func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func printStats(w io.Writer, elapsed time.Duration) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	_, _ = fmt.Fprintf(w, "elapsed=%s heap_sys=%.1fMiB total_alloc=%.1fMiB num_gc=%d\n",
		elapsed.Round(time.Millisecond), float64(m.HeapSys)/(1<<20), float64(m.TotalAlloc)/(1<<20), m.NumGC)
}

func parsePairs(pairs []string, what string) (map[string]string, error) {
	out := map[string]string{}
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid %s %q: expected key=value", what, p)
		}
		out[k] = v
	}
	return out, nil
}

// job holds the settings shared by every input file of one invocation.
type job struct {
	cfg     *config
	inOpts  map[string]string
	outOpts map[string]string
	shared  []tsunami.Input
	opts    tsunami.Options
	toDir   bool
	stdin   io.Reader
	stdout  io.Writer
}

func execute(ctx context.Context, cfg *config, stdin io.Reader, stdout io.Writer) error {
	j, err := newJob(cfg, stdin, stdout)
	if err != nil {
		return err
	}
	files := cfg.files
	if len(files) == 0 {
		files = []string{""}
	}
	j.toDir = len(files) > 1 && cfg.output != ""
	if j.toDir {
		if err := os.MkdirAll(cfg.output, 0o755); err != nil {
			return err
		}
	}
	for _, file := range files {
		if err := j.runFile(ctx, file); err != nil {
			if file != "" {
				return fmt.Errorf("%s: %w", file, err)
			}
			return err
		}
	}
	return nil
}

func newJob(cfg *config, stdin io.Reader, stdout io.Writer) (*job, error) {
	inOpts, err := parsePairs(cfg.inOpts, "-in-opt")
	if err != nil {
		return nil, err
	}
	outOpts, err := parsePairs(cfg.outOpts, "-out-opt")
	if err != nil {
		return nil, err
	}
	shared, err := sharedInputs(cfg)
	if err != nil {
		return nil, err
	}
	return &job{
		cfg:     cfg,
		inOpts:  inOpts,
		outOpts: outOpts,
		shared:  shared,
		opts:    tsunami.Options{ModulePaths: cfg.modules, ResourcePaths: cfg.modules},
		stdin:   stdin,
		stdout:  stdout,
	}, nil
}

// runFile transforms one input file, or standard input when file is empty.
func (j *job) runFile(ctx context.Context, file string) error {
	script, err := loadScript(j.cfg, file, j.opts)
	if err != nil {
		return err
	}
	if j.cfg.to != "" || len(j.outOpts) > 0 {
		script = script.WithOutput(j.cfg.to, j.outOpts)
	}
	inputs := append([]tsunami.Input{}, j.shared...)
	if payload, ok := payloadInput(j.cfg, file, j.stdin, j.inOpts); ok {
		inputs = append(inputs, payload)
	}
	dest := j.cfg.output
	if j.toDir {
		base := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		dest = filepath.Join(j.cfg.output, base+extensionFor(script.OutputMime()))
	}
	return runOne(ctx, script, inputs, dest, j.stdout)
}

func runOne(ctx context.Context, script *tsunami.Script, inputs []tsunami.Input, dest string, stdout io.Writer) (err error) {
	w := stdout
	if dest != "" {
		f, err := os.Create(dest)
		if err != nil {
			return err
		}
		defer func() {
			if cerr := f.Close(); err == nil {
				err = cerr
			}
		}()
		w = f
	}
	return script.Run(ctx, w, inputs...)
}

// loadScript picks the script from -e, -t, the input's sibling .tsu/.dwl
// file, or an identity transformation when only -to is given.
func loadScript(cfg *config, file string, opts tsunami.Options) (*tsunami.Script, error) {
	switch {
	case cfg.expr != "":
		return tsunami.Compile(cfg.expr, opts)
	case cfg.script != "":
		return tsunami.CompileFile(cfg.script, opts)
	}
	if sibling := siblingScript(file); sibling != "" {
		return tsunami.CompileFile(sibling, opts)
	}
	if cfg.to != "" {
		return tsunami.Compile("payload", opts)
	}
	return nil, errors.New("no transformation given: use -t <script>, -e <expression> or -to <format>")
}

// siblingScript returns the .tsu or .dwl file next to an input with the same
// base name, if one exists.
func siblingScript(file string) string {
	if file == "" || file == "-" {
		return ""
	}
	base := strings.TrimSuffix(file, filepath.Ext(file))
	for _, ext := range []string{tsunami.ExtFile, tsunami.ExtDataWeave} {
		if _, err := os.Stat(base + ext); err == nil {
			return base + ext
		}
	}
	return ""
}

func payloadInput(cfg *config, file string, stdin io.Reader, props map[string]string) (tsunami.Input, bool) {
	in := tsunami.Input{Name: "payload", MimeType: cfg.from, Props: props}
	switch {
	case file == "-" || (file == "" && !cfg.stdinTTY && stdin != nil):
		in.Reader = stdin
	case file != "":
		in.Path = file
	default:
		return in, false
	}
	return in, true
}

func sharedInputs(cfg *config) ([]tsunami.Input, error) {
	var out []tsunami.Input
	for _, spec := range cfg.inputs {
		name, path, ok := strings.Cut(spec, "=")
		if !ok || name == "" || path == "" {
			return nil, fmt.Errorf("invalid -i %q: expected name=path", spec)
		}
		out = append(out, tsunami.Input{Name: name, Path: path})
	}
	if len(cfg.vars) == 0 {
		return out, nil
	}
	vars, err := parsePairs(cfg.vars, "-var")
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(vars)
	if err != nil {
		return nil, err
	}
	return append(out, tsunami.Input{Name: "vars", MimeType: "application/json", Data: data}), nil
}

func extensionFor(mime string) string {
	switch strings.ToLower(mime) {
	case "application/csv", "text/csv", "csv":
		return ".csv"
	case "tsv", "text/tab-separated-values":
		return ".tsv"
	case "application/x-ndjson", "application/ndjson", "ndjson", "jsonl":
		return ".ndjson"
	case "application/xml", "text/xml", "xml":
		return ".xml"
	case "application/yaml", "text/yaml", "yaml", "yml":
		return ".yaml"
	case "text/plain", "text":
		return ".txt"
	case "application/dw", "dw":
		return ".dwl"
	}
	return ".json"
}

// startProfiles starts CPU profiling and returns a function that stops it
// and writes the heap profile.
func startProfiles(cfg *config) (func() error, error) {
	cpu, err := startCPUProfile(cfg.cpuProf)
	if err != nil {
		return nil, err
	}
	return func() error {
		if cpu != nil {
			pprof.StopCPUProfile()
			if err := cpu.Close(); err != nil {
				return err
			}
		}
		return writeHeapProfile(cfg.memProf)
	}, nil
}

func startCPUProfile(path string) (*os.File, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

func writeHeapProfile(path string) error {
	if path == "" {
		return nil
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := pprof.Lookup("heap").WriteTo(f, 0); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
