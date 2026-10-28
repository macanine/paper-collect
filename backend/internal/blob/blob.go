// Package blob implements the local content-addressed store (CAS) and file-signature sniffing.
package blob

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Kinds of accepted content.
const (
	KindPDF  = "pdf"
	KindDOC  = "doc"
	KindDOCX = "docx"
	KindPPTX = "pptx"
	KindZIP  = "zip"
)

// MediaTypes maps kinds to the Content-Type served for them.
var MediaTypes = map[string]string{
	KindPDF:  "application/pdf",
	KindDOC:  "application/msword",
	KindDOCX: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	KindPPTX: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	KindZIP:  "application/zip",
}

// ErrUnknownContent means the bytes are not one of the accepted kinds.
var ErrUnknownContent = errors.New("blob: unrecognised file content")

// Store is the CAS rooted at DATA_DIR. Blobs live at blobs/<sha[0:2]>/<sha>.
type Store struct {
	dataDir string
}

// NewStore prepares the directory layout under dataDir.
func NewStore(dataDir string) (*Store, error) {
	for _, d := range []string{filepath.Join(dataDir, "blobs"), filepath.Join(dataDir, "tmp")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return nil, fmt.Errorf("创建数据目录失败: %w", err)
		}
	}
	return &Store{dataDir: dataDir}, nil
}

// DataDir returns the root data directory.
func (s *Store) DataDir() string { return s.dataDir }

// Path returns the CAS path for a SHA-256 hex digest.
func (s *Store) Path(sha string) string {
	return filepath.Join(s.dataDir, "blobs", sha[:2], sha)
}

// Exists reports whether a blob is present in the CAS.
func (s *Store) Exists(sha string) bool {
	st, err := os.Stat(s.Path(sha))
	return err == nil && st.Mode().IsRegular()
}

// NewTemp creates a private temporary file for streaming writes.
func (s *Store) NewTemp() (*os.File, error) {
	return os.CreateTemp(filepath.Join(s.dataDir, "tmp"), "stage-*.part")
}

// Publish moves a fully written and verified temporary file into the CAS atomically.
// If the blob already exists the temporary file is discarded: content is addressed by hash.
func (s *Store) Publish(tmpPath, sha string) error {
	dest := s.Path(sha)
	if s.Exists(sha) {
		return os.Remove(tmpPath)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

// CopyHashed streams src into dst and returns the byte count and SHA-256 hex digest.
func CopyHashed(dst io.Writer, src io.Reader) (int64, string, error) {
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, h), src)
	return n, hex.EncodeToString(h.Sum(nil)), err
}

// Sniff identifies the real content of a file from its header and, for OOXML,
// its ZIP central directory. The file extension is never trusted.
func Sniff(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	head := make([]byte, 1024)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", err
	}
	head = head[:n]

	// ZIP magic is checked before searching for "%PDF-": a small ZIP whose members are
	// stored uncompressed can contain PDF bytes inside its header window.
	switch {
	case bytes.HasPrefix(head, []byte("PK\x03\x04")) || bytes.HasPrefix(head, []byte("PK\x05\x06")):
		return sniffZIP(path)
	case bytes.Contains(head, []byte("%PDF-")):
		return KindPDF, nil
	case bytes.HasPrefix(head, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}):
		return KindDOC, nil
	default:
		return "", ErrUnknownContent
	}
}

// sniffZIP distinguishes OOXML documents from plain ZIP archives by entry names.
// It reads only the central directory and never decompresses members.
func sniffZIP(path string) (string, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return "", ErrUnknownContent
	}
	defer zr.Close()
	names := make(map[string]bool, len(zr.File))
	for _, f := range zr.File {
		names[f.Name] = true
	}
	if names["[Content_Types].xml"] {
		switch {
		case names["word/document.xml"]:
			return KindDOCX, nil
		case names["ppt/presentation.xml"]:
			return KindPPTX, nil
		}
	}
	return KindZIP, nil
}

// ExtensionKind maps an accepted lowercase extension to the content kind it requires.
func ExtensionKind(name string) (string, bool) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pdf":
		return KindPDF, true
	case ".doc":
		return KindDOC, true
	case ".docx":
		return KindDOCX, true
	case ".pptx":
		return KindPPTX, true
	case ".zip":
		return KindZIP, true
	}
	return "", false
}
