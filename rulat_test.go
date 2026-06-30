package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNativeRules(t *testing.T) {
	cases := map[string]string{
		"Я":           "Ja",
		"ЖУК":         "ZSUK",
		"Женя":        "Zsaenea",
		"жена":        "zsaena",
		"жизнь":       "zsizne",
		"мажь":        "mazs",
		"вражий":      "vrazsij",
		"вражьи":      "vrazsji",
		"шина":        "szina",
		"шея":         "szaeja",
		"щука":        "szeuka",
		"чай":         "czaj",
		"ночь":        "nocz",
		"вечный":      "veecznij",
		"русский":     "russkeij",
		"сзади":       "zzadei",
		"французский": "frantzusskeij",
		"подъезд":     "podjezd",
		"семья":       "seemeja",
		"Марья":       "Mareja",
		"Мариа":       "Mareia",
		"Мария":       "Mareija",
		"поэт":        "poaet",
		"поёт":        "pojot",
		"поит":        "poit",
		"эй":          "ej",
		"ёж":          "jozs",
		"ел":          "jel",
		"ель":         "jele",
	}
	for in, want := range cases {
		got := Transliterate(in, nil, false)
		if got != want {
			t.Fatalf("%s: got %q want %q", in, got, want)
		}
	}
}

func TestLoanDictionary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "loans.csv")
	csv := "cyrillic_stem,latin_stem,mode,case_mode,match_case,source,notes,suffix_context\n" +
		"шин,Schien,stem,preserve,any,German,test,\n" +
		"поэт,poët,stem,auto,any,French-Greek,test,\n" +
		"зевс,Zevs,stem,auto,capitalized,Greek,test,\n" +
		"евангелие,Evangelije,word,auto,any,Greek,test,\n" +
		"жюль,Jule,word,auto,capitalized,French,test,\n" +
		"жюл,Jule,stem,auto,capitalized,French,test,soft\n"
	if err := os.WriteFile(path, []byte(csv), 0644); err != nil {
		t.Fatal(err)
	}
	entries, err := loadDictionary(path)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"шина":      "Schiena",
		"шины":      "Schieni",
		"поэт":      "poët",
		"поэта":     "poëta",
		"Зевса":     "Zevsa",
		"зевса":     "zeevsa",
		"Евангелие": "Evangelije",
		"Жюль":      "Jule",
		"Жюля":      "Julea",
		"жюля":      "zsulea",
	}
	for in, want := range cases {
		got := Transliterate(in, entries, false)
		if got != want {
			t.Fatalf("%s: got %q want %q", in, got, want)
		}
	}

	got := Transliterate("Зевса", entries, true)
	if got != "Zevs'a" {
		t.Fatalf("apostrophe: got %q want %q", got, "Zevs'a")
	}
}
