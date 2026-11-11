package ooxml

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func archive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func openBytes(t *testing.T, data []byte) (*Package, error) {
	t.Helper()
	return newPackage(bytes.NewReader(data), int64(len(data)), true)
}

func openBytesAllowingExternal(t *testing.T, data []byte) (*Package, error) {
	t.Helper()
	return NewAllowingExternal(bytes.NewReader(data), int64(len(data)))
}

func TestRejectsExternalResourcesAndAllowsHyperlinks(t *testing.T) {
	for _, tc := range []struct {
		name, kind, mode string
		wantError        bool
	}{
		{"external image", "image", "External", true},
		{"external template", "attachedTemplate", "External", true},
		{"hyperlink", "hyperlink", "External", false},
		{"internal image", "image", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := archive(t, map[string]string{
				"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="r1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/` + tc.kind + `" Target="resource" TargetMode="` + tc.mode + `"/></Relationships>`,
			})
			_, err := openBytes(t, data)
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v, wantError %v", err, tc.wantError)
			}
			if err != nil && strings.Contains(err.Error(), "resource") {
				t.Fatal("external target was exposed")
			}
		})
	}
}

func TestInertParserCanOpenExternalRelationships(t *testing.T) {
	data := archive(t, map[string]string{
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="r1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="https://example.test/image.png" TargetMode="External"/>` +
			`<Relationship Id="r2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.test/" TargetMode="External"/>` +
			`</Relationships>`,
	})
	pkg, err := openBytesAllowingExternal(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer pkg.Close()
	relationships, err := pkg.Relationships("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	if len(relationships) != 2 {
		t.Fatalf("relationships = %+v", relationships)
	}
}

func TestSanitizeExternalResources(t *testing.T) {
	data := archive(t, map[string]string{
		"word/document.xml": "body",
		"word/_rels/document.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="image" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="https://example.test/image.png" TargetMode="External"/>` +
			`<Relationship Id="hyperlink" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.test/" TargetMode="External"/>` +
			`<Relationship Id="embedded" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/image1.png"/>` +
			`</Relationships>`,
	})
	dir := t.TempDir()
	source := filepath.Join(dir, "source.docx")
	dest := filepath.Join(dir, "clean.docx")
	if err := os.WriteFile(source, data, 0o600); err != nil {
		t.Fatal(err)
	}
	removed, err := SanitizeExternalResources(source, dest)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	pkg, err := Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer pkg.Close()
	relationships, err := pkg.Relationships("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	if len(relationships) != 2 || relationships[0].ID != "hyperlink" || relationships[1].ID != "embedded" {
		t.Fatalf("relationships = %+v", relationships)
	}
}

func TestRejectsTraversalAndDuplicates(t *testing.T) {
	for _, name := range []string{"../evil", "word/../../evil", "/absolute", `word\evil`} {
		data := archive(t, map[string]string{name: "x"})
		if _, err := openBytes(t, data); err == nil {
			t.Fatalf("path %q was accepted", name)
		}
	}

	data := archive(t, map[string]string{
		"word/document.xml":   "a",
		"word/./document.xml": "b",
	})
	// zip.Writer keeps both names; path.Clean maps them to the same part.
	if _, err := openBytes(t, data); err == nil {
		t.Fatal("duplicate normalized path was accepted")
	}
}

func TestResolveRelativePartAndLimit(t *testing.T) {
	data := archive(t, map[string]string{
		"word/document.xml":   "body",
		"customXml/item1.xml": "item",
	})
	pkg, err := openBytes(t, data)
	if err != nil {
		t.Fatal(err)
	}
	defer pkg.Close()
	got, ok := pkg.Resolve("word/document.xml", "../customXml/item1.xml")
	if !ok || got != "customXml/item1.xml" {
		t.Fatalf("resolve = %q, %v", got, ok)
	}
	if _, ok := pkg.Resolve("word/document.xml", "../../escape"); ok {
		t.Fatal("escaping target was resolved")
	}
	if _, err := pkg.Read("word/document.xml", 3); err == nil {
		t.Fatal("limit was not enforced")
	}
}
