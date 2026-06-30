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
go run . -dict loan_stems.csv -apostrophe < input.txt > output.txt
```

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
rulat.go          Go CLI converter
loan_stems.csv    starter loanword/name stem dictionary
rulat_test.go     regression tests for native rules and dictionary behavior
README.md         this developer summary
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
cyrillic_stem,latin_stem,mode,case_mode,source,notes,suffix_context
```

Columns:

```text
cyrillic_stem   Cyrillic stem or full word to match.
latin_stem      Latin spelling to output.
mode            stem or word. Default: stem.
case_mode       auto or preserve. Default: auto.
source          Optional source-language note.
notes           Optional free-text note.
suffix_context  Optional override for how suffixes attach.
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
жюль,Jule,word,auto,French,name from Jules,
жюл,Jule,stem,auto,French,name stem from Jules,soft
```

This lets the converter produce:

```text
Жюль -> Jule
Жюля -> Julea
```

without treating the Russian suffix as if it followed a hard native consonant.

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
