// Package ooxml reads and validates Office Open XML packages (DOCX, PPTX).
// Validation happens before any caller reads a part, so a hostile package
// never reaches the document parsers.
package ooxml

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/nic/paper-collect/backend/internal/apperr"
)

const (
	maxEntries       = 10000
	maxExpanded      = 2 << 30
	maxRelationships = 8 << 20
)

// Package is a validated OPC/ZIP container.
type Package struct {
	files map[string]*zip.File
	close func() error
}

// Open validates and opens a package from disk.
func Open(name string) (*Package, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, apperr.Internal(err)
	}
	pkg, err := New(f, info.Size())
	if err != nil {
		f.Close()
		return nil, err
	}
	pkg.close = f.Close
	return pkg, nil
}

// New validates a package read from readerAt. The caller retains ownership of
// the reader and must keep it open until it is done with the package.
func New(readerAt io.ReaderAt, size int64) (*Package, error) {
	zr, err := zip.NewReader(readerAt, size)
	if err != nil {
		return nil, apperr.Unprocessable("文档不是有效的 Office 压缩包")
	}
	if len(zr.File) > maxEntries {
		return nil, apperr.TooLarge("文档包含过多内部文件")
	}
	pkg := &Package{files: make(map[string]*zip.File, len(zr.File))}
	var total uint64
	for _, f := range zr.File {
		name := path.Clean(f.Name)
		if invalidName(f.Name, name) {
			return nil, apperr.Unprocessable("文档包含无效的内部文件路径")
		}
		if f.Flags&1 != 0 {
			return nil, apperr.Unprocessable("文档已加密，无法预览或解析")
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return nil, apperr.Unprocessable("文档包含符号链接")
		}
		if f.FileInfo().IsDir() {
			continue
		}
		if _, exists := pkg.files[name]; exists {
			return nil, apperr.Unprocessable("文档包含重复的内部文件路径")
		}
		if f.UncompressedSize64 > maxExpanded || total > maxExpanded-f.UncompressedSize64 {
			return nil, apperr.TooLarge("文档解压后的内容超过限制")
		}
		total += f.UncompressedSize64
		pkg.files[name] = f
	}
	if err := pkg.validateRelationships(); err != nil {
		return nil, err
	}
	return pkg, nil
}

// Close releases the file opened by Open.
func (p *Package) Close() error {
	if p == nil || p.close == nil {
		return nil
	}
	return p.close()
}

// Entry returns a package part by its normalized name.
func (p *Package) Entry(name string) (*zip.File, bool) {
	f, ok := p.files[path.Clean(name)]
	return f, ok
}

// Read returns a part, bounded by limit.
func (p *Package) Read(name string, limit int64) ([]byte, error) {
	f, ok := p.Entry(name)
	if !ok {
		return nil, apperr.Unprocessable("文档缺少内部文件")
	}
	if limit > 0 && f.UncompressedSize64 > uint64(limit) {
		return nil, apperr.TooLarge("文档内部文件过大")
	}
	rc, err := f.Open()
	if err != nil {
		return nil, apperr.Unprocessable("文档内部文件损坏")
	}
	defer rc.Close()
	if limit <= 0 {
		data, err := io.ReadAll(rc)
		if err != nil {
			return nil, apperr.Unprocessable("读取文档内部文件失败")
		}
		return data, nil
	}
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, apperr.Unprocessable("读取文档内部文件失败")
	}
	if limit > 0 && int64(len(data)) > limit {
		return nil, apperr.TooLarge("文档内部文件过大")
	}
	return data, nil
}

// Relationship is one OPC relationship.
type Relationship struct {
	ID       string
	Type     string
	Target   string
	External bool
}

type relationshipXML struct {
	ID         string `xml:"Id,attr"`
	Type       string `xml:"Type,attr"`
	Target     string `xml:"Target,attr"`
	TargetMode string `xml:"TargetMode,attr"`
}

// Relationships returns the relationships declared for a part. A missing
// relationship file is not an error.
func (p *Package) Relationships(part string) ([]Relationship, error) {
	name := relsName(part)
	f, ok := p.files[name]
	if !ok {
		return nil, nil
	}
	rels, err := readRelationships(f)
	if err != nil {
		return nil, err
	}
	return rels, nil
}

// Resolve maps an internal relationship target to a package part name.
func (p *Package) Resolve(part, target string) (string, bool) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", false
	}
	absolute := strings.HasPrefix(target, "/")
	target = strings.TrimPrefix(target, "/")
	var clean string
	if absolute {
		clean = path.Clean(target)
	} else {
		clean = path.Clean(path.Join(path.Dir(part), target))
	}
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return "", false
	}
	if _, ok := p.files[clean]; ok {
		return clean, true
	}
	if unescaped, err := url.PathUnescape(clean); err == nil {
		if _, ok := p.files[unescaped]; ok {
			return unescaped, true
		}
	}
	return "", false
}

// Validate opens and validates a package without retaining it.
func Validate(name string) error {
	pkg, err := Open(name)
	if err != nil {
		return err
	}
	return pkg.Close()
}

func invalidName(raw, clean string) bool {
	if raw == "" || clean == "." || clean == ".." {
		return true
	}
	if strings.ContainsRune(raw, '\\') || strings.HasPrefix(raw, "/") {
		return true
	}
	if len(raw) >= 2 && raw[1] == ':' {
		return true
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	if strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") {
		return true
	}
	return false
}

func relsName(part string) string {
	if part == "" {
		return "_rels/.rels"
	}
	return path.Join(path.Dir(part), "_rels", path.Base(part)+".rels")
}

func (p *Package) validateRelationships() error {
	for name, f := range p.files {
		if !strings.HasSuffix(strings.ToLower(name), ".rels") {
			continue
		}
		rels, err := readRelationships(f)
		if err != nil {
			return apperr.Unprocessable("文档内部关系文件损坏")
		}
		for _, rel := range rels {
			// Templates, images, OLE objects and data sources must be embedded.
			// Clickable hyperlinks are inert and remain allowed.
			if rel.External && !strings.HasSuffix(strings.ToLower(rel.Type), "/hyperlink") {
				return apperr.Unprocessable("文档包含外部引用资源，请嵌入资源后重试，或下载原件查看")
			}
		}
	}
	return nil
}

func readRelationships(f *zip.File) ([]Relationship, error) {
	if f.UncompressedSize64 > maxRelationships {
		return nil, apperr.TooLarge("文档内部关系文件过大")
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var doc struct {
		Items []relationshipXML `xml:"Relationship"`
	}
	if err := xml.NewDecoder(io.LimitReader(rc, maxRelationships+1)).Decode(&doc); err != nil {
		return nil, err
	}
	rels := make([]Relationship, 0, len(doc.Items))
	for _, item := range doc.Items {
		rels = append(rels, Relationship{
			ID:       item.ID,
			Type:     item.Type,
			Target:   item.Target,
			External: strings.EqualFold(item.TargetMode, "External"),
		})
	}
	return rels, nil
}
