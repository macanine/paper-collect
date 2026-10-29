package auth

import (
	"strings"
	"testing"
)

func TestOpaqueAPIKeyFormatAndParsing(t *testing.T) {
	id, full, hash, err := NewAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(full, "sk-") || strings.Contains(full, id) {
		t.Fatal("key should be opaque")
	}
	parsedID, secret, err := ParseAPIKey(full)
	if err != nil || parsedID != "" || HashToken(secret) != hash {
		t.Fatal("failed to parse new key")
	}
	legacyID, legacySecret, err := ParseAPIKey("pc_" + id + ".legacy-test-secret")
	if err != nil || legacyID != id || legacySecret != "legacy-test-secret" {
		t.Fatal("legacy key must remain valid")
	}
	for _, invalid := range []string{"sk-", "sk-abc", full + "=", "pc_invalid.secret", "unknown"} {
		if _, _, err := ParseAPIKey(invalid); err == nil {
			t.Fatal("malformed credential accepted")
		}
	}
}
