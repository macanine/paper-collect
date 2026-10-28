package archive

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

func writeZip(t *testing.T, names []string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "a.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	zw := zip.NewWriter(f)
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create: %v", err)
		}
		if _, err := w.Write([]byte("x")); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return path
}

func encodedName(t *testing.T, enc interface{ String(string) (string, error) }, name string) string {
	t.Helper()
	out, err := enc.String(name)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return out
}

func TestInspectDecodesLegacyZipNames(t *testing.T) {
	cases := []struct {
		name string
		src  string
		enc  func() interface{ String(string) (string, error) }
	}{
		{"GBK", "化学/教师版.pdf", func() interface{ String(string) (string, error) } { return simplifiedchinese.GBK.NewEncoder() }},
		{"Big5", "化學/教師版.pdf", func() interface{ String(string) (string, error) } { return traditionalchinese.Big5.NewEncoder() }},
		{"ShiftJIS", "数学/教師版.pdf", func() interface{ String(string) (string, error) } { return japanese.ShiftJIS.NewEncoder() }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := encodedName(t, tc.enc(), tc.src)
			path := writeZip(t, []string{raw})
			members, err := Inspect(path, DefaultLimits)
			if err != nil {
				t.Fatalf("inspect: %v", err)
			}
			if len(members) != 1 || members[0].Path != tc.src {
				t.Fatalf("decoded members: %+v, want %q", members, tc.src)
			}
		})
	}
}

func TestInspectKeepsUTF8Names(t *testing.T) {
	path := writeZip(t, []string{"数学/学生版.pdf"})
	members, err := Inspect(path, DefaultLimits)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if len(members) != 1 || members[0].Path != "数学/学生版.pdf" {
		t.Fatalf("members: %+v", members)
	}
}
