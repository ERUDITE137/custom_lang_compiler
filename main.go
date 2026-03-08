package main

import (
	"bufio"
	"fmt"
	"lux/interpreter"
	"lux/lexer"
	"lux/parser"
	"os"
	"strings"
)

func main() {
	if len(os.Args) > 1 {
		runFile(os.Args[1])
	} else {
		runREPL()
	}
}

func runFile(path string) {
	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading %q: %v\n", path, err)
		os.Exit(1)
	}
	if err := execute(string(src)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runREPL() {
	fmt.Println("Lux REPL  (type 'exit' or Ctrl-D to quit)")
	interp := interpreter.New()
	scanner := bufio.NewScanner(os.Stdin)
	var buf strings.Builder

	for {
		if buf.Len() == 0 {
			fmt.Print(">>> ")
		} else {
			fmt.Print("... ")
		}

		if !scanner.Scan() {
			fmt.Println()
			break
		}
		line := scanner.Text()

		if strings.TrimSpace(line) == "exit" {
			break
		}

		buf.WriteString(line)
		buf.WriteByte('\n')

		// If line ends with '{', keep reading until we close the block
		if needsMore(buf.String()) {
			continue
		}

		src := buf.String()
		buf.Reset()

		if err := executeWith(src, interp); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}
}

// needsMore returns true when the input has an unmatched '{'.
func needsMore(src string) bool {
	depth := 0
	for _, ch := range src {
		if ch == '{' {
			depth++
		} else if ch == '}' {
			depth--
		}
	}
	return depth > 0
}

// execute creates a fresh interpreter and runs src.
func execute(src string) error {
	return executeWith(src, interpreter.New())
}

// executeWith runs src in an existing interpreter (used by REPL for persistent env).
func executeWith(src string, interp *interpreter.Interpreter) error {
	l, err := lexer.New(src)
	if err != nil {
		return fmt.Errorf("lex error: %w", err)
	}

	p := parser.New(l.Tokens())
	prog, err := p.Parse()
	if err != nil {
		return fmt.Errorf("parse error: %w", err)
	}

	if err := interp.Run(prog); err != nil {
		return err
	}
	return nil
}
