// Package archive validates and expands ZIP packages safely.
// Inspect checks the whole central directory before any byte is extracted, so a
// hostile package never leaves partial output behind.
package archive

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nic/paper-collect/backend/internal/paths"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// Limits bound a single expansion.
type Limits struct {
	MaxTotalBytes int64   // declared uncompressed total of all non-ignored files
	MaxEntries    int     // non-ignored, non-directory entries
	MaxRatio      float64 // uncompressed / compressed per entry
}

// DefaultLimits are the production limits from the plan: 2 GiB, 5000 entries, ratio 200.
var DefaultLimits = Limits{
	MaxTotalBytes: 2 << 30,
	MaxEntries:    5000,
	MaxRatio:      200,
}

// Rejected means the package violates a rule. Reason is safe to show to users.
type Rejected struct{ Reason string }

func (r *Rejected) Error() string { return r.Reason }

func reject(format string, args ...any) error {
	return &Rejected{Reason: fmt.Sprintf(format, args...)}
}

// SupportedExtensions are the member types extracted from a package.
// Other entries are not extracted and are reported to the user.
var SupportedExtensions = []string{".pdf", ".doc", ".docx", ".pptx", ".zip"}

// Member is one extractable file inside a package.
type Member struct {
	Path  string // normalized path inside the package
	Index int    // index in zip.Reader.File
	Size  int64  // declared uncompressed size
}

// Inspect validates the full central directory and returns the supported members.
// It rejects traversal, absolute or backslash paths, symlinks, special files,
// encrypted entries, duplicate paths, bombs, and oversize or over-count packages.
func Inspect(zipPath string, lim Limits) ([]Member, error) {
	var skipped int
	return inspect(zipPath, lim, &skipped)
}

// InspectWithSkipped also counts non-ignored files whose formats are unsupported.
func InspectWithSkipped(zipPath string, lim Limits) ([]Member, int, error) {
	var skipped int
	members, err := inspect(zipPath, lim, &skipped)
	return members, skipped, err
}

func inspect(zipPath string, lim Limits, skipped *int) ([]Member, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, reject("压缩包结构损坏，无法读取")
	}
	defer zr.Close()

	var (
		members []Member
		seen    = make(map[string]bool, len(zr.File))
		total   int64
		count   int
	)
	decoded := decodeArchiveNames(zr.File)
	for i, f := range zr.File {
		name := decoded[i]
		if err := validateEntryName(name); err != nil {
			return nil, err
		}
		if paths.IsIgnored(name) {
			continue
		}
		if strings.HasSuffix(name, "/") {
			continue // directory entry
		}
		mode := f.Mode()
		if mode&fs.ModeSymlink != 0 {
			return nil, reject("压缩包包含符号链接，已拒绝")
		}
		if mode&(fs.ModeDevice|fs.ModeCharDevice|fs.ModeNamedPipe|fs.ModeSocket|fs.ModeIrregular) != 0 {
			return nil, reject("压缩包包含特殊文件，已拒绝")
		}
		if f.Flags&0x1 != 0 {
			return nil, reject("压缩包已加密，已拒绝")
		}
		if seen[name] {
			return nil, reject("压缩包包含重复路径，已拒绝")
		}
		seen[name] = true

		count++
		if count > lim.MaxEntries {
			return nil, reject("压缩包文件数超过 %d 条限制", lim.MaxEntries)
		}
		size := f.UncompressedSize64
		if size > uint64(lim.MaxTotalBytes) {
			return nil, reject("压缩包成员过大，已拒绝")
		}
		sz := int64(size)
		comp := f.CompressedSize64
		if sz > 0 {
			if comp == 0 || float64(sz)/float64(comp) > lim.MaxRatio {
				return nil, reject("压缩比超过 %.0f 的限制，疑似压缩炸弹", lim.MaxRatio)
			}
		}
		total += sz
		if total > lim.MaxTotalBytes {
			return nil, reject("压缩包展开总大小超过 2 GiB 限制")
		}
		if isSupported(name) {
			members = append(members, Member{Path: name, Index: i, Size: sz})
		} else {
			(*skipped)++
		}
	}
	return members, nil
}

// Extract streams each member through fn. fn must consume the reader fully.
// The reader is capped at the declared size and verified afterwards.
func Extract(zipPath string, members []Member, fn func(m Member, r io.Reader) error) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return reject("压缩包结构损坏，无法读取")
	}
	defer zr.Close()

	for _, m := range members {
		if m.Index < 0 || m.Index >= len(zr.File) {
			return reject("压缩包索引异常，已拒绝")
		}
		rc, err := zr.File[m.Index].Open()
		if err != nil {
			return reject("无法读取压缩包成员，压缩包可能损坏")
		}
		cr := &countingReader{r: io.LimitReader(rc, m.Size+1)}
		err = fn(m, cr)
		_ = rc.Close()
		if err != nil {
			if errors.Is(err, zip.ErrChecksum) || errors.Is(err, zip.ErrFormat) || errors.Is(err, zip.ErrAlgorithm) {
				return reject("压缩包成员校验失败，压缩包可能损坏")
			}
			return err
		}
		if cr.n != m.Size {
			return reject("压缩包成员实际大小与声明不符，已拒绝")
		}
	}
	return nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func validateEntryName(name string) error {
	if name == "" {
		return reject("压缩包包含空路径，已拒绝")
	}
	trimmed := strings.TrimSuffix(name, "/")
	if trimmed == "" {
		return reject("压缩包包含非法路径，已拒绝")
	}
	if len(name) > paths.MaxPathBytes {
		return reject("压缩包路径超过 1024 字节，已拒绝")
	}
	if strings.ContainsRune(name, '\\') {
		return reject("压缩包路径包含反斜杠，已拒绝")
	}
	if strings.HasPrefix(name, "/") {
		return reject("压缩包包含绝对路径，已拒绝")
	}
	if len(name) >= 2 && name[1] == ':' {
		return reject("压缩包包含盘符路径，已拒绝")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return reject("压缩包路径包含控制字符，已拒绝")
		}
	}
	for _, seg := range strings.Split(trimmed, "/") {
		switch seg {
		case "":
			return reject("压缩包路径包含空段，已拒绝")
		case ".", "..":
			return reject("压缩包包含路径穿越，已拒绝")
		}
	}
	return nil
}

func isSupported(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range SupportedExtensions {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// Legacy ZIP tools often store GBK/Big5/Shift-JIS filenames without the UTF-8
// flag. Rather than assuming one encoding, score every candidate over the
// whole archive and use the most plausible decoding consistently.
func decodeArchiveNames(files []*zip.File) []string {
	raw := make([]string, len(files))
	allUTF8 := true
	utf8Flag := false
	for i, f := range files {
		raw[i] = f.Name
		if !utf8.ValidString(f.Name) {
			allUTF8 = false
		}
		if f.Flags&0x800 != 0 {
			utf8Flag = true
		}
	}
	if allUTF8 && utf8Flag {
		return raw
	}
	type candidate struct {
		names []string
		score float64
		order int
	}
	candidates := make([]candidate, 0, 4)
	if allUTF8 {
		candidates = append(candidates, candidate{names: raw, score: scoreNames(raw), order: 0})
	}
	decoders := []struct {
		dec   *encoding.Decoder
		order int
	}{
		{simplifiedchinese.GB18030.NewDecoder(), 1},
		{traditionalchinese.Big5.NewDecoder(), 2},
		{japanese.ShiftJIS.NewDecoder(), 3},
	}
	for _, item := range decoders {
		decoded := make([]string, len(raw))
		ok := true
		for i, name := range raw {
			value, err := item.dec.String(name)
			if err != nil || !utf8.ValidString(value) {
				ok = false
				break
			}
			decoded[i] = value
		}
		if !ok {
			continue
		}
		candidates = append(candidates, candidate{names: decoded, score: scoreNames(decoded), order: item.order})
	}
	if len(candidates) == 0 {
		return raw
	}
	best := candidates[0]
	for _, cand := range candidates[1:] {
		if cand.score > best.score || (cand.score == best.score && cand.order < best.order) {
			best = cand
		}
	}
	if best.score < 0.2 {
		return raw
	}
	return best.names
}

// scoreNames rewards CJK/printable text and rejects names with replacement or
// control characters, so a wrong decoder cannot win on a short sample.
// commonHan is a compact frequency set for simplified and traditional Chinese
// plus common Japanese kanji. It breaks ties between legacy encodings that all
// produce valid (but mostly rare) Han characters for the wrong decoder.
var commonHan = func() map[rune]bool {
	const chars = "的一是在不了有和人这中大为上个国我以要他时来用们生到作地于出就分对成会可主发年动同工也能下过子说产种面而方后多定行学法所民得经十三之进着等部度家电力里如水化高自二理起小物现实加量都两体制机当使点从业本去把性好应开它合还因由其些然前外天政四日那社义事平形相全表间样与关各重新线内数正心反你明看原又么利比或但质气第向道命此变条只没结解问意建月公无系军很情者最立代想已通并提直题党程展五果料象员革位入常文总次品式活设及管特件长求老头基资边流路级少图山统接知较将组见计别她手角期根论运农指几九区强放决西被干做必战先回则任取据处队南给色光门即保治北造百规热领七海口东导器压志世金增争济阶油思术极交受联什认六共权收证改清己美再采转更单风切打白教速花带安场身车例真务具万每目至达走积示议声报斗完类八离华名确才科张信马节话米整空元况今集温传土许步群广石记需段研界拉林律叫且究观越织装影算低持音众书布复容儿须际商非验连断深难近矿千周委素技备半办青省列习响约支般史感劳便团往酸历市克何除消构府称太准精值号率族维划选标写存候毛亲快效斯院查江型眼王按格养易置派层片始却专状育厂京识适属圆包火住调满县局照参红细引听该铁价严龙飞"
	set := make(map[rune]bool, len(chars))
	for _, r := range chars {
		set[r] = true
	}
	return set
}()

func scoreNames(names []string) float64 {
	total := 0.0
	runes := 0
	for _, name := range names {
		for _, r := range name {
			runes++
			switch {
			case r == utf8.RuneError:
				return -1
			case r < 0x20 || r == 0x7f:
				return -1
			case unicode.Is(unicode.Han, r):
				total += 4
				if commonHan[r] {
					total += 3
				}
			case unicode.IsLetter(r) || unicode.IsDigit(r):
				total += 1
			case strings.ContainsRune("-_. ()[]{}【】、，。·—（）", r):
				total += 1
			case unicode.IsPrint(r):
				// neutral punctuation and symbols
			default:
				total -= 2
			}
		}
	}
	if runes == 0 {
		return 0
	}
	return total / float64(runes)
}
