package service

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// mathFragment keeps the source span as well as its canonical form. Spans are
// rune offsets so they can also be used for API highlight ranges.
type mathFragment struct {
	canonical string
	start     int
	end       int
}

// mathFragments recognises expressions embedded in Chinese or English text.
// It intentionally does not try to prove algebraic equivalence: operators,
// operands and their order remain significant.
func mathFragments(text string) []mathFragment {
	runes := []rune(text)
	var out []mathFragment
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		for start < end && unicode.IsSpace(runes[start]) {
			start++
		}
		for end > start && unicode.IsSpace(runes[end-1]) {
			end--
		}
		if start < end {
			canonical := canonicalMath(string(runes[start:end]))
			if len([]rune(canonical)) >= 2 && likelyMath(string(runes[start:end]), canonical) {
				out = append(out, mathFragment{canonical: canonical, start: start, end: end})
			}
		}
		start = -1
	}
	for i, r := range runes {
		if !mathSourceRune(r) || r == '\n' || r == '\r' {
			flush(i)
			continue
		}
		if unicode.IsSpace(r) {
			if start < 0 {
				continue
			}
			left := i - 1
			for left >= start && unicode.IsSpace(runes[left]) {
				left--
			}
			right := i + 1
			for right < len(runes) && unicode.IsSpace(runes[right]) && runes[right] != '\n' && runes[right] != '\r' {
				right++
			}
			// Separate natural-language words, while keeping spaces around
			// operators in expressions such as "x + y".
			if left >= start && right < len(runes) && mathWordRune(runes[left]) && mathWordRune(runes[right]) {
				flush(i)
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	flush(len(runes))
	return out
}

func mathWordRune(r rune) bool {
	return (unicode.IsLetter(r) && !unicode.Is(unicode.Han, r)) || unicode.IsDigit(r) || isSuperOrSubscript(r)
}

func mathSourceRune(r rune) bool {
	if unicode.IsSpace(r) || mathWordRune(r) {
		return true
	}
	if r >= 0xff01 && r <= 0xff5e {
		r -= 0xfee0
	}
	return strings.ContainsRune(`+-−﹣－*×·⋅÷/=≠<>≤≥≈≃∝→^_±∓√∛∜∞∈∉∑∏∫()[]{}\$|!%. '`, r)
}

func containsMathOperator(s string) bool {
	return strings.ContainsAny(s, "+-*/=<>≤≥≠≈≃∝→^_±∓√∈∉∑∏∫|!%")
}

func containsMathOperand(s string) bool {
	for _, r := range s {
		if mathWordRune(r) || r == '∞' {
			return true
		}
	}
	return false
}

func likelyMath(source, canonical string) bool {
	if !containsMathOperator(canonical) || !containsMathOperand(canonical) {
		return false
	}
	if strings.ContainsAny(canonical, "=<>≤≥≠≈≃∝→^_±∓√∞∈∉∑∏∫|!%") || strings.Contains(source, `\`) {
		return true
	}
	// Avoid treating ordinary hyphenated words and paths as formulas, while
	// retaining short variables and numbers such as x-y and 1/2.
	letters := 0
	for _, r := range canonical {
		if unicode.IsDigit(r) {
			return true
		}
		if unicode.IsLetter(r) {
			letters++
			continue
		}
		if letters == 1 {
			return true
		}
		letters = 0
	}
	return letters == 1
}

var superDigits = map[rune]rune{'⁰': '0', '¹': '1', '²': '2', '³': '3', '⁴': '4', '⁵': '5', '⁶': '6', '⁷': '7', '⁸': '8', '⁹': '9', '⁻': '-', '⁺': '+'}
var subDigits = map[rune]rune{'₀': '0', '₁': '1', '₂': '2', '₃': '3', '₄': '4', '₅': '5', '₆': '6', '₇': '7', '₈': '8', '₉': '9', '₋': '-', '₊': '+'}

func isSuperOrSubscript(r rune) bool {
	_, super := superDigits[r]
	_, sub := subDigits[r]
	return super || sub
}

var mathCommands = map[string]string{
	"times": "*", "cdot": "*", "div": "/", "pm": "±", "mp": "∓",
	"le": "≤", "leq": "≤", "ge": "≥", "geq": "≥", "ne": "≠", "neq": "≠",
	"in": "∈", "notin": "∉", "infty": "∞", "sum": "∑", "prod": "∏", "int": "∫",
	"approx": "≈", "simeq": "≃", "propto": "∝", "to": "→", "rightarrow": "→",
	"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ", "theta": "θ",
	"lambda": "λ", "mu": "μ", "pi": "π", "sigma": "σ", "phi": "φ", "omega": "ω",
	"Gamma": "Γ", "Delta": "Δ", "Theta": "Θ", "Lambda": "Λ", "Pi": "Π", "Sigma": "Σ", "Phi": "Φ", "Omega": "Ω",
}

// canonicalMath normalises common OCR and LaTeX spellings without removing
// mathematical signs or changing the case of mathematical variables.
func canonicalMath(s string) string {
	p := mathParser{runes: []rune(s)}
	return p.parse(0, 0)
}

type mathParser struct {
	runes []rune
	pos   int
}

func (p *mathParser) parse(end rune, depth int) string {
	if depth > 32 {
		// Keep the remaining input bounded and searchable rather than recursing
		// indefinitely on untrusted model output.
		p.pos = len(p.runes)
		return ""
	}
	var out strings.Builder
	for p.pos < len(p.runes) {
		r := p.runes[p.pos]
		if end != 0 && r == end {
			p.pos++
			break
		}
		p.pos++
		if unicode.IsSpace(r) || r == '$' {
			continue
		}
		if r >= 0xff01 && r <= 0xff5e {
			r -= 0xfee0
		}
		if value, ok := superDigits[r]; ok {
			out.WriteRune('^')
			out.WriteRune(value)
			for p.pos < len(p.runes) {
				next, ok := superDigits[p.runes[p.pos]]
				if !ok {
					break
				}
				out.WriteRune(next)
				p.pos++
			}
			continue
		}
		if value, ok := subDigits[r]; ok {
			out.WriteRune('_')
			out.WriteRune(value)
			for p.pos < len(p.runes) {
				next, ok := subDigits[p.runes[p.pos]]
				if !ok {
					break
				}
				out.WriteRune(next)
				p.pos++
			}
			continue
		}
		switch r {
		case '−', '﹣', '－':
			out.WriteRune('-')
		case '×', '·', '⋅':
			out.WriteRune('*')
		case '÷':
			out.WriteRune('/')
		case '∛':
			out.WriteString("√[3]")
		case '∜':
			out.WriteString("√[4]")
		case '!':
			if p.pos < len(p.runes) && p.runes[p.pos] == '=' {
				p.pos++
				out.WriteRune('≠')
			} else {
				out.WriteRune(r)
			}
		case '<', '>':
			if p.pos < len(p.runes) && p.runes[p.pos] == '=' {
				p.pos++
				if r == '<' {
					out.WriteRune('≤')
				} else {
					out.WriteRune('≥')
				}
			} else {
				out.WriteRune(r)
			}
		case '^', '_':
			out.WriteRune(r)
			p.skipSpaces()
			if p.pos < len(p.runes) && p.runes[p.pos] == '{' {
				p.pos++
				out.WriteString(mathGroup(p.parse('}', depth+1)))
			}
		case '{':
			out.WriteString(mathGroup(p.parse('}', depth+1)))
		case '\\':
			out.WriteString(p.command(depth))
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

func (p *mathParser) skipSpaces() {
	for p.pos < len(p.runes) && unicode.IsSpace(p.runes[p.pos]) {
		p.pos++
	}
}

func (p *mathParser) group(depth int) (string, bool) {
	p.skipSpaces()
	if p.pos >= len(p.runes) || p.runes[p.pos] != '{' {
		return "", false
	}
	p.pos++
	return p.parse('}', depth+1), true
}

func mathGroup(s string) string {
	if s == "" {
		return "()"
	}
	for index, r := range s {
		if index == 0 && r == '√' {
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return "(" + s + ")"
		}
	}
	return s
}

func (p *mathParser) command(depth int) string {
	if p.pos >= len(p.runes) {
		return `\`
	}
	if strings.ContainsRune(`()[]`, p.runes[p.pos]) {
		p.pos++ // LaTeX math delimiters \( \) \[ \]
		return ""
	}
	if !unicode.IsLetter(p.runes[p.pos]) {
		r := p.runes[p.pos]
		p.pos++
		if strings.ContainsRune(`{}%_|$`, r) {
			return string(r)
		}
		// Other non-letter commands are spacing commands such as \, and \;.
		return ""
	}
	start := p.pos
	for p.pos < len(p.runes) && unicode.IsLetter(p.runes[p.pos]) {
		p.pos++
	}
	name := string(p.runes[start:p.pos])
	switch name {
	case "left", "right", "displaystyle", "textstyle":
		return ""
	case "frac", "dfrac", "tfrac":
		num, okNum := p.group(depth)
		den, okDen := p.group(depth)
		if okNum && okDen {
			return mathGroup(num) + "/" + mathGroup(den)
		}
		return `\` + name + num + den
	case "sqrt":
		p.skipSpaces()
		degree := ""
		if p.pos < len(p.runes) && p.runes[p.pos] == '[' {
			p.pos++
			degree = "[" + p.parse(']', depth+1) + "]"
		}
		if value, ok := p.group(depth); ok {
			return "√" + degree + mathGroup(value)
		}
		return "√" + degree
	case "mathrm", "mathbf", "mathit", "operatorname":
		if value, ok := p.group(depth); ok {
			return value
		}
	}
	if value, ok := mathCommands[name]; ok {
		return value
	}
	return `\` + name
}

// mathTerms adds operator-bearing bigrams and trigrams to the existing word index. A
// literal prefix cannot collide with terms produced by searchTerms.
func mathTerms(fragments []mathFragment) map[string]int {
	terms := map[string]int{}
	for _, fragment := range fragments {
		runes := []rune(fragment.canonical)
		for _, width := range []int{2, 3} {
			for i := 0; i+width <= len(runes); i++ {
				part := string(runes[i : i+width])
				if containsMathOperator(part) {
					terms["m:"+part]++
				}
			}
		}
	}
	return terms
}

func matchingMathFragment(text string, wanted []mathFragment) (mathFragment, bool) {
	var first mathFragment
	candidates := mathFragments(text)
	for index, want := range wanted {
		found := false
		for _, candidate := range candidates {
			if mathContains(candidate.canonical, want.canonical) {
				if index == 0 {
					first = candidate
				}
				found = true
				break
			}
		}
		if !found {
			return mathFragment{}, false
		}
	}
	return first, len(wanted) > 0
}

func textOutsideMath(text string, fragments []mathFragment) string {
	runes := []rune(text)
	var result strings.Builder
	position := 0
	for _, fragment := range fragments {
		if fragment.start > position {
			result.WriteString(string(runes[position:fragment.start]))
		}
		result.WriteRune(' ')
		position = fragment.end
	}
	if position < len(runes) {
		result.WriteString(string(runes[position:]))
	}
	return result.String()
}

func questionDedupeText(text string) string {
	fragments := mathFragments(text)
	if len(fragments) == 0 {
		return normalizeSearchText(text)
	}
	var key strings.Builder
	key.WriteString(normalizeSearchText(textOutsideMath(text, fragments)))
	for _, fragment := range fragments {
		key.WriteRune('\x1f')
		key.WriteString(fragment.canonical)
	}
	return key.String()
}

func mathContains(haystack, needle string) bool {
	if needle == "" || len(needle) > len(haystack) {
		return false
	}
	first, _ := utf8.DecodeRuneInString(needle)
	last, _ := utf8.DecodeLastRuneInString(needle)
	for offset := 0; offset+len(needle) <= len(haystack); {
		match := strings.Index(haystack[offset:], needle)
		if match < 0 {
			return false
		}
		start := offset + match
		end := start + len(needle)
		previous, _ := utf8.DecodeLastRuneInString(haystack[:start])
		next, _ := utf8.DecodeRuneInString(haystack[end:])
		validStart := start == 0 || !(mathWordRune(first) &&
			(mathWordRune(previous) || strings.ContainsRune("-*/^_", previous)))
		validEnd := end == len(haystack) || !(mathWordRune(next) && mathWordRune(last))
		if validStart && validEnd {
			return true
		}
		_, width := utf8.DecodeRuneInString(haystack[start:])
		offset = start + width
	}
	return false
}

func highlightMathSnippet(text string, match mathFragment, limit int) (string, [][2]int) {
	if limit <= 0 {
		limit = 180
	}
	return snippetForMatch(text, match.start, match.end, limit)
}
