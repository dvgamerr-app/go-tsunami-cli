package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/alexflint/go-arg"
)

func check(e error) {
	if e != nil {
		panic(e)
	}
}

func readCSV(filename string) {
	f, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}

	// remember to close the file at the end of the program
	defer f.Close()

	// read csv values using csv.Reader
	var line uint64
	var headers []string
	csvReader := csv.NewReader(f)
	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		check(err)
		// do something with read line
		// fmt.Printf("%+v\n", rec)
		if line == 0 {
			headers = record
		}
		line++
	}
	fmt.Printf("%+v\n", headers[0])
	fmt.Println("------")
	fmt.Printf("- Read %d|%s [Complate]\n", line, filename)
}

type OutputType struct {
	Input  string
	Output string
}

type InputArgs struct {
	Output    string   `arg:"-o"`
	Transfrom string   `arg:"-t"`
	Input     []string `arg:"positional"`
}

func main() {
	var args InputArgs
	arg.MustParse(&args)
	fmt.Println("Input:", args.Input)
	fmt.Println("Output:", args.Output)

	for _, f := range args.Input {

		transfromFile := args.Transfrom
		if transfromFile == "" {
			transfromFile = fmt.Sprintf("%s/%s.tsu", filepath.Dir(f), strings.Replace(filepath.Base(f), filepath.Ext(f), "", -1))
		}

		header, payload, output := syntaxTsunamiTransfrom(transfromFile)

		fmt.Printf("Header: %s\n%s\n---\n", transfromFile, header)
		fmt.Printf("Payload:\n%s\n---\n", payload)
		fmt.Printf("Output: %s\n", output)

		// open file
		// readCSV("in.product.csv")
	}
}

func syntaxTsunamiTransfrom(file string) ([]byte, []byte, []byte) {
	var header []byte

	output := []byte("application/json")
	payload, err := os.ReadFile(file)
	check(err)
	fmt.Println("------")

	rOut := regexp.MustCompile(`output (.*?)\s`)
	rSt, _ := regexp.Compile(`---\W`)

	mStntax := rSt.FindAllStringSubmatchIndex(string(payload), -1)
	if len(mStntax) > 0 {
		header = payload[0:mStntax[0][0]]
		payload = payload[mStntax[0][1]:]

		outputType := rOut.FindAllStringSubmatchIndex(string(header), -1)
		if len(outputType) > 0 {
			output = header[outputType[0][2]:outputType[0][3]]
			header = header[outputType[0][1]:]
		}
		return header, payload, output
	}
	return nil, nil, output
}
