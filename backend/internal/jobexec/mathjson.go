package jobexec

import "strings"

func asciiLetter(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// repairJSONMathCommands protects LaTeX commands present in the supplied OCR
// text when a model emits them with a single backslash in a JSON string. In
// particular, JSON otherwise accepts \f, \t and \n as control characters and
// silently corrupts commands such as \frac, \theta and \neq. Properly escaped
// commands and ordinary JSON escapes are left unchanged.
func repairJSONMathCommands(raw, source string) string {
	commands := map[string]bool{}
	for i := 0; i+1 < len(source); i++ {
		if source[i] != '\\' || !asciiLetter(source[i+1]) {
			continue
		}
		end := i + 1
		for end < len(source) && asciiLetter(source[end]) {
			end++
		}
		if end > i+2 {
			commands[source[i+1:end]] = true
		}
		i = end - 1
	}
	if len(commands) == 0 {
		return raw
	}
	var out strings.Builder
	out.Grow(len(raw))
	inString := false
	for i := 0; i < len(raw); {
		if !inString {
			if raw[i] == '"' {
				inString = true
			}
			out.WriteByte(raw[i])
			i++
			continue
		}
		if raw[i] == '"' {
			inString = false
			out.WriteByte(raw[i])
			i++
			continue
		}
		if raw[i] != '\\' || i+1 >= len(raw) {
			out.WriteByte(raw[i])
			i++
			continue
		}
		if raw[i+1] == '\\' || raw[i+1] == '"' {
			out.WriteString(raw[i : i+2])
			i += 2
			continue
		}
		if asciiLetter(raw[i+1]) {
			end := i + 1
			for end < len(raw) && asciiLetter(raw[end]) {
				end++
			}
			if commands[raw[i+1:end]] {
				out.WriteByte('\\')
				out.WriteString(raw[i:end])
				i = end
				continue
			}
		}
		out.WriteString(raw[i : i+2])
		i += 2
	}
	return out.String()
}

func hasInvalidFormulaControls(text, source string) bool {
	for _, r := range text {
		if r < 0x20 && r != '\n' && r != '\r' && r != '\t' {
			return true
		}
	}
	// These are characteristic signs of an unescaped LaTeX command being
	// interpreted as a valid JSON control escape. Allow control sequences that
	// existed in the input, and do not mistake an English line beginning with
	// "equation" for a broken \neq command.
	for _, damaged := range []string{"\theta", "\times", "\text", "\neq", "\nabla", "\notin", "\right", "\rho"} {
		if strings.Contains(source, damaged) {
			continue
		}
		for offset := 0; offset < len(text); {
			match := strings.Index(text[offset:], damaged)
			if match < 0 {
				break
			}
			end := offset + match + len(damaged)
			if end == len(text) || !asciiLetter(text[end]) {
				return true
			}
			offset = end
		}
	}
	return false
}
