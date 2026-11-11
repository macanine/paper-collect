package catalogai

import "testing"

func TestCheckCardRejectsFullPaperWithAnswersOnPageOne(t *testing.T) {
	in := CardInput{FileName: "数学.pdf", PageCount: 6, Document: "[第 1 页]\n一、选择题\n\n[第 5 页]\n参考答案"}
	reply := `{"role":"full","answer_start_page":1}`
	_, problems, err := CheckCard(reply, in)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range problems {
		if p.Code == "answer_page_first" {
			found = true
		}
	}
	if !found {
		t.Fatalf("page 1 answer start was accepted: %+v", problems)
	}
	card, problems, _ := CheckCard(`{"role":"full","answer_start_page":5}`, in)
	if card.AnswerStartPage != 5 {
		t.Fatalf("page 5 was rejected: %+v %+v", card, problems)
	}
}
