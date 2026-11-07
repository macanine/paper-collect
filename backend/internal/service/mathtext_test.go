package service

import (
	"strings"
	"testing"
)

func TestMathCanonicalRepresentations(t *testing.T) {
	for _, tt := range []struct {
		left, right string
	}{
		{"x²+2x", `x^{2}+2x`},
		{"x₁−y₂", `x_{1}-y_{2}`},
		{`\frac{x+1}{2}`, "(x+1)/2"},
		{`\frac{x}{y}`, "x/y"},
		{`\frac{{x}^{2}+1}{\sqrt{y}}`, "(x²+1)/√y"},
		{`\sqrt{x+1}`, "√(x+1)"},
		{`\sqrt[3]{x}`, "∛x"},
		{`\alpha\leq 2`, "α≤2"},
		{`x\neq y`, "x!=y"},
		{`x\approx y`, "x≈y"},
		{"Ａ＋Ｂ＝３", "A+B=3"},
		{`\left(x+y\right)`, "(x+y)"},
	} {
		if left, right := canonicalMath(tt.left), canonicalMath(tt.right); left != right {
			t.Errorf("%q -> %q, %q -> %q", tt.left, left, tt.right, right)
		}
	}
	for _, pair := range [][2]string{{"x+y", "x-y"}, {"x^2", "x_2"}, {"x+1", "X+1"}, {"a/b", "b/a"}, {"x!", "x"}} {
		if canonicalMath(pair[0]) == canonicalMath(pair[1]) {
			t.Errorf("different formulas collapsed: %q and %q", pair[0], pair[1])
		}
	}
}

func TestMathFragmentsAndTerms(t *testing.T) {
	text := `已知函数 $f(x)=x^{2}+2x$，求 f(x) 的最小值。`
	fragments := mathFragments(text)
	if len(fragments) != 1 || fragments[0].canonical != "f(x)=x^2+2x" {
		t.Fatalf("fragments = %#v", fragments)
	}
	runes := []rune(text)
	if got := string(runes[fragments[0].start:fragments[0].end]); !strings.Contains(got, `x^{2}`) {
		t.Fatalf("source span = %q", got)
	}
	for _, term := range []string{"m:x^2", "m:2+2", "m:+2x"} {
		if mathTerms(fragments)[term] == 0 {
			t.Fatalf("missing %q in %v", term, mathTerms(fragments))
		}
	}
	if _, ok := matchingMathFragment(text, mathFragments("x²+2x")); !ok {
		t.Fatal("equivalent superscript notation did not match")
	}
	if _, ok := matchingMathFragment(text, mathFragments("x²-2x")); ok {
		t.Fatal("different operator matched")
	}
	if _, ok := matchingMathFragment("已知 x+10=3", mathFragments("x+1")); ok {
		t.Fatal("partial numeric operand matched")
	}
	if _, ok := matchingMathFragment("已知 -x+y=3", mathFragments("x+y")); ok {
		t.Fatal("a leading negative sign was ignored")
	}
	if got := mathTerms(mathFragments("-x")); got["m:-x"] == 0 {
		t.Fatalf("unary operator was not indexed: %v", got)
	}
}

func TestMathSnippetHighlightsTheMatchedFormula(t *testing.T) {
	text := strings.Repeat("背景", 80) + `🚀已知 \(x^{2}+1\)，求值。` + strings.Repeat("说明", 80)
	match, ok := matchingMathFragment(text, mathFragments("x²+1"))
	if !ok {
		t.Fatal("formula was not found")
	}
	snippet, highlights := highlightMathSnippet(text, match, 50)
	if !strings.HasPrefix(snippet, "…") || !strings.HasSuffix(snippet, "…") || len(highlights) != 1 {
		t.Fatalf("unexpected formula excerpt: %q %v", snippet, highlights)
	}
	marked := string([]rune(snippet)[highlights[0][0]:highlights[0][1]])
	if marked != `\(x^{2}+1\)` {
		t.Fatalf("highlighted %q instead of the formula", marked)
	}
}

func TestMathContainsChecksBoundariesAndLaterOccurrences(t *testing.T) {
	for _, tt := range []struct {
		text, query string
		want        bool
	}{
		{"badx+y+x+y", "x+y", true},
		{"x+10+x+1", "x+1", true},
		{"x+10", "x+1", false},
		{"α+β", "α+β", true},
		{"γα+β", "α+β", false},
		{"-x+y", "x+y", false},
	} {
		if got := mathContains(tt.text, tt.query); got != tt.want {
			t.Errorf("mathContains(%q, %q) = %v, want %v", tt.text, tt.query, got, tt.want)
		}
	}
}

func TestMathFragmentsSplitNaturalLanguage(t *testing.T) {
	fragments := mathFragments("求 x + y 的值，Solve a - b now.")
	if len(fragments) != 2 || fragments[0].canonical != "x+y" || fragments[1].canonical != "a-b" {
		t.Fatalf("fragments = %#v", fragments)
	}
	if got := mathFragments("查找 new-api 和 cloud-storage"); len(got) != 0 {
		t.Fatalf("ordinary hyphenated words were treated as formulas: %#v", got)
	}
}

func TestQuestionDedupeKeepsDifferentFormulaOperators(t *testing.T) {
	plus := questionDedupeText("求 x+y 的值")
	minus := questionDedupeText("求 x-y 的值")
	if plus == minus {
		t.Fatal("deduplication collapsed different formulas")
	}
	if questionDedupeText("求 x²+1 的值") != questionDedupeText(`求 x^{2}+1 的值`) {
		t.Fatal("equivalent superscript notation should deduplicate")
	}
}
