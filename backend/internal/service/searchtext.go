package service

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxSearchTermRunes = 64

// normalizeSearchText folds width/case and strips punctuation so queries and
// indexed text share one representation.
func normalizeSearchText(text string) string {
	var b strings.Builder
	for _, r := range text {
		if normalized, ok := normalizeSearchRune(r); ok {
			b.WriteRune(normalized)
		}
	}
	return b.String()
}

func normalizeSearchRune(r rune) (rune, bool) {
	switch {
	case r >= 0xFF01 && r <= 0xFF5E:
		r -= 0xFEE0
	case r == 0x3000:
		r = ' '
	}
	if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
		return 0, false
	}
	return unicode.ToLower(r), true
}

// searchTerms emits CJK unigrams+bigrams and contiguous latin/digit runs.
func searchTerms(normalized string) map[string]int {
	terms := map[string]int{}
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
			terms[boundedSearchTerm(latin.String())]++
			latin.Reset()
		}
	}
	for _, r := range normalized {
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

// boundedSearchTerm keeps an unbounded Latin/digit run within the posting-key
// column. Hashing preserves exact whole-run matching without making PostgreSQL
// reject the entire index transaction.
func boundedSearchTerm(term string) string {
	if utf8.RuneCountInString(term) <= maxSearchTermRunes {
		return term
	}
	sum := sha256.Sum256([]byte(term))
	return "h:" + hex.EncodeToString(sum[:])[:maxSearchTermRunes-2]
}

// A suffix automaton locates the longest continuous match without repeatedly
// scanning long questions for every one-character search term. It is built
// once per query and shared by all results.
type snippetState struct {
	next   map[rune]int
	link   int
	length int
}

type snippetMatcher struct{ states []snippetState }

func newSnippetMatcher(normalizedQuery string) *snippetMatcher {
	m := &snippetMatcher{states: []snippetState{{next: map[rune]int{}, link: -1}}}
	last := 0
	for _, r := range normalizedQuery {
		current := len(m.states)
		m.states = append(m.states, snippetState{next: map[rune]int{}, length: m.states[last].length + 1})
		p := last
		for p >= 0 {
			if _, ok := m.states[p].next[r]; ok {
				break
			}
			m.states[p].next[r] = current
			p = m.states[p].link
		}
		if p < 0 {
			m.states[current].link = 0
		} else {
			q := m.states[p].next[r]
			if m.states[p].length+1 == m.states[q].length {
				m.states[current].link = q
			} else {
				clone := len(m.states)
				next := make(map[rune]int, len(m.states[q].next))
				for key, value := range m.states[q].next {
					next[key] = value
				}
				m.states = append(m.states, snippetState{next: next, link: m.states[q].link, length: m.states[p].length + 1})
				for p >= 0 && m.states[p].next[r] == q {
					m.states[p].next[r] = clone
					p = m.states[p].link
				}
				m.states[q].link = clone
				m.states[current].link = clone
			}
		}
		last = current
	}
	return m
}

func (m *snippetMatcher) highlight(text string, limit int) (string, [][2]int) {
	positions := make([]int, 0, len(text))
	state, length, bestLength, bestEnd := 0, 0, 0, 0
	for original, r := range []rune(text) {
		normalized, ok := normalizeSearchRune(r)
		if !ok {
			continue
		}
		positions = append(positions, original)
		next, found := m.states[state].next[normalized]
		for !found && state != 0 {
			state = m.states[state].link
			if length > m.states[state].length {
				length = m.states[state].length
			}
			next, found = m.states[state].next[normalized]
		}
		if found {
			state = next
			length++
		} else {
			state, length = 0, 0
		}
		if length > bestLength {
			bestLength, bestEnd = length, len(positions)
		}
	}
	if bestLength == 0 {
		return snippetForMatch(text, -1, -1, limit)
	}
	return snippetForMatch(text, positions[bestEnd-bestLength], positions[bestEnd-1]+1, limit)
}

// snippetForMatch returns an excerpt and its rune-based highlight. Ellipses
// make the missing surrounding text explicit without affecting API offsets.
func snippetForMatch(text string, matchStart, matchEnd, limit int) (string, [][2]int) {
	runes := []rune(text)
	if limit <= 0 {
		limit = 160
	}
	start := 0
	if matchStart > limit/3 {
		start = matchStart - limit/3
	}
	if matchEnd > start+limit && matchEnd-matchStart <= limit {
		start = matchEnd - limit
	}
	end := start + limit
	if end > len(runes) {
		end = len(runes)
	}
	var snippet strings.Builder
	prefix := 0
	if start > 0 {
		snippet.WriteRune('…')
		prefix = 1
	}
	snippet.WriteString(string(runes[start:end]))
	if end < len(runes) {
		snippet.WriteRune('…')
	}
	hiStart, hiEnd := matchStart-start+prefix, matchEnd-start+prefix
	if hiEnd > end-start+prefix {
		hiEnd = end - start + prefix
	}
	if hiStart < prefix || hiStart >= hiEnd {
		return snippet.String(), nil
	}
	return snippet.String(), [][2]int{{hiStart, hiEnd}}
}
