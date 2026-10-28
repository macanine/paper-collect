package service

import (
	"strings"
	"unicode"
)

// normalizeSearchText folds width/case and strips punctuation so queries and
// indexed text share one representation.
func normalizeSearchText(text string) string {
	var b strings.Builder
	for _, r := range text {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E:
			r -= 0xFEE0
		case r == 0x3000:
			r = ' '
		}
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// searchTerms emits CJK unigrams+bigrams and contiguous latin/digit runs.
func searchTerms(normalized string) map[string]int {
	terms := map[string]int{}
	runes := []rune(normalized)
	var cjk []rune
	flushCJK := func() {
		for _, r := range cjk {
			terms[string(r)]++
		}
		for i := 0; i+1 < len(cjk); i++ {
			terms[string(cjk[i:i+2])]++
		}
		cjk = cjk[:0]
	}
	var latin strings.Builder
	flushLatin := func() {
		if latin.Len() > 0 {
			terms[latin.String()]++
			latin.Reset()
		}
	}
	for _, r := range runes {
		switch {
		case unicode.Is(unicode.Han, r):
			flushLatin()
			cjk = append(cjk, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushCJK()
			latin.WriteRune(r)
		default:
			flushCJK()
			flushLatin()
		}
	}
	flushCJK()
	flushLatin()
	return terms
}

// highlightSnippet returns a short excerpt and character ranges of query terms.
func highlightSnippet(text, normalizedQuery string, limit int) (string, [][2]int) {
	if text == "" {
		return "", nil
	}
	runes := []rune(text)
	lowerText := strings.ToLower(text)
	candidates := []string{}
	seen := map[string]bool{}
	for term := range searchTerms(normalizedQuery) {
		if len([]rune(term)) >= 1 && !seen[term] {
			seen[term] = true
			candidates = append(candidates, term)
		}
	}
	best := -1
	for _, term := range candidates {
		if idx := strings.Index(lowerText, strings.ToLower(term)); idx >= 0 {
			pos := len([]rune(text[:idx]))
			if best < 0 || pos < best {
				best = pos
			}
		}
	}
	start := 0
	if best > 60 {
		start = best - 60
	}
	end := len(runes)
	if limit <= 0 {
		limit = 160
	}
	if start+limit < end {
		end = start + limit
	}
	snippetRunes := runes[start:end]
	snippet := string(snippetRunes)
	lowerSnippet := strings.ToLower(snippet)
	var highlights [][2]int
	for _, term := range candidates {
		needle := strings.ToLower(term)
		offset := 0
		for {
			idx := strings.Index(lowerSnippet[offset:], needle)
			if idx < 0 || len(highlights) >= 12 {
				break
			}
			byteStart := offset + idx
			runeStart := len([]rune(lowerSnippet[:byteStart]))
			runeLen := len([]rune(needle))
			highlights = append(highlights, [2]int{runeStart, runeStart + runeLen})
			offset = byteStart + len(needle)
			if offset >= len(lowerSnippet) {
				break
			}
		}
	}
	return snippet, highlights
}
