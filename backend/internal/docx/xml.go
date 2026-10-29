package docx

import (
	"encoding/xml"
	"strings"
)

const (
	nsWord       = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	nsWordStrict = "http://purl.oclc.org/ooxml/wordprocessingml/main"
	nsMath       = "http://schemas.openxmlformats.org/officeDocument/2006/math"
	nsDrawing    = "http://schemas.openxmlformats.org/drawingml/2006/main"
	nsRel        = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	nsVML        = "urn:schemas-microsoft-com:vml"
)

func isWord(name xml.Name, local string) bool {
	return name.Local == local && (name.Space == nsWord || name.Space == nsWordStrict)
}

func isMath(name xml.Name, local string) bool {
	return name.Local == local && name.Space == nsMath
}

func isDrawing(name xml.Name, local string) bool {
	return name.Local == local && name.Space == nsDrawing
}

func isVML(name xml.Name, local string) bool {
	return name.Local == local && name.Space == nsVML
}

func attribute(start xml.StartElement, space, local string) string {
	for _, attr := range start.Attr {
		if attr.Name.Local == local && attr.Name.Space == space {
			return attr.Value
		}
	}
	return ""
}

func wordAttribute(start xml.StartElement, local string) string {
	for _, space := range []string{nsWord, nsWordStrict} {
		if value := attribute(start, space, local); value != "" {
			return value
		}
	}
	return ""
}

func sameName(name xml.Name, want xml.Name) bool {
	return name.Local == want.Local && name.Space == want.Space
}

func cleanText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r >= 0x20 {
			return r
		}
		return -1
	}, value)
	return strings.TrimSpace(value)
}
