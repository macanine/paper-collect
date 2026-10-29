package docx

import (
	"encoding/xml"
	"io"
	"strings"

	"github.com/nic/paper-collect/backend/internal/apperr"
)

func (p *parser) parseTable(dec *xml.Decoder) ([]Block, error) {
	var rows [][]string
	var images []Block
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return nil, apperr.Unprocessable("Word 文档表格不完整")
			}
			return nil, apperr.Unprocessable("Word 文档表格损坏")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch {
			case isWord(t.Name, "tr"):
				cells, nested, err := p.parseRow(dec)
				if err != nil {
					return nil, err
				}
				rows = append(rows, cells)
				images = append(images, nested...)
			case isWord(t.Name, "tblPr"), isWord(t.Name, "tblGrid"), isWord(t.Name, "tblPrEx"):
				if err := skip(dec); err != nil {
					return nil, err
				}
			default:
				if err := skip(dec); err != nil {
					return nil, err
				}
			}
		case xml.EndElement:
			if isWord(t.Name, "tbl") {
				blocks := make([]Block, 0, 1+len(images))
				if text := renderTable(rows); text != "" {
					blocks = append(blocks, Block{Kind: KindText, Text: text})
				}
				blocks = append(blocks, images...)
				return blocks, nil
			}
		}
	}
}

func (p *parser) parseRow(dec *xml.Decoder) ([]string, []Block, error) {
	var cells []string
	var images []Block
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return nil, nil, apperr.Unprocessable("Word 文档表格行不完整")
			}
			return nil, nil, apperr.Unprocessable("Word 文档表格损坏")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch {
			case isWord(t.Name, "tc"):
				text, nested, err := p.parseCell(dec)
				if err != nil {
					return nil, nil, err
				}
				cells = append(cells, text)
				images = append(images, nested...)
			default:
				if err := skip(dec); err != nil {
					return nil, nil, err
				}
			}
		case xml.EndElement:
			if isWord(t.Name, "tr") {
				return cells, images, nil
			}
		}
	}
}

func (p *parser) parseCell(dec *xml.Decoder) (string, []Block, error) {
	var parts []string
	var images []Block
	if err := p.parseCellContent(dec, xml.Name{Space: nsWord, Local: "tc"}, &parts, &images); err != nil {
		return "", nil, err
	}
	return strings.Join(parts, "\n"), images, nil
}

func (p *parser) parseCellContent(dec *xml.Decoder, end xml.Name, parts *[]string, images *[]Block) error {
	appendBlocks := func(blocks []Block) {
		for _, block := range blocks {
			switch block.Kind {
			case KindText:
				if block.Text != "" {
					*parts = append(*parts, block.Text)
				}
			case KindImage:
				*images = append(*images, block)
			}
		}
	}
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return apperr.Unprocessable("Word 文档单元格不完整")
			}
			return apperr.Unprocessable("Word 文档表格损坏")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch {
			case isWord(t.Name, "p"):
				blocks, err := p.parseParagraph(dec)
				if err != nil {
					return err
				}
				appendBlocks(blocks)
			case isWord(t.Name, "tbl"):
				blocks, err := p.parseTable(dec)
				if err != nil {
					return err
				}
				appendBlocks(blocks)
			case isWord(t.Name, "sdt"), isWord(t.Name, "sdtContent"),
				isWord(t.Name, "customXml"), isWord(t.Name, "ins"), isWord(t.Name, "moveTo"):
				if err := p.parseCellContent(dec, t.Name, parts, images); err != nil {
					return err
				}
			case isWord(t.Name, "del"), isWord(t.Name, "moveFrom"):
				if err := skip(dec); err != nil {
					return err
				}
			default:
				if err := skip(dec); err != nil {
					return err
				}
			}
		case xml.EndElement:
			if sameName(t.Name, end) {
				return nil
			}
		}
	}
}

func renderTable(rows [][]string) string {
	columns := 0
	hasContent := false
	for _, row := range rows {
		if len(row) > columns {
			columns = len(row)
		}
		for _, cell := range row {
			if strings.TrimSpace(cell) != "" {
				hasContent = true
			}
		}
	}
	if columns == 0 || !hasContent {
		return ""
	}
	var out strings.Builder
	writeRow := func(row []string) {
		out.WriteString("|")
		for i := 0; i < columns; i++ {
			value := ""
			if i < len(row) {
				value = escapeCell(row[i])
			}
			out.WriteString(" ")
			out.WriteString(value)
			out.WriteString(" |")
		}
		out.WriteString("\n")
	}
	writeRow(rows[0])
	out.WriteString("|")
	for i := 0; i < columns; i++ {
		out.WriteString(" --- |")
	}
	out.WriteString("\n")
	for _, row := range rows[1:] {
		writeRow(row)
	}
	return strings.TrimSpace(out.String())
}

func escapeCell(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\n", "<br>")
	return strings.TrimSpace(value)
}
