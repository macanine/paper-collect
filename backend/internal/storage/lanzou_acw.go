package storage

import (
	"fmt"
	"regexp"
	"strings"
)

// 蓝奏 acw_sc__v2 反爬挑战，算法与参考实现 computeAcwScV2 一致：
// 按 40 项置换表重排 arg1，再与固定 key 逐字节 XOR。
var acwPermutation = [40]int{
	0xf, 0x23, 0x1d, 0x18, 0x21, 0x10, 0x1, 0x26, 0xa, 0x9, 0x13, 0x1f, 0x28,
	0x1b, 0x16, 0x17, 0x19, 0xd, 0x6, 0xb, 0x27, 0x12, 0x14, 0x8, 0xe, 0x15,
	0x20, 0x1a, 0x2, 0x1e, 0x7, 0x4, 0x11, 0x5, 0x3, 0x1c, 0x22, 0x25, 0xc,
	0x24,
}

const acwKey = "3000176000856006061501533003690027800375"

var acwArg1Re = regexp.MustCompile(`var\s+arg1\s*=\s*['"]([0-9A-Fa-f]+)['"]`)

// extractAcwArg1 从页面中取出 arg1；不存在时返回空串。
func extractAcwArg1(html string) string {
	if m := acwArg1Re.FindStringSubmatch(trimHTML(html)); m != nil {
		return m[1]
	}
	return ""
}

// computeAcwScV2 返回 40 字符十六进制结果；arg1 非法时返回空串（由调用方归类）。
func computeAcwScV2(arg1 string) string {
	if arg1 == "" || len(arg1)%2 != 0 {
		return ""
	}
	// 与 JS 数组语义一致：只有被置换表命中的位置有值，join 时跳过空洞。
	slots := make([]byte, len(acwPermutation))
	filled := make([]bool, len(acwPermutation))
	for i := 0; i < len(arg1); i++ {
		for index, position := range acwPermutation {
			if position == i+1 {
				slots[index] = arg1[i]
				filled[index] = true
				break
			}
		}
	}
	permuted := make([]byte, 0, len(acwPermutation))
	for index := range slots {
		if filled[index] {
			permuted = append(permuted, slots[index])
		}
	}

	n := min(len(permuted), len(acwKey))
	var out strings.Builder
	for i := 0; i < n; i += 2 {
		left, ok := parseHexPrefix(permuted[i:min(i+2, len(permuted))])
		if !ok {
			return ""
		}
		right, ok := parseHexPrefix([]byte(acwKey[i:min(i+2, len(acwKey))]))
		if !ok {
			return ""
		}
		out.WriteString(fmt.Sprintf("%02x", left^right))
	}
	return out.String()
}

// parseHexPrefix 与 JS parseInt(s, 16) 对齐：读取开头的十六进制数字，没有则失败。
func parseHexPrefix(s []byte) (int, bool) {
	value := 0
	digits := 0
	for _, c := range s {
		var d int
		switch {
		case c >= '0' && c <= '9':
			d = int(c - '0')
		case c >= 'a' && c <= 'f':
			d = int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = int(c-'A') + 10
		default:
			return value, digits > 0
		}
		value = value*16 + d
		digits++
	}
	return value, digits > 0
}
