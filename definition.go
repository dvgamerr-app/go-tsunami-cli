package tsunami

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var (
	outputPattern    = regexp.MustCompile(`output\s([\w/+.-]+)`)
	separatorPattern = regexp.MustCompile(`---\W`)
)

// Definition is the raw split of a transformation definition into its
// header, body and output media type, without compiling it.
type Definition struct {
	Header string
	Body   string
	Output string
}

// ParseDefinition splits a definition at its first --- separator and reads
// the first output directive. A string ending in .tsu or .dwl is read as a
// file; anything else is parsed as inline syntax. The output media type
// defaults to application/json.
func ParseDefinition(syntax string) (Definition, error) {
	header, body, output, err := syntaxSplit(syntax)
	if err != nil {
		return Definition{}, err
	}
	return Definition{Header: string(header), Body: string(body), Output: string(output)}, nil
}

// PipeFile prints the header, body and output media type of a definition.
//
// Deprecated: use Compile or CompileFile and Script.Run to execute a
// transformation, or ParseDefinition to inspect one.
func PipeFile(plaintext string) error {
	def, err := ParseDefinition(plaintext)
	if err != nil {
		return err
	}
	fmt.Printf("Header: %s\n%s\n---\n", plaintext, def.Header)
	fmt.Printf("Payload:\n%s\n---\n", def.Body)
	fmt.Printf("Output: %s\n", def.Output)
	return nil
}

// isScriptPath reports whether syntax names a script file rather than
// holding inline syntax.
func isScriptPath(syntax string) bool {
	ext := filepath.Ext(syntax)
	return ext == ExtFile || ext == ExtDataWeave
}

func syntaxSplit(plainSyntax string) ([]byte, []byte, []byte, error) {
	var header []byte
	payload := []byte(plainSyntax)
	output := []byte(defaultOutputType)

	if isScriptPath(plainSyntax) {
		var err error
		payload, err = readScriptFile(plainSyntax)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	separator := separatorPattern.FindIndex(payload)
	if separator != nil {
		header = trimByte(payload[:separator[0]])
		payload = trimByte(payload[separator[1]:])
		outputType := outputPattern.FindSubmatchIndex(header)
		if outputType != nil {
			output = trimByte(header[outputType[2]:outputType[3]])
			header = trimByte(header[outputType[1]:])
		}
		return header, payload, output, nil
	}
	return nil, payload, output, nil
}

// readScriptFile reads a script, reporting a missing file by name.
func readScriptFile(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("can't find the file '%s'", filepath.Base(path))
		}
		return nil, err
	}
	return b, nil
}

func trimByte(data []byte) []byte {
	return bytes.Trim(data, "\t\n\r ")
}
