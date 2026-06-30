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
	"sync"
	"time"
	"unicode"
)

const (
	apiURL                              = "https://ru.wiktionary.org/w/api.php"
	defaultAppendixTitle                = "Приложение:Заимствованные слова в русском языке"
	defaultLanguageWhitelist            = "English,German,French,Italian,Greek,Latin,Dutch,Hebrew,Swedish,Danish,Spanish"
	defaultTranslationLanguageWhitelist = defaultLanguageWhitelist
	defaultHTTPRetries                  = 5
	defaultHTTPRetryDelay               = 5 * time.Second
	defaultMaxlag                       = 5
	defaultRequestDelay                 = 350 * time.Millisecond
	maxHTTPRetryDelay                   = 30 * time.Second
	defaultUserAgent                    = "rulat-wiktionary-loan-crawler/0.1 (https://github.com/pjalybin/rulat)"
	wiktionaryBaseURL                   = "https://ru.wiktionary.org/wiki/"
)

type csvRow struct {
	CyrillicStem          string
	LatinStem             string
	MatchedRussianReading string
	OriginalLatin         string
	OriginalGreek         string
	Mode                  string
	CaseMode              string
	MatchCase             string
	Source                string
	Notes                 string
	SuffixContext         string
	URL                   string
	pageTitle             string
}

type wikiResponse struct {
	Continue map[string]string `json:"continue"`
	Error    *wikiAPIError     `json:"error"`
	Query    struct {
		Pages []wikiPage `json:"pages"`
	} `json:"query"`
}

type wikiAPIError struct {
	Code string  `json:"code"`
	Info string  `json:"info"`
	Lag  float64 `json:"lag"`
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
	Source     string
	Latin      string
	Greek      string
	SourceKind string
}

type templateResolver struct {
	client *http.Client
	cache  map[string][]loanCandidate
}

var (
	apiMaxlag        = defaultMaxlag
	apiRequestDelay  = defaultRequestDelay
	apiUserAgent     = defaultUserAgent
	httpRetries      = defaultHTTPRetries
	httpRetryDelay   = defaultHTTPRetryDelay
	lastAPIRequestAt time.Time
	apiRequestMu     sync.Mutex
	retrySleep       = time.Sleep
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
	meaningLanguageMarkers = []struct {
		Pattern string
		Source  string
	}{
		{`англ(?:\.|ийск)|англи|британ|великобрит|соедин[её]нн(?:ые|ых)\s+штат|сша|американск|канада|канадск`, "English"},
		{`нем(?:\.|ецк)|герман(?:и|ск)|австри(?:я|йск)|швейцар(?:и|ск)|прусс(?:и|к)|бавар(?:и|ск)`, "German"},
		{`франц(?:\.|узск)|франци|бельги(?:я|йск)|квебек|франкоязыч`, "French"},
		{`итал(?:\.|ьянск)|итали|сицили(?:я|йск)|сардин(?:и|ск)`, "Italian"},
		{`др\.-греч\.|древнегреч|греч(?:\.|еск)|греци|эллад|византи(?:я|йск)`, "Greek"},
		{`лат(?:\.|инск)|римск|римлян|древнеримск|ватикан`, "Latin"},
		{`нидерл(?:\.|андск)|голл(?:\.|андск)|нидерланд|голланд|фламандск|фландри`, "Dutch"},
		{`ивр(?:\.|ит)|евр(?:\.|ейск)|древнеевр|израил|иудейск`, "Hebrew"},
		{`швед(?:\.|ск)|швеци`, "Swedish"},
		{`датск|дани(?:я|и|ю|ей|йск)`, "Danish"},
		{`исп(?:\.|анск)|испани|латиноамериканск|мексик|аргентин|перуан|чилийск|колумби(?:я|йск)|кубинск`, "Spanish"},
		{`япон(?:\.|ск)|япони`, "Japanese"},
	}
)

func main() {
	crawlSource := flag.String("source", "pages", "crawl source: pages or appendix")
	sourceTitle := flag.String("source-page", defaultAppendixTitle, "Russian Wiktionary appendix page to crawl")
	outPath := flag.String("out", "", "CSV output path; empty generates a name from content-affecting flags")
	limit := flag.Int("limit", 0, "maximum rows to write; 0 means all parsed rows")
	pageLimit := flag.Int("page-limit", 0, "maximum main-namespace pages to inspect in -source pages mode; 0 means unlimited")
	exactTitle := flag.String("title", "", "exact main-namespace page title to inspect in -source pages mode")
	allPagesFrom := flag.String("from", "", "main-namespace page title to start from in -source pages mode")
	batchSize := flag.Int("batch-size", 50, "MediaWiki allpages batch size in -source pages mode")
	progressEvery := flag.Int("progress-every", 0, "log page-mode progress every N inspected pages; 0 disables progress logs")
	languages := flag.String("languages", defaultLanguageWhitelist, "comma-separated source-language whitelist; empty means all languages")
	translationLanguages := flag.String("translation-languages", defaultTranslationLanguageWhitelist, "comma-separated source-language whitelist for translation-section fallback; defaults to -languages default; empty means all translation languages")
	enrichPages := flag.Bool("enrich-pages", false, "fetch each word page and parse etymology/category markers")
	resolveTemplates := flag.Bool("resolve-etymology-templates", true, "resolve {{этимология:...}} templates in -source pages mode")
	includePhrases := flag.Bool("include-phrases", false, "keep multi-word Latin source forms")
	stripStemDiacritics := flag.Bool("strip-stem-diacritics", true, "remove Latin diacritics from generated latin_stem values")
	trimFinalStemVowels := flag.Bool("trim-final-stem-vowels", true, "drop final Latin vowels when generated stems attach to Russian consonant stems")
	delay := flag.Duration("delay", 100*time.Millisecond, "delay between page requests when -enrich-pages is set")
	retryAttempts := flag.Int("http-retries", defaultHTTPRetries, "retry attempts for transient HTTP failures; 0 disables retries")
	retryBaseDelay := flag.Duration("http-retry-delay", defaultHTTPRetryDelay, "initial delay for transient HTTP retries; doubles up to 30s")
	maxlag := flag.Int("maxlag", defaultMaxlag, "MediaWiki maxlag value in seconds; use -1 to disable")
	requestDelay := flag.Duration("request-delay", defaultRequestDelay, "minimum delay between Wikimedia API requests")
	userAgent := flag.String("user-agent", defaultUserAgent, "HTTP User-Agent sent to Wikimedia APIs; include a URL or email contact")
	cacheFlags := registerAPICacheFlags()
	flag.Parse()

	if *retryAttempts < 0 {
		exitf("-http-retries must be >= 0")
	}
	if *retryBaseDelay < 0 {
		exitf("-http-retry-delay must be >= 0")
	}
	if *maxlag < -1 {
		exitf("-maxlag must be >= -1")
	}
	if *requestDelay < 0 {
		exitf("-request-delay must be >= 0")
	}
	if strings.TrimSpace(*userAgent) == "" {
		exitf("-user-agent must not be empty")
	}
	httpRetries = *retryAttempts
	httpRetryDelay = *retryBaseDelay
	apiMaxlag = *maxlag
	apiRequestDelay = *requestDelay
	apiUserAgent = strings.TrimSpace(*userAgent)
	if err := configureAPICache(cacheFlags); err != nil {
		exitf("%v", err)
	}

	languageWhitelist, err := parseLanguageWhitelist(*languages)
	if err != nil {
		exitf("parse language whitelist: %v", err)
	}
	translationLanguageWhitelist, err := parseLanguageWhitelist(*translationLanguages)
	if err != nil {
		exitf("parse translation language whitelist: %v", err)
	}
	outputPath := strings.TrimSpace(*outPath)
	if outputPath == "" {
		outputPath = generatedOutputPath(outputNameOptions{
			Source:                       *crawlSource,
			SourceTitle:                  *sourceTitle,
			Limit:                        *limit,
			PageLimit:                    *pageLimit,
			Title:                        *exactTitle,
			From:                         *allPagesFrom,
			LanguageWhitelist:            languageWhitelist,
			TranslationLanguageWhitelist: translationLanguageWhitelist,
			EnrichPages:                  *enrichPages,
			ResolveTemplates:             *resolveTemplates,
			IncludePhrases:               *includePhrases,
			StripStemDiacritics:          *stripStemDiacritics,
			TrimFinalStemVowels:          *trimFinalStemVowels,
		})
	}
	fmt.Fprintf(os.Stderr, "output file: %s\n", outputPath)

	client := &http.Client{Timeout: 30 * time.Second}
	rowWriter, err := newCSVRowWriter(outputPath)
	if err != nil {
		exitf("open csv: %v", err)
	}
	rowWriterClosed := false
	closeRowWriter := func() {
		if rowWriterClosed {
			return
		}
		if err := rowWriter.Close(); err != nil {
			exitf("close csv: %v", err)
		}
		rowWriterClosed = true
	}
	defer closeRowWriter()

	var rows []csvRow
	var skipped, filtered, inspected int
	enriched := 0
	accepted := 0
	processAcceptedRow := func(row *csvRow) error {
		accepted++
		if *enrichPages {
			meta, err := fetchPageMeta(client, row.pageTitle)
			if err != nil {
				row.Notes = appendNote(row.Notes, "page enrichment failed")
			} else {
				applyPageMeta(row, meta, languageWhitelist, *trimFinalStemVowels, *stripStemDiacritics)
				enriched++
			}
			if *delay > 0 {
				time.Sleep(*delay)
			}
			if *progressEvery > 0 && accepted%*progressEvery == 0 {
				fmt.Fprintf(os.Stderr, "progress: enriched=%d accepted=%d current=%q\n", enriched, accepted, row.pageTitle)
			}
		}
		return rowWriter.Write(*row)
	}

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
		for i := range rows {
			if err := processAcceptedRow(&rows[i]); err != nil {
				exitf("write csv: %v", err)
			}
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
			LanguageWhitelist:            languageWhitelist,
			TranslationLanguageWhitelist: translationLanguageWhitelist,
			IncludePhrases:               *includePhrases,
			StripStemDiacritics:          *stripStemDiacritics,
			TrimFinalStemVowels:          *trimFinalStemVowels,
			ProgressEvery:                *progressEvery,
			Title:                        *exactTitle,
			From:                         *allPagesFrom,
			BatchSize:                    *batchSize,
			PageLimit:                    *pageLimit,
			RowLimit:                     *limit,
			Resolver:                     resolver,
			OnRow:                        processAcceptedRow,
		})
		if err != nil {
			exitf("crawl pages: %v", err)
		}
	default:
		exitf("unknown -source %q; use pages or appendix", *crawlSource)
	}

	closeRowWriter()
	fmt.Fprintf(os.Stderr, "wrote %d rows to %s", len(rows), outputPath)
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, " (%d entries skipped)", skipped)
	}
	if filtered > 0 {
		fmt.Fprintf(os.Stderr, "; %d rows filtered by language whitelist", filtered)
	}
	if inspected > 0 {
		fmt.Fprintf(os.Stderr, "; inspected %d pages", inspected)
	}
	fmt.Fprintf(os.Stderr, "; downloaded %d Wiktionary API responses (cache misses)", apiCacheMissCount())
	if *enrichPages {
		fmt.Fprintf(os.Stderr, "; enriched %d pages", enriched)
	}
	fmt.Fprintln(os.Stderr)
}

type outputNameOptions struct {
	Source                       string
	SourceTitle                  string
	Limit                        int
	PageLimit                    int
	Title                        string
	From                         string
	LanguageWhitelist            map[string]bool
	TranslationLanguageWhitelist map[string]bool
	EnrichPages                  bool
	ResolveTemplates             bool
	IncludePhrases               bool
	StripStemDiacritics          bool
	TrimFinalStemVowels          bool
}

func generatedOutputPath(opts outputNameOptions) string {
	source := strings.ToLower(strings.TrimSpace(opts.Source))
	if source == "" {
		source = "pages"
	}
	parts := []string{"loan_stems", "wiktionary", safeFileToken(source)}

	if source == "appendix" {
		title := strings.TrimSpace(opts.SourceTitle)
		if title != "" && title != defaultAppendixTitle {
			parts = append(parts, "source-"+safeFileToken(title))
		}
	} else {
		if title := strings.TrimSpace(opts.Title); title != "" {
			parts = append(parts, "title-"+safeFileToken(title))
		} else if from := strings.TrimSpace(opts.From); from != "" {
			parts = append(parts, "from-"+safeFileToken(from))
		}
		if opts.PageLimit > 0 {
			parts = append(parts, fmt.Sprintf("page-limit-%d", opts.PageLimit))
		}
		if !opts.ResolveTemplates {
			parts = append(parts, "no-template-resolve")
		}
	}

	if opts.Limit > 0 && strings.TrimSpace(opts.Title) == "" {
		parts = append(parts, fmt.Sprintf("limit-%d", opts.Limit))
	}
	if langToken := outputLanguageToken(opts.LanguageWhitelist); langToken != "" {
		parts = append(parts, "langs-"+safeFileToken(langToken))
	}
	if langToken := outputTranslationLanguageToken(opts.TranslationLanguageWhitelist); langToken != "" {
		parts = append(parts, "translation-langs-"+safeFileToken(langToken))
	}
	if opts.EnrichPages {
		parts = append(parts, "enriched")
	}
	if opts.IncludePhrases {
		parts = append(parts, "phrases")
	}
	if !opts.StripStemDiacritics {
		parts = append(parts, "keep-diacritics")
	}
	if !opts.TrimFinalStemVowels {
		parts = append(parts, "keep-final-vowels")
	}

	for i, part := range parts {
		if part == "" {
			parts[i] = "default"
		}
	}
	return strings.Join(parts, ".") + ".csv"
}

func outputLanguageToken(whitelist map[string]bool) string {
	defaultWhitelist, _ := parseLanguageWhitelist(defaultLanguageWhitelist)
	return languageToken(whitelist, defaultWhitelist)
}

func outputTranslationLanguageToken(whitelist map[string]bool) string {
	defaultWhitelist, _ := parseLanguageWhitelist(defaultTranslationLanguageWhitelist)
	return languageToken(whitelist, defaultWhitelist)
}

func languageToken(whitelist, defaultWhitelist map[string]bool) string {
	if languageMapsEqual(whitelist, defaultWhitelist) {
		return ""
	}
	if len(whitelist) == 0 {
		return "all"
	}
	langs := make([]string, 0, len(whitelist))
	for lang := range whitelist {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	return strings.Join(langs, "-")
}

func languageMapsEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func safeFileToken(s string) string {
	s = compactSpaces(strings.TrimSpace(s))
	var out strings.Builder
	lastDash := false
	written := 0
	for _, r := range s {
		var write string
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			write = string(r)
		case r == '_' || r == '.':
			write = string(r)
		default:
			if lastDash {
				continue
			}
			write = "-"
			lastDash = true
		}
		if write != "-" {
			lastDash = false
		}
		out.WriteString(write)
		written++
		if written >= 80 {
			break
		}
	}
	token := strings.Trim(out.String(), "-_.")
	if token == "" {
		return "default"
	}
	return token
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

	parsed, err := doAPIQuery(client, values)
	if err != nil {
		return wikiPage{}, err
	}
	if len(parsed.Query.Pages) == 0 {
		return wikiPage{}, errors.New("API response has no pages")
	}
	return parsed.Query.Pages[0], nil
}

func newAPIRequest(values url.Values) (*http.Request, error) {
	if apiMaxlag >= 0 {
		values = cloneURLValues(values)
		values.Set("maxlag", fmt.Sprintf("%d", apiMaxlag))
	}
	req, err := http.NewRequest(http.MethodGet, apiURL+"?"+values.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", apiUserAgent)
	return req, nil
}

func doAPIQuery(client *http.Client, values url.Values) (wikiResponse, error) {
	attempts := httpRetries + 1
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		req, err := newAPIRequest(values)
		if err != nil {
			return wikiResponse{}, err
		}
		cacheURL := req.URL.String()
		if body, ok := readCachedAPIResponse(cacheURL); ok {
			parsed, err := decodeAPIResponseBody(body)
			if err == nil && (parsed.Error == nil || parsed.Error.Code != "maxlag") {
				return parsed, nil
			}
		}
		resp, err := doAPIRequest(client, req)
		if err != nil {
			return wikiResponse{}, err
		}
		retryAfter := resp.Header.Get("Retry-After")

		body, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil {
			return wikiResponse{}, readErr
		}
		if closeErr != nil {
			return wikiResponse{}, closeErr
		}
		parsed, err := decodeAPIResponseBody(body)
		if err != nil {
			return wikiResponse{}, err
		}
		if parsed.Error == nil || parsed.Error.Code != "maxlag" {
			recordAPICacheMiss()
			writeCachedAPIResponse(cacheURL, body)
			return parsed, nil
		}

		lastErr = fmt.Errorf("GET %s: MediaWiki maxlag: %s", req.URL.Redacted(), strings.TrimSpace(parsed.Error.Info))
		if attempt+1 >= attempts {
			return wikiResponse{}, lastErr
		}
		delay := httpRetryBackoffDelay(attempt, retryAfter)
		logHTTPRetry(req, attempt+2, attempts, delay, lastErr)
		retrySleep(delay)
	}
	return wikiResponse{}, lastErr
}

func decodeAPIResponseBody(body []byte) (wikiResponse, error) {
	var parsed wikiResponse
	err := json.Unmarshal(body, &parsed)
	return parsed, err
}

func doAPIRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	attempts := httpRetries + 1
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		waitForAPIRequestSlot()
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

func waitForAPIRequestSlot() {
	if apiRequestDelay <= 0 {
		return
	}
	apiRequestMu.Lock()
	defer apiRequestMu.Unlock()

	now := time.Now()
	if !lastAPIRequestAt.IsZero() {
		next := lastAPIRequestAt.Add(apiRequestDelay)
		if now.Before(next) {
			retrySleep(next.Sub(now))
			now = time.Now()
		}
	}
	lastAPIRequestAt = now
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

func cloneURLValues(values url.Values) url.Values {
	out := url.Values{}
	for k, vals := range values {
		out[k] = append([]string(nil), vals...)
	}
	return out
}

type crawlOptions struct {
	LanguageWhitelist            map[string]bool
	TranslationLanguageWhitelist map[string]bool
	IncludePhrases               bool
	StripStemDiacritics          bool
	TrimFinalStemVowels          bool
	ProgressEvery                int
	Title                        string
	From                         string
	BatchSize                    int
	PageLimit                    int
	RowLimit                     int
	Resolver                     *templateResolver
	OnRow                        func(*csvRow) error
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
		title := strings.TrimSpace(opts.Title)
		if !isRussianAlphabetPageTitle(title) {
			return rows, skipped + 1, filtered, 1, nil
		}
		page, err := fetchPage(client, title, true)
		if err != nil {
			return nil, skipped, filtered, inspected, err
		}
		inspected = 1
		row, ok, wasFiltered := rowFromWordPage(page, opts)
		if wasFiltered {
			filtered++
		} else if ok {
			if opts.OnRow != nil {
				if err := opts.OnRow(&row); err != nil {
					return rows, skipped, filtered, inspected, err
				}
			}
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
			if !isRussianAlphabetPageTitle(page.Title) {
				skipped++
				maybeLogProgress(page.Title)
				continue
			}
			page, err := fetchPage(client, page.Title, true)
			if err != nil {
				return nil, skipped, filtered, inspected, err
			}
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
			if opts.OnRow != nil {
				if err := opts.OnRow(&row); err != nil {
					return rows, skipped, filtered, inspected, err
				}
			}
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
	for k, v := range cont {
		values.Set(k, v)
	}

	parsed, err := doAPIQuery(client, values)
	if err != nil {
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
	russianStem, ok := extractRussianLoanStem(ruSection, cyr)
	if !ok {
		return csvRow{}, false, false
	}
	cyr = russianStem

	candidates := extractPageLoanCandidates(ruSection, opts.Resolver, opts.TranslationLanguageWhitelist)
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
		latin, originalLatin, originalGreek, matchedRussianReading, ok := stemFromCandidate(cyr, mode, candidate, opts.IncludePhrases, opts.TrimFinalStemVowels, opts.StripStemDiacritics)
		if !ok {
			continue
		}
		notes := "wiktionary word-page etymology candidate; review before merging"
		if candidate.SourceKind == "translation" {
			notes = "wiktionary word-page translation candidate; review before merging"
		}
		return csvRow{
			CyrillicStem:          cyr,
			LatinStem:             latin,
			MatchedRussianReading: matchedRussianReading,
			OriginalLatin:         originalLatin,
			OriginalGreek:         originalGreek,
			Mode:                  mode,
			CaseMode:              "auto",
			MatchCase:             matchCaseForWordPage(page, ruSection),
			Source:                candidate.Source,
			Notes:                 notes,
			SuffixContext:         "",
			URL:                   wordURL(page.Title),
			pageTitle:             page.Title,
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

func extractRussianLoanStem(ruSection, fallbackTitle string) (string, bool) {
	morphologyText := russianMorphologyText(ruSection)

	hasNoun := false
	for _, call := range templateCalls(morphologyText) {
		if len(call) == 0 {
			continue
		}
		name := normalizeTemplateName(call[0])
		if isRussianNonNounPOSTemplate(name) {
			return "", false
		}
		if isRussianNounOrNameTemplate(name) {
			hasNoun = true
		}
	}
	if !hasNoun {
		return "", false
	}

	stem := extractRussianDeclensionStem(morphologyText)
	if stem == "" {
		stem = fallbackTitle
	}
	stem = cleanRussianStemCandidate(stem)
	fallback := cleanRussianStemCandidate(fallbackTitle)
	if stem == "" {
		stem = fallback
	}
	if fallback != "" && strings.HasSuffix(fallback, "ь") && stem+"ь" == fallback {
		stem = fallback
	}
	if stem == "" || !hasRussianLetter(stem) {
		return "", false
	}
	return stem, true
}

func russianMorphologyText(ruSection string) string {
	sections := extractNamedSubsections(ruSection, map[string]bool{
		"морфологические и синтаксические свойства": true,
	})
	if len(sections) > 0 {
		return strings.Join(sections, "\n")
	}
	if loc := etymologyHeadingRe.FindStringIndex(ruSection); loc != nil {
		return ruSection[:loc[0]]
	}
	return ruSection
}

func matchCaseForWordPage(page wikiPage, ruSection string) string {
	if matchCaseForTitle(page.Title) == "capitalized" ||
		russianMorphologyLooksProperName(ruSection) ||
		categoriesLookProperName(page.Categories) {
		return "capitalized"
	}
	return "any"
}

func matchCaseForTitle(title string) string {
	for _, r := range strings.TrimSpace(title) {
		if isRussianAlphabetLetter(unicode.ToLower(r)) {
			if unicode.IsUpper(r) {
				return "capitalized"
			}
			return "any"
		}
	}
	return "any"
}

func russianMorphologyLooksProperName(ruSection string) bool {
	morphology := russianMorphologyText(ruSection)
	lowerRaw := strings.ToLower(morphology)
	if strings.Contains(lowerRaw, "{{собств") {
		return true
	}
	lower := strings.ToLower(stripWikiMarkup(morphology))
	for _, marker := range []string{
		"имя собственное",
		"личное имя",
		"мужское имя",
		"женское имя",
		"топоним",
		"фамилия",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func categoriesLookProperName(categories []wikiCategory) bool {
	for _, cat := range categories {
		title := strings.ToLower(cat.Title)
		for _, marker := range []string{
			"имена собственные",
			"мужские имена",
			"женские имена",
			"фамилии",
			"топонимы",
		} {
			if strings.Contains(title, marker) {
				return true
			}
		}
	}
	return false
}

func extractRussianDeclensionStem(text string) string {
	for _, call := range templateCalls(text) {
		if len(call) == 0 {
			continue
		}
		if !isRussianNounOrNameTemplate(normalizeTemplateName(call[0])) {
			continue
		}
		if stem := russianNamedStemFromTemplate(call); stem != "" {
			return stem
		}
	}
	for _, call := range templateCalls(text) {
		if len(call) == 0 {
			continue
		}
		name := normalizeTemplateName(call[0])
		if !isRussianMorphologyTemplate(name) {
			continue
		}
		if stem := russianStemFromMorphologyTemplate(call); stem != "" {
			return stem
		}
	}
	for _, call := range templateCalls(text) {
		if len(call) == 0 {
			continue
		}
		if !isRussianNounOrNameTemplate(normalizeTemplateName(call[0])) {
			continue
		}
		if stem := russianStemFromNounTemplate(call); stem != "" {
			return stem
		}
	}
	return ""
}

func russianNamedStemFromTemplate(call []string) string {
	named := namedTemplateArgs(call[1:])
	for _, key := range []string{"основа", "основа1"} {
		if stem := cleanRussianStemCandidate(named[key]); stem != "" {
			return stem
		}
	}
	return ""
}

func russianStemFromMorphologyTemplate(call []string) string {
	named := namedTemplateArgs(call[1:])
	if stem := cleanRussianStemCandidate(named["основа"]); stem != "" {
		return stem
	}
	var out strings.Builder
	for i := 1; i <= 4; i++ {
		for _, key := range []string{
			fmt.Sprintf("прист%d", i),
			fmt.Sprintf("корень%d", i),
			fmt.Sprintf("интерфикс%d", i),
			fmt.Sprintf("суфф%d", i),
		} {
			out.WriteString(cleanRussianStemCandidate(named[key]))
		}
	}
	if stem := out.String(); stem != "" {
		return stem
	}
	if stem := cleanRussianStemCandidate(named["корень"]); stem != "" {
		return stem
	}
	if stem := russianStemFromPositionalMorphologyArgs(call[1:]); stem != "" {
		return stem
	}
	return firstRussianTemplateTerm(call[1:])
}

func namedTemplateArgs(args []string) map[string]string {
	named := map[string]string{}
	for _, arg := range args {
		key, value, ok := splitNamedTemplateArg(arg)
		if ok {
			named[key] = value
		}
	}
	return named
}

func russianStemFromPositionalMorphologyArgs(args []string) string {
	var out strings.Builder
	for _, arg := range args {
		if _, _, ok := splitNamedTemplateArg(arg); ok {
			continue
		}
		stem := cleanRussianStemCandidate(arg)
		if stem == "" {
			continue
		}
		out.WriteString(stem)
	}
	return out.String()
}

func russianStemFromNounTemplate(call []string) string {
	lemma := firstRussianTemplateTerm(call[1:])
	if lemma == "" {
		return ""
	}
	if nounTemplateIsIndeclinable(call[1:]) {
		return lemma
	}
	return trimRussianNominalEnding(lemma)
}

func nounTemplateIsIndeclinable(args []string) bool {
	for _, arg := range args {
		_, value, ok := splitNamedTemplateArg(arg)
		if !ok {
			value = arg
		}
		for _, field := range strings.Fields(strings.ToLower(value)) {
			field = strings.Trim(field, " \t\r\n.,;:()[]{}<>\"“”«»")
			if field == "0" || field == "неизм" || field == "нескл" || field == "indecl" {
				return true
			}
		}
	}
	return false
}

func trimRussianNominalEnding(lemma string) string {
	runes := []rune(lemma)
	if len(runes) <= 1 {
		return lemma
	}
	last := runes[len(runes)-1]
	switch last {
	case 'а', 'я', 'ы', 'и', 'о', 'е', 'ё':
		return string(runes[:len(runes)-1])
	default:
		return lemma
	}
}

func firstRussianTemplateTerm(args []string) string {
	for _, arg := range args {
		if _, _, ok := splitNamedTemplateArg(arg); ok {
			continue
		}
		if stem := cleanRussianStemCandidate(arg); stem != "" {
			return stem
		}
	}
	return ""
}

func cleanRussianStemCandidate(s string) string {
	s = cleanRussianTerm(stripWikiMarkup(s))
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "-", "")
	for _, r := range s {
		if !isRussianAlphabetLetter(r) {
			return ""
		}
	}
	return s
}

func splitNamedTemplateArg(arg string) (string, string, bool) {
	i := strings.Index(arg, "=")
	if i < 0 {
		return "", "", false
	}
	key := strings.ToLower(strings.TrimSpace(arg[:i]))
	value := strings.TrimSpace(arg[i+1:])
	if key == "" {
		return "", "", false
	}
	return key, value, true
}

func normalizeTemplateName(name string) string {
	return strings.ToLower(compactSpaces(strings.ReplaceAll(strings.TrimSpace(name), "_", " ")))
}

func isRussianMorphologyTemplate(name string) bool {
	return name == "морфо" || name == "морфо-ru" || name == "морфо ru"
}

func isRussianNounOrNameTemplate(name string) bool {
	if strings.HasPrefix(name, "форма-сущ") {
		return false
	}
	return strings.HasPrefix(name, "сущ") ||
		strings.Contains(name, "proper noun") ||
		strings.Contains(name, "имя собственное")
}

func isRussianNonNounPOSTemplate(name string) bool {
	switch {
	case strings.HasPrefix(name, "прил"),
		strings.HasPrefix(name, "гл"),
		strings.HasPrefix(name, "verb"),
		strings.HasPrefix(name, "нареч"),
		strings.HasPrefix(name, "adv"),
		strings.HasPrefix(name, "прич"),
		strings.HasPrefix(name, "деепр"):
		return true
	default:
		return false
	}
}

func extractPageLoanCandidates(ruSection string, resolver *templateResolver, translationLanguageWhitelist map[string]bool) []loanCandidate {
	var candidates []loanCandidate
	for _, section := range extractEtymologySections(ruSection) {
		candidates = append(candidates, candidatesFromEtymologySection(section, resolver)...)
	}
	if len(candidates) > 0 {
		return candidates
	}

	candidates = extractTranslationCandidates(ruSection)
	if meaningLanguageWhitelist := meaningTranslationLanguageWhitelist(ruSection); len(meaningLanguageWhitelist) > 0 {
		markedCandidates := filterLoanCandidatesByLanguage(candidates, meaningLanguageWhitelist)
		if len(markedCandidates) > 0 {
			return markedCandidates
		}
		return filterLoanCandidatesByLanguage(candidates, translationLanguageWhitelist)
	}
	return nil
}

func candidatesFromEtymologySection(section string, resolver *templateResolver) []loanCandidate {
	var candidates []loanCandidate
	start := 0
	for _, m := range etymologyTemplateRe.FindAllStringSubmatchIndex(section, -1) {
		if m[0] > start {
			candidates = append(candidates, candidatesFromEtymologyChunk(section[start:m[0]])...)
		}
		if resolver != nil && m[2] >= 0 && m[3] >= 0 {
			name := strings.TrimSpace(section[m[2]:m[3]])
			candidates = append(candidates, resolver.resolve(name)...)
		}
		start = m[1]
	}
	if start < len(section) {
		candidates = append(candidates, candidatesFromEtymologyChunk(section[start:])...)
	}
	return candidates
}

func candidatesFromEtymologyChunk(chunk string) []loanCandidate {
	var candidates []loanCandidate
	candidates = append(candidates, candidatesFromTextMarkers(stripWikiMarkup(chunk))...)
	candidates = append(candidates, candidatesFromTemplates(chunk)...)
	return candidates
}

func extractTranslationCandidates(ruSection string) []loanCandidate {
	var candidates []loanCandidate
	for _, section := range extractTranslationSections(ruSection) {
		candidates = append(candidates, markCandidateSourceKind(candidatesFromTextMarkers(stripWikiMarkup(section)), "translation")...)
		candidates = append(candidates, markCandidateSourceKind(candidatesFromTemplates(section), "translation")...)
	}
	return candidates
}

func meaningTranslationLanguageWhitelist(ruSection string) map[string]bool {
	sections := extractMeaningSections(ruSection)
	if len(sections) == 0 {
		return nil
	}
	text := strings.ToLower(stripWikiMarkup(strings.Join(sections, "\n")))
	whitelist := map[string]bool{}
	for _, marker := range meaningLanguageMarkers {
		re := regexp.MustCompile(`(?i)` + marker.Pattern)
		if re.MatchString(text) {
			whitelist[marker.Source] = true
		}
	}
	if len(whitelist) == 0 {
		return nil
	}
	return whitelist
}

func filterLoanCandidatesByLanguage(candidates []loanCandidate, languageWhitelist map[string]bool) []loanCandidate {
	if languageWhitelist == nil {
		return candidates
	}
	filtered := candidates[:0]
	for _, candidate := range candidates {
		source := canonicalLanguageName(candidate.Source)
		if source == "" {
			continue
		}
		if languageAllowed(source, languageWhitelist) {
			candidate.Source = source
			filtered = append(filtered, candidate)
		}
	}
	return filtered
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

func extractTranslationSections(ruSection string) []string {
	return extractNamedSubsections(ruSection, map[string]bool{
		"перевод":          true,
		"список переводов": true,
	})
}

func extractMeaningSections(ruSection string) []string {
	return extractNamedSubsections(ruSection, map[string]bool{
		"значение": true,
		"значения": true,
	})
}

func extractNamedSubsections(text string, names map[string]bool) []string {
	var sections []string
	activeStart := -1
	activeLevel := 0
	offset := 0

	for _, line := range strings.SplitAfter(text, "\n") {
		lineStart := offset
		offset += len(line)

		level, title, ok := parseWikiHeadingLine(line)
		if !ok {
			continue
		}
		if activeStart >= 0 && level <= activeLevel {
			if section := strings.TrimSpace(text[activeStart:lineStart]); section != "" {
				sections = append(sections, text[activeStart:lineStart])
			}
			activeStart = -1
		}
		if activeStart < 0 && names[normalizeSectionHeading(title)] {
			activeStart = offset
			activeLevel = level
		}
	}
	if activeStart >= 0 {
		if section := strings.TrimSpace(text[activeStart:]); section != "" {
			sections = append(sections, text[activeStart:])
		}
	}
	return sections
}

func parseWikiHeadingLine(line string) (int, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || line[0] != '=' {
		return 0, "", false
	}
	leading := 0
	for leading < len(line) && line[leading] == '=' {
		leading++
	}
	trailing := 0
	for i := len(line) - 1; i >= 0 && line[i] == '='; i-- {
		trailing++
	}
	if leading < 2 || trailing < leading || leading+trailing >= len(line) {
		return 0, "", false
	}
	title := strings.TrimSpace(line[leading : len(line)-trailing])
	if title == "" {
		return 0, "", false
	}
	return leading, title, true
}

func normalizeSectionHeading(title string) string {
	return strings.ToLower(compactSpaces(stripWikiMarkup(title)))
}

func markCandidateSourceKind(candidates []loanCandidate, kind string) []loanCandidate {
	for i := range candidates {
		if candidates[i].SourceKind == "" {
			candidates[i].SourceKind = kind
		}
	}
	return candidates
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
			MatchCase:     matchCaseForTitle(pageTitle),
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

func stemFromCandidate(cyr, mode string, candidate loanCandidate, includePhrases, trimFinalStemVowels, stripStemDiacritics bool) (string, string, string, string, bool) {
	var latin, originalLatin, originalGreek, matchedRussianReading string
	if candidate.Latin != "" {
		latin = normalizeLatinCandidate(candidate.Latin)
		if strings.Contains(latin, " ") && !includePhrases {
			return "", "", "", "", false
		}
		if !isLatinCandidate(latin) {
			return "", "", "", "", false
		}
		originalLatin = latin
		var ok bool
		latin, matchedRussianReading, ok = trimLatinStemToRussianSound(cyr, latin, mode, trimFinalStemVowels)
		if !ok {
			return "", "", "", "", false
		}
	} else if candidate.Greek != "" {
		originalGreek = normalizeGreekCandidate(candidate.Greek)
		if strings.Contains(originalGreek, " ") && !includePhrases {
			return "", "", "", "", false
		}
		if !isGreekCandidate(originalGreek) {
			return "", "", "", "", false
		}
		var ok bool
		greekStem := originalGreek
		if candidate.SourceKind != "translation" {
			greekStem = stemGreekDeclension(originalGreek)
		}
		latin, ok = transliterateGreek(greekStem)
		if !ok || !isLatinCandidate(latin) {
			return "", "", "", "", false
		}
		originalLatin = latin
		latin, matchedRussianReading, ok = trimLatinStemToRussianSound(cyr, latin, mode, trimFinalStemVowels)
		if !ok {
			return "", "", "", "", false
		}
	} else {
		return "", "", "", "", false
	}

	if stripStemDiacritics {
		latin = stripLatinDiacritics(latin)
	}
	return latin, originalLatin, originalGreek, matchedRussianReading, true
}

func trimLatinStemToRussianSound(cyr, latin, mode string, trimFinalStemVowels bool) (string, string, bool) {
	targetReading := russianReadingTarget(cyr)
	if len(targetReading) == 0 {
		return "", "", false
	}
	runes := []rune(strings.TrimSpace(latin))
	for end := 1; end <= len(runes); end++ {
		prefix := strings.TrimRight(string(runes[:end]), " \t\r\n.,;:()[]{}<>\"“”«»„'’ʼʻ-")
		if prefix == "" {
			continue
		}
		for _, stem := range latinStemCandidatesForPrefix(cyr, prefix, mode, trimFinalStemVowels) {
			readings := sourceSoundReadings(stem)
			if matchedVariant, ok := readingsMatchTargetVariant(readings, targetReading); ok {
				if !latinPrefixIsSubstantial(latin, prefix) {
					continue
				}
				return stem, russianReadingCSVString(matchedVariant), true
			}
		}
	}
	return "", "", false
}

func latinStemCandidatesForPrefix(cyr, prefix, mode string, trimFinalStemVowels bool) []string {
	baseCandidates := []string{prefix}
	trimmed := makeLatinStem(cyr, prefix, mode, trimFinalStemVowels)
	trimmed = trimFinalLatinVowelsForRussianSound(cyr, trimmed, mode, trimFinalStemVowels)
	if trimmed != prefix {
		baseCandidates = append(baseCandidates, trimmed)
	}

	var candidates []string
	for _, candidate := range baseCandidates {
		if withoutH, ok := dropLeadingLatinH(candidate); ok {
			candidates = appendUnique(candidates, withoutH)
		}
		candidates = appendUnique(candidates, candidate)
	}
	return candidates
}

func dropLeadingLatinH(s string) (string, bool) {
	runes := []rune(s)
	if len(runes) < 2 {
		return "", false
	}
	if runes[0] != 'h' && runes[0] != 'H' {
		return "", false
	}
	dropped := append([]rune(nil), runes[1:]...)
	if unicode.IsUpper(runes[0]) && len(dropped) > 0 {
		dropped[0] = unicode.ToUpper(dropped[0])
	}
	return string(dropped), true
}

func latinPrefixIsSubstantial(latin, prefix string) bool {
	sourceLetters := countLetters(latin)
	prefixLetters := countLetters(prefix)
	if sourceLetters == 0 || prefixLetters == 0 {
		return false
	}
	return prefixLetters*2 >= sourceLetters
}

func countLetters(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			n++
		}
	}
	return n
}

func trimFinalLatinVowelsForRussianSound(cyr, latin, mode string, trimFinalStemVowels bool) string {
	if !trimFinalStemVowels || mode != "stem" || !endsWithRussianConsonantSound(cyr) {
		return latin
	}
	runes := []rune(latin)
	for len(runes) > 1 && isLatinVowel(runes[len(runes)-1]) {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}

func endsWithRussianConsonantSound(s string) bool {
	runes := []rune(strings.ToLower(s))
	for i := len(runes) - 1; i >= 0; i-- {
		r := runes[i]
		if unicode.IsSpace(r) || r == '-' || r == '\'' {
			continue
		}
		if !unicode.Is(unicode.Cyrillic, r) {
			return false
		}
		return !isRussianVowel(r)
	}
	return false
}

type consonantReading struct {
	Alternatives [][]string
}

func readingsMatch(readings []consonantReading, target []string) bool {
	if len(readings) == 0 || len(target) == 0 {
		return len(readings) == 0 && len(target) == 0
	}
	positions := map[int]bool{0: true}
	for _, reading := range readings {
		nextPositions := map[int]bool{}
		for pos := range positions {
			for _, alt := range reading.Alternatives {
				if readingAlternativeMatches(target, pos, alt) {
					nextPositions[pos+len(alt)] = true
				}
			}
		}
		if len(nextPositions) == 0 {
			return false
		}
		positions = nextPositions
	}
	return positions[len(target)]
}

func consonantReadingsMatch(readings []consonantReading, target []string) bool {
	return readingsMatch(readings, target)
}

func readingAlternativeMatches(target []string, pos int, alt []string) bool {
	if pos+len(alt) > len(target) {
		return false
	}
	for i, token := range alt {
		if target[pos+i] != token {
			return false
		}
	}
	return true
}

func maxTokensInReadings(readings []consonantReading) int {
	total := 0
	for _, reading := range readings {
		maxLen := 0
		for _, alt := range reading.Alternatives {
			if len(alt) > maxLen {
				maxLen = len(alt)
			}
		}
		total += maxLen
	}
	return total
}

func maxConsonantsInReadings(readings []consonantReading) int {
	return maxTokensInReadings(readings)
}

func sourceConsonantsMatchRussian(cyr, source string) bool {
	return readingsMatch(sourceConsonantReadings(source), russianConsonantSequence(cyr))
}

func sourceSoundsMatchRussian(cyr, source string) bool {
	return readingsMatchTarget(sourceSoundReadings(source), russianReadingTarget(cyr))
}

func russianConsonantReadingString(consonants []string) string {
	return russianReadingString(consonants)
}

func russianReadingString(tokens []string) string {
	return strings.Join(tokens, " ")
}

func russianReadingCSVString(tokens []string) string {
	return strings.Join(tokens, "")
}

func russianConsonantSequence(s string) []string {
	s = strings.ToLower(s)
	var out []string
	previousToken := ""
	previousWasConsonant := false
	for _, r := range s {
		if token := russianConsonantToken(r); token != "" {
			if previousWasConsonant && previousToken == token {
				continue
			}
			out = append(out, token)
			previousToken = token
			previousWasConsonant = true
			continue
		}
		previousWasConsonant = false
		previousToken = ""
	}
	return out
}

func russianReadingSequence(s string) []string {
	target := russianReadingTarget(s)
	out := make([]string, 0, len(target))
	for _, token := range target {
		out = append(out, token.Display)
	}
	return out
}

type russianReadingTargetToken struct {
	Alternatives []string
	Display      string
}

func russianReadingTarget(s string) []russianReadingTargetToken {
	s = strings.ToLower(s)
	previousToken := ""
	previousWasConsonant := false
	var target []russianReadingTargetToken
	for _, r := range s {
		if token := russianConsonantToken(r); token != "" {
			if previousWasConsonant && previousToken == token {
				continue
			}
			target = append(target, russianReadingTargetToken{
				Alternatives: []string{token},
				Display:      token,
			})
			previousToken = token
			previousWasConsonant = true
			continue
		}
		if token := russianVowelToken(r); token != "" {
			target = append(target, russianReadingTargetToken{
				Alternatives: []string{token},
				Display:      token,
			})
			previousToken = ""
			previousWasConsonant = false
			continue
		}
		previousToken = ""
		previousWasConsonant = false
	}
	return target
}

func russianConsonantToken(r rune) string {
	switch unicode.ToLower(r) {
	case 'б':
		return "б"
	case 'п':
		return "п"
	case 'ф':
		return "ф"
	case 'в':
		return "в"
	case 'й':
		return "й"
	case 'г':
		return "г"
	case 'к':
		return "к"
	case 'х':
		return "х"
	case 'ч':
		return "ч"
	case 'д':
		return "д"
	case 'т':
		return "т"
	case 'с':
		return "с"
	case 'з':
		return "з"
	case 'ш':
		return "ш"
	case 'ж':
		return "ж"
	case 'щ':
		return "щ"
	case 'ц':
		return "ц"
	case 'л':
		return "л"
	case 'р':
		return "р"
	case 'м':
		return "м"
	case 'н':
		return "н"
	default:
		return ""
	}
}

func russianVowelToken(r rune) string {
	switch unicode.ToLower(r) {
	case 'а', 'я':
		return "а"
	case 'о':
		return "о"
	case 'ё':
		return "е"
	case 'у', 'ю':
		return "у"
	case 'ы':
		return "и"
	case 'э':
		return "е"
	case 'и':
		return "и"
	case 'е':
		return "е"
	default:
		return ""
	}
}

func sourceSoundReadings(s string) []consonantReading {
	return sourceReadings(s, true)
}

func sourceConsonantReadings(s string) []consonantReading {
	return sourceReadings(s, false)
}

func sourceReadings(s string, includeVowels bool) []consonantReading {
	if hasGreekSourceLetter(s) {
		return sourceGreekReadings(s, includeVowels)
	}
	return sourceLatinReadings(s, includeVowels)
}

func readingsMatchTarget(readings []consonantReading, target []russianReadingTargetToken) bool {
	_, ok := readingsMatchTargetVariant(readings, target)
	return ok
}

func readingsMatchTargetVariant(readings []consonantReading, target []russianReadingTargetToken) ([]string, bool) {
	if len(readings) == 0 || len(target) == 0 {
		if len(readings) == 0 && len(target) == 0 {
			return nil, true
		}
		return nil, false
	}
	positions := map[int][]string{0: nil}
	for _, reading := range readings {
		nextPositions := map[int][]string{}
		for pos, variant := range positions {
			for _, alt := range reading.Alternatives {
				if readingAlternativeMatchesTarget(target, pos, alt) {
					nextPos := pos + len(alt)
					if _, exists := nextPositions[nextPos]; exists {
						continue
					}
					nextPositions[nextPos] = append(append([]string(nil), variant...), alt...)
				}
			}
		}
		if len(nextPositions) == 0 {
			return nil, false
		}
		positions = nextPositions
	}
	variant, ok := positions[len(target)]
	return variant, ok
}

func readingAlternativeMatchesTarget(target []russianReadingTargetToken, pos int, alt []string) bool {
	if pos+len(alt) > len(target) {
		return false
	}
	for i, token := range alt {
		if !targetTokenMatches(target[pos+i], token) {
			return false
		}
	}
	return true
}

func targetTokenMatches(target russianReadingTargetToken, token string) bool {
	for _, alternative := range target.Alternatives {
		if alternative == token {
			return true
		}
	}
	return false
}

func sourceLatinReadings(s string, includeVowels bool) []consonantReading {
	s = strings.ToLower(stripLatinDiacritics(s))
	runes := []rune(s)
	var out []consonantReading
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if !unicode.IsLetter(r) {
			continue
		}
		if i+2 < len(runes) {
			if reading, ok := sourceLatinClusterReadings(string([]rune{r, runes[i+1], runes[i+2]}), includeVowels); ok {
				out = append(out, reading)
				i += 2
				continue
			}
		}
		if i+1 < len(runes) {
			if reading, ok := sourceLatinClusterReadings(string([]rune{r, runes[i+1]}), includeVowels); ok {
				out = append(out, reading)
				i++
				continue
			}
		}
		if includeVowels {
			if reading, ok := sourceVowelRuneReadings(r); ok {
				out = append(out, reading)
				continue
			}
		}
		if isLatinVowel(r) || r == 'y' {
			continue
		}
		if reading, ok := sourceConsonantRuneReadings(r); ok {
			out = append(out, reading)
		}
	}
	return out
}

func sourceLatinClusterReadings(cluster string, includeVowels bool) (consonantReading, bool) {
	switch cluster {
	case "sch":
		return reading("ш", "с", "ж"), true
	case "eau":
		if includeVowels {
			return reading("о"), true
		}
	case "zh":
		return reading("ж"), true
	case "ph":
		return reading("ф", "п"), true
	case "th":
		return reading("т", "ф"), true
	case "ch":
		return reading("х", "ч", "ш", "к"), true
	case "sh":
		return reading("ш"), true
	case "ll":
		return reading("л"), true
	case "cu", "qu":
		if includeVowels {
			return readingSeqs([]string{"к", "в"}, []string{"к"}, []string{"к", "у"}), true
		}
		return readingSeqs([]string{"к", "в"}, []string{"к"}), true
	case "sc":
		return readingSeqs([]string{"с", "к"}, []string{"ш"}, []string{"щ"}), true
	case "ts", "tz":
		return reading("ц"), true
	case "cz":
		return reading("ц", "ч"), true
	case "ck":
		return reading("к"), true
	}
	if !includeVowels {
		return consonantReading{}, false
	}
	switch cluster {
	case "ai":
		return readingSeqs([]string{"е"}, []string{"а", "й"}, []string{"а", "и"}, []string{"е", "й"}), true
	case "au":
		return readingSeqs([]string{"о"}, []string{"а", "у"}, []string{"а", "в"}), true
	case "eu":
		return readingSeqs([]string{"е"}, []string{"е", "в"}, []string{"о", "й"}), true
	case "ou":
		return readingSeqs([]string{"у"}, []string{"о", "у"}, []string{"а", "у"}, []string{"о"}), true
	case "ui":
		return readingSeqs([]string{"у", "и"}, []string{"и"}, []string{"е", "й"}, []string{"а", "у"}), true
	case "oe":
		return readingSeqs([]string{"е"}, []string{"о", "е"}, []string{"у"}), true
	case "yo":
		return readingSeqs([]string{"е"}, []string{"й", "о"}), true
	case "ei":
		return readingSeqs([]string{"е", "й"}, []string{"е"}, []string{"а", "й"}), true
	case "oi":
		return readingSeqs([]string{"о", "й"}, []string{"у", "а"}, []string{"о", "и"}), true
	case "ae":
		return readingSeqs([]string{"е"}, []string{"а", "й"}, []string{"а", "е"}), true
	case "ie":
		return readingSeqs([]string{"и"}, []string{"и", "е"}, []string{"е"}), true
	case "ea":
		return readingSeqs([]string{"и"}, []string{"е"}, []string{"е", "а"}), true
	case "ee":
		return reading("и", "е"), true
	case "oo":
		return reading("у", "о"), true
	case "ue":
		return reading("у", "е"), true
	}
	return consonantReading{}, false
}

func sourceConsonantRuneReadings(r rune) (consonantReading, bool) {
	switch r {
	case 'b':
		return reading("б", "в"), true
	case 'p':
		return reading("п"), true
	case 'f':
		return reading("ф"), true
	case 'v', 'w':
		return reading("в"), true
	case 'g':
		return readingSeqs([]string{"г"}, []string{"ж"}, []string{"д", "ж"}), true
	case 'c':
		return reading("к", "с", "ц", "ч"), true
	case 'k', 'q':
		return reading("к"), true
	case 'h':
		return readingSeqs([]string{"х"}, []string{"к"}, []string{"г"}, nil), true
	case 'x':
		return readingSeqs([]string{"х"}, []string{"к", "с"}, []string{"з"}), true
	case 'd':
		return reading("д"), true
	case 't':
		return reading("т"), true
	case 's':
		return reading("с", "з", "ш", "ж", "щ", "ц"), true
	case 'z':
		return reading("з", "с", "ц", "ж"), true
	case 'j':
		return readingSeqs([]string{"й"}, []string{"ж"}, []string{"д", "ж"}), true
	case 'ç':
		return reading("с"), true
	case 'l':
		return reading("л"), true
	case 'r':
		return reading("р"), true
	case 'm':
		return reading("м"), true
	case 'n':
		return reading("н"), true
	default:
		return consonantReading{}, false
	}
}

func sourceVowelRuneReadings(r rune) (consonantReading, bool) {
	switch r {
	case 'a':
		return reading("а", "е"), true
	case 'e':
		return reading("е", "и", "а"), true
	case 'i':
		return readingSeqs([]string{"и"}, []string{"е"}, []string{"й"}, []string{"а", "й"}), true
	case 'o':
		return reading("о"), true
	case 'u':
		return reading("у", "и"), true
	case 'y':
		return readingSeqs([]string{"и"}, []string{"а", "й"}, []string{"й"}, nil), true
	default:
		return consonantReading{}, false
	}
}

func hasGreekSourceLetter(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Greek, r) {
			return true
		}
	}
	return false
}

func sourceGreekReadings(s string, includeVowels bool) []consonantReading {
	tokens := greekTokens(s)
	var out []consonantReading
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if !token.letter {
			continue
		}
		if next, ok := nextGreekToken(tokens, i+1); ok {
			if reading, ok := sourceGreekClusterReadings(token.base, next.base, includeVowels); ok {
				out = append(out, reading)
				i = next.index
				continue
			}
		}
		if includeVowels {
			if reading, ok := sourceGreekVowelReadings(token.base); ok {
				out = append(out, reading)
				continue
			}
		}
		if reading, ok := sourceGreekConsonantReadings(token.base); ok {
			out = append(out, reading)
		}
	}
	return out
}

func sourceGreekClusterReadings(a, b rune, includeVowels bool) (consonantReading, bool) {
	cluster := string([]rune{a, b})
	switch cluster {
	case "γγ":
		return readingSeqs([]string{"н", "г"}, []string{"г"}), true
	case "γκ":
		return readingSeqs([]string{"н", "к"}, []string{"г", "к"}, []string{"к"}), true
	case "γχ":
		return readingSeqs([]string{"н", "х"}, []string{"г", "х"}), true
	case "μπ":
		return readingSeqs([]string{"б"}, []string{"м", "п"}), true
	case "ντ":
		return readingSeqs([]string{"д"}, []string{"н", "т"}), true
	case "τζ":
		return readingSeqs([]string{"д", "з"}, []string{"д", "ж"}), true
	case "τσ":
		return readingSeqs([]string{"ц"}, []string{"т", "с"}), true
	}
	if !includeVowels {
		return consonantReading{}, false
	}
	switch cluster {
	case "αι":
		return readingSeqs([]string{"е"}, []string{"а", "й"}, []string{"а", "и"}), true
	case "ει":
		return readingSeqs([]string{"и"}, []string{"е", "й"}, []string{"е", "и"}), true
	case "οι":
		return readingSeqs([]string{"и"}, []string{"о", "й"}, []string{"о", "и"}), true
	case "υι":
		return readingSeqs([]string{"и"}, []string{"у", "и"}, []string{"в", "и"}), true
	case "ου":
		return reading("у"), true
	case "αυ":
		return readingSeqs([]string{"а", "в"}, []string{"а", "у"}), true
	case "ευ":
		return readingSeqs([]string{"е", "в"}, []string{"е", "у"}), true
	case "ηυ":
		return readingSeqs([]string{"е", "в"}, []string{"е", "у"}), true
	}
	return consonantReading{}, false
}

func sourceGreekVowelReadings(r rune) (consonantReading, bool) {
	switch r {
	case 'α':
		return reading("а", "е"), true
	case 'ε':
		return reading("е"), true
	case 'η':
		return reading("и", "е"), true
	case 'ι':
		return reading("и", "й"), true
	case 'ο', 'ω':
		return reading("о"), true
	case 'υ':
		return reading("и", "у", "в"), true
	default:
		return consonantReading{}, false
	}
}

func sourceGreekConsonantReadings(r rune) (consonantReading, bool) {
	switch r {
	case 'β':
		return reading("в", "б"), true
	case 'γ':
		return reading("г", "й"), true
	case 'δ':
		return reading("д"), true
	case 'ζ':
		return readingSeqs([]string{"з"}, []string{"д", "з"}), true
	case 'θ':
		return reading("т", "ф"), true
	case 'κ':
		return reading("к"), true
	case 'λ':
		return reading("л"), true
	case 'μ':
		return reading("м"), true
	case 'ν':
		return reading("н"), true
	case 'ξ':
		return readingSeqs([]string{"к", "с"}, []string{"х"}), true
	case 'π':
		return reading("п"), true
	case 'ρ':
		return reading("р"), true
	case 'σ':
		return reading("с", "з"), true
	case 'τ':
		return reading("т"), true
	case 'φ':
		return reading("ф", "п"), true
	case 'χ':
		return reading("х", "к"), true
	case 'ψ':
		return readingSeqs([]string{"п", "с"}), true
	default:
		return consonantReading{}, false
	}
}

func reading(values ...string) consonantReading {
	alternatives := make([][]string, 0, len(values))
	for _, value := range values {
		alternatives = append(alternatives, []string{value})
	}
	return consonantReading{Alternatives: alternatives}
}

func readingSeqs(values ...[]string) consonantReading {
	return consonantReading{Alternatives: values}
}

func candidatesFromTemplates(text string) []loanCandidate {
	var candidates []loanCandidate
	for _, call := range templateCalls(text) {
		if len(call) < 3 {
			continue
		}
		name := normalizeTemplateName(call[0])
		switch name {
		case "lang":
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
		if isTranslationTermTemplate(name) {
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

func isTranslationTermTemplate(name string) bool {
	switch name {
	case "перев", "перев+", "перев-", "t", "t+", "t-", "t0", "tø", "trad", "trad+", "trad-":
		return true
	default:
		return false
	}
}

func candidatesFromTextMarkers(text string) []loanCandidate {
	type positionedCandidate struct {
		index     int
		candidate loanCandidate
	}
	var positioned []positionedCandidate
	for _, marker := range textLanguageMarkers {
		markerPattern := `(?:` + marker.Pattern + `)`
		re := regexp.MustCompile(`(?i)(?:^|[\s,;:({])` + markerPattern + `\s*([A-Za-zÀ-ÖØ-öø-ÿĀ-žḀ-ỹ][A-Za-zÀ-ÖØ-öø-ÿĀ-žḀ-ỹ\pM'’ʼʻ.-]*(?:\s+[A-Za-zÀ-ÖØ-öø-ÿĀ-žḀ-ỹ][A-Za-zÀ-ÖØ-öø-ÿĀ-žḀ-ỹ\pM'’ʼʻ.-]*){0,4})`)
		for _, m := range re.FindAllStringSubmatchIndex(text, -1) {
			if m[2] < 0 || m[3] < 0 {
				continue
			}
			latin := normalizeLatinCandidate(text[m[2]:m[3]])
			if isLatinCandidate(latin) {
				positioned = append(positioned, positionedCandidate{
					index:     m[0],
					candidate: loanCandidate{Source: marker.Source, Latin: latin},
				})
			}
		}
		if marker.Source == "Greek" {
			re := regexp.MustCompile(`(?i)(?:^|[\s,;:({])` + markerPattern + `\s*(` + greekCandidateRe.String() + `)`)
			for _, m := range re.FindAllStringSubmatchIndex(text, -1) {
				if m[2] < 0 || m[3] < 0 {
					continue
				}
				greek := normalizeGreekCandidate(text[m[2]:m[3]])
				if isGreekCandidate(greek) {
					positioned = append(positioned, positionedCandidate{
						index:     m[0],
						candidate: loanCandidate{Source: marker.Source, Greek: greek},
					})
				}
			}
		}
	}
	sort.SliceStable(positioned, func(i, j int) bool {
		return positioned[i].index < positioned[j].index
	})
	candidates := make([]loanCandidate, 0, len(positioned))
	for _, item := range positioned {
		candidates = append(candidates, item.candidate)
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
					inner := text[start:j]
					calls = append(calls, templateCalls(inner)...)
					calls = append(calls, splitTemplateArgs(inner))
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

type csvRowWriter struct {
	f *os.File
	w *csv.Writer
}

func newCSVRowWriter(path string) (_ *csvRowWriter, err error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = f.Close()
		}
	}()

	writer := &csvRowWriter{
		f: f,
		w: csv.NewWriter(f),
	}
	if err := writer.writeRecord(csvHeaderRecord()); err != nil {
		return nil, err
	}
	return writer, nil
}

func (w *csvRowWriter) Write(row csvRow) error {
	return w.writeRecord(csvRowRecord(row))
}

func (w *csvRowWriter) Close() error {
	return w.f.Close()
}

func (w *csvRowWriter) writeRecord(record []string) error {
	return writeCSVRecord(w.w, w.f, record)
}

func writeCSV(path string, rows []csvRow) (err error) {
	writer, err := newCSVRowWriter(path)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := writer.Close(); err == nil {
			err = closeErr
		}
	}()

	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	return nil
}

func csvHeaderRecord() []string {
	return []string{"cyrillic_stem", "latin_stem", "matched_russian_reading", "original_latin", "original_greek", "mode", "case_mode", "match_case", "source", "notes", "suffix_context", "url"}
}

func csvRowRecord(row csvRow) []string {
	return []string{
		row.CyrillicStem,
		row.LatinStem,
		row.MatchedRussianReading,
		row.OriginalLatin,
		row.OriginalGreek,
		row.Mode,
		row.CaseMode,
		csvMatchCase(row.MatchCase),
		row.Source,
		row.Notes,
		row.SuffixContext,
		row.URL,
	}
}

func csvMatchCase(matchCase string) string {
	matchCase = strings.TrimSpace(matchCase)
	if matchCase == "" {
		return "any"
	}
	return matchCase
}

func writeCSVRecord(w *csv.Writer, f *os.File, record []string) error {
	if err := w.Write(record); err != nil {
		return err
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	return f.Sync()
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

func isRussianAlphabetPageTitle(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" {
		return false
	}
	for _, r := range title {
		if !isRussianAlphabetLetter(r) {
			return false
		}
	}
	return true
}

func isRussianAlphabetLetter(r rune) bool {
	switch unicode.ToLower(r) {
	case 'а', 'б', 'в', 'г', 'д', 'е', 'ё', 'ж', 'з', 'и', 'й',
		'к', 'л', 'м', 'н', 'о', 'п', 'р', 'с', 'т', 'у', 'ф',
		'х', 'ц', 'ч', 'ш', 'щ', 'ъ', 'ы', 'ь', 'э', 'ю', 'я':
		return true
	default:
		return false
	}
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
