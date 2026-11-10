package server_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestReviewPacksArePagedBySuite(t *testing.T) {
	env := newEnv(t)
	uploadID, _ := env.upload(t, "套卷待确认", map[string][]byte{
		"同卷/试题.pdf": pdfWith("questions"),
		"同卷/答案.pdf": pdfWith("answers"),
	})
	env.finalizeAndWait(t, uploadID)

	status, body := env.do(t, http.MethodGet, "/api/v1/materials/review-packs?stage=review&page=1&page_size=100", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("review packs %d: %s", status, body)
	}
	var page struct {
		Data struct {
			Total int `json:"total"`
			Items []struct {
				Pack struct {
					Members []struct {
						Stage string `json:"stage"`
					} `json:"members"`
				} `json:"pack"`
				Items []struct {
					ID string `json:"id"`
				} `json:"items"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("review pack body: %s", body)
	}
	if page.Data.Total != 1 || len(page.Data.Items) != 1 {
		t.Fatalf("expected one suite, got total=%d items=%d: %s", page.Data.Total, len(page.Data.Items), body)
	}
	item := page.Data.Items[0]
	if len(item.Pack.Members) != 2 || len(item.Items) != 2 {
		t.Fatalf("suite response must include both members and both review items: %s", body)
	}
	for _, member := range item.Pack.Members {
		if member.Stage != "review" {
			t.Fatalf("member stage = %q, want review: %s", member.Stage, body)
		}
	}
}
