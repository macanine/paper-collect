package storage

import (
	"net/url"
	"regexp"
	"strings"
)

// 蓝奏页面解析，移植自 gaogao src/ops/cloud/lanzou/page.ts。全部为纯函数。
// RE2 不支持反向引用与 >1000 的重复次数，因此对 JS 正则做了等价改写（见各函数注释）。

const lanzouMaxHTMLBytes = 2 << 20

func trimHTML(html string) string {
	if len(html) > lanzouMaxHTMLBytes {
		return html[:lanzouMaxHTMLBytes]
	}
	return html
}

var (
	iframeFnRe  = regexp.MustCompile(`(?i)<iframe[^>]+src=["']([^"']*/fn\?[^"']+)["']`)
	dataBlockRe = regexp.MustCompile(`(?i)\bdata\s*:\s*\{([^}]*)\}`)
	// JS 原式：(['"]?)([A-Za-z_$][\w$]*)\1\s*:... 依赖反向引用。改写为三种键形式的交替，语义等价于配对引号或无引号。
	dataPairRe     = regexp.MustCompile(`(?:'([A-Za-z_$][\w$]*)'|"([A-Za-z_$][\w$]*)"|([A-Za-z_$][\w$]*))\s*:\s*(?:'([^']*)'|"([^"]*)"|([^,}\s]+))`)
	ajaxEndpointRe = regexp.MustCompile(`((?:https?://[^'"\s]+)?/ajax(?:m|file)\.php\?file=\d+)`)
	urlFieldRe     = regexp.MustCompile(`url\s*:\s*['"]([^'"]+)['"]`)
	uidRe          = regexp.MustCompile(`uid=([^'"&;\s]+)`)
	loginPageRe    = regexp.MustCompile(`(?i)accounts\.woozooo\.com/accounts\.php|网盘用户登录|请先登录`)
	passwordDivRe  = regexp.MustCompile(`(?i)pwdload|passwddiv`)
	absoluteHTTPRe = regexp.MustCompile(`(?i)^https?://`)
	numberRe       = regexp.MustCompile(`^\d+$`)
)

func extractIframeFnPath(html string) string {
	if m := iframeFnRe.FindStringSubmatch(trimHTML(html)); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func extractJSStringVar(html, variable string) (string, bool) {
	re := regexp.MustCompile(`(?i)var\s+` + regexp.QuoteMeta(variable) + `\s*=\s*['"]([^'"]*)['"]`)
	if m := re.FindStringSubmatch(trimHTML(html)); m != nil {
		return m[1], true
	}
	return "", false
}

// buildAbsoluteURL 按 base 解析相对地址（等价于 new URL(path, base)）。
func buildAbsoluteURL(base, pathOrURL string) (string, bool) {
	if pathOrURL == "" {
		return "", false
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", false
	}
	ref, err := url.Parse(pathOrURL)
	if err != nil {
		return "", false
	}
	return baseURL.ResolveReference(ref).String(), true
}

func buildDirectDownloadURL(dom, filePath, suffix string) (string, bool) {
	normalizedDom := strings.TrimRight(strings.TrimSpace(dom), "/")
	normalizedPath := strings.TrimLeft(strings.TrimSpace(filePath), "/")
	if normalizedDom == "" || normalizedPath == "" {
		return "", false
	}
	return normalizedDom + "/file/" + normalizedPath + suffix, true
}

func extractUID(html string) string {
	if m := uidRe.FindStringSubmatch(trimHTML(html)); m != nil {
		return m[1]
	}
	return ""
}

func looksLikeLoginPage(html string) bool {
	return loginPageRe.MatchString(trimHTML(html))
}

// stripJsComments 去掉 JS 行注释与块注释，保留字符串内的 // 与 /*。
// 分享页的注释中可能包含 data 块，需先排除再解析有效参数。
// 按字节处理：注释与引号都是 ASCII，多字节 UTF-8 序列不会与之冲突。
func stripJsComments(source string) string {
	var out strings.Builder
	out.Grow(len(source))
	n := len(source)
	inBlock := false
	inLine := false
	var quote byte
	for i := 0; i < n; {
		c := source[i]
		var next byte
		if i+1 < n {
			next = source[i+1]
		}
		if inLine {
			if c == '\n' {
				inLine = false
				out.WriteByte(c)
			}
			i++
			continue
		}
		if inBlock {
			if c == '*' && next == '/' {
				inBlock = false
				i += 2
				continue
			}
			i++
			continue
		}
		if quote != 0 {
			out.WriteByte(c)
			if c == '\\' && i+1 < n {
				out.WriteByte(next)
				i += 2
				continue
			}
			if c == quote {
				quote = 0
			}
			i++
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			out.WriteByte(c)
			i++
			continue
		}
		if c == '/' && next == '*' {
			inBlock = true
			i += 2
			continue
		}
		if c == '/' && next == '/' {
			inLine = true
			i += 2
			continue
		}
		out.WriteByte(c)
		i++
	}
	return out.String()
}

// extractJSVarValue 取 JS 变量最后一次非空赋值（蓝奏页面会先写空串再写真实值）。
func extractJSVarValue(html, variable string) (string, bool) {
	re := regexp.MustCompile(`var\s+` + regexp.QuoteMeta(variable) + `\s*=\s*(?:'([^']*)'|"([^"]*)"|([^;\s]+))\s*;`)
	last := ""
	for _, m := range re.FindAllStringSubmatch(trimHTML(html), -1) {
		value := m[1]
		if value == "" {
			value = m[2]
		}
		if value == "" {
			value = m[3]
		}
		if value != "" {
			last = value
		}
	}
	return last, last != ""
}

type downprocessParams struct {
	ajaxURL string
	fields  map[string]string
}

// parseDataBlocks 解析页面中所有 data:{...} 块，裸标识符按同页 var 赋值替换。
// 只支持单层对象（与参考实现一致）。
func parseDataBlocks(source string) []map[string]string {
	var blocks []map[string]string
	for _, block := range dataBlockRe.FindAllStringSubmatch(source, -1) {
		body := block[1]
		fields := map[string]string{}
		for _, pair := range dataPairRe.FindAllStringSubmatch(body, -1) {
			key := pair[1]
			if key == "" {
				key = pair[2]
			}
			if key == "" {
				key = pair[3]
			}
			if key == "" {
				continue
			}
			// 值为引号形式时 pair[6]（裸值）为空；引号值允许为空串。
			bare := pair[6]
			if bare == "" {
				fields[key] = pair[4] + pair[5]
				continue
			}
			if numberRe.MatchString(bare) {
				fields[key] = bare
				continue
			}
			if value, ok := extractJSVarValue(source, bare); ok {
				fields[key] = value
			} else {
				fields[key] = bare
			}
		}
		if len(fields) > 0 {
			blocks = append(blocks, fields)
		}
	}
	return blocks
}

func findAjaxEndpoint(source, baseURL string) (string, bool) {
	for _, m := range ajaxEndpointRe.FindAllStringSubmatch(source, -1) {
		raw := m[1]
		if strings.HasPrefix(raw, "http") {
			return raw, true
		}
		path := raw
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		if resolved, ok := buildAbsoluteURL(baseURL, path); ok {
			return resolved, true
		}
	}
	return "", false
}

// extractDownprocessParams 找分享页或 fn 页里生效的 downprocess 参数与 ajax 地址。
func extractDownprocessParams(html, baseURL string) (downprocessParams, bool) {
	source := stripJsComments(trimHTML(html))
	ajaxURL, ok := findAjaxEndpoint(source, baseURL)
	if !ok {
		return downprocessParams{}, false
	}
	for _, fields := range parseDataBlocks(source) {
		if strings.EqualFold(fields["action"], "downprocess") {
			return downprocessParams{ajaxURL: ajaxURL, fields: fields}, true
		}
	}
	return downprocessParams{}, false
}

// extractDownloadVerifyParams 直链返回“验证并下载”页时，提取 file/sign 与相对 ajax 地址。
func extractDownloadVerifyParams(html, baseURL string) (downprocessParams, bool) {
	source := stripJsComments(trimHTML(html))
	m := urlFieldRe.FindStringSubmatch(source)
	if m == nil {
		return downprocessParams{}, false
	}
	ajaxURL, ok := buildAbsoluteURL(baseURL, m[1])
	if !ok {
		return downprocessParams{}, false
	}
	for _, fields := range parseDataBlocks(source) {
		_, hasFile := fields["file"]
		_, hasSign := fields["sign"]
		if hasFile && hasSign {
			fields["el"] = "2"
			return downprocessParams{ajaxURL: ajaxURL, fields: fields}, true
		}
	}
	return downprocessParams{}, false
}
