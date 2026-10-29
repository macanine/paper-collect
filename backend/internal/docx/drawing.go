package docx

import (
	"bytes"
	"encoding/xml"
	"io"
	"path"
	"strings"

	"github.com/nic/paper-collect/backend/internal/apperr"
)

const (
	maxImageBytes = 12 << 20
	maxImageTotal = 64 << 20
	maxImages     = 20
)

func (p *parser) parsePict(dec *xml.Decoder, text *strings.Builder, blocks *[]Block) error {
	return p.walkDrawing(dec, xml.Name{Space: nsWord, Local: "pict"}, text, blocks)
}

// walkDrawing descends through both DrawingML and VML containers. Only image
// references and text boxes contribute content; chart labels and shape names
// are ignored.
func (p *parser) walkDrawing(dec *xml.Decoder, end xml.Name, text *strings.Builder, blocks *[]Block) error {
	for {
		tok, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return apperr.Unprocessable("Word 文档图形不完整")
			}
			return apperr.Unprocessable("Word 文档图形损坏")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch {
			case isDrawing(t.Name, "blip"):
				if id := attribute(t, nsRel, "embed"); id != "" {
					if index, ok := p.addImage(id); ok {
						*blocks = append(*blocks, Block{Kind: KindImage, Image: index})
					}
				}
				if err := skip(dec); err != nil {
					return err
				}
			case isVML(t.Name, "imagedata"):
				if id := attribute(t, nsRel, "id"); id != "" {
					if index, ok := p.addImage(id); ok {
						*blocks = append(*blocks, Block{Kind: KindImage, Image: index})
					}
				}
				if err := skip(dec); err != nil {
					return err
				}
			case isWord(t.Name, "txbxContent"):
				if err := p.parseTextBox(dec, text, blocks); err != nil {
					return err
				}
			case isWord(t.Name, "p"):
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
			default:
				if err := p.walkDrawing(dec, t.Name, text, blocks); err != nil {
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

func (p *parser) addImage(relID string) (int, bool) {
	rel, ok := p.rels[relID]
	if !ok || rel.External {
		return 0, false
	}
	target, ok := p.pkg.Resolve("word/document.xml", rel.Target)
	if !ok {
		return 0, false
	}
	if index, ok := p.imageByTarget[target]; ok {
		return index, true
	}
	if len(p.images) >= maxImages {
		return 0, false
	}
	data, err := p.pkg.Read(target, maxImageBytes)
	if err != nil || p.imageBytes+len(data) > maxImageTotal {
		return 0, false
	}
	mediaType := imageMediaType(data)
	if mediaType == "" {
		return 0, false
	}
	index := len(p.images)
	p.images = append(p.images, Image{Name: path.Base(target), MediaType: mediaType, Data: data})
	p.imageByTarget[target] = index
	p.imageBytes += len(data)
	return index, true
}

func imageMediaType(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}):
		return "image/jpeg"
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return "image/gif"
	case bytes.HasPrefix(data, []byte("BM")):
		return "image/bmp"
	case bytes.HasPrefix(data, []byte("II*\x00")), bytes.HasPrefix(data, []byte("MM\x00*")):
		return "image/tiff"
	case len(data) >= 12 && bytes.HasPrefix(data, []byte("RIFF")) && string(data[8:12]) == "WEBP":
		return "image/webp"
	default:
		return ""
	}
}
