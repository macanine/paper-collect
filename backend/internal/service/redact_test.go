package service

import (
	"strings"
	"testing"
)

func TestRedactOpaqueAndLegacyAPIKeys(t *testing.T) {
	newKey := "sk-" + strings.Repeat("a", 43)
	oldKey := "pc_12345678-1234-1234-1234-123456789abc." + strings.Repeat("b", 43)
	got := redact("diagnostic " + newKey + " " + oldKey)
	if strings.Contains(got, newKey) || strings.Contains(got, oldKey) || !strings.Contains(got, "<api-key>") {
		t.Fatal("raw API key was not redacted")
	}
}
