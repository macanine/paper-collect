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
