package main

import (
	"bufio"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"unicode"
)

type prevKind int

const (
	prevNone prevKind = iota
	prevVowel
	prevPairedConsonant
	prevAlwaysHard
	prevAlwaysSoft
	prevSign
	prevJ
)

type dictEntry struct {
	CyrStem       string
	CyrRunes      []rune
	Latin         string
	OriginalLatin string
	OriginalGreek string
	Mode          string // stem or word
	CaseMode      string // auto or preserve
	Source        string
	Notes         string
	URL           string
	SuffixKind    prevKind
	HasSuffixKind bool
}

func main() {
	dictPath := flag.String("dict", "", "CSV dictionary of loanword stems")
	loanApostrophe := flag.Bool("loan-apostrophe", false, "insert apostrophe between dictionary loan stem and converted Russian suffix")
	apostrophe := flag.Bool("apostrophe", false, "alias for -loan-apostrophe")
	flag.Parse()

	var entries []dictEntry
	var err error
	if *dictPath != "" {
		entries, err = loadDictionary(*dictPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "load dictionary: %v\n", err)
			os.Exit(2)
		}
	}

	input, err := io.ReadAll(bufio.NewReader(os.Stdin))
	if err != nil {
		fmt.Fprintf(os.Stderr, "read stdin: %v\n", err)
		os.Exit(1)
	}

	fmt.Print(Transliterate(string(input), entries, *loanApostrophe || *apostrophe))
}

func loadDictionary(path string) ([]dictEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	r.LazyQuotes = true

	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, errors.New("empty CSV")
	}

	header := map[string]int{}
	for i, name := range records[0] {
		header[strings.ToLower(strings.TrimSpace(name))] = i
	}
	need := []string{"cyrillic_stem", "latin_stem"}
	for _, name := range need {
		if _, ok := header[name]; !ok {
			return nil, fmt.Errorf("missing required column %q", name)
		}
	}

	get := func(rec []string, name, def string) string {
		idx, ok := header[name]
		if !ok || idx >= len(rec) {
			return def
		}
		v := strings.TrimSpace(rec[idx])
		if v == "" {
			return def
		}
		return v
	}

	var entries []dictEntry
	for row, rec := range records[1:] {
		if len(rec) == 0 {
			continue
		}
		stem := strings.TrimSpace(get(rec, "cyrillic_stem", ""))
		latin := strings.TrimSpace(get(rec, "latin_stem", ""))
		if stem == "" && latin == "" {
			continue
		}
		if stem == "" || latin == "" {
			return nil, fmt.Errorf("row %d: both cyrillic_stem and latin_stem are required", row+2)
		}
		mode := strings.ToLower(get(rec, "mode", "stem"))
		if mode != "stem" && mode != "word" {
			return nil, fmt.Errorf("row %d: mode must be stem or word", row+2)
		}
		caseMode := strings.ToLower(get(rec, "case_mode", "auto"))
		if caseMode != "auto" && caseMode != "preserve" {
			return nil, fmt.Errorf("row %d: case_mode must be auto or preserve", row+2)
		}
		lowerStem := strings.ToLower(stem)
		entry := dictEntry{
			CyrStem:       lowerStem,
			CyrRunes:      []rune(lowerStem),
			Latin:         latin,
			OriginalLatin: get(rec, "original_latin", ""),
			OriginalGreek: get(rec, "original_greek", ""),
			Mode:          mode,
			CaseMode:      caseMode,
			Source:        get(rec, "source", ""),
			Notes:         get(rec, "notes", ""),
			URL:           get(rec, "url", ""),
		}
		if sk, ok := parseSuffixContext(get(rec, "suffix_context", "native")); ok {
			entry.SuffixKind = sk
			entry.HasSuffixKind = true
		}
		entries = append(entries, entry)
	}

	sort.SliceStable(entries, func(i, j int) bool {
		li := len(entries[i].CyrRunes)
		lj := len(entries[j].CyrRunes)
		if li != lj {
			return li > lj
		}
		if entries[i].Mode != entries[j].Mode {
			return entries[i].Mode == "word"
		}
		return entries[i].CyrStem < entries[j].CyrStem
	})

	return entries, nil
}

func parseSuffixContext(v string) (prevKind, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "native":
		return prevNone, false
	case "none":
		return prevNone, true
	case "vowel":
		return prevVowel, true
	case "paired":
		return prevPairedConsonant, true
	case "hard":
		return prevAlwaysHard, true
	case "soft":
		return prevAlwaysSoft, true
	case "sign":
		return prevSign, true
	case "j":
		return prevJ, true
	default:
		return prevNone, false
	}
}

func Transliterate(s string, entries []dictEntry, apostrophe bool) string {
	runes := []rune(s)
	var out strings.Builder

	for i := 0; i < len(runes); {
		lower := unicode.ToLower(runes[i])
		if !isRussianLetter(lower) {
			out.WriteRune(runes[i])
			i++
			continue
		}

		start := i
		for i < len(runes) && isRussianLetter(unicode.ToLower(runes[i])) {
			i++
		}
		word := runes[start:i]
		out.WriteString(transliterateWord(word, entries, apostrophe))
	}

	return out.String()
}

func transliterateWord(word []rune, entries []dictEntry, apostrophe bool) string {
	if len(entries) > 0 {
		lower := strings.ToLower(string(word))
		lowerRunes := []rune(lower)
		for _, e := range entries {
			if e.Mode == "word" {
				if lower == e.CyrStem {
					return applyDictCase(word, e.Latin, e.CaseMode)
				}
				continue
			}
			if hasRunePrefix(lowerRunes, e.CyrRunes) {
				stemPart := word[:len(e.CyrRunes)]
				suffix := word[len(e.CyrRunes):]
				stemOut := applyDictCase(stemPart, e.Latin, e.CaseMode)
				ctxKind, ctxLower := contextFromStem(e.CyrRunes)
				if e.HasSuffixKind {
					ctxKind = e.SuffixKind
					ctxLower = 0
				}
				suffixOut := transliterateNativeRunes(suffix, wordAllCaps(word), ctxKind, ctxLower)
				if apostrophe && suffixOut != "" {
					return stemOut + "'" + suffixOut
				}
				return stemOut + suffixOut
			}
		}
	}
	return transliterateNativeRunes(word, wordAllCaps(word), prevNone, 0)
}

func hasRunePrefix(word, prefix []rune) bool {
	if len(prefix) > len(word) {
		return false
	}
	for i := range prefix {
		if word[i] != prefix[i] {
			return false
		}
	}
	return true
}

func contextFromStem(stem []rune) (prevKind, rune) {
	if len(stem) == 0 {
		return prevNone, 0
	}
	last := stem[len(stem)-1]
	return kindAfterRussianRune(last), last
}

func transliterateNativeRunes(word []rune, allCaps bool, initialKind prevKind, initialLower rune) string {
	var out strings.Builder
	prev := initialKind
	prevLower := initialLower

	for i := 0; i < len(word); i++ {
		r := word[i]
		lower := unicode.ToLower(r)
		repl := ""

		// Assimilated escape spellings so zs remains Ж and sz remains Ш.
		if i+1 < len(word) {
			nextLower := unicode.ToLower(word[i+1])
			if lower == 'с' && nextLower == 'з' {
				repl = "zz"
				out.WriteString(applyCase(r, repl, allCaps))
				prev = prevPairedConsonant
				prevLower = 'з'
				i++
				continue
			}
			if lower == 'з' && nextLower == 'с' {
				repl = "ss"
				out.WriteString(applyCase(r, repl, allCaps))
				prev = prevPairedConsonant
				prevLower = 'с'
				i++
				continue
			}
		}

		switch lower {
		case 'а':
			repl = "a"
			prev = prevVowel
		case 'о':
			repl = "o"
			prev = prevVowel
		case 'у':
			repl = "u"
			prev = prevVowel
		case 'ы':
			repl = "i"
			prev = prevVowel
		case 'э':
			if prev == prevNone {
				repl = "e"
			} else {
				repl = "ae"
			}
			prev = prevVowel
		case 'и':
			repl = transliterateI(prev)
			prev = prevVowel
		case 'е':
			repl = transliterateE(prev)
			prev = prevVowel
		case 'ё':
			repl = transliterateJotatingOrSoft(prev, "o", "eo", "jo")
			prev = prevVowel
		case 'ю':
			repl = transliterateJotatingOrSoft(prev, "u", "eu", "ju")
			prev = prevVowel
		case 'я':
			repl = transliterateJotatingOrSoft(prev, "a", "ea", "ja")
			prev = prevVowel
		case 'й':
			repl = "j"
			prev = prevJ
		case 'ь':
			if prev == prevPairedConsonant {
				repl = "e"
			}
			prev = prevSign
		case 'ъ':
			prev = prevSign
		default:
			if v, ok := pairedConsonants[lower]; ok {
				repl = v
				prev = prevPairedConsonant
			} else if v, ok := alwaysHardConsonants[lower]; ok {
				repl = v
				prev = prevAlwaysHard
			} else if v, ok := alwaysSoftConsonants[lower]; ok {
				repl = v
				prev = prevAlwaysSoft
			} else {
				repl = string(r)
				prev = prevNone
			}
		}

		out.WriteString(applyCase(r, repl, allCaps))
		prevLower = lower
	}
	_ = prevLower
	return out.String()
}

var pairedConsonants = map[rune]string{
	'б': "b",
	'в': "v",
	'г': "g",
	'д': "d",
	'з': "z",
	'к': "k",
	'л': "l",
	'м': "m",
	'н': "n",
	'п': "p",
	'р': "r",
	'с': "s",
	'т': "t",
	'ф': "f",
	'х': "x",
}

var alwaysHardConsonants = map[rune]string{
	'ж': "zs",
	'ш': "sz",
	'ц': "tz",
}

var alwaysSoftConsonants = map[rune]string{
	'ч': "cz",
	'щ': "sze",
}

func transliterateI(prev prevKind) string {
	switch prev {
	case prevPairedConsonant:
		return "ei"
	case prevSign:
		return "ji"
	default:
		return "i"
	}
}

func transliterateE(prev prevKind) string {
	switch prev {
	case prevPairedConsonant:
		return "ee"
	case prevAlwaysHard:
		return "ae"
	case prevAlwaysSoft, prevJ:
		return "e"
	case prevSign, prevVowel, prevNone:
		return "je"
	default:
		return "je"
	}
}

func transliterateJotatingOrSoft(prev prevKind, hard, soft, jotated string) string {
	switch prev {
	case prevPairedConsonant:
		return soft
	case prevAlwaysHard, prevAlwaysSoft, prevJ:
		return hard
	case prevSign, prevVowel, prevNone:
		return jotated
	default:
		return jotated
	}
}

func kindAfterRussianRune(lower rune) prevKind {
	if _, ok := pairedConsonants[lower]; ok {
		return prevPairedConsonant
	}
	if _, ok := alwaysHardConsonants[lower]; ok {
		return prevAlwaysHard
	}
	if _, ok := alwaysSoftConsonants[lower]; ok {
		return prevAlwaysSoft
	}
	switch lower {
	case 'а', 'о', 'у', 'ы', 'э', 'и', 'е', 'ё', 'ю', 'я':
		return prevVowel
	case 'й':
		return prevJ
	case 'ь', 'ъ':
		return prevSign
	default:
		return prevNone
	}
}

func isRussianLetter(lower rune) bool {
	switch lower {
	case 'а', 'б', 'в', 'г', 'д', 'е', 'ё', 'ж', 'з', 'и', 'й',
		'к', 'л', 'м', 'н', 'о', 'п', 'р', 'с', 'т', 'у', 'ф', 'х',
		'ц', 'ч', 'ш', 'щ', 'ъ', 'ы', 'ь', 'э', 'ю', 'я':
		return true
	default:
		return false
	}
}

func wordAllCaps(word []rune) bool {
	hasUpper := false
	hasLower := false
	cased := 0
	for _, r := range word {
		if unicode.IsUpper(r) {
			hasUpper = true
			cased++
		} else if unicode.IsLower(r) {
			hasLower = true
			cased++
		}
	}
	return cased > 1 && hasUpper && !hasLower
}

func applyCase(src rune, repl string, allCaps bool) string {
	if repl == "" {
		return ""
	}
	if allCaps {
		return strings.ToUpper(repl)
	}
	if unicode.IsUpper(src) {
		return upperFirst(repl)
	}
	return repl
}

func applyDictCase(word []rune, latin, caseMode string) string {
	if latin == "" {
		return ""
	}
	allCaps := wordAllCaps(word)
	if allCaps {
		return strings.ToUpper(latin)
	}
	if caseMode == "preserve" {
		return latin
	}
	if len(word) > 0 && unicode.IsUpper(word[0]) {
		return upperFirst(latin)
	}
	return lowerFirst(latin)
}

func upperFirst(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func lowerFirst(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	r[0] = unicode.ToLower(r[0])
	return string(r)
}
