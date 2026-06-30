package main

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type greekToken struct {
	base      rune
	text      string
	start     int
	end       int
	upper     bool
	rough     bool
	diaeresis bool
	subscript bool
	long      bool
	short     bool
	letter    bool
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
			out.WriteString(transliterateGreekDiphthong(t, next.greekToken, wordStart))
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

		out.WriteString(transliterateGreekSingle(t, wordStart, prevGreek))
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
		t.rough = r%2 == 1
		return t, true
	case r >= 0x1F10 && r <= 0x1F1F:
		t.base = 'ε'
		t.upper = r >= 0x1F18
		t.rough = r%2 == 1
		return t, true
	case r >= 0x1F20 && r <= 0x1F2F:
		t.base = 'η'
		t.upper = r >= 0x1F28
		t.rough = r%2 == 1
		return t, true
	case r >= 0x1F30 && r <= 0x1F3F:
		t.base = 'ι'
		t.upper = r >= 0x1F38
		t.rough = r%2 == 1
		return t, true
	case r >= 0x1F40 && r <= 0x1F4F:
		t.base = 'ο'
		t.upper = r >= 0x1F48
		t.rough = r%2 == 1
		return t, true
	case r >= 0x1F50 && r <= 0x1F5F:
		t.base = 'υ'
		t.upper = r >= 0x1F58
		t.rough = r%2 == 1
		return t, true
	case r >= 0x1F60 && r <= 0x1F6F:
		t.base = 'ω'
		t.upper = r >= 0x1F68
		t.rough = r%2 == 1
		return t, true
	case r >= 0x1F80 && r <= 0x1F8F:
		t.base = 'α'
		t.upper = r >= 0x1F88
		t.rough = r%2 == 1
		t.subscript = true
		return t, true
	case r >= 0x1F90 && r <= 0x1F9F:
		t.base = 'η'
		t.upper = r >= 0x1F98
		t.rough = r%2 == 1
		t.subscript = true
		return t, true
	case r >= 0x1FA0 && r <= 0x1FAF:
		t.base = 'ω'
		t.upper = r >= 0x1FA8
		t.rough = r%2 == 1
		t.subscript = true
		return t, true
	}

	switch r {
	case 'α', 'ά', 'ὰ', 'ᾶ', 'ᾰ', 'ᾱ':
		t.base = 'α'
	case 'Α', 'Ά', 'Ὰ', 'Ᾱ', 'Ᾰ':
		t.base = 'α'
		t.upper = true
	case 'ᾳ', 'ᾴ', 'ᾲ', 'ᾷ':
		t.base = 'α'
		t.subscript = true
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
	case 'ε', 'έ', 'ὲ':
		t.base = 'ε'
	case 'Ε', 'Έ', 'Ὲ':
		t.base = 'ε'
		t.upper = true
	case 'ζ':
		t.base = 'ζ'
	case 'Ζ':
		t.base = 'ζ'
		t.upper = true
	case 'η', 'ή', 'ὴ', 'ῆ':
		t.base = 'η'
	case 'Η', 'Ή', 'Ὴ':
		t.base = 'η'
		t.upper = true
	case 'ῃ', 'ῄ', 'ῂ', 'ῇ':
		t.base = 'η'
		t.subscript = true
	case 'ῌ':
		t.base = 'η'
		t.upper = true
		t.subscript = true
	case 'θ':
		t.base = 'θ'
	case 'Θ':
		t.base = 'θ'
		t.upper = true
	case 'ι', 'ί', 'ὶ', 'ῖ', 'ῐ', 'ῑ':
		t.base = 'ι'
	case 'Ι', 'Ί', 'Ὶ', 'Ῑ', 'Ῐ':
		t.base = 'ι'
		t.upper = true
	case 'ϊ', 'ΐ', 'ῒ', 'ῗ':
		t.base = 'ι'
		t.diaeresis = true
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
	case 'ο', 'ό', 'ὸ':
		t.base = 'ο'
	case 'Ο', 'Ό', 'Ὸ':
		t.base = 'ο'
		t.upper = true
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
	case 'υ', 'ύ', 'ὺ', 'ῦ', 'ῠ', 'ῡ':
		t.base = 'υ'
	case 'Υ', 'Ύ', 'Ὺ', 'Ῡ', 'Ῠ':
		t.base = 'υ'
		t.upper = true
	case 'ϋ', 'ΰ', 'ῢ', 'ῧ':
		t.base = 'υ'
		t.diaeresis = true
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
	case 'ω', 'ώ', 'ὼ', 'ῶ':
		t.base = 'ω'
	case 'Ω', 'Ώ', 'Ὼ':
		t.base = 'ω'
		t.upper = true
	case 'ῳ', 'ῴ', 'ῲ', 'ῷ':
		t.base = 'ω'
		t.subscript = true
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

func transliterateGreekDiphthong(a, b greekToken, wordStart bool) string {
	digraph := ""
	switch {
	case a.base == 'α' && b.base == 'ι':
		digraph = "ai"
	case a.base == 'α' && b.base == 'υ':
		digraph = "av"
	case a.base == 'ε' && b.base == 'ι':
		digraph = "ei"
	case a.base == 'ε' && b.base == 'υ':
		digraph = "ev"
	case a.base == 'η' && b.base == 'υ':
		digraph = "ēv"
	case a.base == 'ο' && b.base == 'ι':
		digraph = "oi"
	case a.base == 'ο' && b.base == 'υ':
		digraph = "ov"
	case a.base == 'υ' && b.base == 'ι':
		digraph = "vi"
	case a.base == 'ω' && b.base == 'υ':
		digraph = "ōv"
	}
	if a.rough || b.rough || (wordStart && a.base == 'υ') {
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

func transliterateGreekSingle(t greekToken, wordStart bool, prevGreek rune) string {
	out := ""
	switch t.base {
	case 'α':
		out = "a"
		if t.long || t.subscript {
			out = "ā"
		} else if t.short {
			out = "ă"
		}
		if t.subscript {
			out += "i"
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
		out = "ē"
		if t.subscript {
			out += "i"
		}
	case 'θ':
		out = "th"
	case 'ι':
		out = "i"
		if t.diaeresis {
			out = "ï"
		} else if t.long {
			out = "ī"
		} else if t.short {
			out = "ĭ"
		}
	case 'κ':
		out = "k"
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
		out = "u"
		if t.diaeresis {
			out = "ü"
		} else if t.long {
			out = "ū"
		} else if t.short {
			out = "ŭ"
		}
		if t.rough || wordStart {
			out = "h" + out
		}
	case 'φ':
		out = "ph"
	case 'χ':
		out = "ch"
	case 'ψ':
		out = "ps"
	case 'ω':
		out = "ō"
		if t.subscript {
			out += "i"
		}
	case 'ϝ':
		out = "w"
	}
	if t.rough && t.base != 'ρ' && t.base != 'υ' {
		out = "h" + out
	}
	return applyGreekCase(out, t.upper)
}

func applyGreekCase(s string, upper bool) string {
	if !upper || s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
