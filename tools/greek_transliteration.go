package main

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type greekToken struct {
	base       rune
	text       string
	start      int
	end        int
	upper      bool
	rough      bool
	acute      bool
	grave      bool
	circumflex bool
	diaeresis  bool
	subscript  bool
	long       bool
	short      bool
	letter     bool
}

type greekRomanizationScheme int

const (
	greekRomanizationClassical greekRomanizationScheme = iota
	greekRomanizationALALC
)

type greekTransliterationOptions struct {
	scheme            greekRomanizationScheme
	accents           bool
	lengthMarks       bool
	defaultShortMarks bool
	chi               string
}

func normalizeGreekCandidate(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.ReplaceAll(s, "’", "'")
	s = strings.ReplaceAll(s, "ʼ", "'")
	s = strings.ReplaceAll(s, "ʻ", "'")
	s = strings.Trim(s, " \t\r\n.,;:()[]{}<>\"“”«»„")
	fields := strings.Fields(s)
	for i, field := range fields {
		fields[i] = strings.Trim(field, " \t\r\n.,;:()[]{}<>\"“”«»„")
	}
	return strings.Join(fields, " ")
}

func isGreekCandidate(s string) bool {
	if s == "" {
		return false
	}
	hasGreek := false
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Greek, r):
			hasGreek = true
		case unicode.Is(unicode.Mn, r):
			continue
		case unicode.IsSpace(r), r == '-', r == '\'', r == '.', r == '·':
			continue
		default:
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return false
			}
		}
	}
	return hasGreek
}

func stemGreekDeclension(s string) string {
	tokens := greekTokens(s)
	letterIndexes := greekLetterIndexes(tokens)
	if len(letterIndexes) < 3 {
		return s
	}

	bases := greekBaseString(tokens, letterIndexes)
	switch {
	case strings.HasSuffix(bases, "μα"):
		return cutGreekEnding(s, tokens, letterIndexes, 1)
	case strings.HasSuffix(bases, "ξ"):
		return replaceGreekEnding(s, tokens, letterIndexes, 1, "κ")
	case strings.HasSuffix(bases, "ψ"):
		return replaceGreekEnding(s, tokens, letterIndexes, 1, "π")
	case strings.HasSuffix(bases, "ευσ"):
		return cutGreekEnding(s, tokens, letterIndexes, 3)
	case strings.HasSuffix(bases, "ιον"),
		strings.HasSuffix(bases, "ιου"):
		return cutGreekEnding(s, tokens, letterIndexes, 2)
	case strings.HasSuffix(bases, "ιοισ"):
		return cutGreekEnding(s, tokens, letterIndexes, 3)
	case strings.HasSuffix(bases, "ιων"),
		strings.HasSuffix(bases, "ιοι"):
		return cutGreekEnding(s, tokens, letterIndexes, 2)
	case strings.HasSuffix(bases, "ιω"),
		strings.HasSuffix(bases, "ια"):
		return cutGreekEnding(s, tokens, letterIndexes, 1)
	case strings.HasSuffix(bases, "ον"):
		return cutGreekEnding(s, tokens, letterIndexes, 1)
	case strings.HasSuffix(bases, "οσ"),
		strings.HasSuffix(bases, "ασ"),
		strings.HasSuffix(bases, "ησ"),
		strings.HasSuffix(bases, "ουσ"),
		strings.HasSuffix(bases, "ισ"),
		strings.HasSuffix(bases, "υσ"),
		strings.HasSuffix(bases, "ωσ"):
		return cutGreekEnding(s, tokens, letterIndexes, 1)
	default:
		return s
	}
}

func greekLetterIndexes(tokens []greekToken) []int {
	var indexes []int
	for i, token := range tokens {
		if token.letter {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

func greekBaseString(tokens []greekToken, letterIndexes []int) string {
	var out strings.Builder
	for _, idx := range letterIndexes {
		out.WriteRune(tokens[idx].base)
	}
	return out.String()
}

func cutGreekEnding(s string, tokens []greekToken, letterIndexes []int, letters int) string {
	if letters <= 0 || letters >= len(letterIndexes) {
		return s
	}
	cutToken := tokens[letterIndexes[len(letterIndexes)-letters]]
	return strings.TrimRight(s[:cutToken.start], " \t\r\n-'’ʼʻ.")
}

func replaceGreekEnding(s string, tokens []greekToken, letterIndexes []int, letters int, replacement string) string {
	prefix := cutGreekEnding(s, tokens, letterIndexes, letters)
	if prefix == s {
		return s
	}
	return prefix + replacement
}

func transliterateGreek(s string) (string, bool) {
	return romanizeGreekClassical(s)
}

func romanizeGreekClassical(s string) (string, bool) {
	return transliterateGreekWithOptions(s, greekTransliterationOptions{
		scheme: greekRomanizationClassical,
		chi:    "ch",
	})
}

func romanizeGreekAncient(s string, keepDiacritics bool) (string, bool) {
	return romanizeGreekALALC(s, keepDiacritics)
}

func romanizeGreekALALC(s string, keepDiacritics bool) (string, bool) {
	return transliterateGreekWithOptions(s, greekTransliterationOptions{
		scheme:      greekRomanizationALALC,
		lengthMarks: keepDiacritics,
		chi:         "ch",
	})
}

func romanizeGreekALALCRich(s string) (string, bool) {
	return transliterateGreekWithOptions(s, greekTransliterationOptions{
		scheme:            greekRomanizationALALC,
		accents:           true,
		lengthMarks:       true,
		defaultShortMarks: true,
		chi:               "ch",
	})
}

func transliterateGreekWithOptions(s string, opts greekTransliterationOptions) (string, bool) {
	tokens := greekTokens(s)
	hasGreek := false
	var out strings.Builder
	wordStart := true
	var prevGreek rune

	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if !t.letter {
			out.WriteString(t.text)
			wordStart = true
			prevGreek = 0
			continue
		}
		hasGreek = true

		if next, ok := nextGreekToken(tokens, i+1); ok && formsGreekDiphthong(t, next.greekToken) {
			out.WriteString(transliterateGreekDiphthong(t, next.greekToken, wordStart, opts))
			i = next.index
			wordStart = false
			prevGreek = next.base
			continue
		}

		if t.base == 'γ' && hasGammaNasal(tokens, i+1) {
			out.WriteString(applyGreekCase("n", t.upper))
			wordStart = false
			prevGreek = t.base
			continue
		}

		out.WriteString(transliterateGreekSingle(t, wordStart, prevGreek, opts))
		wordStart = false
		prevGreek = t.base
	}

	return out.String(), hasGreek
}

type indexedGreekToken struct {
	greekToken
	index int
}

func nextGreekToken(tokens []greekToken, start int) (indexedGreekToken, bool) {
	if start < len(tokens) && tokens[start].letter {
		return indexedGreekToken{greekToken: tokens[start], index: start}, true
	}
	return indexedGreekToken{}, false
}

func greekTokens(s string) []greekToken {
	var tokens []greekToken
	for i, r := range s {
		end := i + utf8.RuneLen(r)
		if len(tokens) > 0 {
			last := &tokens[len(tokens)-1]
			switch r {
			case '\u0301', '\u0341':
				last.acute = true
				last.end = end
				continue
			case '\u0300', '\u0340':
				last.grave = true
				last.end = end
				continue
			case '\u0342', '\u0302':
				last.circumflex = true
				last.end = end
				continue
			case '\u0314':
				last.rough = true
				last.end = end
				continue
			case '\u0308':
				last.diaeresis = true
				last.end = end
				continue
			case '\u0345':
				last.subscript = true
				last.end = end
				continue
			case '\u0304':
				last.long = true
				last.end = end
				continue
			case '\u0306':
				last.short = true
				last.end = end
				continue
			}
		}
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if token, ok := greekRuneToken(r); ok {
			token.start = i
			token.end = end
			tokens = append(tokens, token)
			continue
		}
		tokens = append(tokens, greekToken{text: string(r), start: i, end: end})
	}
	return tokens
}

func greekRuneToken(r rune) (greekToken, bool) {
	t := greekToken{letter: true}
	switch {
	case r >= 0x1F00 && r <= 0x1F0F:
		t.base = 'α'
		t.upper = r >= 0x1F08
		setGreekBreathingAccent(&t, int(r-0x1F00))
		return t, true
	case r >= 0x1F10 && r <= 0x1F1F:
		t.base = 'ε'
		t.upper = r >= 0x1F18
		setGreekBreathingAccent(&t, int(r-0x1F10))
		return t, true
	case r >= 0x1F20 && r <= 0x1F2F:
		t.base = 'η'
		t.upper = r >= 0x1F28
		setGreekBreathingAccent(&t, int(r-0x1F20))
		return t, true
	case r >= 0x1F30 && r <= 0x1F3F:
		t.base = 'ι'
		t.upper = r >= 0x1F38
		setGreekBreathingAccent(&t, int(r-0x1F30))
		return t, true
	case r >= 0x1F40 && r <= 0x1F4F:
		t.base = 'ο'
		t.upper = r >= 0x1F48
		setGreekBreathingAccent(&t, int(r-0x1F40))
		return t, true
	case r >= 0x1F50 && r <= 0x1F5F:
		t.base = 'υ'
		t.upper = r >= 0x1F58
		setGreekBreathingAccent(&t, int(r-0x1F50))
		return t, true
	case r >= 0x1F60 && r <= 0x1F6F:
		t.base = 'ω'
		t.upper = r >= 0x1F68
		setGreekBreathingAccent(&t, int(r-0x1F60))
		return t, true
	case r >= 0x1F80 && r <= 0x1F8F:
		t.base = 'α'
		t.upper = r >= 0x1F88
		setGreekBreathingAccent(&t, int(r-0x1F80))
		t.subscript = true
		return t, true
	case r >= 0x1F90 && r <= 0x1F9F:
		t.base = 'η'
		t.upper = r >= 0x1F98
		setGreekBreathingAccent(&t, int(r-0x1F90))
		t.subscript = true
		return t, true
	case r >= 0x1FA0 && r <= 0x1FAF:
		t.base = 'ω'
		t.upper = r >= 0x1FA8
		setGreekBreathingAccent(&t, int(r-0x1FA0))
		t.subscript = true
		return t, true
	}

	switch r {
	case 'α':
		t.base = 'α'
	case 'ά':
		t.base = 'α'
		t.acute = true
	case 'ὰ':
		t.base = 'α'
		t.grave = true
	case 'ᾶ':
		t.base = 'α'
		t.circumflex = true
	case 'ᾰ':
		t.base = 'α'
		t.short = true
	case 'ᾱ':
		t.base = 'α'
		t.long = true
	case 'Α':
		t.base = 'α'
		t.upper = true
	case 'Ά':
		t.base = 'α'
		t.upper = true
		t.acute = true
	case 'Ὰ':
		t.base = 'α'
		t.upper = true
		t.grave = true
	case 'Ᾱ':
		t.base = 'α'
		t.upper = true
		t.long = true
	case 'Ᾰ':
		t.base = 'α'
		t.upper = true
		t.short = true
	case 'ᾳ', 'ᾴ', 'ᾲ', 'ᾷ':
		t.base = 'α'
		t.subscript = true
		switch r {
		case 'ᾴ':
			t.acute = true
		case 'ᾲ':
			t.grave = true
		case 'ᾷ':
			t.circumflex = true
		}
	case 'ᾼ':
		t.base = 'α'
		t.upper = true
		t.subscript = true
	case 'β':
		t.base = 'β'
	case 'Β':
		t.base = 'β'
		t.upper = true
	case 'γ':
		t.base = 'γ'
	case 'Γ':
		t.base = 'γ'
		t.upper = true
	case 'δ':
		t.base = 'δ'
	case 'Δ':
		t.base = 'δ'
		t.upper = true
	case 'ε':
		t.base = 'ε'
	case 'έ':
		t.base = 'ε'
		t.acute = true
	case 'ὲ':
		t.base = 'ε'
		t.grave = true
	case 'Ε':
		t.base = 'ε'
		t.upper = true
	case 'Έ':
		t.base = 'ε'
		t.upper = true
		t.acute = true
	case 'Ὲ':
		t.base = 'ε'
		t.upper = true
		t.grave = true
	case 'ζ':
		t.base = 'ζ'
	case 'Ζ':
		t.base = 'ζ'
		t.upper = true
	case 'η':
		t.base = 'η'
	case 'ή':
		t.base = 'η'
		t.acute = true
	case 'ὴ':
		t.base = 'η'
		t.grave = true
	case 'ῆ':
		t.base = 'η'
		t.circumflex = true
	case 'Η':
		t.base = 'η'
		t.upper = true
	case 'Ή':
		t.base = 'η'
		t.upper = true
		t.acute = true
	case 'Ὴ':
		t.base = 'η'
		t.upper = true
		t.grave = true
	case 'ῃ', 'ῄ', 'ῂ', 'ῇ':
		t.base = 'η'
		t.subscript = true
		switch r {
		case 'ῄ':
			t.acute = true
		case 'ῂ':
			t.grave = true
		case 'ῇ':
			t.circumflex = true
		}
	case 'ῌ':
		t.base = 'η'
		t.upper = true
		t.subscript = true
	case 'θ':
		t.base = 'θ'
	case 'Θ':
		t.base = 'θ'
		t.upper = true
	case 'ι':
		t.base = 'ι'
	case 'ί':
		t.base = 'ι'
		t.acute = true
	case 'ὶ':
		t.base = 'ι'
		t.grave = true
	case 'ῖ':
		t.base = 'ι'
		t.circumflex = true
	case 'ῐ':
		t.base = 'ι'
		t.short = true
	case 'ῑ':
		t.base = 'ι'
		t.long = true
	case 'Ι':
		t.base = 'ι'
		t.upper = true
	case 'Ί':
		t.base = 'ι'
		t.upper = true
		t.acute = true
	case 'Ὶ':
		t.base = 'ι'
		t.upper = true
		t.grave = true
	case 'Ῑ':
		t.base = 'ι'
		t.upper = true
		t.long = true
	case 'Ῐ':
		t.base = 'ι'
		t.upper = true
		t.short = true
	case 'ϊ', 'ΐ', 'ῒ', 'ῗ':
		t.base = 'ι'
		t.diaeresis = true
		switch r {
		case 'ΐ':
			t.acute = true
		case 'ῒ':
			t.grave = true
		case 'ῗ':
			t.circumflex = true
		}
	case 'Ϊ':
		t.base = 'ι'
		t.upper = true
		t.diaeresis = true
	case 'κ':
		t.base = 'κ'
	case 'Κ':
		t.base = 'κ'
		t.upper = true
	case 'λ':
		t.base = 'λ'
	case 'Λ':
		t.base = 'λ'
		t.upper = true
	case 'μ':
		t.base = 'μ'
	case 'Μ':
		t.base = 'μ'
		t.upper = true
	case 'ν':
		t.base = 'ν'
	case 'Ν':
		t.base = 'ν'
		t.upper = true
	case 'ξ':
		t.base = 'ξ'
	case 'Ξ':
		t.base = 'ξ'
		t.upper = true
	case 'ο':
		t.base = 'ο'
	case 'ό':
		t.base = 'ο'
		t.acute = true
	case 'ὸ':
		t.base = 'ο'
		t.grave = true
	case 'Ο':
		t.base = 'ο'
		t.upper = true
	case 'Ό':
		t.base = 'ο'
		t.upper = true
		t.acute = true
	case 'Ὸ':
		t.base = 'ο'
		t.upper = true
		t.grave = true
	case 'π':
		t.base = 'π'
	case 'Π':
		t.base = 'π'
		t.upper = true
	case 'ρ', 'ῤ':
		t.base = 'ρ'
	case 'ῥ':
		t.base = 'ρ'
		t.rough = true
	case 'Ρ':
		t.base = 'ρ'
		t.upper = true
	case 'Ῥ':
		t.base = 'ρ'
		t.upper = true
		t.rough = true
	case 'σ', 'ς':
		t.base = 'σ'
	case 'Σ':
		t.base = 'σ'
		t.upper = true
	case 'τ':
		t.base = 'τ'
	case 'Τ':
		t.base = 'τ'
		t.upper = true
	case 'υ':
		t.base = 'υ'
	case 'ύ':
		t.base = 'υ'
		t.acute = true
	case 'ὺ':
		t.base = 'υ'
		t.grave = true
	case 'ῦ':
		t.base = 'υ'
		t.circumflex = true
	case 'ῠ':
		t.base = 'υ'
		t.short = true
	case 'ῡ':
		t.base = 'υ'
		t.long = true
	case 'Υ':
		t.base = 'υ'
		t.upper = true
	case 'Ύ':
		t.base = 'υ'
		t.upper = true
		t.acute = true
	case 'Ὺ':
		t.base = 'υ'
		t.upper = true
		t.grave = true
	case 'Ῡ':
		t.base = 'υ'
		t.upper = true
		t.long = true
	case 'Ῠ':
		t.base = 'υ'
		t.upper = true
		t.short = true
	case 'ϋ', 'ΰ', 'ῢ', 'ῧ':
		t.base = 'υ'
		t.diaeresis = true
		switch r {
		case 'ΰ':
			t.acute = true
		case 'ῢ':
			t.grave = true
		case 'ῧ':
			t.circumflex = true
		}
	case 'Ϋ':
		t.base = 'υ'
		t.upper = true
		t.diaeresis = true
	case 'φ':
		t.base = 'φ'
	case 'Φ':
		t.base = 'φ'
		t.upper = true
	case 'χ':
		t.base = 'χ'
	case 'Χ':
		t.base = 'χ'
		t.upper = true
	case 'ψ':
		t.base = 'ψ'
	case 'Ψ':
		t.base = 'ψ'
		t.upper = true
	case 'ω':
		t.base = 'ω'
	case 'ώ':
		t.base = 'ω'
		t.acute = true
	case 'ὼ':
		t.base = 'ω'
		t.grave = true
	case 'ῶ':
		t.base = 'ω'
		t.circumflex = true
	case 'Ω':
		t.base = 'ω'
		t.upper = true
	case 'Ώ':
		t.base = 'ω'
		t.upper = true
		t.acute = true
	case 'Ὼ':
		t.base = 'ω'
		t.upper = true
		t.grave = true
	case 'ῳ', 'ῴ', 'ῲ', 'ῷ':
		t.base = 'ω'
		t.subscript = true
		switch r {
		case 'ῴ':
			t.acute = true
		case 'ῲ':
			t.grave = true
		case 'ῷ':
			t.circumflex = true
		}
	case 'ῼ':
		t.base = 'ω'
		t.upper = true
		t.subscript = true
	case 'ϝ', 'Ϝ':
		t.base = 'ϝ'
		t.upper = r == 'Ϝ'
	default:
		return greekToken{}, false
	}

	switch r {
	case 'ᾱ', 'ῑ', 'ῡ', 'Ᾱ', 'Ῑ', 'Ῡ':
		t.long = true
	case 'ᾰ', 'ῐ', 'ῠ', 'Ᾰ', 'Ῐ', 'Ῠ':
		t.short = true
	}
	return t, true
}

func setGreekBreathingAccent(t *greekToken, offset int) {
	marks := offset % 8
	t.rough = marks%2 == 1
	switch marks {
	case 2, 3:
		t.grave = true
	case 4, 5:
		t.acute = true
	case 6, 7:
		t.circumflex = true
	}
}

func formsGreekDiphthong(a, b greekToken) bool {
	if b.diaeresis || a.subscript || b.subscript {
		return false
	}
	switch a.base {
	case 'α':
		return b.base == 'ι' || b.base == 'υ'
	case 'ε':
		return b.base == 'ι' || b.base == 'υ'
	case 'η':
		return b.base == 'υ'
	case 'ο':
		return b.base == 'ι' || b.base == 'υ'
	case 'υ':
		return b.base == 'ι'
	case 'ω':
		return b.base == 'υ'
	default:
		return false
	}
}

func transliterateGreekDiphthong(a, b greekToken, wordStart bool, opts greekTransliterationOptions) string {
	digraph := ""
	switch opts.scheme {
	case greekRomanizationClassical:
		switch {
		case a.base == 'α' && b.base == 'ι':
			digraph = "ae"
		case a.base == 'α' && b.base == 'υ':
			digraph = "au"
		case a.base == 'ε' && b.base == 'ι':
			digraph = "e"
		case a.base == 'ε' && b.base == 'υ':
			digraph = "eu"
		case a.base == 'η' && b.base == 'υ':
			digraph = "eu"
		case a.base == 'ο' && b.base == 'ι':
			digraph = "oe"
		case a.base == 'ο' && b.base == 'υ':
			digraph = "u"
		case a.base == 'υ' && b.base == 'ι':
			digraph = "ui"
		case a.base == 'ω' && b.base == 'υ':
			digraph = "ou"
		}
	default:
		switch {
		case a.base == 'α' && b.base == 'ι':
			digraph = "ai"
		case a.base == 'α' && b.base == 'υ':
			digraph = "au"
		case a.base == 'ε' && b.base == 'ι':
			digraph = "ei"
		case a.base == 'ε' && b.base == 'υ':
			digraph = "eu"
		case a.base == 'η' && b.base == 'υ':
			if opts.lengthMarks {
				digraph = "ēu"
			} else {
				digraph = "eu"
			}
		case a.base == 'ο' && b.base == 'ι':
			digraph = "oi"
		case a.base == 'ο' && b.base == 'υ':
			digraph = "ou"
		case a.base == 'υ' && b.base == 'ι':
			digraph = "ui"
		case a.base == 'ω' && b.base == 'υ':
			if opts.lengthMarks {
				digraph = "ōu"
			} else {
				digraph = "ou"
			}
		}
	}
	if opts.accents {
		if hasGreekAccent(b) {
			digraph = accentLastLatinVowel(digraph, b)
		} else if hasGreekAccent(a) {
			digraph = accentFirstLatinVowel(digraph, a)
		}
	}
	if a.rough || b.rough {
		digraph = "h" + digraph
	}
	return applyGreekCase(digraph, a.upper)
}

func hasGammaNasal(tokens []greekToken, start int) bool {
	next, ok := nextGreekToken(tokens, start)
	if !ok {
		return false
	}
	switch next.base {
	case 'γ', 'κ', 'ξ', 'χ':
		return true
	default:
		return false
	}
}

func transliterateGreekSingle(t greekToken, wordStart bool, prevGreek rune, opts greekTransliterationOptions) string {
	out := ""
	switch t.base {
	case 'α':
		out = greekAlphaLatin(t, opts)
		if t.subscript {
			if opts.scheme == greekRomanizationClassical {
				out = "ai"
			} else {
				out = "ai"
			}
		}
	case 'β':
		out = "b"
	case 'γ':
		out = "g"
	case 'δ':
		out = "d"
	case 'ε':
		out = "e"
	case 'ζ':
		out = "z"
	case 'η':
		if opts.scheme == greekRomanizationClassical {
			out = "e"
		} else if opts.lengthMarks {
			out = "ē"
		} else {
			out = "e"
		}
		if t.subscript {
			if opts.scheme == greekRomanizationClassical {
				out = "ei"
			} else {
				out += "i"
			}
		}
	case 'θ':
		out = "th"
	case 'ι':
		out = "i"
		if opts.lengthMarks && t.diaeresis {
			out = "ï"
		} else if opts.lengthMarks && t.long {
			out = "ī"
		} else if opts.lengthMarks && (t.short || opts.defaultShortMarks) {
			out = "ĭ"
		}
	case 'κ':
		if opts.scheme == greekRomanizationClassical {
			out = "c"
		} else {
			out = "k"
		}
	case 'λ':
		out = "l"
	case 'μ':
		out = "m"
	case 'ν':
		out = "n"
	case 'ξ':
		out = "x"
	case 'ο':
		out = "o"
	case 'π':
		out = "p"
	case 'ρ':
		out = "r"
		if t.rough || wordStart || prevGreek == 'ρ' {
			out = "rh"
		}
	case 'σ':
		out = "s"
	case 'τ':
		out = "t"
	case 'υ':
		out = "y"
		if opts.lengthMarks && t.long {
			out = "ȳ"
		} else if opts.lengthMarks && (t.short || opts.defaultShortMarks) {
			out = "y̆"
		}
		if t.rough {
			out = "h" + out
		}
	case 'φ':
		out = "ph"
	case 'χ':
		if opts.chi != "" {
			out = opts.chi
		} else {
			out = "ch"
		}
	case 'ψ':
		out = "ps"
	case 'ω':
		if opts.scheme == greekRomanizationClassical {
			out = "o"
		} else if opts.lengthMarks {
			out = "ō"
		} else {
			out = "o"
		}
		if t.subscript {
			if opts.scheme == greekRomanizationClassical {
				out = "oi"
			} else {
				out += "i"
			}
		}
	case 'ϝ':
		out = "w"
	}
	if opts.accents && hasGreekAccent(t) {
		out = accentFirstLatinVowel(out, t)
	}
	if t.rough && t.base != 'ρ' && t.base != 'υ' {
		out = "h" + out
	}
	return applyGreekCase(out, t.upper)
}

func greekAlphaLatin(t greekToken, opts greekTransliterationOptions) string {
	if !opts.lengthMarks {
		return "a"
	}
	if t.long || t.subscript {
		return "ā"
	}
	if t.short || opts.defaultShortMarks {
		return "ă"
	}
	return "a"
}

func hasGreekAccent(t greekToken) bool {
	return t.acute || t.grave || t.circumflex
}

func accentFirstLatinVowel(s string, t greekToken) string {
	return accentLatinVowel(s, t, false)
}

func accentLastLatinVowel(s string, t greekToken) string {
	return accentLatinVowel(s, t, true)
}

func accentLatinVowel(s string, t greekToken, last bool) string {
	runes := []rune(s)
	if last {
		for i := len(runes) - 1; i >= 0; i-- {
			if isLatinVowelForGreek(runes[i]) {
				return string(runes[:i]) + accentLatinRune(runes[i], t) + string(runes[i+1:])
			}
		}
		return s
	}
	for i, r := range runes {
		if isLatinVowelForGreek(r) {
			return string(runes[:i]) + accentLatinRune(r, t) + string(runes[i+1:])
		}
	}
	return s
}

func isLatinVowelForGreek(r rune) bool {
	switch r {
	case 'a', 'e', 'i', 'o', 'u', 'y',
		'A', 'E', 'I', 'O', 'U', 'Y',
		'ă', 'Ă', 'ā', 'Ā', 'ĭ', 'Ĭ', 'ī', 'Ī',
		'ŭ', 'Ŭ', 'ū', 'Ū', 'ē', 'Ē', 'ō', 'Ō',
		'ȳ', 'Ȳ', 'ï', 'Ï', 'ü', 'Ü':
		return true
	default:
		return false
	}
}

func accentLatinRune(r rune, t greekToken) string {
	if t.circumflex {
		switch r {
		case 'a':
			return "â"
		case 'A':
			return "Â"
		case 'e':
			return "ê"
		case 'E':
			return "Ê"
		case 'i':
			return "î"
		case 'I':
			return "Î"
		case 'o':
			return "ô"
		case 'O':
			return "Ô"
		case 'u':
			return "û"
		case 'U':
			return "Û"
		case 'y':
			return "ŷ"
		case 'Y':
			return "Ŷ"
		case 'ȳ', 'Ȳ':
			return string(r) + "\u0302"
		}
		return string(r) + "\u0302"
	}
	if t.acute || t.grave {
		switch r {
		case 'a':
			return "á"
		case 'A':
			return "Á"
		case 'ă':
			return "ắ"
		case 'Ă':
			return "Ắ"
		case 'e':
			return "é"
		case 'E':
			return "É"
		case 'i':
			return "í"
		case 'I':
			return "Í"
		case 'o':
			return "ó"
		case 'O':
			return "Ó"
		case 'u':
			return "ú"
		case 'U':
			return "Ú"
		case 'y':
			return "ý"
		case 'Y':
			return "Ý"
		case 'ȳ', 'Ȳ':
			return string(r) + "\u0301"
		}
		return string(r) + "\u0301"
	}
	return string(r)
}

func applyGreekCase(s string, upper bool) string {
	if !upper || s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
