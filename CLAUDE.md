# CLAUDE.md

## Project overview

`go-tsunami` is a Go library and CLI that executes DataWeave-compatible
transformation scripts (`.dwl` and `.tsu`). Scripts compile to Go closures and
run against streaming readers and writers for JSON, NDJSON, CSV/TSV, XML,
YAML, text and DataWeave literals, so large record files use constant memory.

## Working conventions

- Preserve the distinction between inline syntax and a path ending in `.tsu`
  or `.dwl`.
- Keep `application/json` as the default output media type.
- Preserve the `---` separator and `output <media-type>` directive syntax.
- Keep parser hot-path regular expressions compiled at package initialization.
- Keep the module free of third-party dependencies; use the standard
  `testing` package for assertions.
- Keep streaming intact: record-oriented readers return lazy arrays, and
  array operations (`map`, `filter`, selectors, `++`, `flatten`, `take`)
  must stay lazy over streams. Only materialize when random access is needed.
- Builtins must not retain their `args` slice; callers reuse it.
- Avoid `\u` escape sequences in files written by tools that unescape them;
  build such strings from parts or constants instead.
- Keep functions under qlty's cognitive complexity limit (15) by extracting
  helpers.
- Run `gofmt` only on changed Go files.
- Before handoff, run:
  - `go test -count=1 ./...`
  - `go build -buildvcs=false ./...`
  - `go vet ./...`
  - `git diff --check`
  - `qlty check -a`

## Architecture

- `tsunami.go`: public API (`Compile`, `CompileFile`, `Script.Run`,
  `Script.Transform`, `Script.WithOutput`, `Input`, `Options`).
- `definition.go`: `ParseDefinition` and the deprecated `PipeFile`, which only
  split a definition into header, body and output media type.
- `cmd/tsunami/`: CLI (`main.go`) and runtime tuning (`runtime.go`).
- `internal/syntax/`: lexer, AST and parser.
- `internal/interp/`: compiler (`compile.go`), operators and coercion
  (`ops.go`), dates (`temporal.go`, `pattern.go`), number formatting
  (`numfmt.go`) and builtins (`builtins_*.go`).
- `internal/codec/`: media type registry and streaming readers/writers.
- `internal/value/`: value model (`Value`, `Array` streams, `Object` shapes).
- `example/`: example scripts and input data.
- `testdata/mulesoft/`: real MuleSoft scripts with MUnit assertions.
- `testdata/samples/`: real scripts with reviewed golden outputs.

There are no HTML templates or CSS assets in this repository.

## Behavior notes

- A string ending in `.tsu` or `.dwl` is treated as a file path; other strings
  are compiled as inline syntax.
- A missing script file returns `can't find the file '<name>'`.
- `ParseDefinition` uses only the first separator and first output directive,
  and trims spaces, tabs, carriage returns and line feeds.
- Variables are lazy; a binding read more than once buffers one-shot streams.
  File inputs are restartable, so large payloads read twice are re-read from
  disk instead of held in memory.
- Unknown identifiers resolve to named inputs at run time and fail only when
  evaluated.
- Numbers keep their source text; `as String {format}` follows
  `DecimalFormat` with decimal HALF_EVEN rounding (or `roundMode`).
- `go.mod` declares `go 1.22` with `toolchain go1.26.8`: qlty runs
  golangci-lint with Go 1.22 (built with go1.26), and osv-scanner evaluates
  the standard library at the toolchain version.

## Change log

### 2026-10-08

- Replaced the split-only parser with a DataWeave-compatible engine: lexer,
  parser, closure compiler with slot-resolved variables, about 130 builtins,
  pattern matching, modules, `try`, MUnit `must equalTo` assertions.
- Added streaming codecs for JSON, NDJSON, CSV/TSV, XML, YAML, text and
  DataWeave literals; a custom CSV reader packs each row into one string.
- Rewrote the CLI on the standard `flag` package (removed `go-arg`) with
  `-t/-e/-to/-from/-o/-i/-var/-in-opt/-out-opt/-m/-stats/-memlimit` and
  profiling flags; it caps `GOMAXPROCS` at 4 and sets `GOGC=400` by default.
- Restructured into the standard layout (`cmd/`, `internal/`) with module path
  `github.com/touno-io/go-tsunami-cli`.
- All 418 unique scripts from the reference MuleSoft projects compile; golden
  tests run their MUnit assertions.
- Measured 2.4 GB CSV filter+map+date conversion in 14 s and 5.3 GB in 33 s
  with heap at or below 24 MiB.
- `qlty check -a` reports no issues.

### 2026-08-02

- Moved regular-expression compilation out of the parser hot path.
- Replaced all-match searches with first-match searches where only the first
  result is consumed.
- Matched regular expressions directly against byte slices to avoid temporary
  string allocations.
- Replaced string round trips during byte trimming with direct byte-slice
  trimming.
- Removed the redundant file `Stat` call before `ReadFile` while preserving
  the missing-file error contract.
- Reworked tests to use the standard library and added file, missing-file,
  header, default-output, and trimming coverage.
- Tidied module metadata after removing direct test assertion usage.
- Expanded `README.md` with current build, usage, syntax, profiling, and
  validation instructions.
- Parser benchmark improved from approximately 4.3–5.4 microseconds, 4.88 KB,
  and 60 allocations per operation to 0.44–0.66 microseconds, 96 bytes, and
  4 allocations per operation on the validation machine.
