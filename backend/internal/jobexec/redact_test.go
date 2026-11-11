package jobexec

import (
	"strings"
	"testing"
)

func TestRedactOpaqueAPIKeys(t *testing.T) {
	newKey := "sk-" + strings.Repeat("a", 43)
	got := Redact("diagnostic " + newKey)
	if strings.Contains(got, newKey) || !strings.Contains(got, "<api-key>") {
		t.Fatal("raw API key was not redacted")
	}
}
