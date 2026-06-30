package main

import "testing"

func TestRomanizeGreekAncientText(t *testing.T) {
	input := "Ἄνδρα μοι ἔννεπε, Μοῦσα, πολύτροπον, ὃς μάλα πολλὰ"
	want := "Ắndră moi énnepe, Moûsă, polŭ́tropon, hós mắlă pollắ"

	got, ok := romanizeGreekAncient(input, true)
	if !ok {
		t.Fatal("romanizeGreekAncient did not detect Greek input")
	}
	if got != want {
		t.Fatalf("romanizeGreekAncient() = %q, want %q", got, want)
	}

	plain, ok := romanizeGreekAncient(input, false)
	if !ok {
		t.Fatal("romanizeGreekAncient plain did not detect Greek input")
	}
	const plainWant = "Andra moi ennepe, Mousa, polutropon, hos mala polla"
	if plain != plainWant {
		t.Fatalf("romanizeGreekAncient plain = %q, want %q", plain, plainWant)
	}
}

func TestGreekLoanDiphthongs(t *testing.T) {
	cases := map[string]string{
		"ου": "u",
		"αυ": "av",
		"ευ": "ev",
		"υι": "yi",
		"ῃ":  "ei",
		"ηυ": "ev",
		"ῳ":  "oi",
		"ωυ": "ov",
		"ᾳ":  "ai",
		"ᾱυ": "av",
		"ᾰυ": "av",
	}
	for input, want := range cases {
		got, ok := transliterateGreek(input)
		if !ok {
			t.Fatalf("transliterateGreek(%q) did not detect Greek input", input)
		}
		if got != want {
			t.Fatalf("transliterateGreek(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestAncientDiphthongsRemainClassical(t *testing.T) {
	cases := map[string]string{
		"ου": "ou",
		"αυ": "au",
		"ευ": "eu",
		"υι": "ui",
		"ηυ": "eu",
		"ωυ": "ou",
	}
	for input, want := range cases {
		got, ok := romanizeGreekAncient(input, false)
		if !ok {
			t.Fatalf("romanizeGreekAncient(%q) did not detect Greek input", input)
		}
		if got != want {
			t.Fatalf("romanizeGreekAncient(%q) = %q, want %q", input, got, want)
		}
	}
}
