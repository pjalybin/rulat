# rulat: Russian Latin orthography converter

`rulat` converts Cyrillic Russian text into the custom Latin orthography designed in this project. It supports two layers:

1. **Native Russian layer**: deterministic phonological spelling for ordinary Russian words.
2. **Loanword layer**: optional CSV dictionary that preserves recognizable Greek, Latin, French, Italian, English, German, Dutch, Hebrew-Greek, and other source stems, then adds Russian endings.

The project is intentionally experimental. The orthography is still being tuned, so keep tests close to every rule change.

## Quick start

```bash
go run . < input.txt > output.txt
```

With the loanword dictionary:

```bash
go run . -dict loan_stems.csv < input.txt > output.txt
```

With visible apostrophes between preserved loan stems and converted Russian suffixes:

```bash
go run . -dict loan_stems.csv -loan-apostrophe < input.txt > output.txt
```

`-apostrophe` is kept as a shorter alias for `-loan-apostrophe`.

Run tests:

```bash
go test ./...
```

Build:

```bash
go build -o rulat .
./rulat -dict loan_stems.csv < input.txt
```

## Project files

```text
rulat.go                         Go CLI converter
loan_stems.csv                   starter loanword/name stem dictionary
loan_stems.wiktionary.csv        generated Wiktionary loanword candidates
tools/crawl_wiktionary_loans.go  Wiktionary word-page crawler
rulat_test.go                    regression tests for native rules and dictionary behavior
README.md                        this developer summary
```

## Current native orthography

### Main principles

```text
e = palatalization / softness
j = й / jotification
```

Native spelling avoids `q`, `w`, and `y`. Loan stems may preserve source letters such as `c`, `h`, `j`, `u`, `y`, etc. when the word is intentionally kept in source-aware spelling.

### Basic vowels

```text
а = a
о = o
у = u
ы = i

э = e word-initially
э = ae elsewhere
```

Examples:

```text
это  -> eto
эхо  -> exo
эй   -> ej
мэр  -> maer
поэт -> poaet      native
```

Loan-aware spelling may override native spelling:

```text
поэт    -> poët
фаэтон  -> phaëthon
маэстро -> maestro
```

### Soft and jotified vowels

After an ordinary paired consonant, `e` marks softness:

```text
я = ea
е = ee
ё = eo
и = ei
ю = eu
```

At the beginning of a word, after a vowel, after `ъ`, or after `ь` as a separator, use `j`:

```text
я = ja
е = je
ё = jo
ю = ju
й = j
```

Examples:

```text
тя   -> tea
те   -> tee
тё   -> teo
ти   -> tei
тю   -> teu
ть   -> te

я    -> ja
ел   -> jel
ель  -> jele
ёж   -> jozs
юг   -> jug
мой  -> moj
мои  -> moi
поёт -> pojot
поит -> poit
```

### Ordinary consonants

```text
б = b
в = v
г = g
д = d
з = z
к = k
л = l
м = m
н = n
п = p
р = r
с = s
т = t
ф = f
х = x
```

### Special consonants

```text
ж = zs
ш = sz
щ = sze
ц = tz
ч = cz
```

Use longest-match parsing mentally: `sze` is `щ`, not `ш` + `э`; `zs` is `ж`; `sz` is `ш`; `tz` is `ц`; `cz` is `ч`.

### Always-hard Ж / Ш / Ц

Russian `ж`, `ш`, and `ц` are treated as always hard in native words.

```text
жа = zsa     ша = sza     ца = tza
же = zsae    ше = szae    це = tzae
жэ = zsae    шэ = szae    цэ = tzae
жи = zsi     ши = szi     ци = tzi
жо = zso     шо = szo     цо = tzo
жё = zso     шё = szo     цё = tzo
жу = zsu     шу = szu     цу = tzu
жь = zs      шь = sz      ц  = tz
```

Examples:

```text
Женя   -> Zsaenea
жена   -> zsaena
жизнь  -> zsizne
жук    -> zsuk
мажь   -> mazs
рожь   -> rozs

шина   -> szina
шея    -> szaeja
шёл    -> szol
мышь   -> misz

цена   -> tzaena
цирк   -> tzirk
царь   -> tzare
```

### Inherently soft Ч and soft Ш-like Щ

`ч = cz` is inherently palatal; do not add an extra `e` after `cz`.

```text
ча = cza
че = cze
чи = czi
чо = czo
чё = czo
чу = czu
чь = cz
```

`щ = sze` is soft `ш`:

```text
ща = szea
ще = szee
щи = szei
що = szeo
щё = szeo
щу = szeu
щ  = sze
```

Examples:

```text
чай    -> czaj
ночь   -> nocz
вечный -> veecznij

щи     -> szei
щука   -> szeuka
вещь   -> veesze
борщ   -> borsze
```

### Soft sign and hard sign

```text
ь after ordinary paired consonants = e
ь after ж/ш/ч/щ = zero
ъ = zero, but the following я/е/ё/ю uses j
```

Examples:

```text
мать   -> mate
конь   -> kone
мазь   -> maze

рожь   -> rozs
мышь   -> misz
ночь   -> nocz

семя   -> seemea
семья  -> seemeja
вьюга  -> vejuga
подъезд -> podjezd
```

The restored `j` solves common `ий` / `ьи` ambiguity:

```text
вражий  -> vrazsij
вражьи  -> vrazsji
божий   -> bozsij
божьи   -> bozsji
Марья   -> Mareja
Мариа   -> Mareia
Мария   -> Mareija
дьявол  -> dejavol
диавол  -> deiavol
```

### Assimilation escape for `сз` and `зс`

Because `sz = ш` and `zs = ж`, real Cyrillic clusters `сз` and `зс` are converted phonologically:

```text
сз -> zz
зс -> ss
```

Examples:

```text
сзади        -> zzadei
сзывать      -> zzivate
госзаказ     -> gozzakaz
французский  -> frantzusskeij
кавказский   -> kavkasskeij
```

If exact morpheme spelling is needed, apostrophe can be used manually in text or future tooling:

```text
s'zadei
frantzuz'skeij
```

## Loanword and name dictionary

The converter can load a CSV dictionary of stems or whole words. Dictionary entries preserve source-aware Latin spelling and convert any remaining Russian suffix natively.

Typical examples:

```text
шина      -> Schiena       from German Schiene, stem Schien-
поэт      -> poët          source-aware Greek/French/Latin spelling
фаэтон    -> phaëthon      Greek Phaethon/Phaëthon layer
маэстро   -> maestro       Italian
мэр       -> maire         French
цирк      -> circ          Latin root
Зевса     -> Zevsa         Greek Υ reflected as v in Russian tradition
Зевеса    -> Zeveesa       poetic Russified form
Евангелие -> Evangelije    Greek/Church loan form
Жюль      -> Jule          French name layer
Жюстина   -> Justina       French/Latin name layer
```

Russian endings can be shown with optional apostrophes:

```text
Schiena    / Schien'a
Schieni    / Schien'i
Zevsa      / Zevs'a
poëta      / poët'a
maestrom   / maestro'm
```

### CSV format

Header:

```csv
cyrillic_stem,latin_stem,original_latin,original_greek,mode,case_mode,source,notes,suffix_context,url
```

Columns:

```text
cyrillic_stem   Cyrillic stem or full word to match.
latin_stem      Latin spelling to output.
original_latin  Optional Latin source spelling or Greek transliteration before stem normalization.
original_greek  Optional Greek source spelling before transliteration.
mode            stem or word. Default: stem.
case_mode       auto or preserve. Default: auto.
source          Optional source-language note.
notes           Optional free-text note.
suffix_context  Optional override for how suffixes attach.
url             Optional source URL for dictionary provenance.
```

`suffix_context` values:

```text
native   infer from the last Cyrillic rune of the stem; default
none     no previous Russian phonological context
vowel    suffix follows a vowel
paired   suffix follows an ordinary paired consonant
hard     suffix follows always-hard ж/ш/ц
soft     suffix follows inherently soft ч/щ
sign     suffix follows ь/ъ
j        suffix follows й/jotation context
```

Useful example:

```csv
жюль,Jule,Jules,,word,auto,French,name from Jules,,
жюл,Jule,Jules,,stem,auto,French,name stem from Jules,soft,
```

This lets the converter produce:

```text
Жюль -> Jule
Жюля -> Julea
```

without treating the Russian suffix as if it followed a hard native consonant.

### Crawling Wiktionary loanword candidates

The curated `loan_stems.csv` is still the hand-reviewed dictionary. The crawler
builds a separate candidate file from Russian Wiktionary word pages:

```bash
GO111MODULE=off go run ./tools -out loan_stems.wiktionary.csv
```

The default `-source pages` mode walks main-namespace pages through MediaWiki's
API, extracts the Russian section, reads `=== Этимология ===`, resolves
`{{этимология:...}}` templates, parses `{{lang|...}}`/`{{lang2|...}}` source
forms, filters by source language, and writes the source word page into the
`url` column. Generated rows keep Latin source spelling in `original_latin`,
and Greek source spelling in `original_greek` while using a loanword-oriented
Greek-to-Latin stem conversion for `latin_stem`. Page mode lists titles first
and only loads word pages whose titles consist exclusively of Russian alphabet
letters, case-insensitively.
Generated `latin_stem` values remove Latin diacritics and drop final Latin
vowels when the Russian stem ends in a consonant, so `альков` from French
`alcôve` becomes `latin_stem=alcov,original_latin=alcôve`.
For Greek-script etymologies, basic Ancient Greek declensional stemming is
applied before transliteration: `εὐαγγέλιον` becomes
`latin_stem=evangeli,original_latin=evangeli,original_greek=εὐαγγέλιον`.
The `-ιον` family stems to `-ι`, so `εὐαγγέλιον`, `εὐαγγελίου`,
`εὐαγγελίῳ`, and `εὐαγγέλιᾰ` all produce `evangeli`. Common endings such as
`-ος`, `-ον`, `-ης`, `-ας`, `-ις`, `-υς`, and `-ους` lose the final case
consonant. Russian loan stems use the nominative loan shape for common Greek
neuters, so `πρόβλημα` becomes `problem`, not `problemat`; `-εύς` loses the
whole ending for names such as `Ἀχιλλεύς -> Achill`, while `-ων` stays intact
as in `Ἀπόλλων -> Apollon`. Final `ξ` and `ψ` recover basic velar/labial stems
as `κ` and `π`.
Greek loan diphthongs use later phonetic values where Russian borrowing
normally reflects Byzantine pronunciation: `ου -> u`, `αυ/ευ -> av/ev`,
`υι -> yi`, `ηυ -> ev`, `ωυ -> ov`; iota subscripts map as `ᾳ -> ai`,
`ῃ -> ei`, and `ῳ -> oi`.

The Greek romanizer can also be built and run by itself. It is a text filter
using standard Ancient Greek romanization; by default it keeps romanization
diacritics, and `-plain` strips them:

```bash
GO111MODULE=off go build -tags greektranslit -o greektranslit ./tools
printf 'Ἄνδρα μοι ἔννεπε, Μοῦσα\n' | ./greektranslit
printf 'Ἄνδρα μοι ἔννεπε, Μοῦσα\n' | ./greektranslit -plain
```

By default it only keeps candidates from this source-language whitelist:

```text
English, German, French, Italian, Greek, Latin, Dutch, Hebrew, Swedish, Danish, Spanish
```

Use `-languages English,French,Latin` to choose a narrower set, or
`-languages ''` to disable language filtering. The generated rows are candidates
and should be reviewed before merging into the curated dictionary.

For a slower run that adds page-category metadata and incomplete-etymology
markers to generated notes:

```bash
GO111MODULE=off go run ./tools -enrich-pages -out loan_stems.wiktionary.csv
```

The older appendix/prose parser is still available for comparison:

```bash
GO111MODULE=off go run ./tools -source appendix -out loan_stems.wiktionary.csv
```

Useful page-mode development commands:

```bash
GO111MODULE=off go run ./tools -title альков -languages French -out /tmp/alcov.csv
GO111MODULE=off go run ./tools -from альков -page-limit 100 -limit 20 -progress-every 50
```

`-progress-every N` logs page-mode counters to stderr after each `N` inspected
pages. The default is `0`, which disables progress logs.

Crawler HTTP behavior follows Wikimedia API rate-limit etiquette: requests are
serial, the default `User-Agent` identifies this project, `maxlag=5` is sent by
default, and `-request-delay` defaults to `350ms` to stay below the
unauthenticated User-Agent-only request bucket. Override the contact string with
`-user-agent` when running your own crawl. HTTP requests retry transient
failures by default: `408`, `429`, `500`, `502`, `503`, and `504`, plus
transport errors and MediaWiki `maxlag` responses. `-http-retries` controls the
retry budget and defaults to `5`; `-http-retry-delay` controls the initial
exponential backoff delay and defaults to `5s`. `Retry-After` is honored when
the server sends it.

## Dictionary matching behavior

The current implementation uses longest-prefix matching inside each Cyrillic word:

1. Exact `word` entries match only the whole word.
2. `stem` entries match the beginning of a word.
3. Entries are sorted by descending Cyrillic stem length, so longer/more specific entries win.
4. If no dictionary entry matches, the word is converted by native rules.

This is simple and fast, but not a full morphological analyzer. A stem entry can overmatch if it is too short or too general. Prefer longer stems and add regression tests for each important loanword.

## Current examples

Input:

```text
Я люблю русский язык. Женя ест борщ и щи.
Ночь, жизнь, чай, шина, цирк.
Мэр, поэт, фаэтон, аэропорт.
Зевса и Зевеса. Евангелие. Жюль Жюстина.
сзади французский вражий вражьи мажь
```

With `-dict loan_stems.csv`:

```text
Ja leubleu russkeij jazik. Zsaenea jest borsze i szei.
Nocz, zsizne, czaj, Schiena, circ.
Maire, poët, phaëthon, aeroport.
Zevsa i Zeveesa. Evangelije. Jule Justina.
zzadei frantzusskeij vrazsij vrazsji mazs
```

Without a dictionary, loanwords fall back to native spelling:

```text
шина -> szina
поэт -> poaet
мэр -> maer
Евангелие -> Jevangeleije
Жюстина -> Zsustina
```

## Development notes for Codex

When continuing development:

1. Keep the orthography rules and tests synchronized. Every rule change should add or update examples in `rulat_test.go`.
2. Treat `loan_stems.csv` as data, not code. Expand it gradually with well-attested stems and names.
3. Prefer longer dictionary stems to avoid accidental prefix overmatching.
4. Add tests for dictionary suffix behavior whenever adding `suffix_context` entries.
5. Preserve capitalization behavior:
   - `Женя -> Zsaenea`
   - `ЖУК -> ZSUK`
   - dictionary `case_mode=preserve` keeps source capitalization, useful for `Schien`.
6. Avoid adding diaeresis to native spelling. Diaeresis is allowed in preserved loan stems such as `poët` and `phaëthon`.
7. Keep `j` as the only native jotification/Й marker.

Potential next tasks:

```text
- Add a reverse converter from this Latin orthography back to Cyrillic.
- Add a mode that emits explanations/token traces for each converted word.
- Add a dictionary lint command to detect prefix conflicts and likely overmatches.
- Add benchmarks for large texts.
- Add corpus-based tests for Pushkin/Gnedich sample passages.
- Add a flag to disable assimilation сз -> zz / зс -> ss for exact morphemic mode.
- Add CSV columns for part of speech, source language confidence, and stem priority.
```

## Current limitations

```text
- The loanword dictionary is a starter list, not a complete etymological dictionary.
- Prefix matching is not morphology-aware.
- Some phonological mergers are intentional: шёл/шол, мышь/мыш, ночь/ноч, жить/жыть.
- Source-aware loan spelling is partly editorial; different editions may choose native spelling instead.
- Reverse conversion is not implemented yet.
```
