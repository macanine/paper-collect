package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatIdentifiesOutputLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"finish_reason": "length",
				"message":       map[string]any{"content": ""},
			}},
		})
	}))
	defer server.Close()

	client, err := NewOpenAI(server.URL, "", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Chat(context.Background(), "test-model", []ChatMessage{TextMessage("user", "test")})
	if err == nil || !strings.Contains(err.Error(), "输出达到上限") {
		t.Fatalf("error = %v", err)
	}
}
