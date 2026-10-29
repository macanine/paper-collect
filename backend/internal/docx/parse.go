package docx

import (
	"bytes"
	"encoding/xml"
	"io"
	"strconv"
	"strings"

	"github.com/nic/paper-collect/backend/internal/apperr"
	"github.com/nic/paper-collect/backend/internal/ooxml"
)

const (
	maxDocumentXML = 64 << 20
	maxTextBytes   = 64 << 20
	maxBlocks      = 200000
)

type parser struct {
	pkg           *ooxml.Package
	rels          map[string]ooxml.Relationship
	pages         []Page
	images        []Image
	imageByTarget map[string]int
	imageBytes    int
	textBytes     int
	blockCount    int
}

func (p *parser) parse(body []byte) error {
	return p.parseBody(xml.NewDecoder(bytes.NewReader(body)))
}

func (p *parser) parseBody(dec *xml.Decoder) error {
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return apperr.Unprocessable("Word 文档正文损坏")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch {
			case isWord(t.Name, "document"), isWord(t.Name, "body"):
				// Descend; the token loop handles the children.
			default:
				if err := p.parseBodyElement(dec, t); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if isWord(t.Name, "body") {
				return nil
			}
		}
	}
}

func (p *parser) parseBodyElement(dec *xml.Decoder, start xml.StartElement) error {
	switch {
	case isWord(start.Name, "p"):
		blocks, err := p.parseParagraph(dec)
		if err != nil {
			return err
		}
		return p.emit(blocks)
	case isWord(start.Name, "tbl"):
		blocks, err := p.parseTable(dec)
		if err != nil {
			return err
		}
		return p.emit(blocks)
	case isWord(start.Name, "sdt"), isWord(start.Name, "sdtContent"),
		isWord(start.Name, "customXml"), isWord(start.Name, "ins"), isWord(start.Name, "moveTo"):
		return p.parseBodyChildren(dec, start.Name)
	case isWord(start.Name, "del"), isWord(start.Name, "moveFrom"):
		return dec.Skip()
	default:
		return dec.Skip()
	}
}

func (p *parser) parseBodyChildren(dec *xml.Decoder, end xml.Name) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return apperr.Unprocessable("Word 文档正文不完整")
			}
			return apperr.Unprocessable("Word 文档正文损坏")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if err := p.parseBodyElement(dec, t); err != nil {
				return err
			}
		case xml.EndElement:
			if sameName(t.Name, end) {
				return nil
			}
		}
	}
}

func (p *parser) parseParagraph(dec *xml.Decoder) ([]Block, error) {
	var text strings.Builder
	var blocks []Block
	heading := 0
	flush := func() {
		value := cleanText(text.String())
		text.Reset()
		if value == "" {
			return
		}
		blocks = append(blocks, Block{Kind: KindText, Text: value, Heading: heading})
		heading = 0
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return nil, apperr.Unprocessable("Word 文档段落不完整")
			}
			return nil, apperr.Unprocessable("Word 文档正文损坏")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch {
			case isWord(t.Name, "pPr"):
				level, breakBefore, err := p.parseParagraphProperties(dec)
				if err != nil {
					return nil, err
				}
				if breakBefore {
					blocks = append(blocks, Block{Kind: KindPageBreak})
				}
				heading = level
			case isWord(t.Name, "p"):
				nested, err := p.parseParagraph(dec)
				if err != nil {
					return nil, err
				}
				appendBlockText(&text, nested)
			case isWord(t.Name, "tbl"):
				nested, err := p.parseTable(dec)
				if err != nil {
					return nil, err
				}
				appendBlockText(&text, nested)
			default:
				if err := p.parseInline(dec, t, &text, &blocks, flush); err != nil {
					return nil, err
				}
			}
		case xml.EndElement:
			if isWord(t.Name, "p") {
				flush()
				return blocks, nil
			}
		}
	}
}

func (p *parser) parseParagraphProperties(dec *xml.Decoder) (heading int, pageBreak bool, err error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return 0, false, apperr.Unprocessable("Word 文档段落属性损坏")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch {
			case isWord(t.Name, "pStyle"):
				var value struct {
					Val string `xml:"val,attr"`
				}
				if err := dec.DecodeElement(&value, &t); err != nil {
					return 0, false, apperr.Unprocessable("Word 文档段落属性损坏")
				}
				heading = headingFromStyle(value.Val)
			case isWord(t.Name, "outlineLvl"):
				var value struct {
					Val string `xml:"val,attr"`
				}
				if err := dec.DecodeElement(&value, &t); err != nil {
					return 0, false, apperr.Unprocessable("Word 文档段落属性损坏")
				}
				if level, convErr := strconv.Atoi(strings.TrimSpace(value.Val)); convErr == nil {
					heading = level + 1
					if heading < 1 {
						heading = 1
					}
					if heading > 6 {
						heading = 6
					}
				}
			case isWord(t.Name, "pageBreakBefore"):
				var value struct {
					Val string `xml:"val,attr"`
				}
				if err := dec.DecodeElement(&value, &t); err != nil {
					return 0, false, apperr.Unprocessable("Word 文档段落属性损坏")
				}
				pageBreak = value.Val == "" || value.Val == "1" || strings.EqualFold(value.Val, "true")
			default:
				if err := dec.Skip(); err != nil {
					return 0, false, apperr.Unprocessable("Word 文档段落属性损坏")
				}
			}
		case xml.EndElement:
			if isWord(t.Name, "pPr") {
				return heading, pageBreak, nil
			}
		}
	}
}

func headingFromStyle(style string) int {
	lower := strings.ToLower(strings.TrimSpace(style))
	for i := 1; i <= 6; i++ {
		number := strconv.Itoa(i)
		if strings.Contains(lower, "heading"+number) || strings.Contains(lower, "heading "+number) ||
			strings.Contains(style, "标题"+number) || strings.Contains(style, "标题 "+number) {
			return i
		}
	}
	if lower == "title" {
		return 1
	}
	return 0
}

func (p *parser) parseInline(dec *xml.Decoder, start xml.StartElement, text *strings.Builder, blocks *[]Block, flush func()) error {
	switch {
	case isWord(start.Name, "t"), isMath(start.Name, "t"):
		var value string
		if err := dec.DecodeElement(&value, &start); err != nil {
			return apperr.Unprocessable("Word 文档文本损坏")
		}
		text.WriteString(value)
	case isWord(start.Name, "tab"):
		text.WriteString("\t")
		return skip(dec)
	case isWord(start.Name, "br"):
		var value struct {
			Type string `xml:"type,attr"`
		}
		if err := dec.DecodeElement(&value, &start); err != nil {
			return apperr.Unprocessable("Word 文档换行损坏")
		}
		if value.Type == "page" {
			flush()
			*blocks = append(*blocks, Block{Kind: KindPageBreak})
		} else {
			text.WriteString("\n")
		}
	case isWord(start.Name, "cr"):
		text.WriteString("\n")
		return skip(dec)
	case isWord(start.Name, "noBreakHyphen"):
		text.WriteString("-")
		return skip(dec)
	case isWord(start.Name, "lastRenderedPageBreak"):
		flush()
		*blocks = append(*blocks, Block{Kind: KindPageBreak})
		return skip(dec)
	case isWord(start.Name, "drawing"), isWord(start.Name, "object"):
		return p.walkDrawing(dec, start.Name, text, blocks)
	case isWord(start.Name, "pict"):
		return p.parsePict(dec, text, blocks)
	case isWord(start.Name, "txbxContent"):
		return p.parseTextBox(dec, text, blocks)
	case isWord(start.Name, "p"):
		nested, err := p.parseParagraph(dec)
		if err != nil {
			return err
		}
		appendBlockText(text, nested)
		for _, block := range nested {
			if block.Kind == KindImage {
				*blocks = append(*blocks, block)
			}
		}
	case isWord(start.Name, "tbl"):
		nested, err := p.parseTable(dec)
		if err != nil {
			return err
		}
		appendBlockText(text, nested)
		for _, block := range nested {
			if block.Kind == KindImage {
				*blocks = append(*blocks, block)
			}
		}
	case isWord(start.Name, "r"), isWord(start.Name, "hyperlink"), isWord(start.Name, "ins"),
		isWord(start.Name, "moveTo"), isWord(start.Name, "smartTag"), isWord(start.Name, "fldSimple"),
		isWord(start.Name, "sdt"), isWord(start.Name, "sdtContent"), isWord(start.Name, "customXml"),
		isWord(start.Name, "dir"), isWord(start.Name, "bdo"),
		isMath(start.Name, "oMath"), isMath(start.Name, "oMathPara"):
		return p.parseInlineChildren(dec, start.Name, text, blocks, flush)
	case start.Name.Space == nsMath:
		return p.parseInlineChildren(dec, start.Name, text, blocks, flush)
	case isWord(start.Name, "del"), isWord(start.Name, "moveFrom"), isWord(start.Name, "instrText"),
		isWord(start.Name, "delText"), isWord(start.Name, "pPr"), isWord(start.Name, "rPr"),
		isWord(start.Name, "sectPr"), isWord(start.Name, "footnoteReference"),
		isWord(start.Name, "endnoteReference"), isWord(start.Name, "commentReference"),
		isWord(start.Name, "bookmarkStart"), isWord(start.Name, "bookmarkEnd"),
		isWord(start.Name, "proofErr"), isWord(start.Name, "permStart"), isWord(start.Name, "permEnd"):
		return skip(dec)
	default:
		return skip(dec)
	}
	return nil
}

func (p *parser) parseInlineChildren(dec *xml.Decoder, end xml.Name, text *strings.Builder, blocks *[]Block, flush func()) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return apperr.Unprocessable("Word 文档段落不完整")
			}
			return apperr.Unprocessable("Word 文档正文损坏")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if err := p.parseInline(dec, t, text, blocks, flush); err != nil {
				return err
			}
		case xml.EndElement:
			if sameName(t.Name, end) {
				return nil
			}
		}
	}
}

func (p *parser) parseTextBox(dec *xml.Decoder, text *strings.Builder, blocks *[]Block) error {
	var content strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return apperr.Unprocessable("Word 文档文本框不完整")
			}
			return apperr.Unprocessable("Word 文档文本框损坏")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch {
			case isWord(t.Name, "p"):
				nested, err := p.parseParagraph(dec)
				if err != nil {
					return err
				}
				appendBlockText(&content, nested)
				for _, block := range nested {
					if block.Kind == KindImage {
						*blocks = append(*blocks, block)
					}
				}
			case isWord(t.Name, "tbl"):
				nested, err := p.parseTable(dec)
				if err != nil {
					return err
				}
				appendBlockText(&content, nested)
				for _, block := range nested {
					if block.Kind == KindImage {
						*blocks = append(*blocks, block)
					}
				}
			default:
				if err := dec.Skip(); err != nil {
					return apperr.Unprocessable("Word 文档文本框损坏")
				}
			}
		case xml.EndElement:
			if isWord(t.Name, "txbxContent") {
				value := cleanText(content.String())
				if value == "" {
					return nil
				}
				if text.Len() > 0 {
					text.WriteString("\n")
				}
				text.WriteString(value)
				return nil
			}
		}
	}
}

func (p *parser) emit(blocks []Block) error {
	for _, block := range blocks {
		switch block.Kind {
		case KindPageBreak:
			p.pageBreak()
		case KindText:
			value := cleanText(block.Text)
			if value == "" {
				continue
			}
			if p.textBytes+len(value) > maxTextBytes {
				return apperr.TooLarge("Word 文档正文过大")
			}
			p.textBytes += len(value)
			block.Text = value
			page := p.currentPage()
			page.Blocks = append(page.Blocks, block)
			p.blockCount++
		case KindImage:
			if block.Image < 0 || block.Image >= len(p.images) {
				continue
			}
			page := p.currentPage()
			p.images[block.Image].Page = page.Number
			page.Blocks = append(page.Blocks, block)
			p.blockCount++
		}
		if p.blockCount > maxBlocks {
			return apperr.TooLarge("Word 文档结构过于复杂")
		}
	}
	return nil
}

func (p *parser) currentPage() *Page {
	return &p.pages[len(p.pages)-1]
}

func (p *parser) pageBreak() {
	if len(p.currentPage().Blocks) == 0 {
		return
	}
	p.pages = append(p.pages, Page{Number: p.currentPage().Number + 1})
}

func (p *parser) trimTrailingEmptyPages() {
	for len(p.pages) > 0 && len(p.pages[len(p.pages)-1].Blocks) == 0 {
		p.pages = p.pages[:len(p.pages)-1]
	}
}

func appendBlockText(text *strings.Builder, blocks []Block) {
	for _, block := range blocks {
		if block.Kind != KindText || block.Text == "" {
			continue
		}
		if text.Len() > 0 {
			text.WriteString("\n")
		}
		text.WriteString(block.Text)
	}
}

func skip(dec *xml.Decoder) error {
	if err := dec.Skip(); err != nil {
		return apperr.Unprocessable("Word 文档正文损坏")
	}
	return nil
}
