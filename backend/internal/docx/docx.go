// Package docx extracts text, tables, page breaks and images from Word
// documents without a renderer. It reads the OOXML parts directly so parsing
// does not depend on LibreOffice, a PDF round trip or an OCR text layer.
package docx

import (
	"fmt"
	"io"
	"strings"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/ooxml"
)

// BlockKind distinguishes page content.
type BlockKind int

const (
	KindText BlockKind = iota
	KindImage
	KindPageBreak
)

// Block is one paragraph, table, image or page break.
type Block struct {
	Kind    BlockKind
	Text    string
	Heading int // 1..6 for headings, 0 otherwise
	Image   int // index into Document.Images
}

// Page is a logical page. DOCX pagination comes from explicit page breaks and
// Word's last-rendered page-break hints; continuous documents stay on page 1.
type Page struct {
	Number int
	Blocks []Block
}

// Image is an embedded raster image that the caller may OCR. Data is bounded.
type Image struct {
	Name      string
	MediaType string
	Data      []byte
	Page      int
}

// Document is the parsed Word body.
type Document struct {
	Pages  []Page
	Images []Image
}

// PageText is one page after image OCR text has been merged in.
type PageText struct {
	PageNo int
	Text   string
}

// Parse reads a DOCX package from readerAt.
func Parse(readerAt io.ReaderAt, size int64) (*Document, error) {
	pkg, err := ooxml.New(readerAt, size)
	if err != nil {
		return nil, err
	}
	return parse(pkg)
}

// ParseFile reads a DOCX package from disk.
func ParseFile(name string) (*Document, error) {
	pkg, err := ooxml.Open(name)
	if err != nil {
		return nil, err
	}
	defer pkg.Close()
	return parse(pkg)
}

func parse(pkg *ooxml.Package) (*Document, error) {
	body, err := pkg.Read("word/document.xml", maxDocumentXML)
	if err != nil {
		return nil, apperr.Unprocessable("Word 文档缺少可读取的正文")
	}
	rels, err := pkg.Relationships("word/document.xml")
	if err != nil {
		return nil, err
	}
	p := &parser{
		pkg:           pkg,
		rels:          make(map[string]ooxml.Relationship, len(rels)),
		pages:         []Page{{Number: 1}},
		imageByTarget: map[string]int{},
	}
	for _, rel := range rels {
		p.rels[rel.ID] = rel
	}
	if err := p.parse(body); err != nil {
		return nil, err
	}
	p.trimTrailingEmptyPages()
	return &Document{Pages: p.pages, Images: p.images}, nil
}

// Render merges image OCR text into page blocks and returns page text plus a
// Markdown view for analysis. Pages that only contain failed images are
// omitted; original page numbers are preserved.
func (d *Document) Render(imageText map[int]string) ([]PageText, string) {
	pages := make([]PageText, 0, len(d.Pages))
	var markdown strings.Builder
	for _, page := range d.Pages {
		parts := make([]string, 0, len(page.Blocks))
		for _, block := range page.Blocks {
			switch block.Kind {
			case KindText:
				if block.Text != "" {
					parts = append(parts, headingPrefix(block.Heading)+block.Text)
				}
			case KindImage:
				if text := strings.TrimSpace(imageText[block.Image]); text != "" {
					parts = append(parts, text)
				}
			}
		}
		text := strings.TrimSpace(strings.Join(parts, "\n\n"))
		if text == "" {
			continue
		}
		pages = append(pages, PageText{PageNo: page.Number, Text: text})
		if len(d.Pages) > 1 {
			fmt.Fprintf(&markdown, "## 第 %d 页\n\n", page.Number)
		}
		markdown.WriteString(text)
		markdown.WriteString("\n\n")
	}
	return pages, strings.TrimSpace(markdown.String())
}

func headingPrefix(level int) string {
	if level < 1 || level > 6 {
		return ""
	}
	return strings.Repeat("#", level) + " "
}
