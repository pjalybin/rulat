//go:build greektranslit

package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	fs := flag.NewFlagSet("greektranslit", flag.ExitOnError)
	diacritics := fs.Bool("diacritics", true, "keep Ancient Greek romanization diacritics")
	plain := fs.Bool("plain", false, "strip Ancient Greek romanization diacritics")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: %s [flags] [GREEK TEXT...]\n\n", os.Args[0])
		fmt.Fprintln(fs.Output(), "With no text arguments, reads Greek text from stdin.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	text, err := greekRomanizerInputText(fs.Args(), os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read input: %v\n", err)
		os.Exit(1)
	}
	if text == "" {
		fs.Usage()
		os.Exit(2)
	}

	out, _ := romanizeGreekAncient(text, *diacritics && !*plain)
	fmt.Print(out)
}

func greekRomanizerInputText(args []string, r io.Reader) (string, error) {
	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
