package tsunami

import (
	"os"
	"regexp"
	"strings"
)

func check(e error) {
	if e != nil {
		panic(e)
	}
}

func tsunamiSyntax(plainSyntax string) ([]byte, []byte, []byte) {
	var header []byte
	var payload []byte = []byte(plainSyntax)

	output := []byte("application/json")
	if _, err := os.Stat(plainSyntax); err == nil {
		payload, err = os.ReadFile(plainSyntax)
		check(err)
	}

	rOut := regexp.MustCompile(`output\s([\w/]+)`)
	rSt, _ := regexp.Compile(`---\W`)

	mStntax := rSt.FindAllStringSubmatchIndex(string(payload), -1)
	if len(mStntax) > 0 {
		header = trimByte(payload[0:mStntax[0][0]])
		payload = trimByte(payload[mStntax[0][1]:])
		outputType := rOut.FindAllStringSubmatchIndex(string(header), -1)
		if len(outputType) > 0 {
			output = trimByte(header[outputType[0][2]:outputType[0][3]])
			header = trimByte(header[outputType[0][1]:])
		}
		return header, payload, output
	}
	return nil, payload, output
}

func trimByte(data []byte) []byte {
	return []byte(strings.Trim(string(data), "\t\n\r "))
}
