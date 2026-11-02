// Command jsoncheck says if each line of its input is one JSON value in valid UTF-8.
// It is for the tests of the wtree contract on a host with no python3.
//
//	jsoncheck [-events] [file...]
//
// With no file it reads stdin. With -events, each line must also decode as an event
// of this contract. The exit code is 0 when all lines are good, 1 when a line is
// bad, and 2 when a file cannot be read.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"unicode/utf8"

	"github.com/afruth/wtree/tui/internal/core"
)

const maxLine = 64 << 20

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stderr))
}

func run(args []string, stdin io.Reader, stderr io.Writer) int {
	flags := flag.NewFlagSet("jsoncheck", flag.ContinueOnError)
	flags.SetOutput(stderr)
	events := flags.Bool("events", false, "each line must be an event of the contract")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	status := 0
	check := func(name string, input io.Reader) {
		bad, err := checkLines(name, input, *events, stderr)
		switch {
		case err != nil:
			fmt.Fprintf(stderr, "%s: %v\n", name, err)
			status = 2
		case bad > 0 && status == 0:
			status = 1
		}
	}
	if flags.NArg() == 0 {
		check("stdin", stdin)
		return status
	}
	for _, name := range flags.Args() {
		file, err := os.Open(name)
		if err != nil {
			fmt.Fprintln(stderr, err)
			status = 2
			continue
		}
		check(name, file)
		file.Close()
	}
	return status
}

func checkLines(name string, input io.Reader, events bool, stderr io.Writer) (int, error) {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64<<10), maxLine)
	bad, number := 0, 0
	for scanner.Scan() {
		number++
		if err := checkLine(scanner.Bytes(), events); err != nil {
			bad++
			fmt.Fprintf(stderr, "%s:%d: %v\n", name, number, err)
		}
	}
	return bad, scanner.Err()
}

func checkLine(line []byte, events bool) error {
	if !utf8.Valid(line) {
		return errors.New("not valid UTF-8")
	}
	var value any
	if err := json.Unmarshal(line, &value); err != nil {
		return err
	}
	if events {
		if _, err := core.DecodeEvent(line); err != nil {
			return err
		}
	}
	return nil
}
