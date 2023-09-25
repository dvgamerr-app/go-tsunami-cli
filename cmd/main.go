package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"tsunami"

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
			transfromFile = fmt.Sprintf("%s/%s", filepath.Dir(f), strings.Replace(filepath.Base(f), filepath.Ext(f), tsunami.ExtFile, -1))
		}

		err := tsunami.PipeFile(transfromFile)
		check(err)
		// open file
		// readCSV("in.product.csv")
	}
}
