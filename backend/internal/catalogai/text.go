package catalogai

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// foldWidth maps full-width ASCII and the ideographic space to their
// half-width forms and lowercases letters.
func foldWidth(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '　':
			r = ' '
		case r >= '！' && r <= '～':
			r = r - 0xFEE0
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// compact folds width and drops whitespace, so quotes can be compared with
// OCR output whose line breaks and spacing are arbitrary.
func compact(s string) string {
	var b strings.Builder
	for _, r := range foldWidth(s) {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Corpus is the text a reply may quote from. Quotes are matched after width
// folding and whitespace removal.
type Corpus struct {
	text string
}

// NewCorpus joins evidence sources into one searchable text.
func NewCorpus(parts ...string) Corpus {
	return Corpus{text: compact(strings.Join(parts, "\n"))}
}

// Contains reports whether quote appears in the corpus. Quotes shorter than
// two characters prove nothing and are rejected.
func (c Corpus) Contains(quote string) bool {
	q := compact(quote)
	if utf8.RuneCountInString(q) < 2 {
		return false
	}
	return strings.Contains(c.text, q)
}

// nameNoise is removed when comparing organisation and tag names. It covers
// punctuation and the administrative suffixes that are written inconsistently
// ("杭州市第二中学" / "杭州第二中学", "浙江省" / "浙江").
var nameNoise = strings.NewReplacer("省", "", "市", "", "（", "", "）", "", "(", "", ")", "", "【", "", "】", "",
	"·", "", "•", "", "-", "", "—", "", "_", "", "、", "", ",", "", "，", "", ".", "", "。", "", "\"", "", "“", "", "”", "",
	"'", "", "‘", "", "’", "", "《", "", "》", "", "<", "", ">", "")

// NormalizeName reduces a name to the form used for exact alias matching.
// It never rewrites abbreviations: deciding that "杭二中" is "杭州市第二中学"
// is the model's job.
func NormalizeName(s string) string {
	return nameNoise.Replace(compact(s))
}

// Similarity scores two names by shared character bigrams after
// normalisation (0..1). It only ranks candidates shown to the model.
func Similarity(a, b string) float64 {
	a, b = NormalizeName(a), NormalizeName(b)
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	if strings.Contains(a, b) || strings.Contains(b, a) {
		return 0.8
	}
	ga, gb := bigrams(a), bigrams(b)
	if len(ga) == 0 || len(gb) == 0 {
		return 0
	}
	shared := 0
	for g := range ga {
		if gb[g] {
			shared++
		}
	}
	return float64(2*shared) / float64(len(ga)+len(gb))
}

func bigrams(s string) map[string]bool {
	r := []rune(s)
	out := map[string]bool{}
	if len(r) == 1 {
		out[s] = true
	}
	for i := 0; i+1 < len(r); i++ {
		out[string(r[i:i+2])] = true
	}
	return out
}

// ErrNotJSON means a reply had no decodable JSON object.
var ErrNotJSON = errors.New("模型输出不是合法 JSON")

// decodeObject extracts the outermost JSON object from a reply that may carry
// stray text or a code fence, and decodes it into dst.
func decodeObject(content string, dst any) error {
	trimmed := strings.TrimSpace(content)
	if i := strings.Index(trimmed, "{"); i >= 0 {
		if j := strings.LastIndex(trimmed, "}"); j > i {
			trimmed = trimmed[i : j+1]
		}
	}
	if err := json.Unmarshal([]byte(trimmed), dst); err != nil {
		return ErrNotJSON
	}
	return nil
}

// clip trims a string and caps it at limit runes.
func clip(s string, limit int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > limit {
		return string(r[:limit])
	}
	return s
}

func validMonth(v string) (int, int, bool) {
	if len(v) != 7 || v[4] != '-' {
		return 0, 0, false
	}
	year, month := 0, 0
	for i, c := range v {
		if i == 4 {
			continue
		}
		if c < '0' || c > '9' {
			return 0, 0, false
		}
		if i < 4 {
			year = year*10 + int(c-'0')
		} else {
			month = month*10 + int(c-'0')
		}
	}
	if year < 1990 || year > 2100 || month < 1 || month > 12 {
		return 0, 0, false
	}
	return year, month, true
}
