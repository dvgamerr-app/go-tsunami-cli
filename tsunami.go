package tsunami

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const ExtFile string = ".tsu"

func PipeFile(plaintext string) error {

	header, payload, output, err := syntaxSplit(plaintext)
	if err != nil {
		return err
	}

	fmt.Printf("Header: %s\n%s\n---\n", plaintext, header)
	fmt.Printf("Payload:\n%s\n---\n", payload)
	fmt.Printf("Output: %s\n", output)

	return nil
}

func syntaxSplit(plainSyntax string) ([]byte, []byte, []byte, error) {
	var header []byte
	var payload []byte = []byte(plainSyntax)
	var output []byte = []byte("application/json")

	if filepath.Ext(plainSyntax) == ExtFile {
		var err error
		if _, err = os.Stat(plainSyntax); err != nil {
			return nil, nil, nil, fmt.Errorf("can't find the file '%s'", filepath.Base(plainSyntax))
		}

		payload, err = os.ReadFile(plainSyntax)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	rOutput := regexp.MustCompile(`output\s([\w/]+)`)
	rSt, _ := regexp.Compile(`---\W`)

	mStntax := rSt.FindAllStringSubmatchIndex(string(payload), -1)
	if len(mStntax) > 0 {
		header = trimByte(payload[0:mStntax[0][0]])
		payload = trimByte(payload[mStntax[0][1]:])
		outputType := rOutput.FindAllStringSubmatchIndex(string(header), -1)
		if len(outputType) > 0 {
			output = trimByte(header[outputType[0][2]:outputType[0][3]])
			header = trimByte(header[outputType[0][1]:])
		}
		return header, payload, output, nil
	}
	return nil, payload, output, nil
}

func trimByte(data []byte) []byte {
	return []byte(strings.Trim(string(data), "\t\n\r "))
}
