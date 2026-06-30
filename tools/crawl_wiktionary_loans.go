//go:build !greektranslit

package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	apiURL                   = "https://ru.wiktionary.org/w/api.php"
	defaultAppendixTitle     = "Приложение:Заимствованные слова в русском языке"
	defaultLanguageWhitelist = "English,German,French,Italian,Greek,Latin,Dutch,Hebrew,Swedish,Danish,Spanish"
	defaultHTTPRetries       = 5
	defaultHTTPRetryDelay    = 2 * time.Second
	maxHTTPRetryDelay        = 30 * time.Second
	wiktionaryBaseURL        = "https://ru.wiktionary.org/wiki/"
)

type csvRow struct {
	CyrillicStem  string
	LatinStem     string
	OriginalLatin string
	OriginalGreek string
	Mode          string
	CaseMode      string
	Source        string
	Notes         string
	SuffixContext string
	URL           string
	pageTitle     string
}

type wikiResponse struct {
	Continue map[string]string `json:"continue"`
	Query    struct {
		Pages []wikiPage `json:"pages"`
	} `json:"query"`
}

type wikiPage struct {
	Title      string         `json:"title"`
	Missing    bool           `json:"missing"`
	Revisions  []wikiRevision `json:"revisions"`
	Categories []wikiCategory `json:"categories"`
}

type wikiRevision struct {
	Slots struct {
		Main struct {
			Content string `json:"content"`
		} `json:"main"`
	} `json:"slots"`
}

type wikiCategory struct {
	Title string `json:"title"`
}

type pageMeta struct {
	SourceTerms     []string
	OriginLanguages []string
	NeedsEtymology  bool
}

type loanCandidate struct {
	Source string
	Latin  string
	Greek  string
}

type templateResolver struct {
	client *http.Client
	cache  map[string][]loanCandidate
}

var (
	httpRetries    = defaultHTTPRetries
	httpRetryDelay = defaultHTTPRetryDelay
	retrySleep     = time.Sleep
)

var (
	headingRe              = regexp.MustCompile(`^==+\s*Из\s+(.+?)\s*==+\s*$`)
	bulletRe               = regexp.MustCompile(`^\*+\s*\[\[([^]|#]+)(?:#[^]|]*)?(?:\|[^]]*)?\]\]\s*(?:&nbsp;|\x{00a0}|\s)*(?:—|–|-)\s*(.+?)\s*$`)
	wikiLinkRe             = regexp.MustCompile(`\[\[([^]|]+)(?:\|([^]]+))?\]\]`)
	htmlCommentRe          = regexp.MustCompile(`(?s)<!--.*?-->`)
	refRe                  = regexp.MustCompile(`(?s)<ref\b[^>]*>.*?</ref>|<ref\b[^/]*/>`)
	langTemplateRe         = regexp.MustCompile(`(?i)\{\{\s*lang\s*\|\s*([^|{}]+)\s*\|\s*([^|{}]+)(?:\|[^{}]*)?\}\}`)
	etymologyTemplateRe    = regexp.MustCompile(`(?i)\{\{\s*этимология:([^|{}\n]+)(?:\|[^{}]*)?\}\}`)
	simpleTemplateRe       = regexp.MustCompile(`\{\{[^{}]*\}\}`)
	latinCandidateRe       = regexp.MustCompile(`[\p{Latin}][\p{Latin}\pM'’ʼʻ.-]*(?:\s+[\p{Latin}][\p{Latin}\pM'’ʼʻ.-]*){0,4}`)
	greekCandidateRe       = regexp.MustCompile(`[\p{Greek}][\p{Greek}\pM'’ʼʻ.-]*(?:\s+[\p{Greek}][\p{Greek}\pM'’ʼʻ.-]*){0,4}`)
	originCategoryRe       = regexp.MustCompile(`^Категория:(?:Слова|.+имена)\s+(.+?)\s+происхождения/ru$`)
	nameLanguageCategoryRe = regexp.MustCompile(`^Категория:(.+?)\s+имена\s+по\s+языкам$`)
	topLevelSectionRe      = regexp.MustCompile(`(?m)^=\s*[^=\n].*?=\s*$`)
	russianSectionRe       = regexp.MustCompile(`(?m)^=\s*\{\{-ru-\}\}\s*=\s*$`)
	etymologyHeadingRe     = regexp.MustCompile(`(?m)^===\s*Этимология\s*===\s*$`)
	nextSubsectionRe       = regexp.MustCompile(`(?m)^===\s+[^=\n].*?===\s*$`)
	languageNameAliases    = map[string]string{
		"english":               "English",
		"german":                "German",
		"french":                "French",
		"italian":               "Italian",
		"greek":                 "Greek",
		"latin":                 "Latin",
		"dutch":                 "Dutch",
		"hebrew":                "Hebrew",
		"swedish":               "Swedish",
		"danish":                "Danish",
		"spanish":               "Spanish",
		"arabic":                "Arabic",
		"portuguese":            "Portuguese",
		"turkish":               "Turkish",
		"turkic":                "Turkic",
		"persian":               "Persian",
		"chinese":               "Chinese",
		"japanese":              "Japanese",
		"korean":                "Korean",
		"polish":                "Polish",
		"finnish":               "Finnish",
		"norwegian":             "Norwegian",
		"armenian":              "Armenian",
		"sanskrit":              "Sanskrit",
		"abkhaz":                "Abkhaz",
		"albanian":              "Albanian",
		"hungarian":             "Hungarian",
		"malay":                 "Malay",
		"aboriginal australian": "Aboriginal Australian",
	}
	languageCodeNames = map[string]string{
		"en":  "English",
		"de":  "German",
		"fr":  "French",
		"it":  "Italian",
		"el":  "Greek",
		"grc": "Greek",
		"la":  "Latin",
		"nl":  "Dutch",
		"he":  "Hebrew",
		"hbo": "Hebrew",
		"sv":  "Swedish",
		"da":  "Danish",
		"es":  "Spanish",
		"ja":  "Japanese",
	}
	textLanguageMarkers = []struct {
		Pattern string
		Source  string
	}{
		{`англ(?:\.|ийск)`, "English"},
		{`нем(?:\.|ецк)`, "German"},
		{`франц(?:\.|узск)`, "French"},
		{`итал(?:\.|ьянск)`, "Italian"},
		{`др\.-греч\.|древнегреч|греч(?:\.|еск)`, "Greek"},
		{`лат(?:\.|инск)`, "Latin"},
		{`нидерл(?:\.|андск)|голл(?:\.|андск)`, "Dutch"},
		{`ивр(?:\.|ит)|евр(?:\.|ейск)|древнеевр`, "Hebrew"},
		{`швед(?:\.|ск)`, "Swedish"},
		{`датск`, "Danish"},
		{`исп(?:\.|анск)`, "Spanish"},
		{`япон(?:\.|ск)`, "Japanese"},
	}
)

func main() {
	crawlSource := flag.String("source", "pages", "crawl source: pages or appendix")
	sourceTitle := flag.String("source-page", defaultAppendixTitle, "Russian Wiktionary appendix page to crawl")
	outPath := flag.String("out", "loan_stems.wiktionary.csv", "CSV output path")
	limit := flag.Int("limit", 0, "maximum rows to write; 0 means all parsed rows")
	pageLimit := flag.Int("page-limit", 0, "maximum main-namespace pages to inspect in -source pages mode; 0 means unlimited")
	exactTitle := flag.String("title", "", "exact main-namespace page title to inspect in -source pages mode")
	allPagesFrom := flag.String("from", "", "main-namespace page title to start from in -source pages mode")
	batchSize := flag.Int("batch-size", 50, "MediaWiki allpages batch size in -source pages mode")
	progressEvery := flag.Int("progress-every", 0, "log page-mode progress every N inspected pages; 0 disables progress logs")
	languages := flag.String("languages", defaultLanguageWhitelist, "comma-separated source-language whitelist; empty means all languages")
	enrichPages := flag.Bool("enrich-pages", false, "fetch each word page and parse etymology/category markers")
	resolveTemplates := flag.Bool("resolve-etymology-templates", true, "resolve {{этимология:...}} templates in -source pages mode")
	includePhrases := flag.Bool("include-phrases", false, "keep multi-word Latin source forms")
	stripStemDiacritics := flag.Bool("strip-stem-diacritics", true, "remove Latin diacritics from generated latin_stem values")
	trimFinalStemVowels := flag.Bool("trim-final-stem-vowels", true, "drop final Latin vowels when generated stems attach to Russian consonant stems")
	delay := flag.Duration("delay", 100*time.Millisecond, "delay between page requests when -enrich-pages is set")
	retryAttempts := flag.Int("http-retries", defaultHTTPRetries, "retry attempts for transient HTTP failures; 0 disables retries")
	retryBaseDelay := flag.Duration("http-retry-delay", defaultHTTPRetryDelay, "initial delay for transient HTTP retries; doubles up to 30s")
	flag.Parse()

	if *retryAttempts < 0 {
		exitf("-http-retries must be >= 0")
	}
	if *retryBaseDelay < 0 {
		exitf("-http-retry-delay must be >= 0")
	}
	httpRetries = *retryAttempts
	httpRetryDelay = *retryBaseDelay

	languageWhitelist, err := parseLanguageWhitelist(*languages)
	if err != nil {
		exitf("parse language whitelist: %v", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}

	var rows []csvRow
	var skipped, filtered, inspected int

	switch *crawlSource {
	case "appendix":
		text, err := fetchWikitext(client, *sourceTitle)
		if err != nil {
			exitf("fetch appendix: %v", err)
		}
		rows, skipped, filtered = parseAppendix(text, *includePhrases, languageWhitelist, *trimFinalStemVowels)
		normalizeGeneratedRows(rows, *stripStemDiacritics)
		if *limit > 0 && *limit < len(rows) {
			rows = rows[:*limit]
		}
	case "pages":
		var resolver *templateResolver
		if *resolveTemplates {
			resolver = &templateResolver{
				client: client,
				cache:  map[string][]loanCandidate{},
			}
		}
		rows, skipped, filtered, inspected, err = crawlWordPages(client, crawlOptions{
			LanguageWhitelist:   languageWhitelist,
			IncludePhrases:      *includePhrases,
			StripStemDiacritics: *stripStemDiacritics,
			TrimFinalStemVowels: *trimFinalStemVowels,
			ProgressEvery:       *progressEvery,
			Title:               *exactTitle,
			From:                *allPagesFrom,
			BatchSize:           *batchSize,
			PageLimit:           *pageLimit,
			RowLimit:            *limit,
			Resolver:            resolver,
		})
		if err != nil {
			exitf("crawl pages: %v", err)
		}
	default:
		exitf("unknown -source %q; use pages or appendix", *crawlSource)
	}

	enriched := 0
	if *enrichPages {
		for i := range rows {
			meta, err := fetchPageMeta(client, rows[i].pageTitle)
			if err != nil {
				rows[i].Notes = appendNote(rows[i].Notes, "page enrichment failed")
			} else {
				applyPageMeta(&rows[i], meta, languageWhitelist, *trimFinalStemVowels, *stripStemDiacritics)
				enriched++
			}
			if *delay > 0 && i+1 < len(rows) {
				time.Sleep(*delay)
			}
			if *progressEvery > 0 && (i+1)%*progressEvery == 0 {
				fmt.Fprintf(os.Stderr, "progress: enriched=%d/%d current=%q\n", i+1, len(rows), rows[i].pageTitle)
			}
		}
	}

	if err := writeCSV(*outPath, rows); err != nil {
		exitf("write csv: %v", err)
	}

	fmt.Fprintf(os.Stderr, "wrote %d rows to %s", len(rows), *outPath)
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, " (%d entries skipped)", skipped)
	}
	if filtered > 0 {
		fmt.Fprintf(os.Stderr, "; %d rows filtered by language whitelist", filtered)
	}
	if inspected > 0 {
		fmt.Fprintf(os.Stderr, "; inspected %d pages", inspected)
	}
	if *enrichPages {
		fmt.Fprintf(os.Stderr, "; enriched %d pages", enriched)
	}
	fmt.Fprintln(os.Stderr)
}

func fetchWikitext(client *http.Client, title string) (string, error) {
	page, err := fetchPage(client, title, false)
	if err != nil {
		return "", err
	}
	if page.Missing {
		return "", fmt.Errorf("page %q is missing", title)
	}
	if len(page.Revisions) == 0 {
		return "", fmt.Errorf("page %q has no revisions", title)
	}
	return page.Revisions[0].Slots.Main.Content, nil
}

func fetchPageMeta(client *http.Client, title string) (pageMeta, error) {
	page, err := fetchPage(client, title, true)
	if err != nil {
		return pageMeta{}, err
	}
	if page.Missing {
		return pageMeta{}, fmt.Errorf("page %q is missing", title)
	}

	var meta pageMeta
	if len(page.Revisions) > 0 {
		meta.SourceTerms = extractTemplateSourceTerms(page.Revisions[0].Slots.Main.Content)
	}
	for _, cat := range page.Categories {
		title := strings.TrimPrefix(cat.Title, "Category:")
		if strings.Contains(title, "Нужна этимология") {
			meta.NeedsEtymology = true
			continue
		}
		if m := originCategoryRe.FindStringSubmatch(title); m != nil {
			if lang := normalizeLanguage(m[1]); lang != "" {
				meta.OriginLanguages = appendUnique(meta.OriginLanguages, lang)
			}
			continue
		}
		if m := nameLanguageCategoryRe.FindStringSubmatch(title); m != nil {
			if lang := normalizeLanguage(m[1]); lang != "" {
				meta.OriginLanguages = appendUnique(meta.OriginLanguages, lang)
			}
		}
	}
	return meta, nil
}

func fetchPage(client *http.Client, title string, categories bool) (wikiPage, error) {
	values := url.Values{}
	values.Set("action", "query")
	values.Set("format", "json")
	values.Set("formatversion", "2")
	values.Set("titles", title)
	values.Set("prop", "revisions")
	values.Set("rvprop", "content")
	values.Set("rvslots", "main")
	if categories {
		values.Set("prop", "revisions|categories")
		values.Set("cllimit", "max")
	}

	req, err := newAPIRequest(values)
	if err != nil {
		return wikiPage{}, err
	}
	resp, err := doAPIRequest(client, req)
	if err != nil {
		return wikiPage{}, err
	}
	defer resp.Body.Close()

	var parsed wikiResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return wikiPage{}, err
	}
	if len(parsed.Query.Pages) == 0 {
		return wikiPage{}, errors.New("API response has no pages")
	}
	return parsed.Query.Pages[0], nil
}

func newAPIRequest(values url.Values) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, apiURL+"?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "rulat-wiktionary-loan-crawler/0.1 (https://ru.wiktionary.org/)")
	return req, nil
}

func doAPIRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	attempts := httpRetries + 1
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		resp, err := client.Do(req.Clone(req.Context()))
		retryAfter := ""
		if err == nil && resp.StatusCode == http.StatusOK {
			return resp, nil
		}
		if err != nil {
			if req.Context().Err() != nil {
				return nil, err
			}
			lastErr = fmt.Errorf("GET %s: %w", req.URL.Redacted(), err)
		} else {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			retryAfter = resp.Header.Get("Retry-After")
			statusErr := fmt.Errorf("GET %s: %s: %s", req.URL.Redacted(), resp.Status, strings.TrimSpace(string(body)))
			resp.Body.Close()
			if !isRetryableHTTPStatus(resp.StatusCode) {
				return nil, statusErr
			}
			lastErr = statusErr
		}

		if attempt+1 >= attempts {
			return nil, lastErr
		}
		delay := httpRetryBackoffDelay(attempt, retryAfter)
		logHTTPRetry(req, attempt+2, attempts, delay, lastErr)
		retrySleep(delay)
	}
	return nil, lastErr
}

func isRetryableHTTPStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func httpRetryBackoffDelay(failedAttempt int, retryAfter string) time.Duration {
	if delay, ok := parseRetryAfter(retryAfter); ok {
		return delay
	}
	if httpRetryDelay <= 0 {
		return 0
	}
	delay := httpRetryDelay
	for i := 0; i < failedAttempt; i++ {
		if delay >= maxHTTPRetryDelay/2 {
			return maxHTTPRetryDelay
		}
		delay *= 2
	}
	if delay > maxHTTPRetryDelay {
		return maxHTTPRetryDelay
	}
	return delay
}

func parseRetryAfter(value string) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := time.ParseDuration(value + "s"); err == nil && seconds >= 0 {
		return seconds, true
	}
	t, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	delay := time.Until(t)
	if delay < 0 {
		delay = 0
	}
	return delay, true
}

func logHTTPRetry(req *http.Request, nextAttempt, attempts int, delay time.Duration, err error) {
	if delay <= 0 {
		fmt.Fprintf(os.Stderr, "retry: %v; retrying GET %s immediately (attempt %d/%d)\n", err, req.URL.Redacted(), nextAttempt, attempts)
		return
	}
	fmt.Fprintf(os.Stderr, "retry: %v; waiting %s before GET %s attempt %d/%d\n", err, delay, req.URL.Redacted(), nextAttempt, attempts)
}

type crawlOptions struct {
	LanguageWhitelist   map[string]bool
	IncludePhrases      bool
	StripStemDiacritics bool
	TrimFinalStemVowels bool
	ProgressEvery       int
	Title               string
	From                string
	BatchSize           int
	PageLimit           int
	RowLimit            int
	Resolver            *templateResolver
}

func crawlWordPages(client *http.Client, opts crawlOptions) ([]csvRow, int, int, int, error) {
	if opts.BatchSize <= 0 || opts.BatchSize > 50 {
		opts.BatchSize = 50
	}

	var rows []csvRow
	seen := map[string]bool{}
	skipped := 0
	filtered := 0
	inspected := 0
	lastProgress := 0
	lastInspectedTitle := ""
	maybeLogProgress := func(title string) {
		if opts.ProgressEvery <= 0 || inspected-lastProgress < opts.ProgressEvery {
			return
		}
		logPageProgress(opts.ProgressEvery, inspected, len(rows), skipped, filtered, title)
		lastProgress = inspected
	}

	if strings.TrimSpace(opts.Title) != "" {
		page, err := fetchPage(client, strings.TrimSpace(opts.Title), true)
		if err != nil {
			return nil, skipped, filtered, inspected, err
		}
		inspected = 1
		row, ok, wasFiltered := rowFromWordPage(page, opts)
		if wasFiltered {
			filtered++
		} else if ok {
			rows = append(rows, row)
		} else {
			skipped++
		}
		return rows, skipped, filtered, inspected, nil
	}

	cont := map[string]string{}
	if strings.TrimSpace(opts.From) != "" {
		cont["gapfrom"] = strings.TrimSpace(opts.From)
	}

	for {
		pages, next, err := fetchAllPagesBatch(client, opts.BatchSize, cont)
		if err != nil {
			return nil, skipped, filtered, inspected, err
		}
		for _, page := range pages {
			if opts.PageLimit > 0 && inspected >= opts.PageLimit {
				maybeLogProgress(lastInspectedTitle)
				return rows, skipped, filtered, inspected, nil
			}
			inspected++
			lastInspectedTitle = page.Title
			row, ok, wasFiltered := rowFromWordPage(page, opts)
			if wasFiltered {
				filtered++
				maybeLogProgress(page.Title)
				continue
			}
			if !ok {
				skipped++
				maybeLogProgress(page.Title)
				continue
			}
			if seen[row.CyrillicStem] {
				skipped++
				maybeLogProgress(page.Title)
				continue
			}
			seen[row.CyrillicStem] = true
			rows = append(rows, row)
			maybeLogProgress(page.Title)
			if opts.RowLimit > 0 && len(rows) >= opts.RowLimit {
				return rows, skipped, filtered, inspected, nil
			}
		}
		if len(next) == 0 {
			break
		}
		cont = next
	}
	return rows, skipped, filtered, inspected, nil
}

func logPageProgress(every int, inspected, accepted, skipped, filtered int, title string) {
	if every <= 0 || inspected == 0 {
		return
	}
	if title != "" {
		fmt.Fprintf(os.Stderr, "progress: inspected=%d accepted=%d skipped=%d filtered=%d last=%q\n", inspected, accepted, skipped, filtered, title)
		return
	}
	fmt.Fprintf(os.Stderr, "progress: inspected=%d accepted=%d skipped=%d filtered=%d\n", inspected, accepted, skipped, filtered)
}

func fetchAllPagesBatch(client *http.Client, limit int, cont map[string]string) ([]wikiPage, map[string]string, error) {
	values := url.Values{}
	values.Set("action", "query")
	values.Set("format", "json")
	values.Set("formatversion", "2")
	values.Set("generator", "allpages")
	values.Set("gapnamespace", "0")
	values.Set("gapfilterredir", "nonredirects")
	values.Set("gaplimit", fmt.Sprintf("%d", limit))
	values.Set("prop", "revisions|categories")
	values.Set("rvprop", "content")
	values.Set("rvslots", "main")
	values.Set("cllimit", "max")
	for k, v := range cont {
		values.Set(k, v)
	}

	req, err := newAPIRequest(values)
	if err != nil {
		return nil, nil, err
	}
	resp, err := doAPIRequest(client, req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	var parsed wikiResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, nil, err
	}
	return parsed.Query.Pages, parsed.Continue, nil
}

func rowFromWordPage(page wikiPage, opts crawlOptions) (csvRow, bool, bool) {
	if page.Missing || len(page.Revisions) == 0 {
		return csvRow{}, false, false
	}
	cyr := cleanRussianTerm(page.Title)
	if cyr == "" || !hasRussianLetter(cyr) {
		return csvRow{}, false, false
	}
	ruSection, ok := extractRussianSection(page.Revisions[0].Slots.Main.Content)
	if !ok {
		return csvRow{}, false, false
	}

	candidates := extractPageLoanCandidates(ruSection, opts.Resolver)
	candidates = append(candidates, categoryCandidates(page.Categories)...)
	for _, candidate := range candidates {
		candidate.Source = canonicalLanguageName(candidate.Source)
		if candidate.Source == "" {
			continue
		}
		if !languageAllowed(candidate.Source, opts.LanguageWhitelist) {
			continue
		}
		mode := inferMode(cyr)
		latin, originalLatin, originalGreek, ok := stemFromCandidate(cyr, mode, candidate, opts.IncludePhrases, opts.TrimFinalStemVowels, opts.StripStemDiacritics)
		if !ok {
			continue
		}
		return csvRow{
			CyrillicStem:  cyr,
			LatinStem:     latin,
			OriginalLatin: originalLatin,
			OriginalGreek: originalGreek,
			Mode:          mode,
			CaseMode:      "auto",
			Source:        candidate.Source,
			Notes:         "wiktionary word-page etymology candidate; review before merging",
			SuffixContext: "",
			URL:           wordURL(page.Title),
			pageTitle:     page.Title,
		}, true, false
	}

	if len(candidates) > 0 {
		return csvRow{}, false, true
	}
	return csvRow{}, false, false
}

func extractRussianSection(text string) (string, bool) {
	loc := russianSectionRe.FindStringIndex(text)
	if loc == nil {
		return "", false
	}
	rest := text[loc[1]:]
	if next := topLevelSectionRe.FindStringIndex(rest); next != nil {
		return rest[:next[0]], true
	}
	return rest, true
}

func extractPageLoanCandidates(ruSection string, resolver *templateResolver) []loanCandidate {
	var candidates []loanCandidate
	for _, section := range extractEtymologySections(ruSection) {
		candidates = append(candidates, candidatesFromTemplates(section)...)
		candidates = append(candidates, candidatesFromTextMarkers(stripWikiMarkup(section))...)
		if resolver != nil {
			for _, name := range etymologyTemplateNames(section) {
				candidates = append(candidates, resolver.resolve(name)...)
			}
		}
	}
	return candidates
}

func extractEtymologySections(ruSection string) []string {
	var sections []string
	locs := etymologyHeadingRe.FindAllStringIndex(ruSection, -1)
	for _, loc := range locs {
		rest := ruSection[loc[1]:]
		if next := nextSubsectionRe.FindStringIndex(rest); next != nil {
			sections = append(sections, rest[:next[0]])
		} else {
			sections = append(sections, rest)
		}
	}
	return sections
}

func parseAppendix(text string, includePhrases bool, languageWhitelist map[string]bool, trimFinalStemVowels bool) ([]csvRow, int, int) {
	var rows []csvRow
	seen := map[string]bool{}
	source := ""
	skipped := 0
	filtered := 0

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if m := headingRe.FindStringSubmatch(line); m != nil {
			source = normalizeLanguage(stripWikiMarkup(m[1]))
			continue
		}
		if !strings.HasPrefix(line, "*") {
			continue
		}
		m := bulletRe.FindStringSubmatch(line)
		if m == nil {
			skipped++
			continue
		}
		if !languageAllowed(source, languageWhitelist) {
			filtered++
			continue
		}
		pageTitle := strings.TrimSpace(m[1])
		cyr := cleanRussianTerm(pageTitle)
		if cyr == "" || !hasRussianLetter(cyr) || seen[cyr] {
			skipped++
			continue
		}
		latin, ok := extractLatinStem(m[2], includePhrases)
		var originalGreek string
		if !ok && canonicalLanguageName(source) == "Greek" {
			originalGreek, latin, ok = extractGreekStem(m[2], includePhrases)
		}
		if !ok {
			skipped++
			continue
		}
		originalLatin := latin
		mode := inferMode(cyr)
		latin = makeLatinStem(cyr, latin, mode, trimFinalStemVowels)
		seen[cyr] = true
		rows = append(rows, csvRow{
			CyrillicStem:  cyr,
			LatinStem:     latin,
			OriginalLatin: originalLatin,
			OriginalGreek: originalGreek,
			Mode:          mode,
			CaseMode:      "auto",
			Source:        source,
			Notes:         "wiktionary appendix candidate; review before merging",
			SuffixContext: "",
			URL:           wordURL(pageTitle),
			pageTitle:     pageTitle,
		})
	}

	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Source != rows[j].Source {
			return rows[i].Source < rows[j].Source
		}
		return rows[i].CyrillicStem < rows[j].CyrillicStem
	})
	return rows, skipped, filtered
}

func extractLatinStem(raw string, includePhrases bool) (string, bool) {
	clean := stripWikiMarkup(raw)
	clean = trimLeadingSourceWords(clean)
	for _, match := range latinCandidateRe.FindAllString(clean, -1) {
		candidate := normalizeLatinCandidate(match)
		if candidate == "" {
			continue
		}
		if strings.Contains(candidate, " ") && !includePhrases {
			continue
		}
		if isLatinCandidate(candidate) {
			return candidate, true
		}
	}
	return "", false
}

func extractGreekStem(raw string, includePhrases bool) (string, string, bool) {
	clean := stripWikiMarkup(raw)
	clean = trimLeadingSourceWords(clean)
	for _, match := range greekCandidateRe.FindAllString(clean, -1) {
		greek := normalizeGreekCandidate(match)
		if greek == "" {
			continue
		}
		if strings.Contains(greek, " ") && !includePhrases {
			continue
		}
		if isGreekCandidate(greek) {
			latin, ok := transliterateGreek(stemGreekDeclension(greek))
			if ok && isLatinCandidate(latin) {
				return greek, latin, true
			}
		}
	}
	return "", "", false
}

func loanCandidateFromSourceTerm(source, raw string) (loanCandidate, bool) {
	term := stripWikiMarkup(raw)
	latin := normalizeLatinCandidate(term)
	if isLatinCandidate(latin) {
		return loanCandidate{Source: source, Latin: latin}, true
	}
	if canonicalLanguageName(source) == "Greek" {
		greek := normalizeGreekCandidate(term)
		if isGreekCandidate(greek) {
			return loanCandidate{Source: source, Greek: greek}, true
		}
	}
	return loanCandidate{}, false
}

func stemFromCandidate(cyr, mode string, candidate loanCandidate, includePhrases, trimFinalStemVowels, stripStemDiacritics bool) (string, string, string, bool) {
	var latin, originalLatin, originalGreek string
	if candidate.Latin != "" {
		latin = normalizeLatinCandidate(candidate.Latin)
		if strings.Contains(latin, " ") && !includePhrases {
			return "", "", "", false
		}
		if !isLatinCandidate(latin) {
			return "", "", "", false
		}
		originalLatin = latin
	} else if candidate.Greek != "" {
		originalGreek = normalizeGreekCandidate(candidate.Greek)
		if strings.Contains(originalGreek, " ") && !includePhrases {
			return "", "", "", false
		}
		if !isGreekCandidate(originalGreek) {
			return "", "", "", false
		}
		var ok bool
		latin, ok = transliterateGreek(stemGreekDeclension(originalGreek))
		if !ok || !isLatinCandidate(latin) {
			return "", "", "", false
		}
		originalLatin = latin
	} else {
		return "", "", "", false
	}

	latin = makeLatinStem(cyr, latin, mode, trimFinalStemVowels)
	if stripStemDiacritics {
		latin = stripLatinDiacritics(latin)
	}
	return latin, originalLatin, originalGreek, true
}

func candidatesFromTemplates(text string) []loanCandidate {
	var candidates []loanCandidate
	for _, call := range templateCalls(text) {
		if len(call) < 3 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(call[0]))
		switch name {
		case "lang":
			source := languageNameFromCode(call[1])
			if source == "" {
				continue
			}
			if candidate, ok := loanCandidateFromSourceTerm(source, call[2]); ok {
				candidates = append(candidates, candidate)
			}
		case "lang2":
			source := languageNameFromCode(call[1])
			if source == "" {
				continue
			}
			for _, arg := range call[2:] {
				if strings.Contains(arg, "=") {
					continue
				}
				if candidate, ok := loanCandidateFromSourceTerm(source, arg); ok {
					candidates = append(candidates, candidate)
					break
				}
			}
		}
	}
	return candidates
}

func candidatesFromTextMarkers(text string) []loanCandidate {
	var candidates []loanCandidate
	for _, marker := range textLanguageMarkers {
		re := regexp.MustCompile(`(?i)(?:^|[\s,;:({])` + marker.Pattern + `\s*([A-Za-zÀ-ÖØ-öø-ÿĀ-žḀ-ỹ][A-Za-zÀ-ÖØ-öø-ÿĀ-žḀ-ỹ\pM'’ʼʻ.-]*(?:\s+[A-Za-zÀ-ÖØ-öø-ÿĀ-žḀ-ỹ][A-Za-zÀ-ÖØ-öø-ÿĀ-žḀ-ỹ\pM'’ʼʻ.-]*){0,4})`)
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			latin := normalizeLatinCandidate(m[1])
			if isLatinCandidate(latin) {
				candidates = append(candidates, loanCandidate{Source: marker.Source, Latin: latin})
			}
		}
		if marker.Source == "Greek" {
			re := regexp.MustCompile(`(?i)(?:^|[\s,;:({])` + marker.Pattern + `\s*(` + greekCandidateRe.String() + `)`)
			for _, m := range re.FindAllStringSubmatch(text, -1) {
				greek := normalizeGreekCandidate(m[1])
				if isGreekCandidate(greek) {
					candidates = append(candidates, loanCandidate{Source: marker.Source, Greek: greek})
				}
			}
		}
	}
	return candidates
}

func etymologyTemplateNames(text string) []string {
	var names []string
	for _, call := range templateCalls(text) {
		if len(call) == 0 {
			continue
		}
		name := strings.TrimSpace(call[0])
		if strings.HasPrefix(strings.ToLower(name), "этимология:") {
			name = strings.TrimSpace(strings.TrimPrefix(name, "этимология:"))
			name = strings.TrimSpace(strings.TrimPrefix(name, "Этимология:"))
			if name != "" {
				names = appendUnique(names, name)
			}
		}
	}
	return names
}

func categoryCandidates(categories []wikiCategory) []loanCandidate {
	var candidates []loanCandidate
	for _, cat := range categories {
		title := strings.TrimPrefix(cat.Title, "Category:")
		title = strings.TrimPrefix(title, "Категория:")
		if m := originCategoryRe.FindStringSubmatch("Категория:" + title); m != nil {
			if lang := normalizeLanguage(m[1]); lang != "" {
				candidates = append(candidates, loanCandidate{Source: lang})
			}
			continue
		}
		if m := nameLanguageCategoryRe.FindStringSubmatch("Категория:" + title); m != nil {
			if lang := normalizeLanguage(m[1]); lang != "" {
				candidates = append(candidates, loanCandidate{Source: lang})
			}
		}
	}
	return candidates
}

func (r *templateResolver) resolve(name string) []loanCandidate {
	if r == nil {
		return nil
	}
	if candidates, ok := r.cache[name]; ok {
		return candidates
	}
	text, err := fetchWikitext(r.client, "Шаблон:этимология:"+name)
	if err != nil {
		r.cache[name] = nil
		return nil
	}
	candidates := candidatesFromTemplates(text)
	candidates = append(candidates, candidatesFromTextMarkers(stripWikiMarkup(text))...)
	r.cache[name] = candidates
	return candidates
}

func templateCalls(text string) [][]string {
	var calls [][]string
	for i := 0; i+1 < len(text); i++ {
		if text[i] != '{' || text[i+1] != '{' {
			continue
		}
		start := i + 2
		depth := 1
		j := start
		for j+1 < len(text) {
			switch {
			case text[j] == '{' && text[j+1] == '{':
				depth++
				j += 2
			case text[j] == '}' && text[j+1] == '}':
				depth--
				if depth == 0 {
					calls = append(calls, splitTemplateArgs(text[start:j]))
					i = j + 1
					goto next
				}
				j += 2
			default:
				j++
			}
		}
	next:
	}
	return calls
}

func splitTemplateArgs(inner string) []string {
	var parts []string
	start := 0
	depth := 0
	for i := 0; i < len(inner); i++ {
		if i+1 < len(inner) && inner[i] == '{' && inner[i+1] == '{' {
			depth++
			i++
			continue
		}
		if i+1 < len(inner) && inner[i] == '}' && inner[i+1] == '}' {
			if depth > 0 {
				depth--
			}
			i++
			continue
		}
		if inner[i] == '|' && depth == 0 {
			parts = append(parts, strings.TrimSpace(inner[start:i]))
			start = i + 1
		}
	}
	parts = append(parts, strings.TrimSpace(inner[start:]))
	return parts
}

func languageNameFromCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	code = strings.TrimPrefix(code, "lang=")
	return languageCodeNames[code]
}

func extractTemplateSourceTerms(text string) []string {
	terms := []string{}
	etymology := etymologySection(text)
	for _, m := range etymologyTemplateRe.FindAllStringSubmatch(etymology, -1) {
		term := normalizeLatinCandidate(stripWikiMarkup(m[1]))
		if isLatinCandidate(term) {
			terms = appendUnique(terms, term)
		}
	}
	for _, m := range langTemplateRe.FindAllStringSubmatch(etymology, -1) {
		term := normalizeLatinCandidate(stripWikiMarkup(m[2]))
		if isLatinCandidate(term) {
			terms = appendUnique(terms, term)
		}
	}
	return terms
}

func applyPageMeta(row *csvRow, meta pageMeta, languageWhitelist map[string]bool, trimFinalStemVowels, stripStemDiacritics bool) {
	if len(meta.SourceTerms) > 0 && len([]rune(meta.SourceTerms[0])) > len([]rune(row.LatinStem)) {
		row.OriginalLatin = meta.SourceTerms[0]
		row.LatinStem = makeLatinStem(row.CyrillicStem, row.OriginalLatin, row.Mode, trimFinalStemVowels)
		if stripStemDiacritics {
			row.LatinStem = stripLatinDiacritics(row.LatinStem)
		}
		row.Notes = appendNote(row.Notes, "latin stem from word-page etymology marker")
	}
	if len(meta.OriginLanguages) > 0 {
		allowed := filterLanguages(meta.OriginLanguages, languageWhitelist)
		if len(allowed) > 0 {
			row.Source = strings.Join(allowed, "-")
			row.Notes = appendNote(row.Notes, "source from word-page origin category")
		}
	}
	if meta.NeedsEtymology {
		row.Notes = appendNote(row.Notes, "word page marks etymology as incomplete")
	}
}

func etymologySection(text string) string {
	const title = "=== Этимология ==="
	i := strings.Index(text, title)
	if i < 0 {
		return text
	}
	rest := text[i+len(title):]
	if j := regexp.MustCompile(`(?m)^===\s+`).FindStringIndex(rest); j != nil {
		return rest[:j[0]]
	}
	return rest
}

func writeCSV(path string, rows []csvRow) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{"cyrillic_stem", "latin_stem", "original_latin", "original_greek", "mode", "case_mode", "source", "notes", "suffix_context", "url"}); err != nil {
		return err
	}
	for _, row := range rows {
		if err := w.Write([]string{
			row.CyrillicStem,
			row.LatinStem,
			row.OriginalLatin,
			row.OriginalGreek,
			row.Mode,
			row.CaseMode,
			row.Source,
			row.Notes,
			row.SuffixContext,
			row.URL,
		}); err != nil {
			return err
		}
	}
	return w.Error()
}

func normalizeGeneratedRows(rows []csvRow, stripDiacritics bool) {
	if !stripDiacritics {
		return
	}
	for i := range rows {
		rows[i].LatinStem = stripLatinDiacritics(rows[i].LatinStem)
	}
}

func stripWikiMarkup(s string) string {
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = htmlCommentRe.ReplaceAllString(s, " ")
	s = refRe.ReplaceAllString(s, " ")
	s = langTemplateRe.ReplaceAllString(s, "$2")
	s = etymologyTemplateRe.ReplaceAllString(s, "$1")
	for {
		next := wikiLinkRe.ReplaceAllStringFunc(s, func(match string) string {
			parts := wikiLinkRe.FindStringSubmatch(match)
			if len(parts) >= 3 && parts[2] != "" {
				return parts[2]
			}
			if len(parts) >= 2 {
				return parts[1]
			}
			return match
		})
		if next == s {
			break
		}
		s = next
	}
	s = strings.ReplaceAll(s, "'''", "")
	s = strings.ReplaceAll(s, "''", "")
	for {
		next := simpleTemplateRe.ReplaceAllString(s, " ")
		if next == s {
			break
		}
		s = next
	}
	return compactSpaces(s)
}

func trimLeadingSourceWords(s string) string {
	s = strings.TrimSpace(s)
	prefixes := []string{
		"от ",
		"из ",
		"сокр. от ",
		"сокр. ",
		"искаж. ",
		"собств. ",
		"собственное ",
	}
	lower := strings.ToLower(s)
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(s[len(prefix):])
		}
	}
	return s
}

func stripLatinDiacritics(s string) string {
	var out strings.Builder
	for _, r := range s {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if repl, ok := latinDiacriticReplacements[r]; ok {
			out.WriteString(repl)
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

var latinDiacriticReplacements = map[rune]string{
	'À': "A", 'Á': "A", 'Â': "A", 'Ã': "A", 'Ä': "A", 'Å': "A", 'Ā': "A", 'Ă': "A", 'Ą': "A", 'Ǎ': "A", 'Ǻ': "A", 'Ḁ': "A",
	'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'ä': "a", 'å': "a", 'ā': "a", 'ă': "a", 'ą': "a", 'ǎ': "a", 'ǻ': "a", 'ḁ': "a",
	'Æ': "AE", 'Ǽ': "AE", 'Ǣ': "AE",
	'æ': "ae", 'ǽ': "ae", 'ǣ': "ae",
	'Ḃ': "B", 'Ḅ': "B", 'Ḇ': "B",
	'ḃ': "b", 'ḅ': "b", 'ḇ': "b",
	'Ç': "C", 'Ć': "C", 'Ĉ': "C", 'Ċ': "C", 'Č': "C", 'Ḉ': "C",
	'ç': "c", 'ć': "c", 'ĉ': "c", 'ċ': "c", 'č': "c", 'ḉ': "c",
	'Ð': "D", 'Ď': "D", 'Đ': "D", 'Ḋ': "D", 'Ḍ': "D", 'Ḏ': "D", 'Ḑ': "D", 'Ḓ': "D",
	'ð': "d", 'ď': "d", 'đ': "d", 'ḋ': "d", 'ḍ': "d", 'ḏ': "d", 'ḑ': "d", 'ḓ': "d",
	'È': "E", 'É': "E", 'Ê': "E", 'Ë': "E", 'Ē': "E", 'Ĕ': "E", 'Ė': "E", 'Ę': "E", 'Ě': "E", 'Ḕ': "E", 'Ḗ': "E", 'Ḙ': "E", 'Ḛ': "E", 'Ḝ': "E",
	'è': "e", 'é': "e", 'ê': "e", 'ë': "e", 'ē': "e", 'ĕ': "e", 'ė': "e", 'ę': "e", 'ě': "e", 'ḕ': "e", 'ḗ': "e", 'ḙ': "e", 'ḛ': "e", 'ḝ': "e",
	'Ḟ': "F",
	'ḟ': "f",
	'Ĝ': "G", 'Ğ': "G", 'Ġ': "G", 'Ģ': "G",
	'ĝ': "g", 'ğ': "g", 'ġ': "g", 'ģ': "g", 'ḡ': "g",
	'Ĥ': "H", 'Ħ': "H", 'Ḣ': "H", 'Ḥ': "H", 'Ḧ': "H", 'Ḩ': "H", 'Ḫ': "H",
	'ĥ': "h", 'ħ': "h", 'ḣ': "h", 'ḥ': "h", 'ḧ': "h", 'ḩ': "h", 'ḫ': "h",
	'Ì': "I", 'Í': "I", 'Î': "I", 'Ï': "I", 'Ĩ': "I", 'Ī': "I", 'Ĭ': "I", 'Į': "I", 'İ': "I", 'Ǐ': "I", 'Ḭ': "I", 'Ḯ': "I",
	'ì': "i", 'í': "i", 'î': "i", 'ï': "i", 'ĩ': "i", 'ī': "i", 'ĭ': "i", 'į': "i", 'ı': "i", 'ǐ': "i", 'ḭ': "i", 'ḯ': "i",
	'Ĵ': "J",
	'ĵ': "j",
	'Ķ': "K", 'Ḱ': "K", 'Ḳ': "K", 'Ḵ': "K",
	'ķ': "k", 'ĸ': "k", 'ḱ': "k", 'ḳ': "k", 'ḵ': "k",
	'Ĺ': "L", 'Ļ': "L", 'Ľ': "L", 'Ŀ': "L", 'Ł': "L", 'Ḷ': "L", 'Ḹ': "L", 'Ḻ': "L", 'Ḽ': "L",
	'ĺ': "l", 'ļ': "l", 'ľ': "l", 'ŀ': "l", 'ł': "l", 'ḷ': "l", 'ḹ': "l", 'ḻ': "l", 'ḽ': "l",
	'Ḿ': "M", 'Ṁ': "M", 'Ṃ': "M",
	'ḿ': "m", 'ṁ': "m", 'ṃ': "m",
	'Ñ': "N", 'Ń': "N", 'Ņ': "N", 'Ň': "N", 'Ṅ': "N", 'Ṇ': "N", 'Ṉ': "N", 'Ṋ': "N",
	'ñ': "n", 'ń': "n", 'ņ': "n", 'ň': "n", 'ŉ': "n", 'ṅ': "n", 'ṇ': "n", 'ṉ': "n", 'ṋ': "n",
	'Ò': "O", 'Ó': "O", 'Ô': "O", 'Õ': "O", 'Ö': "O", 'Ø': "O", 'Ō': "O", 'Ŏ': "O", 'Ő': "O", 'Ơ': "O", 'Ǒ': "O", 'Ǿ': "O", 'Ṍ': "O", 'Ṏ': "O", 'Ṑ': "O", 'Ṓ': "O",
	'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o", 'ö': "o", 'ø': "o", 'ō': "o", 'ŏ': "o", 'ő': "o", 'ơ': "o", 'ǒ': "o", 'ǿ': "o", 'ṍ': "o", 'ṏ': "o", 'ṑ': "o", 'ṓ': "o",
	'Œ': "OE",
	'œ': "oe",
	'Ṕ': "P", 'Ṗ': "P",
	'ṕ': "p", 'ṗ': "p",
	'Ŕ': "R", 'Ŗ': "R", 'Ř': "R", 'Ṙ': "R", 'Ṛ': "R", 'Ṝ': "R", 'Ṟ': "R",
	'ŕ': "r", 'ŗ': "r", 'ř': "r", 'ṙ': "r", 'ṛ': "r", 'ṝ': "r", 'ṟ': "r",
	'Ś': "S", 'Ŝ': "S", 'Ş': "S", 'Š': "S", 'Ṡ': "S", 'Ṣ': "S", 'Ṥ': "S", 'Ṧ': "S", 'Ṩ': "S",
	'ś': "s", 'ŝ': "s", 'ş': "s", 'š': "s", 'ſ': "s", 'ṡ': "s", 'ṣ': "s", 'ṥ': "s", 'ṧ': "s", 'ṩ': "s",
	'ẞ': "SS",
	'ß': "ss",
	'Ţ': "T", 'Ť': "T", 'Ŧ': "T", 'Ṫ': "T", 'Ṭ': "T", 'Ṯ': "T", 'Ṱ': "T",
	'ţ': "t", 'ť': "t", 'ŧ': "t", 'ṫ': "t", 'ṭ': "t", 'ṯ': "t", 'ṱ': "t",
	'Þ': "Th",
	'þ': "th",
	'Ù': "U", 'Ú': "U", 'Û': "U", 'Ü': "U", 'Ũ': "U", 'Ū': "U", 'Ŭ': "U", 'Ů': "U", 'Ű': "U", 'Ų': "U", 'Ư': "U", 'Ǔ': "U", 'Ṳ': "U", 'Ṵ': "U", 'Ṷ': "U", 'Ṹ': "U", 'Ṻ': "U",
	'ù': "u", 'ú': "u", 'û': "u", 'ü': "u", 'ũ': "u", 'ū': "u", 'ŭ': "u", 'ů': "u", 'ű': "u", 'ų': "u", 'ư': "u", 'ǔ': "u", 'ṳ': "u", 'ṵ': "u", 'ṷ': "u", 'ṹ': "u", 'ṻ': "u",
	'Ŵ': "W", 'Ẁ': "W", 'Ẃ': "W", 'Ẅ': "W", 'Ẇ': "W", 'Ẉ': "W",
	'ŵ': "w", 'ẁ': "w", 'ẃ': "w", 'ẅ': "w", 'ẇ': "w", 'ẉ': "w",
	'Ẋ': "X", 'Ẍ': "X",
	'ẋ': "x", 'ẍ': "x",
	'Ý': "Y", 'Ŷ': "Y", 'Ÿ': "Y", 'Ẏ': "Y",
	'ý': "y", 'ŷ': "y", 'ÿ': "y", 'ẏ': "y",
	'Ź': "Z", 'Ż': "Z", 'Ž': "Z", 'Ẑ': "Z", 'Ẓ': "Z", 'Ẕ': "Z",
	'ź': "z", 'ż': "z", 'ž': "z", 'ẑ': "z", 'ẓ': "z", 'ẕ': "z",
}

func normalizeLatinCandidate(s string) string {
	s = compactSpaces(s)
	s = strings.ReplaceAll(s, "’", "'")
	s = strings.ReplaceAll(s, "ʼ", "'")
	s = strings.ReplaceAll(s, "ʻ", "'")
	s = strings.Trim(s, " \t\r\n.,;:()[]{}<>\"“”«»")
	fields := strings.Fields(s)
	for i, field := range fields {
		fields[i] = strings.Trim(field, " \t\r\n.,;:()[]{}<>\"“”«»")
	}
	s = strings.Join(fields, " ")
	if strings.HasSuffix(s, ".") {
		s = strings.TrimSuffix(s, ".")
	}
	return s
}

func isLatinCandidate(s string) bool {
	if s == "" {
		return false
	}
	hasLatin := false
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Latin, r):
			hasLatin = true
		case unicode.Is(unicode.Cyrillic, r), unicode.Is(unicode.Greek, r):
			return false
		case unicode.IsSpace(r), r == '-', r == '\'', r == '.', unicode.Is(unicode.Mn, r):
			continue
		default:
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return false
			}
		}
	}
	if !hasLatin {
		return false
	}
	lower := strings.ToLower(s)
	return lower != "nbsp" && lower != "ref"
}

func cleanRussianTerm(s string) string {
	s = strings.TrimSpace(stripWikiMarkup(s))
	s = strings.ReplaceAll(s, "\u0301", "")
	s = strings.Trim(s, " \t\r\n.,;:()[]{}<>\"“”«»")
	return strings.ToLower(s)
}

func hasRussianLetter(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Cyrillic, r) {
			return true
		}
	}
	return false
}

func inferMode(cyr string) string {
	if strings.Contains(cyr, "-") {
		return "word"
	}
	runes := []rune(cyr)
	if len(runes) == 0 {
		return "stem"
	}
	switch runes[len(runes)-1] {
	case 'о', 'е', 'э', 'и', 'у', 'ю':
		return "word"
	default:
		return "stem"
	}
}

func makeLatinStem(cyr, latin, mode string, trimFinalStemVowels bool) string {
	if !trimFinalStemVowels || mode != "stem" || !endsWithRussianConsonant(cyr) {
		return latin
	}
	runes := []rune(latin)
	for len(runes) > 1 && isLatinVowel(runes[len(runes)-1]) {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}

func endsWithRussianConsonant(s string) bool {
	runes := []rune(strings.ToLower(s))
	for i := len(runes) - 1; i >= 0; i-- {
		r := runes[i]
		if unicode.IsSpace(r) || r == '-' || r == '\'' {
			continue
		}
		if !unicode.Is(unicode.Cyrillic, r) {
			return false
		}
		return !isRussianVowel(r) && r != 'ь' && r != 'ъ' && r != 'й'
	}
	return false
}

func isRussianVowel(r rune) bool {
	switch r {
	case 'а', 'е', 'ё', 'и', 'о', 'у', 'ы', 'э', 'ю', 'я':
		return true
	default:
		return false
	}
}

func isLatinVowel(r rune) bool {
	r = unicode.ToLower(r)
	switch r {
	case 'a', 'e', 'i', 'o', 'u', 'y',
		'á', 'à', 'â', 'ä', 'ã', 'å', 'ā', 'ă', 'ą',
		'é', 'è', 'ê', 'ë', 'ē', 'ĕ', 'ė', 'ę', 'ě',
		'í', 'ì', 'î', 'ï', 'ī', 'ĭ', 'į',
		'ó', 'ò', 'ô', 'ö', 'õ', 'ø', 'ō', 'ŏ', 'ő',
		'ú', 'ù', 'û', 'ü', 'ū', 'ŭ', 'ů', 'ű', 'ų',
		'ý', 'ỳ', 'ŷ', 'ÿ':
		return true
	default:
		return false
	}
}

func parseLanguageWhitelist(value string) (map[string]bool, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	allowed := map[string]bool{}
	for _, part := range strings.Split(value, ",") {
		lang := canonicalLanguageName(part)
		if lang == "" {
			continue
		}
		if !knownLanguage(lang) {
			return nil, fmt.Errorf("unknown language %q", strings.TrimSpace(part))
		}
		allowed[lang] = true
	}
	if len(allowed) == 0 {
		return nil, nil
	}
	return allowed, nil
}

func languageAllowed(lang string, whitelist map[string]bool) bool {
	if len(whitelist) == 0 {
		return true
	}
	return whitelist[canonicalLanguageName(lang)]
}

func filterLanguages(langs []string, whitelist map[string]bool) []string {
	filtered := []string{}
	for _, lang := range langs {
		lang = canonicalLanguageName(lang)
		if lang != "" && languageAllowed(lang, whitelist) {
			filtered = appendUnique(filtered, lang)
		}
	}
	return filtered
}

func canonicalLanguageName(value string) string {
	key := strings.ToLower(compactSpaces(value))
	key = strings.Trim(key, " .,:;()[]{}")
	key = strings.TrimSuffix(key, " language")
	if canonical, ok := languageNameAliases[key]; ok {
		return canonical
	}
	for _, canonical := range languageNameAliases {
		if strings.EqualFold(value, canonical) {
			return canonical
		}
	}
	return strings.TrimSpace(value)
}

func knownLanguage(lang string) bool {
	for _, canonical := range languageNameAliases {
		if lang == canonical {
			return true
		}
	}
	return false
}

func normalizeLanguage(s string) string {
	s = strings.ToLower(compactSpaces(s))
	s = strings.Trim(s, " .,:;()[]{}")
	s = strings.TrimPrefix(s, "из ")
	s = strings.ReplaceAll(s, "языка", "")
	s = strings.ReplaceAll(s, "язык", "")
	s = compactSpaces(s)

	checks := []struct {
		Needle string
		Name   string
	}{
		{"англий", "English"},
		{"француз", "French"},
		{"немец", "German"},
		{"герман", "German"},
		{"нидерланд", "Dutch"},
		{"голланд", "Dutch"},
		{"итальян", "Italian"},
		{"латин", "Latin"},
		{"древнегреч", "Greek"},
		{"гречес", "Greek"},
		{"еврей", "Hebrew"},
		{"иврит", "Hebrew"},
		{"араб", "Arabic"},
		{"испан", "Spanish"},
		{"португал", "Portuguese"},
		{"турец", "Turkish"},
		{"тюрк", "Turkic"},
		{"персид", "Persian"},
		{"китай", "Chinese"},
		{"япон", "Japanese"},
		{"корей", "Korean"},
		{"польск", "Polish"},
		{"финск", "Finnish"},
		{"швед", "Swedish"},
		{"датск", "Danish"},
		{"норвеж", "Norwegian"},
		{"армян", "Armenian"},
		{"санскрит", "Sanskrit"},
		{"абхаз", "Abkhaz"},
		{"албан", "Albanian"},
		{"венгер", "Hungarian"},
		{"малай", "Malay"},
		{"австрали", "Aboriginal Australian"},
	}
	for _, check := range checks {
		if strings.Contains(s, check.Needle) {
			return check.Name
		}
	}
	return strings.TrimSpace(s)
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendNote(notes, note string) string {
	if notes == "" {
		return note
	}
	if strings.Contains(notes, note) {
		return notes
	}
	return notes + "; " + note
}

func compactSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func wordURL(title string) string {
	return wiktionaryBaseURL + strings.ReplaceAll(url.PathEscape(title), "%20", "_")
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
