// Package paths holds the relative-path rules shared by upload manifests and ZIP members,
// plus the natural ordering used for collection positions.
package paths

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxPathBytes is the maximum UTF-8 byte length of a relative path.
const MaxPathBytes = 1024

// ValidateFile checks a file path: forward slashes, no absolute prefix, no backslash,
// no ".." or "." segments, no empty segments, valid UTF-8, no control characters,
// and at most MaxPathBytes bytes.
func ValidateFile(p string) error {
	if p == "" {
		return errors.New("路径不能为空")
	}
	if len(p) > MaxPathBytes {
		return errors.New("路径超过 1024 字节")
	}
	if !utf8.ValidString(p) {
		return errors.New("路径不是有效的 UTF-8")
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return errors.New("路径包含控制字符")
		}
	}
	if strings.ContainsRune(p, '\\') {
		return errors.New("路径不能包含反斜杠")
	}
	if strings.HasPrefix(p, "/") {
		return errors.New("路径不能是绝对路径")
	}
	if len(p) >= 2 && p[1] == ':' && isASCIILetter(p[0]) {
		return errors.New("路径不能包含盘符")
	}
	if strings.HasSuffix(p, "/") {
		return errors.New("文件路径不能以斜杠结尾")
	}
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "":
			return errors.New("路径包含空段")
		case ".", "..":
			return errors.New("路径不能包含 . 或 .. 段")
		}
	}
	return nil
}

// IsIgnored reports whether an archive entry is skipped entirely: .DS_Store,
// AppleDouble "._" files, and anything under __MACOSX.
func IsIgnored(p string) bool {
	segs := strings.Split(strings.TrimSuffix(p, "/"), "/")
	base := segs[len(segs)-1]
	if base == ".DS_Store" || strings.HasPrefix(base, "._") {
		return true
	}
	for _, s := range segs {
		if s == "__MACOSX" {
			return true
		}
	}
	return false
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// NaturalLess orders strings so that digit runs compare numerically:
// "2.pdf" < "10.pdf". Other runs compare case-insensitively (per rune), then exactly.
func NaturalLess(a, b string) bool {
	ra, rb := []rune(a), []rune(b)
	i, j := 0, 0
	for i < len(ra) && j < len(rb) {
		ca, cb := ra[i], rb[j]
		if isDigit(ca) && isDigit(cb) {
			si := i
			for i < len(ra) && isDigit(ra[i]) {
				i++
			}
			sj := j
			for j < len(rb) && isDigit(rb[j]) {
				j++
			}
			na := strings.TrimLeft(string(ra[si:i]), "0")
			nb := strings.TrimLeft(string(rb[sj:j]), "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			continue
		}
		la, lb := unicode.ToLower(ca), unicode.ToLower(cb)
		if la != lb {
			return la < lb
		}
		i++
		j++
	}
	if len(ra)-i != len(rb)-j {
		return len(ra)-i < len(rb)-j
	}
	return a < b
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }
