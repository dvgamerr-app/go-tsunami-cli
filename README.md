# go-tsunami

go-tsunami is a Go library and CLI that transforms data with
DataWeave-compatible scripts (`.dwl` and `.tsu`). It reads and writes JSON,
NDJSON, CSV/TSV, XML, YAML, plain text and DataWeave literals, and streams
record-oriented inputs so multi-gigabyte files run in constant memory.

```text
%dw 2.0
output application/json
---
payload
  filter ($.status != "Canceled")
  map (o) -> {
    id: o.id as Number,
    customer: upper(o.name),
    created: o.created as LocalDateTime {format: "yyyy-MM-dd HH:mm:ss.SSS"}
  }
```

## Requirements

- Go 1.22 or newer (no third-party dependencies)

## Build

```powershell
go build -buildvcs=false -o bin/tsunami.exe ./cmd/tsunami
```

## CLI usage

```powershell
# Convert formats (identity transformation)
bin/tsunami.exe -to ndjson data.csv
bin/tsunami.exe -to yaml -o out.yaml data.json

# Run a script; -t defaults to <input>.tsu or <input>.dwl next to the input
bin/tsunami.exe -t transform.dwl -o out.json data.csv

# Inline scripts, standard input and variables
bin/tsunami.exe -e 'payload map { id: $.ID as Number }' data.csv
Get-Content data.json | bin/tsunami.exe -e 'sizeOf(payload)'
bin/tsunami.exe -t t.dwl -var env=prod -i lookup=codes.json data.csv
```

| Flag | Purpose |
| --- | --- |
| `-t`, `-script` | Script file (`.tsu` or `.dwl`) |
| `-e`, `-expr` | Inline script |
| `-to` | Output media type override: `json`, `ndjson`, `csv`, `tsv`, `xml`, `yaml`, `text`, `dw` |
| `-from` | Payload media type (default: from the file extension) |
| `-o`, `-output` | Output file, or a directory when several inputs are given |
| `-i name=path` | Named input available to the script as `name` (`vars` and `attributes` work too) |
| `-var key=value` | Adds `vars.key` |
| `-in-opt key=value` | Reader property, e.g. `separator=;`, `header=false` |
| `-out-opt key=value` | Writer property, e.g. `indent=false`, `skipNullOn=everywhere` |
| `-m dir` | Module and `readUrl("classpath://...")` search directory |
| `-stats` | Prints elapsed time and memory statistics |
| `-memlimit` | Soft memory limit (default `512MiB`, `0` disables) |
| `-cpuprofile`, `-memprofile` | Writes pprof profiles |

The CLI caps `GOMAXPROCS` at 4 and sets `GOGC=400` unless those environment
variables are set: a transformation runs on one goroutine, so these settings
cut garbage-collector overhead without using more memory than necessary.

## Library usage

```go
script, err := tsunami.CompileFile("transform.dwl", tsunami.Options{})
if err != nil {
	return err
}
err = script.Run(ctx, os.Stdout,
	tsunami.Input{Name: "payload", Path: "orders.csv"},
	tsunami.Input{Name: "vars", Data: []byte(`{"env": "prod"}`)},
)
```

`Compile` accepts inline syntax or a path ending in `.tsu`/`.dwl`,
`Script.WithOutput` overrides the output format, and `Script.Transform` is a
convenience for in-memory payloads. `ParseDefinition` still splits a
definition into header, body and output media type without compiling it.

## Supported language

- Header: `%dw 2.0`, `output <mime> [props]`, `input <name> <mime> [props]`,
  `import` (standard `dw::` modules and your own `.dwl` modules), `var`,
  `fun` with defaults, `type` (ignored).
- Expressions: literals including `|2023-01-01|` dates and `/regex/`,
  string interpolation (`"$(expr)"`, `"$name"`), objects with dynamic keys,
  conditional and spread members, arrays, selectors (`.a`, `.*a`, `..a`,
  `[i]`, `[a to b]`, `[?(cond)]`, `a?`), `if`/`else`, `do`, lambdas and
  implicit `$`/`$$`/`$$$` parameters, `match`/`case`, infix calls
  (`items map f`, `s replace "a" with "b"`), `as`/`is` with `format` options,
  `default`, arithmetic, comparison and logical operators.
- 128 functions from `dw::Core`, `Strings`, `Arrays`, `Objects`,
  `Runtime` (`try`, `orElse`, `fail`), `System`, dates, numbers and
  `dw::test::Asserts` (`must equalTo(...)`), plus `read`, `write`, `readUrl`
  and `log`.
- Numbers keep their source text, so `7490.0000` is written back unchanged.
  Number formatting follows `java.text.DecimalFormat` with decimal rounding,
  and date patterns follow `java.time.format.DateTimeFormatter`.

Not supported yet: XML attributes and namespaces, the `update` operator,
YAML input and Java-only regular expression features such as lookaround.

## Formats

| Media type | Read | Write | Properties |
| --- | --- | --- | --- |
| `application/json` | stream (top-level array) | stream | `indent`, `skipNullOn` |
| `application/x-ndjson` | stream | stream | `skipNullOn` |
| `application/csv`, `tsv` | stream | stream | `separator`, `header`, `quote`, `escape`, `quoteValues`, `quoteHeader`, `lineSeparator` |
| `application/xml` | yes | stream | `indent`, `skipNullOn` |
| `application/yaml` | no | stream | `skipNullOn` |
| `text/plain` | yes | yes | |
| `application/dw` | yes | yes | `indent` |

## Performance

Streaming keeps memory flat regardless of input size. On an AMD Ryzen 7
5700X3D with the CLI defaults:

| Input | Transformation | Time | Heap |
| --- | --- | --- | --- |
| 87 MB CSV | to NDJSON | 1.1 s | 20 MiB |
| 1.0 GB CSV (200 columns) | to NDJSON (4.2 GB written) | 13.6 s | 24 MiB |
| 2.4 GB CSV | `filter` + `map` + date parsing to JSON | 14.0 s | 20 MiB |
| 5.3 GB CSV | `map` with number formatting to NDJSON | 33.3 s | 24 MiB |

Key techniques: identifiers resolve to frame slots at compile time and
expressions compile to Go closures; `map`, `filter`, selectors and `++` stay
lazy over streams; CSV rows are packed into one string per row; repeated
JSON object layouts share key shapes; constant literals and date/number
patterns are built once.

## Development

```powershell
go test -count=1 ./...
go build -buildvcs=false ./...
go vet ./...
qlty check -a

# Benchmarks
go test -run '^$' -bench . -benchmem ./...

# Compile every script in a DataWeave project (optional)
$env:TSUNAMI_DW_SAMPLES = 'E:\path\to\mulesoft'; go test ./internal/interp -run TestCompileSamples -v
```

Golden tests in `golden_test.go` run real MuleSoft scripts and their MUnit
assertions from `testdata/`; refresh expected outputs with
`go test -run TestGolden -update .` after reviewing the changes.

## Project layout

- `tsunami.go`, `definition.go`: public API (`Compile`, `Script`, `Input`,
  `ParseDefinition`).
- `cmd/tsunami/`: command-line interface.
- `internal/syntax/`: lexer, AST and parser.
- `internal/interp/`: compiler, runtime and standard library.
- `internal/codec/`: streaming readers and writers.
- `internal/value/`: value model shared by the interpreter and codecs.
- `example/`: sample scripts and input data.
- `testdata/`: golden test fixtures.
