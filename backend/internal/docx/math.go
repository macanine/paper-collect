package docx

import (
	"encoding/xml"
	"io"
	"strings"

	"github.com/nic/paper-collect/backend/internal/apperr"
)

const (
	maxMathDepth     = 64
	maxMathNodes     = 10000
	maxMathTextBytes = 1 << 20
)

// mathNode represents only one bounded OMML formula, not the whole document.
type mathNode struct {
	name     xml.Name
	value    string
	text     string
	children []*mathNode
}

type mathBudget struct {
	nodes     int
	textBytes int
}

func readMathNode(dec *xml.Decoder, start xml.StartElement, depth int, budget *mathBudget) (*mathNode, error) {
	budget.nodes++
	if depth > maxMathDepth || budget.nodes > maxMathNodes {
		return nil, apperr.Unprocessable("Word 公式结构过大或嵌套过深")
	}
	node := &mathNode{name: start.Name, value: attribute(start, nsMath, "val")}
	var text strings.Builder
	for {
		token, err := dec.Token()
		if err != nil {
			if err == io.EOF {
				return nil, apperr.Unprocessable("Word 公式不完整")
			}
			return nil, apperr.Unprocessable("Word 公式损坏")
		}
		switch value := token.(type) {
		case xml.StartElement:
			child, err := readMathNode(dec, value, depth+1, budget)
			if err != nil {
				return nil, err
			}
			node.children = append(node.children, child)
		case xml.CharData:
			if isMath(start.Name, "t") || isWord(start.Name, "t") {
				budget.textBytes += len(value)
				if budget.textBytes > maxMathTextBytes {
					return nil, apperr.Unprocessable("Word 公式文本过大")
				}
				text.Write([]byte(value))
			}
		case xml.EndElement:
			if sameName(value.Name, start.Name) {
				node.text = text.String()
				return node, nil
			}
		}
	}
}

func (n *mathNode) child(local string) *mathNode {
	for _, child := range n.children {
		if isMath(child.name, local) {
			return child
		}
	}
	return nil
}

func (n *mathNode) property(container, property, fallback string) string {
	if n == nil {
		return fallback
	}
	group := n.child(container)
	if group == nil {
		return fallback
	}
	value := group.child(property)
	if value == nil {
		return fallback
	}
	return value.value
}

func (n *mathNode) flag(container, property string) bool {
	value := n.property(container, property, "false")
	return value == "" || value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "on")
}

func mathChild(n *mathNode, local string) string {
	if n == nil {
		return ""
	}
	return renderMath(n.child(local))
}

// renderMath preserves the structure of common OMML constructs as LaTeX.
// Unknown constructs retain their readable child text instead of inventing a
// formula; the original DOCX remains available for authoritative viewing.
func renderMath(n *mathNode) string {
	if n == nil {
		return ""
	}
	if isMath(n.name, "t") || isWord(n.name, "t") {
		return escapeMathText(n.text)
	}
	if n.name.Space == nsMath {
		switch n.name.Local {
		case "f":
			return `\frac{` + mathChild(n, "num") + `}{` + mathChild(n, "den") + `}`
		case "sSup":
			return `{` + mathChild(n, "e") + `}^{` + mathChild(n, "sup") + `}`
		case "sSub":
			return `{` + mathChild(n, "e") + `}_{` + mathChild(n, "sub") + `}`
		case "sSubSup":
			return `{` + mathChild(n, "e") + `}_{` + mathChild(n, "sub") + `}^{` + mathChild(n, "sup") + `}`
		case "rad":
			degree := mathChild(n, "deg")
			if n.flag("radPr", "degHide") {
				degree = ""
			}
			if degree != "" {
				return `\sqrt[` + degree + `]{` + mathChild(n, "e") + `}`
			}
			return `\sqrt{` + mathChild(n, "e") + `}`
		case "d":
			var parts []string
			for _, child := range n.children {
				if isMath(child.name, "e") {
					parts = append(parts, renderMath(child))
				}
			}
			begin := n.property("dPr", "begChr", "(")
			end := n.property("dPr", "endChr", ")")
			separator := n.property("dPr", "sepChr", "|")
			return begin + strings.Join(parts, separator) + end
		case "nary":
			symbol := n.property("naryPr", "chr", "∫")
			switch symbol {
			case "∑":
				symbol = `\sum`
			case "∏":
				symbol = `\prod`
			case "∫":
				symbol = `\int`
			}
			if !n.flag("naryPr", "subHide") {
				if sub := mathChild(n, "sub"); sub != "" {
					symbol += `_{` + sub + `}`
				}
			}
			if !n.flag("naryPr", "supHide") {
				if sup := mathChild(n, "sup"); sup != "" {
					symbol += `^{` + sup + `}`
				}
			}
			return symbol + `{` + mathChild(n, "e") + `}`
		case "limLow":
			return `{` + mathChild(n, "e") + `}_{` + mathChild(n, "lim") + `}`
		case "limUpp":
			return `{` + mathChild(n, "e") + `}^{` + mathChild(n, "lim") + `}`
		case "bar":
			command := `\overline`
			if n.property("barPr", "pos", "top") == "bot" {
				command = `\underline`
			}
			return command + `{` + mathChild(n, "e") + `}`
		case "m":
			var rows []string
			for _, row := range n.children {
				if !isMath(row.name, "mr") {
					continue
				}
				var cells []string
				for _, cell := range row.children {
					if isMath(cell.name, "e") {
						cells = append(cells, renderMath(cell))
					}
				}
				rows = append(rows, strings.Join(cells, " & "))
			}
			return `\begin{matrix}` + strings.Join(rows, ` \\ `) + `\end{matrix}`
		default:
			if strings.HasSuffix(n.name.Local, "Pr") {
				return ""
			}
		}
	}
	var result strings.Builder
	for _, child := range n.children {
		result.WriteString(renderMath(child))
	}
	return result.String()
}

func escapeMathText(text string) string {
	var result strings.Builder
	for _, r := range text {
		switch r {
		case '\\':
			result.WriteString(`\backslash `)
		case '{', '}', '%', '#', '&', '_', '$':
			result.WriteRune('\\')
			result.WriteRune(r)
		default:
			result.WriteRune(r)
		}
	}
	return result.String()
}
