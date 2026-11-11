package catalogai

import (
	"strings"
	"testing"
)

func decideInput() DecideInput {
	return DecideInput{Members: []DecideMember{{
		ID: "f1", FileName: "[物理] 2610-物理试卷-8页A4-带水印.pdf", PageCount: 8,
		Excerpt: "标准学术能力诊断性测试2026年10月测试\n物理试卷\n2. 估算阿伏伽德罗常数 C. 氧分子的体积 浙江物D. 标况下氧气的摩尔体积",
		Card:    Card{Role: "questions", TitleText: "标准学术能力诊断性测试2026年10月测试"},
	}}}
}

func regionOf(t *testing.T, reply string) (string, []Problem) {
	t.Helper()
	d, problems, err := CheckDecide(reply, decideInput())
	if err != nil {
		t.Fatal(err)
	}
	return d.Region, problems
}

const decideBase = `"doc_type":"gaokao_mock","subjects":["physics"],"grade":"grade12","files":[{"id":"f1","role":"questions"}]`

func TestRegionNeedsEvidenceThatIsNotAStrayFragment(t *testing.T) {
	// The watermark fragment "浙江物" is in the material but is too short to be evidence.
	region, problems := regionOf(t, `{`+decideBase+`,"region":"浙江","evidence":{"region":"浙江物"}}`)
	if region != "" {
		t.Fatalf("region %q accepted on a watermark fragment", region)
	}
	found := false
	for _, p := range problems {
		found = found || (p.Field == "region" && p.Code == "evidence")
	}
	if !found {
		t.Fatalf("no region evidence problem: %+v", problems)
	}
	// No evidence at all.
	if region, _ := regionOf(t, `{`+decideBase+`,"region":"浙江"}`); region != "" {
		t.Fatalf("region %q accepted without evidence", region)
	}
	// A quote that is not in the material.
	if region, _ := regionOf(t, `{`+decideBase+`,"region":"浙江","evidence":{"region":"浙江省教育厅统一组织的考试"}}`); region != "" {
		t.Fatalf("region %q accepted with an invented quote", region)
	}
}

func TestRegionAcceptedWithQuoteFromTheMaterial(t *testing.T) {
	in := decideInput()
	in.Members[0].FileName = "2611浙江嘉兴高三一模技术试卷.pdf"
	reply := `{` + decideBase + `,"region":"浙江","evidence":{"region":"2611浙江嘉兴高三一模技术试卷"}}`
	d, _, err := CheckDecide(reply, in)
	if err != nil {
		t.Fatal(err)
	}
	if d.Region != "浙江" {
		t.Fatalf("region = %q", d.Region)
	}
	// Leaving region empty (a national exam) is always fine.
	if region, _ := regionOf(t, `{`+decideBase+`}`); region != "" {
		t.Fatalf("unexpected region %q", region)
	}
}

func TestDecidePromptIsWellFormed(t *testing.T) {
	messages := DecideMessages(decideInput())
	system, _ := messages[0].Content.(string)
	if strings.Contains(system, "%!") || strings.Contains(system, "%s") {
		t.Fatalf("prompt has a formatting error: %.200s", system)
	}
	for _, want := range []string{"analysis", "TDA", "evidence.region"} {
		if !strings.Contains(system, want) {
			t.Fatalf("prompt lacks %q", want)
		}
	}
}
