package jobexec

import (
	"fmt"
	"testing"

	"github.com/nic/paper-collect/backend/internal/ai"
)

// An exhausted OCR quota must not be settled as "this file cannot be read":
// that would catalogue the file by its name alone for good.
func TestClassifyQuotaWaitsInsteadOfFailing(t *testing.T) {
	quota := fmt.Errorf("parse: %w", &ai.APIError{Class: "quota", Detail: "已达每日页数上限"})
	if got := Classify(quota); got != ClassConfig {
		t.Fatalf("quota = %q, want %q", got, ClassConfig)
	}
	if got := Classify(&ai.APIError{Class: "invalid_response", Detail: "bad"}); got != ClassPermanent {
		t.Fatalf("invalid response = %q, want %q", got, ClassPermanent)
	}
}
