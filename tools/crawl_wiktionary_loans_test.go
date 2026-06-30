//go:build !greektranslit

package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDoAPIRequestRetriesTransientHTTPStatus(t *testing.T) {
	var requests int
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if requests < 3 {
			return testHTTPResponse(http.StatusInternalServerError, "resource exhausted"), nil
		}
		return testHTTPResponse(http.StatusOK, `{"ok":true}`), nil
	})}

	restore := setTestHTTPRetrySettings(3, 0)
	defer restore()

	req, err := http.NewRequest(http.MethodGet, "https://example.test/api", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := doAPIRequest(client, req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
}

func TestDoAPIRequestStopsAfterRetryBudget(t *testing.T) {
	var requests int
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		return testHTTPResponse(http.StatusInternalServerError, "resource exhausted"), nil
	})}

	restore := setTestHTTPRetrySettings(2, 0)
	defer restore()

	req, err := http.NewRequest(http.MethodGet, "https://example.test/api", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := doAPIRequest(client, req)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected retry exhaustion error")
	}
	if !strings.Contains(err.Error(), "500 Internal Server Error") || !strings.Contains(err.Error(), "resource exhausted") {
		t.Fatalf("error = %q, want status and body", err)
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
}

func TestHTTPRetryBackoffDelayUsesRetryAfter(t *testing.T) {
	restore := setTestHTTPRetrySettings(5, 2*time.Second)
	defer restore()

	if got := httpRetryBackoffDelay(3, "7"); got != 7*time.Second {
		t.Fatalf("Retry-After seconds delay = %s, want 7s", got)
	}
}

func TestNewAPIRequestSetsUserAgentAndMaxlag(t *testing.T) {
	oldUserAgent := apiUserAgent
	oldMaxlag := apiMaxlag
	apiUserAgent = "test-rulat/0.1 (https://example.test/contact)"
	apiMaxlag = 7
	defer func() {
		apiUserAgent = oldUserAgent
		apiMaxlag = oldMaxlag
	}()

	req, err := newAPIRequest(map[string][]string{"action": {"query"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("User-Agent"); got != apiUserAgent {
		t.Fatalf("User-Agent = %q, want %q", got, apiUserAgent)
	}
	if got := req.URL.Query().Get("maxlag"); got != "7" {
		t.Fatalf("maxlag = %q, want 7", got)
	}
}

func TestDoAPIQueryRetriesMaxlagErrors(t *testing.T) {
	var requests int
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return testHTTPResponse(http.StatusOK, `{"error":{"code":"maxlag","info":"Waiting for 10.1.1.1: 7 seconds lagged","lag":7}}`), nil
		}
		return testHTTPResponse(http.StatusOK, `{"query":{"pages":[{"title":"ёлка"}]}}`), nil
	})}

	restore := setTestHTTPRetrySettings(3, 0)
	defer restore()

	parsed, err := doAPIQuery(client, map[string][]string{"action": {"query"}})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	if len(parsed.Query.Pages) != 1 || parsed.Query.Pages[0].Title != "ёлка" {
		t.Fatalf("pages = %#v, want one ёлка page", parsed.Query.Pages)
	}
}

func TestCrawlWordPagesSkipsNonCyrillicExactTitleBeforeFetch(t *testing.T) {
	var requests int
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		return testHTTPResponse(http.StatusOK, `{}`), nil
	})}

	rows, skipped, filtered, inspected, err := crawlWordPages(client, crawlOptions{Title: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows = %d, want 0", len(rows))
	}
	if skipped != 1 || filtered != 0 || inspected != 1 {
		t.Fatalf("skipped=%d filtered=%d inspected=%d, want skipped=1 filtered=0 inspected=1", skipped, filtered, inspected)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestCrawlWordPagesFetchesOnlyRussianAlphabetTitles(t *testing.T) {
	var fullFetches []string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		q := req.URL.Query()
		if q.Get("generator") == "allpages" {
			if q.Get("prop") != "" || q.Get("rvprop") != "" || q.Get("cllimit") != "" {
				t.Fatalf("allpages request loaded page props: %s", req.URL.RawQuery)
			}
			return testHTTPResponse(http.StatusOK, `{"query":{"pages":[{"title":"*ainaz"},{"title":"ікра"},{"title":"ёлка"}]}}`), nil
		}
		title := q.Get("titles")
		fullFetches = append(fullFetches, title)
		if title != "ёлка" {
			t.Fatalf("full fetch for %q, want only ёлка", title)
		}
		return testHTTPResponse(http.StatusOK, `{"query":{"pages":[{"title":"ёлка","revisions":[{"slots":{"main":{"content":"={{-ru-}}=\n=== Морфологические и синтаксические свойства ===\n{{сущ-ru|ёлка|ж 3a}}\n=== Этимология ===\n{{lang|en|yolka}}\n"}}}],"categories":[]}]}}`), nil
	})}

	rows, skipped, filtered, inspected, err := crawlWordPages(client, crawlOptions{TrimFinalStemVowels: true})
	if err != nil {
		t.Fatal(err)
	}
	if inspected != 3 {
		t.Fatalf("inspected = %d, want 3", inspected)
	}
	if skipped != 2 || filtered != 0 {
		t.Fatalf("skipped=%d filtered=%d, want skipped=2 filtered=0", skipped, filtered)
	}
	if len(fullFetches) != 1 || fullFetches[0] != "ёлка" {
		t.Fatalf("fullFetches = %v, want [ёлка]", fullFetches)
	}
	if len(rows) != 1 || rows[0].CyrillicStem != "ёлк" || rows[0].LatinStem != "yolk" {
		t.Fatalf("rows = %#v, want one ёлк/yolk row", rows)
	}
}

func TestCrawlWordPagesStreamsAcceptedRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stream.csv")
	writer, err := newCSVRowWriter(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		title := req.URL.Query().Get("titles")
		if title != "фаза" {
			t.Fatalf("full fetch for %q, want фаза", title)
		}
		return testHTTPResponse(http.StatusOK, `{"query":{"pages":[{"title":"фаза","revisions":[{"slots":{"main":{"content":"={{-ru-}}=\n=== Морфологические и синтаксические свойства ===\n{{сущ-ru|фа́за|ж 1a}}\n=== Этимология ===\n{{lang|en|phase}}\n"}}}],"categories":[]}]}}`), nil
	})}

	streamed := false
	rows, skipped, filtered, inspected, err := crawlWordPages(client, crawlOptions{
		Title:               "фаза",
		TrimFinalStemVowels: true,
		OnRow: func(row *csvRow) error {
			if err := writer.Write(*row); err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if !strings.Contains(string(data), "фаз,phas,фаз,phase,,stem,auto,any,English") {
				return fmt.Errorf("streamed csv = %q, want accepted row before close", string(data))
			}
			streamed = true
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !streamed {
		t.Fatal("OnRow was not called")
	}
	if skipped != 0 || filtered != 0 || inspected != 1 {
		t.Fatalf("skipped=%d filtered=%d inspected=%d, want skipped=0 filtered=0 inspected=1", skipped, filtered, inspected)
	}
	if len(rows) != 1 || rows[0].CyrillicStem != "фаз" || rows[0].LatinStem != "phas" {
		t.Fatalf("rows = %#v, want one фаз/phas row", rows)
	}
}

func TestIsRussianAlphabetPageTitle(t *testing.T) {
	cases := map[string]bool{
		"а":        true,
		"А":        true,
		"ё":        true,
		"Ё":        true,
		"эконом":   true,
		"*ainaz":   false,
		"alpha":    false,
		"ікра":     false,
		"рус-ский": false,
		"а темпо":  false,
		"русский1": false,
		"":         false,
	}
	for input, want := range cases {
		if got := isRussianAlphabetPageTitle(input); got != want {
			t.Fatalf("isRussianAlphabetPageTitle(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestGeneratedOutputPathUsesContentFlags(t *testing.T) {
	frenchWhitelist, err := parseLanguageWhitelist("French")
	if err != nil {
		t.Fatal(err)
	}
	allLanguages, err := parseLanguageWhitelist("")
	if err != nil {
		t.Fatal(err)
	}
	englishTranslationWhitelist, err := parseLanguageWhitelist("English")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		opts outputNameOptions
		want string
	}{
		{
			name: "default pages",
			opts: outputNameOptions{
				Source:                       "pages",
				LanguageWhitelist:            mustDefaultLanguageWhitelist(t),
				TranslationLanguageWhitelist: mustDefaultTranslationLanguageWhitelist(t),
				ResolveTemplates:             true,
				StripStemDiacritics:          true,
				TrimFinalStemVowels:          true,
			},
			want: "loan_stems.wiktionary.pages.csv",
		},
		{
			name: "from and limit",
			opts: outputNameOptions{
				Source:                       "pages",
				From:                         "ф",
				Limit:                        100,
				LanguageWhitelist:            mustDefaultLanguageWhitelist(t),
				TranslationLanguageWhitelist: mustDefaultTranslationLanguageWhitelist(t),
				ResolveTemplates:             true,
				StripStemDiacritics:          true,
				TrimFinalStemVowels:          true,
			},
			want: "loan_stems.wiktionary.pages.from-ф.limit-100.csv",
		},
		{
			name: "title language and disabled normalizers",
			opts: outputNameOptions{
				Source:                       "pages",
				Title:                        "альков",
				From:                         "ignored",
				Limit:                        10,
				LanguageWhitelist:            frenchWhitelist,
				TranslationLanguageWhitelist: mustDefaultTranslationLanguageWhitelist(t),
				ResolveTemplates:             false,
				StripStemDiacritics:          false,
				TrimFinalStemVowels:          false,
			},
			want: "loan_stems.wiktionary.pages.title-альков.no-template-resolve.langs-French.keep-diacritics.keep-final-vowels.csv",
		},
		{
			name: "appendix source page and all languages",
			opts: outputNameOptions{
				Source:                       "appendix",
				SourceTitle:                  "Приложение:Тест",
				Limit:                        5,
				LanguageWhitelist:            allLanguages,
				TranslationLanguageWhitelist: mustDefaultTranslationLanguageWhitelist(t),
				StripStemDiacritics:          true,
				TrimFinalStemVowels:          true,
			},
			want: "loan_stems.wiktionary.appendix.source-Приложение-Тест.limit-5.langs-all.csv",
		},
		{
			name: "non-default translation fallback languages",
			opts: outputNameOptions{
				Source:                       "pages",
				LanguageWhitelist:            mustDefaultLanguageWhitelist(t),
				TranslationLanguageWhitelist: englishTranslationWhitelist,
				ResolveTemplates:             true,
				StripStemDiacritics:          true,
				TrimFinalStemVowels:          true,
			},
			want: "loan_stems.wiktionary.pages.translation-langs-English.csv",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := generatedOutputPath(tc.opts); got != tc.want {
				t.Fatalf("generatedOutputPath() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDefaultTranslationWhitelistMatchesDefaultLanguageWhitelist(t *testing.T) {
	if !languageMapsEqual(mustDefaultTranslationLanguageWhitelist(t), mustDefaultLanguageWhitelist(t)) {
		t.Fatalf("default translation language whitelist should match default source language whitelist")
	}
}

func TestWriteCSVWritesHeaderAndRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rows.csv")
	rows := []csvRow{{
		CyrillicStem:          "фаз",
		LatinStem:             "phas",
		MatchedRussianReading: "фаз",
		OriginalLatin:         "phase",
		Mode:                  "stem",
		CaseMode:              "auto",
		MatchCase:             "any",
		Source:                "English",
		Notes:                 "test",
		URL:                   "https://example.test/фаза",
	}}
	if err := writeCSV(path, rows); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.Contains(got, "cyrillic_stem,latin_stem,matched_russian_reading,original_latin,original_greek,mode,case_mode,match_case,source,notes,suffix_context,url\n") {
		t.Fatalf("csv = %q, want header", got)
	}
	if !strings.Contains(got, "фаз,phas,фаз,phase,,stem,auto,any,English,test,,https://example.test/фаза\n") {
		t.Fatalf("csv = %q, want row", got)
	}
}

func mustDefaultLanguageWhitelist(t *testing.T) map[string]bool {
	t.Helper()
	whitelist, err := parseLanguageWhitelist(defaultLanguageWhitelist)
	if err != nil {
		t.Fatal(err)
	}
	return whitelist
}

func mustDefaultTranslationLanguageWhitelist(t *testing.T) map[string]bool {
	t.Helper()
	whitelist, err := parseLanguageWhitelist(defaultTranslationLanguageWhitelist)
	if err != nil {
		t.Fatal(err)
	}
	return whitelist
}

func TestTextMarkerCandidatesKeepEtymologyOrder(t *testing.T) {
	text := "От нидерл. abrikoos, далее из франц. abricot, далее из англ. apricot."
	candidates := candidatesFromTextMarkers(text)
	if len(candidates) < 3 {
		t.Fatalf("candidates = %#v, want at least 3", candidates)
	}
	want := []loanCandidate{
		{Source: "Dutch", Latin: "abrikoos"},
		{Source: "French", Latin: "abricot"},
		{Source: "English", Latin: "apricot"},
	}
	for i := range want {
		if candidates[i] != want[i] {
			t.Fatalf("candidate %d = %#v, want %#v; all candidates = %#v", i, candidates[i], want[i], candidates)
		}
	}
}

func TestRowFromWordPagePrefersDirectDutchDonor(t *testing.T) {
	page := testWikiPage("абрикос", ruNounPageContent("{{сущ-ru|абрико́с|м 1a}}\n{{морфо-ru|абрикос|+∅}}", "От нидерл. abrikoos, далее из франц. abricot, далее из англ. apricot."))

	row, ok, filtered := rowFromWordPage(page, crawlOptions{
		LanguageWhitelist:   map[string]bool{"Dutch": true, "French": true, "English": true},
		StripStemDiacritics: true,
		TrimFinalStemVowels: true,
	})
	if filtered {
		t.Fatal("row was unexpectedly filtered")
	}
	if !ok {
		t.Fatal("row was not extracted")
	}
	if row.Source != "Dutch" || row.OriginalLatin != "abrikoos" || row.LatinStem != "abrikoos" {
		t.Fatalf("row = %#v, want Dutch abrikoos", row)
	}
	if row.MatchedRussianReading != "абрикос" {
		t.Fatalf("matched reading = %q, want абрикос", row.MatchedRussianReading)
	}
}

func TestRowFromWordPageFallsBackToTranslationSection(t *testing.T) {
	page := testWikiPage("Ахиллес", "={{-ru-}}=\n=== Морфологические и синтаксические свойства ===\n{{сущ-ru|Ахилле́с|м 1a}}\n=== Значение ===\n# персонаж древнегреческой мифологии\n=== Перевод ===\n==== Список переводов ====\n* {{перев|el|Αχιλλέας|м}}\n=== Родственные слова ===\n{{прил ru|ахилле́сов}}\n")

	row, ok, filtered := rowFromWordPage(page, crawlOptions{
		LanguageWhitelist:            map[string]bool{"Greek": true},
		TranslationLanguageWhitelist: map[string]bool{"Greek": true},
		StripStemDiacritics:          true,
		TrimFinalStemVowels:          true,
	})
	if filtered {
		t.Fatal("row was unexpectedly filtered")
	}
	if !ok {
		t.Fatal("row was not extracted")
	}
	if row.CyrillicStem != "ахиллес" || row.LatinStem != "Achilleas" || row.OriginalGreek != "Αχιλλέας" || row.Source != "Greek" {
		t.Fatalf("row = %#v, want ахиллес/Achilleas from Greek translation", row)
	}
	if row.MatchedRussianReading != "ахилес" {
		t.Fatalf("matched reading = %q, want ахилес", row.MatchedRussianReading)
	}
	if !strings.Contains(row.Notes, "translation candidate") {
		t.Fatalf("notes = %q, want translation candidate note", row.Notes)
	}
}

func TestRowFromWordPageUsesMeaningMarkerForTranslationLanguage(t *testing.T) {
	page := testWikiPage("Айова", "={{-ru-}}=\n=== Морфологические и синтаксические свойства ===\n{{сущ-ru|Айо́ва|ж 0}}\n=== Значение ===\n# [[штат]] в [[США]]\n=== Этимология ===\nПроисходит от ??\n=== Перевод ===\n==== Список переводов ====\n* {{перев|el|Άιοβα|ж}}\n* {{перев|en|Iowa}}\n")

	row, ok, filtered := rowFromWordPage(page, crawlOptions{
		LanguageWhitelist:            map[string]bool{"English": true, "Greek": true},
		TranslationLanguageWhitelist: map[string]bool{"Greek": true},
		StripStemDiacritics:          true,
		TrimFinalStemVowels:          true,
	})
	if filtered {
		t.Fatal("row was unexpectedly filtered")
	}
	if !ok {
		t.Fatal("row was not extracted")
	}
	if row.CyrillicStem != "айова" || row.LatinStem != "Iowa" || row.OriginalLatin != "Iowa" || row.Source != "English" {
		t.Fatalf("row = %#v, want айова/Iowa from English translation", row)
	}
	if row.MatchedRussianReading != "айова" {
		t.Fatalf("matched reading = %q, want айова", row.MatchedRussianReading)
	}
}

func TestRowFromWordPageMarksCapitalizedNameMatchCase(t *testing.T) {
	page := testWikiPage("Гипербореи", ruNounPageContent("{{сущ ru m ina 6a|основа=Гиперборе́}}\n{{морфо-ru|Гипер--|бореj|+и}}", "{{lang|la|Hyperborēī|живущие за Бореем}}"))

	row, ok, filtered := rowFromWordPage(page, crawlOptions{
		StripStemDiacritics: true,
		TrimFinalStemVowels: true,
	})
	if filtered {
		t.Fatal("row was unexpectedly filtered")
	}
	if !ok {
		t.Fatal("row was not extracted")
	}
	if row.MatchCase != "capitalized" {
		t.Fatalf("match case = %q, want capitalized; row = %#v", row.MatchCase, row)
	}
}

func TestMeaningTranslationLanguageWhitelistDetectsDefaultLanguages(t *testing.T) {
	cases := map[string]string{
		"город в США":                  "English",
		"город в Германии":             "German",
		"коммуна во Франции":           "French",
		"область в Италии":             "Italian",
		"персонаж греческой мифологии": "Greek",
		"древнеримское имя":            "Latin",
		"город в Нидерландах":          "Dutch",
		"израильское личное имя":       "Hebrew",
		"шведская фамилия":             "Swedish",
		"датский топоним":              "Danish",
		"испанский остров":             "Spanish",
		"японский город":               "Japanese",
	}
	for meaning, want := range cases {
		ruSection := "=== Значение ===\n# " + meaning + "\n=== Перевод ===\n"
		whitelist := meaningTranslationLanguageWhitelist(ruSection)
		if len(whitelist) != 1 || !whitelist[want] {
			t.Fatalf("meaningTranslationLanguageWhitelist(%q) = %#v, want only %s", meaning, whitelist, want)
		}
	}
}

func TestRowFromWordPageBuildsStemFromPositionalMorphology(t *testing.T) {
	page := testWikiPage("Баромир", "={{-ru-}}=\n=== Морфологические и синтаксические свойства ===\n{{сущ-ru|Ба́ромир|м 1a}}\n{{морфо-ru|Бар|о|мир|+∅|и=пример}}\n=== Значение ===\n# английское имя\n=== Перевод ===\n{{перев-блок|\n|en={{t|en|Baromir}}\n}}\n")

	row, ok, filtered := rowFromWordPage(page, crawlOptions{
		LanguageWhitelist:            map[string]bool{"English": true},
		TranslationLanguageWhitelist: map[string]bool{"English": true},
		StripStemDiacritics:          true,
		TrimFinalStemVowels:          true,
	})
	if filtered {
		t.Fatal("row was unexpectedly filtered")
	}
	if !ok {
		t.Fatal("row was not extracted")
	}
	if row.CyrillicStem != "баромир" || row.LatinStem != "Baromir" || row.MatchedRussianReading != "баромир" {
		t.Fatalf("row = %#v, want баромир/Baromir with matched reading баромир", row)
	}
}

func TestTranslationFallbackRequiresMeaningLanguageMarker(t *testing.T) {
	page := testWikiPage("Баромир", "={{-ru-}}=\n=== Морфологические и синтаксические свойства ===\n{{сущ-ru|Ба́ромир|м 1a}}\n{{морфо-ru|Бар|о|мир|+∅}}\n=== Перевод ===\n{{перев-блок|\n|en={{t|en|Baromir}}\n}}\n")

	row, ok, filtered := rowFromWordPage(page, crawlOptions{
		LanguageWhitelist:            map[string]bool{"English": true, "Greek": true},
		TranslationLanguageWhitelist: map[string]bool{"Greek": true},
		StripStemDiacritics:          true,
		TrimFinalStemVowels:          true,
	})
	if filtered {
		t.Fatal("row was unexpectedly filtered")
	}
	if ok {
		t.Fatalf("row = %#v, want translation candidate ignored without meaning marker", row)
	}
}

func TestSourceConsonantsMatchRussian(t *testing.T) {
	cases := []struct {
		name   string
		cyr    string
		source string
		want   bool
	}{
		{"x as х", "х", "x", true},
		{"x as кс", "кс", "x", true},
		{"x as з", "з", "x", true},
		{"ch as х", "х", "ch", true},
		{"ch as ч", "ч", "ch", true},
		{"ch as ш", "ш", "ch", true},
		{"qu as к", "к", "qu", true},
		{"qu as кв", "кв", "qu", true},
		{"b as б", "б", "b", true},
		{"b as в", "в", "b", true},
		{"sch as ш", "ш", "sch", true},
		{"sch as с", "с", "sch", true},
		{"sch as ж", "ж", "sch", true},
		{"ph as п", "п", "ph", true},
		{"ll as л", "л", "ll", true},
		{"silent h", "онор", "honor", true},
		{"h as г", "г", "h", true},
		{"j as й", "й", "j", true},
		{"j as дж", "дж", "j", true},
		{"ts as ц", "ц", "ts", true},
		{"tz as ц", "ц", "tz", true},
		{"cz as ц", "ц", "cz", true},
		{"cz as ч", "ч", "cz", true},
		{"cedilla as с", "с", "ç", true},
		{"zh as ж", "ж", "zh", true},
		{"g as дж", "дж", "g", true},
		{"cu as кв", "кв", "cu", true},
		{"cu as к", "к", "cu", true},
		{"qu as ку", "ку", "qu", true},
		{"sc as ск", "ск", "sc", true},
		{"sc as ш", "ш", "sc", true},
		{"sc as щ", "щ", "sc", true},
		{"phase as фаз", "фаз", "phase", true},
		{"fals as фальш", "фальш", "fals", true},
		{"adjacent russian duplicate collapses", "ахиллес", "Achilleas", true},
		{"vowel-separated russian duplicate does not collapse", "агиограф", "graph", false},
		{"facere mismatch", "факсимиле", "facere", false},
		{"Falte mismatch", "фалда", "Falte", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sourceConsonantsMatchRussian(tc.cyr, tc.source); got != tc.want {
				t.Fatalf("sourceConsonantsMatchRussian(%q, %q) = %v, want %v", tc.cyr, tc.source, got, tc.want)
			}
		})
	}
}

func TestSourceSoundsMatchRussian(t *testing.T) {
	cases := []struct {
		name   string
		cyr    string
		source string
		want   bool
	}{
		{"factor vowels", "фактор", "factor", true},
		{"phase ending trimmed elsewhere", "фаз", "phas", true},
		{"brandy vowel adaptation", "бренди", "brandy", true},
		{"abrikoos extra vowel", "абрикос", "abrikoos", true},
		{"achilleas extra written vowel", "ахиллес", "Achilleas", true},
		{"yo as ё", "ёлк", "yolk", true},
		{"fals sibilant", "фальш", "fals", true},
		{"zh cluster", "ж", "zh", true},
		{"g as дж", "дж", "g", true},
		{"ui as эй", "эй", "ui", true},
		{"ui as ау", "ау", "ui", true},
		{"oe as у", "у", "oe", true},
		{"cu as ку", "ку", "cu", true},
		{"qu as кв", "кв", "qu", true},
		{"sc as ск", "ск", "sc", true},
		{"sc as ш", "ш", "sc", true},
		{"sc as щ", "щ", "sc", true},
		{"yolk as ёлк", "ёлк", "yolk", true},
		{"yolk as йолк", "йолк", "yolk", true},
		{"i as ай", "ай", "i", true},
		{"Iowa as Айова", "айова", "Iowa", true},
		{"э normalizes to е", "этика", "etica", true},
		{"ы normalizes to и", "сыр", "sir", true},
		{"greek direct diphthong and gamma nasal", "евангел", "ευαγγελ", true},
		{"greek direct single letters", "философ", "φιλοσοφ", true},
		{"vowel-separated russian duplicate does not collapse", "агиограф", "graph", false},
		{"facere mismatch", "факсимиле", "facere", false},
		{"Falte mismatch", "фалда", "Falte", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sourceSoundsMatchRussian(tc.cyr, tc.source); got != tc.want {
				t.Fatalf("sourceSoundsMatchRussian(%q, %q) = %v, want %v", tc.cyr, tc.source, got, tc.want)
			}
		})
	}
}

func TestRussianConsonantSequenceCollapsesOnlyAdjacentDuplicates(t *testing.T) {
	cases := map[string]string{
		"ахиллес":  "х л с",
		"агиограф": "г г р ф",
	}
	for input, want := range cases {
		got := russianConsonantReadingString(russianConsonantSequence(input))
		if got != want {
			t.Fatalf("russianConsonantSequence(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRussianReadingSequenceIncludesVowels(t *testing.T) {
	cases := map[string]string{
		"ахиллес":  "а х и л е с",
		"агиограф": "а г и о г р а ф",
		"ёлка":     "е л к а",
		"йолка":    "й о л к а",
		"жюль":     "ж у л",
		"этика":    "е т и к а",
		"сыр":      "с и р",
	}
	for input, want := range cases {
		got := russianReadingString(russianReadingSequence(input))
		if got != want {
			t.Fatalf("russianReadingSequence(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestTrimLatinStemToRussianSound(t *testing.T) {
	cases := []struct {
		cyr         string
		latin       string
		mode        string
		want        string
		wantReading string
		wantOK      bool
	}{
		{"фактор", "factor", "stem", "factor", "фактор", true},
		{"факсимиле", "facere", "word", "", "", false},
		{"фаз", "phase", "stem", "phas", "фаз", true},
		{"фалда", "Falte", "stem", "", "", false},
		{"бренди", "brandywein", "word", "brandy", "бренди", true},
		{"фальшь", "falsus", "stem", "fals", "фалш", true},
		{"фальшак", "falsus", "stem", "", "", false},
		{"ёлк", "yolk", "stem", "yolk", "елк", true},
		{"йолк", "yolk", "stem", "yolk", "йолк", true},
		{"гиперборе", "Hyperborēī", "word", "Hyperborē", "гиперборе", true},
	}
	for _, tc := range cases {
		got, matchedReading, ok := trimLatinStemToRussianSound(tc.cyr, tc.latin, tc.mode, true)
		if ok != tc.wantOK || got != tc.want {
			t.Fatalf("trimLatinStemToRussianSound(%q, %q, %q) = %q, %v; want %q, %v", tc.cyr, tc.latin, tc.mode, got, ok, tc.want, tc.wantOK)
		}
		if ok && matchedReading != tc.wantReading {
			t.Fatalf("trimLatinStemToRussianSound(%q, %q, %q) matched reading=%q; want %q", tc.cyr, tc.latin, tc.mode, matchedReading, tc.wantReading)
		}
	}
}

func TestRowFromWordPageUsesNounStemAndSoundSimilarity(t *testing.T) {
	cases := []struct {
		name      string
		title     string
		morph     string
		etymology string
		wantStem  string
		wantLatin string
		wantRead  string
		wantOK    bool
	}{
		{
			name:      "factor noun",
			title:     "фактор",
			morph:     "{{сущ-ru|фа́ктор|м 1a}}\n{{морфо-ru|фактор|+∅}}",
			etymology: "{{lang|en|factor}}",
			wantStem:  "фактор",
			wantLatin: "factor",
			wantRead:  "фактор",
			wantOK:    true,
		},
		{
			name:      "facsimile facere mismatch",
			title:     "факсимиле",
			morph:     "{{сущ-ru|факси́миле|с 0}}",
			etymology: "{{lang|la|facere}}",
			wantOK:    false,
		},
		{
			name:      "phase noun ending trimmed",
			title:     "фаза",
			morph:     "{{сущ-ru|фа́за|ж 1a}}",
			etymology: "{{lang|en|phase}}",
			wantStem:  "фаз",
			wantLatin: "phas",
			wantRead:  "фаз",
			wantOK:    true,
		},
		{
			name:      "greek neuter source against russian root",
			title:     "проблема",
			morph:     "{{сущ-ru|пробле́ма|ж 1a}}\n{{морфо-ru|корень=проблем|оконч=а}}",
			etymology: "{{lang|grc|πρόβλημα}}",
			wantStem:  "проблем",
			wantLatin: "problem",
			wantRead:  "проблем",
			wantOK:    true,
		},
		{
			name:      "greek upsilon borrowed as y",
			title:     "вавилон",
			morph:     "{{сущ-ru|Вавило́н|м 1a}}\n{{морфо-ru|вавилон|+∅}}",
			etymology: "{{lang|grc|Βαβυλών}}",
			wantStem:  "вавилон",
			wantLatin: "Babylon",
			wantRead:  "вавилон",
			wantOK:    true,
		},
		{
			name:      "named noun template stem before morpheme split",
			title:     "Гипербореи",
			morph:     "{{сущ ru m ina 6a\n|основа=Гиперборе́\n|основа1=\n|слоги={{по-слогам|Ги|пер|бо|ре́|.|и}}\n|pt=1\n|затрудн=1\n}}\n{{морфо-ru|Гипер--|бореj|+и}}",
			etymology: "{{lang|la|Hyperborēī|живущие за Бореем}}",
			wantStem:  "гиперборе",
			wantLatin: "Hyperbore",
			wantRead:  "гиперборе",
			wantOK:    true,
		},
		{
			name:      "adjective skipped",
			title:     "факультетский",
			morph:     "{{прил ru|факульте́тский}}",
			etymology: "{{lang|en|faculty}}",
			wantOK:    false,
		},
		{
			name:      "phase adjective skipped",
			title:     "фазовый",
			morph:     "{{прил ru|фа́зовый}}",
			etymology: "{{lang|en|phase}}",
			wantOK:    false,
		},
		{
			name:      "falda mismatch",
			title:     "фалда",
			morph:     "{{сущ-ru|фалда́|ж 1a}}",
			etymology: "{{lang|de|Falte}}",
			wantOK:    false,
		},
		{
			name:      "brandy compound trimmed",
			title:     "бренди",
			morph:     "{{сущ-ru|бре́нди|с 0}}",
			etymology: "{{lang|en|brandywein}}",
			wantStem:  "бренди",
			wantLatin: "brandy",
			wantRead:  "бренди",
			wantOK:    true,
		},
		{
			name:      "falsus trimmed",
			title:     "фальшь",
			morph:     "{{сущ-ru|фальшь|ж 8a}}\n{{морфо-ru|фальш|+ь}}",
			etymology: "{{lang|la|falsus}}",
			wantStem:  "фальшь",
			wantLatin: "fals",
			wantRead:  "фалш",
			wantOK:    true,
		},
		{
			name:      "falsus too short",
			title:     "фальшак",
			morph:     "{{сущ-ru|фальша́к|м 3a}}",
			etymology: "{{lang|la|falsus}}",
			wantOK:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row, ok, _ := rowFromWordPage(testWikiPage(tc.title, ruNounPageContent(tc.morph, tc.etymology)), crawlOptions{
				StripStemDiacritics: true,
				TrimFinalStemVowels: true,
			})
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v; row = %#v", ok, tc.wantOK, row)
			}
			if !tc.wantOK {
				return
			}
			if row.CyrillicStem != tc.wantStem || row.LatinStem != tc.wantLatin {
				t.Fatalf("row = %#v, want stem %q latin %q", row, tc.wantStem, tc.wantLatin)
			}
			if row.MatchedRussianReading != tc.wantRead {
				t.Fatalf("row = %#v, want matched reading %q", row, tc.wantRead)
			}
		})
	}
}

func TestLangTemplateSkipsNamedArgumentsBeforeTerm(t *testing.T) {
	text := `{{#switch:{{{1|}}}|nl=|{{lang|nl|скр={{#if:{{{1|}}}||1}}|abrikoos|}}, далее из&nbsp;}}{{этимология:abricot|{{{1|}}}}}`
	candidates := candidatesFromTemplates(text)
	if len(candidates) == 0 {
		t.Fatal("no candidates")
	}
	want := loanCandidate{Source: "Dutch", Latin: "abrikoos"}
	if candidates[0] != want {
		t.Fatalf("first candidate = %#v, want %#v; all candidates = %#v", candidates[0], want, candidates)
	}
}

func TestTranslationTemplateCandidates(t *testing.T) {
	candidates := candidatesFromTemplates(`* {{перев|el|Αχιλλέας|м}}
* {{t+|en|Achilles}}`)
	want := []loanCandidate{
		{Source: "Greek", Greek: "Αχιλλέας"},
		{Source: "English", Latin: "Achilles"},
	}
	if len(candidates) != len(want) {
		t.Fatalf("candidates = %#v, want %#v", candidates, want)
	}
	for i := range want {
		if candidates[i] != want[i] {
			t.Fatalf("candidate %d = %#v, want %#v; all candidates = %#v", i, candidates[i], want[i], candidates)
		}
	}
}

func testWikiPage(title, content string) wikiPage {
	var rev wikiRevision
	rev.Slots.Main.Content = content
	return wikiPage{Title: title, Revisions: []wikiRevision{rev}}
}

func ruNounPageContent(morphology, etymology string) string {
	return "={{-ru-}}=\n=== Морфологические и синтаксические свойства ===\n" + morphology + "\n=== Этимология ===\n" + etymology + "\n"
}

func setTestHTTPRetrySettings(retries int, delay time.Duration) func() {
	oldRetries := httpRetries
	oldDelay := httpRetryDelay
	oldRequestDelay := apiRequestDelay
	oldLastAPIRequestAt := lastAPIRequestAt
	oldSleep := retrySleep
	httpRetries = retries
	httpRetryDelay = delay
	apiRequestDelay = 0
	lastAPIRequestAt = time.Time{}
	retrySleep = func(time.Duration) {}
	return func() {
		httpRetries = oldRetries
		httpRetryDelay = oldDelay
		apiRequestDelay = oldRequestDelay
		lastAPIRequestAt = oldLastAPIRequestAt
		retrySleep = oldSleep
	}
}

func testHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
