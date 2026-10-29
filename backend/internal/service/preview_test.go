package service

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/nic/paper-collect/backend/internal/blob"
	"github.com/nic/paper-collect/backend/internal/model"
)

type fakePreviewConverter struct {
	pdf string
	sha string
}

func (f *fakePreviewConverter) PDF(_ context.Context, sha string, _ func(context.Context) (io.ReadCloser, error)) (string, error) {
	f.sha = sha
	return f.pdf, nil
}

func (f *fakePreviewConverter) Remove(string) {}

func TestDOCXPreviewUsesConverterWithoutLibreOffice(t *testing.T) {
	svc, db := newWorkerTestService(t, t.TempDir(), "preview")
	path := t.TempDir() + "/preview.pdf"
	if err := os.WriteFile(path, []byte("%PDF-1.4 preview"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakePreviewConverter{pdf: path}
	svc.office = fake
	material := model.Material{
		ID: "material", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		FileName: "试卷.docx", MediaType: blob.MediaTypes[blob.KindDOCX], SizeBytes: 10,
	}
	if err := db.Create(&material).Error; err != nil {
		t.Fatal(err)
	}
	download, err := svc.OpenMaterialPreview(t.Context(), material.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer download.Reader.Close()
	data, err := io.ReadAll(download.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "%PDF-1.4 preview" || download.MediaType != "application/pdf" || download.FileName != "preview.pdf" {
		t.Fatalf("download = %+v, data = %q", download, data)
	}
	if fake.sha != material.SHA256 {
		t.Fatalf("converter received sha %q", fake.sha)
	}
}
