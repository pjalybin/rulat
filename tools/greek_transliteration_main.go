//go:build greektranslit

package main

import (
	"bufio"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	fs := flag.NewFlagSet("greek_transliteration", flag.ExitOnError)
	stem := fs.Bool("stem", false, "apply basic Ancient Greek loan-stem extraction before transliteration")
	noHeader := fs.Bool("no-header", false, "omit CSV header")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: %s [flags] [GREEK...]\n\n", os.Args[0])
		fmt.Fprintln(fs.Output(), "With no GREEK arguments, reads one Greek form per stdin line.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	inputs, err := greekRomanizerInputs(fs.Args(), os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read input: %v\n", err)
		os.Exit(1)
	}
	if len(inputs) == 0 {
		fs.Usage()
		os.Exit(2)
	}

	w := csv.NewWriter(os.Stdout)
	if !*noHeader {
		if err := w.Write([]string{"original_greek", "greek_form", "latin", "latin_plain"}); err != nil {
			fmt.Fprintf(os.Stderr, "write output: %v\n", err)
			os.Exit(1)
		}
	}

	hadError := false
	for _, input := range inputs {
		greek := normalizeGreekCandidate(input)
		if !isGreekCandidate(greek) {
			fmt.Fprintf(os.Stderr, "skip non-Greek input: %q\n", input)
			hadError = true
			continue
		}

		romanizedGreek := greek
		if *stem {
			romanizedGreek = stemGreekDeclension(romanizedGreek)
		}

		latin, ok := transliterateGreek(romanizedGreek)
		if !ok {
			fmt.Fprintf(os.Stderr, "skip input with no Greek letters: %q\n", input)
			hadError = true
			continue
		}
		if err := w.Write([]string{greek, romanizedGreek, latin, stripGreekRomanDiacritics(latin)}); err != nil {
			fmt.Fprintf(os.Stderr, "write output: %v\n", err)
			os.Exit(1)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		fmt.Fprintf(os.Stderr, "write output: %v\n", err)
		os.Exit(1)
	}
	if hadError {
		os.Exit(1)
	}
}

func greekRomanizerInputs(args []string, r io.Reader) ([]string, error) {
	if len(args) > 0 {
		return args, nil
	}

	var inputs []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			inputs = append(inputs, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return inputs, nil
}

func stripGreekRomanDiacritics(s string) string {
	replacer := strings.NewReplacer(
		"Ā", "A",
		"ā", "a",
		"Ă", "A",
		"ă", "a",
		"Ē", "E",
		"ē", "e",
		"Ï", "I",
		"ï", "i",
		"Ī", "I",
		"ī", "i",
		"Ĭ", "I",
		"ĭ", "i",
		"Ü", "U",
		"ü", "u",
		"Ū", "U",
		"ū", "u",
		"Ŭ", "U",
		"ŭ", "u",
		"Ō", "O",
		"ō", "o",
	)
	return replacer.Replace(s)
}
