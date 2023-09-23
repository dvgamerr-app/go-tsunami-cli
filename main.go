package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"

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

func main() {
	var args struct {
		Output string   `arg:"-o"`
		Input  []string `arg:"positional"`
	}
	arg.MustParse(&args)
	fmt.Println("Input:", args.Input)
	fmt.Println("Output:", args.Output)

	dat, err := os.ReadFile("./example/basic.tsu")
	check(err)
	fmt.Println("------")

	rOut := regexp.MustCompile(`output (.*?)\s`)
	rSt, _ := regexp.Compile(`\W---\W`)

	mStntax := rSt.FindAllStringSubmatchIndex(string(dat), -1)
	if len(mStntax) == 0 {
		panic("Syntax error")
	}
	header := dat[0 : mStntax[0][0]+1]
	payload := dat[mStntax[0][1]:]

	outputType := rOut.FindStringSubmatch(string(header))
	fmt.Printf("Header:\n%s\n", header)
	fmt.Printf("Payload: %s\n", payload)
	fmt.Printf("Output: %s\n", outputType[1])

	// open file
	// readCSV("in.product.csv")
}
