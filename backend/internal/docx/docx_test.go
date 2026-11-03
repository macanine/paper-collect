package docx

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func packageBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func parsePackage(t *testing.T, data []byte) *Document {
	t.Helper()
	doc, err := Parse(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestSampleParagraphsTableImageAndPages(t *testing.T) {
	doc, err := ParseFile("../testdata/docx/sample.docx")
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages) != 2 {
		t.Fatalf("pages = %d, want 2", len(doc.Pages))
	}
	if len(doc.Images) != 1 || doc.Images[0].MediaType != "image/png" || doc.Images[0].Page != 1 {
		t.Fatalf("images = %+v", doc.Images)
	}
	pages, markdown := doc.Render(map[int]string{0: "图片文字 123"})
	if len(pages) != 2 {
		t.Fatalf("pages = %+v", pages)
	}
	first := pages[0].Text
	for _, want := range []string{"高三数学样例", "第一题", "| 题号 | 答案 |", "| 一 | −1 |", "图片文字 123", "x+y=2"} {
		if !strings.Contains(first, want) {
			t.Fatalf("first page missing %q:\n%s", want, first)
		}
	}
	if strings.Index(first, "图片文字 123") > strings.Index(first, "x+y=2") {
		t.Fatal("image text lost its reading position")
	}
	if !strings.Contains(pages[1].Text, "参考答案") || !strings.Contains(markdown, "## 第 2 页") {
		t.Fatalf("second page = %+v\nmarkdown = %s", pages[1], markdown)
	}
}

func TestTextBoxAndExplicitPageBreak(t *testing.T) {
	document := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:v="urn:schemas-microsoft-com:vml"><w:body>` +
		`<w:p><w:r><w:t>前</w:t></w:r><w:r><w:pict><v:shape><v:textbox><w:txbxContent><w:p><w:r><w:t>框内</w:t></w:r></w:p></w:txbxContent></v:textbox></v:shape></w:pict></w:r><w:r><w:t>后</w:t></w:r></w:p>` +
		`<w:p><w:r><w:br w:type="page"/></w:r></w:p>` +
		`<w:p><w:r><w:t>第二页</w:t></w:r></w:p>` +
		`</w:body></w:document>`
	doc := parsePackage(t, packageBytes(t, map[string]string{"word/document.xml": document}))
	pages, _ := doc.Render(nil)
	if len(pages) != 2 || !strings.Contains(pages[0].Text, "框内") || pages[1].Text != "第二页" {
		t.Fatalf("pages = %+v", pages)
	}
}

func TestImageRelationshipAndOrder(t *testing.T) {
	document := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>` +
		`<w:p><w:r><w:t>前</w:t></w:r><w:r><w:drawing><a:blip r:embed="rId1"/></w:drawing></w:r><w:r><w:t>后</w:t></w:r></w:p>` +
		`</w:body></w:document>`
	files := map[string]string{
		"word/document.xml":            document,
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/image1.png"/></Relationships>`,
		"word/media/image1.png":        "\x89PNG\r\n\x1a\nimage",
	}
	doc := parsePackage(t, packageBytes(t, files))
	if len(doc.Images) != 1 || doc.Images[0].Name != "image1.png" {
		t.Fatalf("images = %+v", doc.Images)
	}
	pages, _ := doc.Render(map[int]string{0: "图内文字"})
	if strings.Index(pages[0].Text, "图内文字") > strings.Index(pages[0].Text, "后") {
		t.Fatalf("image text order = %q", pages[0].Text)
	}
}

func TestDocumentWithoutBodyIsRejected(t *testing.T) {
	data := packageBytes(t, map[string]string{"word/styles.xml": "x"})
	if _, err := Parse(bytes.NewReader(data), int64(len(data))); err == nil {
		t.Fatal("missing document.xml was accepted")
	}
}

func TestContinuousDocumentStaysOnFirstPage(t *testing.T) {
	document := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
		`<w:p><w:r><w:t>第一段</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>第二段</w:t></w:r></w:p>` +
		`</w:body></w:document>`
	doc := parsePackage(t, packageBytes(t, map[string]string{"word/document.xml": document}))
	pages, _ := doc.Render(nil)
	if len(pages) != 1 || !strings.Contains(pages[0].Text, "第二段") {
		t.Fatalf("pages = %+v", pages)
	}
}

func TestRenderedPageBreakSplitsPages(t *testing.T) {
	document := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` +
		`<w:p><w:r><w:t>前</w:t><w:lastRenderedPageBreak/><w:t>后</w:t></w:r></w:p>` +
		`</w:body></w:document>`
	doc := parsePackage(t, packageBytes(t, map[string]string{"word/document.xml": document}))
	pages, _ := doc.Render(nil)
	if len(pages) != 2 || pages[0].Text != "前" || pages[1].Text != "后" {
		t.Fatalf("pages = %+v", pages)
	}
}

func TestNativeMathPreservesFractionScriptsAndRoots(t *testing.T) {
	document := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"><w:body><w:p>` +
		`<w:r><w:t>已知</w:t></w:r><m:oMath>` +
		`<m:f><m:num><m:sSup><m:e><m:r><m:t>x</m:t></m:r></m:e><m:sup><m:r><m:t>2</m:t></m:r></m:sup></m:sSup><m:r><m:t>+1</m:t></m:r></m:num>` +
		`<m:den><m:rad><m:deg/><m:e><m:r><m:t>y</m:t></m:r></m:e></m:rad></m:den></m:f>` +
		`</m:oMath><w:r><w:t>求值</w:t></w:r></w:p></w:body></w:document>`
	doc := parsePackage(t, packageBytes(t, map[string]string{"word/document.xml": document}))
	pages, markdown := doc.Render(nil)
	want := `已知\(\frac{{x}^{2}+1}{\sqrt{y}}\)求值`
	if len(pages) != 1 || pages[0].Text != want || markdown != want {
		t.Fatalf("formula text = %+v, markdown = %q", pages, markdown)
	}
}

func TestNativeMathPreservesLimitsDelimitersAndMatrices(t *testing.T) {
	document := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"><w:body><w:p><m:oMath>` +
		`<m:nary><m:naryPr><m:chr m:val="∑"/></m:naryPr><m:sub><m:r><m:t>i=1</m:t></m:r></m:sub><m:sup><m:r><m:t>n</m:t></m:r></m:sup><m:e><m:sSub><m:e><m:r><m:t>x</m:t></m:r></m:e><m:sub><m:r><m:t>i</m:t></m:r></m:sub></m:sSub></m:e></m:nary>` +
		`<m:d><m:dPr><m:begChr m:val="["/><m:endChr m:val="]"/></m:dPr><m:e><m:r><m:t>a+b</m:t></m:r></m:e></m:d>` +
		`<m:m><m:mr><m:e><m:r><m:t>a</m:t></m:r></m:e><m:e><m:r><m:t>b</m:t></m:r></m:e></m:mr><m:mr><m:e><m:r><m:t>c</m:t></m:r></m:e><m:e><m:r><m:t>d</m:t></m:r></m:e></m:mr></m:m>` +
		`</m:oMath></w:p></w:body></w:document>`
	doc := parsePackage(t, packageBytes(t, map[string]string{"word/document.xml": document}))
	pages, _ := doc.Render(nil)
	for _, want := range []string{`\sum_{i=1}^{n}{{x}_{i}}`, `[a+b]`, `\begin{matrix}a & b \\ c & d\end{matrix}`} {
		if len(pages) != 1 || !strings.Contains(pages[0].Text, want) {
			t.Fatalf("missing %q in %+v", want, pages)
		}
	}
}

func TestNativeMathRejectsExcessiveNesting(t *testing.T) {
	document := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"><w:body><w:p><m:oMath>` +
		strings.Repeat(`<m:e>`, maxMathDepth+2) + `<m:r><m:t>x</m:t></m:r>` + strings.Repeat(`</m:e>`, maxMathDepth+2) +
		`</m:oMath></w:p></w:body></w:document>`
	data := packageBytes(t, map[string]string{"word/document.xml": document})
	if _, err := Parse(bytes.NewReader(data), int64(len(data))); err == nil {
		t.Fatal("excessive formula nesting was accepted")
	}
}

func TestNativeMathRespectsHiddenPartsAndDefaultIntegral(t *testing.T) {
	document := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"><w:body><w:p><m:oMath>` +
		`<m:rad><m:radPr><m:degHide m:val="true"/></m:radPr><m:deg><m:r><m:t>3</m:t></m:r></m:deg><m:e><m:r><m:t>x</m:t></m:r></m:e></m:rad>` +
		`<m:nary><m:naryPr><m:subHide/><m:supHide m:val="0"/></m:naryPr><m:sub><m:r><m:t>0</m:t></m:r></m:sub><m:sup><m:r><m:t>1</m:t></m:r></m:sup><m:e><m:r><m:t>x</m:t></m:r></m:e></m:nary>` +
		`</m:oMath></w:p></w:body></w:document>`
	doc := parsePackage(t, packageBytes(t, map[string]string{"word/document.xml": document}))
	pages, _ := doc.Render(nil)
	want := `\(\sqrt{x}\int^{1}{x}\)`
	if len(pages) != 1 || pages[0].Text != want {
		t.Fatalf("hidden parts or default integral: %+v", pages)
	}
}

func TestNativeMathRejectsExcessiveText(t *testing.T) {
	document := `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math"><w:body><w:p><m:oMath><m:r><m:t>` +
		strings.Repeat("x", maxMathTextBytes+1) + `</m:t></m:r></m:oMath></w:p></w:body></w:document>`
	data := packageBytes(t, map[string]string{"word/document.xml": document})
	if _, err := Parse(bytes.NewReader(data), int64(len(data))); err == nil {
		t.Fatal("excessive formula text was accepted")
	}
}
