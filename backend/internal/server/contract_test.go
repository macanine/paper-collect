package server

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The checked-in TypeScript contract must match the DTOs.
func TestTypeScriptContractIsCurrent(t *testing.T) {
	want, err := TypeScriptContract()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "src", "shared", "api", "contract.gen.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("web/src/shared/api/contract.gen.ts is stale; run `make contract`")
	}
}
