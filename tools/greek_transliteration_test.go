package main

import "testing"

func TestRomanizeGreekALALCText(t *testing.T) {
	input := "Ἄνδρα μοι ἔννεπε, Μοῦσα, πολύτροπον, ὃς μάλα πολλὰ"
	want := "Andra moi ennepe, Mousa, polytropon, hos mala polla"

	got, ok := romanizeGreekALALC(input, true)
	if !ok {
		t.Fatal("romanizeGreekALALC did not detect Greek input")
	}
	if got != want {
		t.Fatalf("romanizeGreekALALC() = %q, want %q", got, want)
	}

	plain, ok := romanizeGreekALALC(input, false)
	if !ok {
		t.Fatal("romanizeGreekALALC plain did not detect Greek input")
	}
	const plainWant = "Andra moi ennepe, Mousa, polytropon, hos mala polla"
	if plain != plainWant {
		t.Fatalf("romanizeGreekALALC plain = %q, want %q", plain, plainWant)
	}
}

func TestRomanizeGreekALALCRichText(t *testing.T) {
	input := "Ἄνδρα μοι ἔννεπε, Μοῦσα, πολύτροπον, ὃς μάλα πολλὰ"
	want := "Ắndră moi énnepe, Moûsă, polý̆tropon, hós mắlă pollắ"

	got, ok := romanizeGreekALALCRich(input)
	if !ok {
		t.Fatal("romanizeGreekALALCRich did not detect Greek input")
	}
	if got != want {
		t.Fatalf("romanizeGreekALALCRich() = %q, want %q", got, want)
	}
}

func TestGreekClassicalDiphthongs(t *testing.T) {
	cases := map[string]string{
		"ου": "u",
		"αυ": "au",
		"ευ": "eu",
		"υι": "ui",
		"ῃ":  "ei",
		"ηυ": "eu",
		"ῳ":  "oi",
		"ωυ": "ou",
		"ᾳ":  "ai",
		"ᾱυ": "au",
		"ᾰυ": "au",
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

func TestGreekClassicalYpsilonBeforeVowelUsesV(t *testing.T) {
	cases := map[string]string{
		"Εὔα":        "Eva",
		"εὐαγγέλιον": "evangelion",
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

func TestGreekClassicalUsesCY(t *testing.T) {
	got, ok := transliterateGreek("Βαβυλών")
	if !ok {
		t.Fatal("transliterateGreek did not detect Greek input")
	}
	if got != "Babylon" {
		t.Fatalf("transliterateGreek(%q) = %q, want %q", "Βαβυλών", got, "Babylon")
	}

	kappa, ok := transliterateGreek("Κύρος")
	if !ok {
		t.Fatal("transliterateGreek did not detect Greek input")
	}
	if kappa != "Cyros" {
		t.Fatalf("transliterateGreek(%q) = %q, want %q", "Κύρος", kappa, "Cyros")
	}

	anchor, ok := transliterateGreek("ἄγκυρα")
	if !ok {
		t.Fatal("transliterateGreek did not detect Greek input")
	}
	if anchor != "ancyra" {
		t.Fatalf("transliterateGreek(%q) = %q, want %q", "ἄγκυρα", anchor, "ancyra")
	}
}

func TestGreekALALCDiphthongs(t *testing.T) {
	cases := map[string]string{
		"ου": "ou",
		"αυ": "au",
		"ευ": "eu",
		"υι": "ui",
		"ηυ": "ēu",
		"ωυ": "ōu",
	}
	for input, want := range cases {
		got, ok := romanizeGreekALALC(input, true)
		if !ok {
			t.Fatalf("romanizeGreekALALC(%q) did not detect Greek input", input)
		}
		if got != want {
			t.Fatalf("romanizeGreekALALC(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestGreekALALCUsesKYAndMacrons(t *testing.T) {
	got, ok := romanizeGreekALALC("Βαβυλών Κύρος", true)
	if !ok {
		t.Fatal("romanizeGreekALALC did not detect Greek input")
	}
	if got != "Babylōn Kyros" {
		t.Fatalf("romanizeGreekALALC() = %q, want %q", got, "Babylōn Kyros")
	}
	plain, ok := romanizeGreekALALC("Βαβυλών Κύρος", false)
	if !ok {
		t.Fatal("romanizeGreekALALC plain did not detect Greek input")
	}
	if plain != "Babylon Kyros" {
		t.Fatalf("romanizeGreekALALC plain = %q, want %q", plain, "Babylon Kyros")
	}
}

func TestGreekALALCConsonantClusters(t *testing.T) {
	cases := map[string]string{
		"Γκιζίκης":    "Gkizikēs",
		"Γκέτεμποργκ": "Gketemporgk",
		"Ουάσιγκτον":  "Ouasinkton",
		"ἄγκυρα":      "ankyra",
		"Μπραντ Πιτ":  "Brant Pit",
		"Ντίνι":       "Dini",
		"Λαμπέρτο":    "Lamperto",
	}
	for input, want := range cases {
		got, ok := romanizeGreekALALC(input, true)
		if !ok {
			t.Fatalf("romanizeGreekALALC(%q) did not detect Greek input", input)
		}
		if got != want {
			t.Fatalf("romanizeGreekALALC(%q) = %q, want %q", input, got, want)
		}
	}
}
