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
	for _, want := range []string{"analysis", "TDA", "evidence.region", "gaokao_real", "chapter"} {
		if !strings.Contains(system, want) {
			t.Fatalf("prompt lacks %q", want)
		}
	}
}

func TestGaokaoRealKeepsNoOrganization(t *testing.T) {
	reply := `{"doc_type":"gaokao_real","subjects":["chemistry"],"grade":"grade12",` +
		`"organization":{"new":"教育部教育考试院"},"files":[{"id":"f1","role":"questions"}]}`
	d, problems, err := CheckDecide(reply, decideInput())
	if err != nil {
		t.Fatal(err)
	}
	if d.Organization != nil {
		t.Fatalf("gaokao_real recorded organization %+v", d.Organization)
	}
	for _, p := range problems {
		if p.Field == "organization" {
			t.Fatalf("gaokao_real reported an organization problem: %+v", p)
		}
	}
}

func topicInput() DecideInput {
	in := decideInput()
	in.Members[0] = DecideMember{
		ID: "f1", FileName: "专题练习 高三 化学 专题44 水的电离与溶液的pH 试题卷.docx", PageCount: 6,
		Excerpt: "专题44 水的电离与溶液的pH\n1. 常温下，0.1 mol/L 盐酸中由水电离出的 c(H+) 为多少",
		Card:    Card{Role: "questions"},
	}
	return in
}

func TestTopicPackIsNamedByItsContent(t *testing.T) {
	name := "高三化学 水的电离与溶液的pH 专题精练"
	reply := `{"exam_name":"` + name + `","doc_type":"topic","subjects":["chemistry"],"grade":"grade12",` +
		`"chapter":"水的电离与溶液的pH","files":[{"id":"f1","role":"questions"}]}`
	d, problems, err := CheckDecide(reply, topicInput())
	if err != nil {
		t.Fatal(err)
	}
	if d.ExamName != name {
		t.Fatalf("exam_name = %q, problems = %+v", d.ExamName, problems)
	}

	// A name that is not anchored in the chapter it claims is rejected.
	reply = `{"exam_name":"高三化学 电化学专题精练","chapter":"电化学","doc_type":"topic",` +
		`"subjects":["chemistry"],"grade":"grade12","files":[{"id":"f1","role":"questions"}]}`
	d, problems, err = CheckDecide(reply, topicInput())
	if err != nil {
		t.Fatal(err)
	}
	if d.ExamName != "" {
		t.Fatalf("unfounded exam_name %q accepted", d.ExamName)
	}
	found := false
	for _, p := range problems {
		found = found || (p.Field == "exam_name" && p.Code == "evidence")
	}
	if !found {
		t.Fatalf("no exam_name evidence problem: %+v", problems)
	}
}

// File names are "套卷标题 + part 标签", so a name ending in 教师版 would show
// up twice: "…（教师版） 教师版.docx".
func TestExamNameDropsTrailingPartWords(t *testing.T) {
	reply := `{"exam_name":"十年（2012-2021）高考化学真题分项汇编 专题44 水的电离与溶液的pH（教师版）",` +
		`"doc_type":"topic","subjects":["chemistry"],"grade":"grade12",` +
		`"chapter":"水的电离与溶液的pH","files":[{"id":"f1","role":"questions"}]}`
	d, problems, err := CheckDecide(reply, topicInput())
	if err != nil {
		t.Fatal(err)
	}
	if d.ExamName != "十年（2012-2021）高考化学真题分项汇编 专题44 水的电离与溶液的pH" {
		t.Fatalf("exam_name = %q, problems = %+v", d.ExamName, problems)
	}
	// A name made of part words alone is kept rather than emptied.
	if got := trimPartWords("教师版"); got != "教师版" {
		t.Fatalf("trimPartWords dropped the whole name: %q", got)
	}
	if got := trimPartWords("化学仪器装置图 学生版"); got != "化学仪器装置图" {
		t.Fatalf("trimPartWords = %q", got)
	}
}

func TestSeriesNeedsThePrintedName(t *testing.T) {
	in := topicInput()
	in.Members[0].FileName = "十年（2012-2021）高考化学真题分项汇编 专题44 水的电离与溶液的pH 试题卷.docx"
	base := `"doc_type":"topic","subjects":["chemistry"],"grade":"grade12",` +
		`"chapter":"水的电离与溶液的pH",` +
		`"exam_name":"十年（2012-2021）高考化学真题分项汇编 专题44 水的电离与溶液的pH",` +
		`"files":[{"id":"f1","role":"questions"}]`

	d, _, err := CheckDecide(`{"series":"十年（2012-2021）高考化学真题分项汇编",`+base+`}`, in)
	if err != nil {
		t.Fatal(err)
	}
	if d.Series != "十年（2012-2021）高考化学真题分项汇编" {
		t.Fatalf("series = %q", d.Series)
	}

	// A series name that is not printed anywhere is not trusted.
	d, problems, err := CheckDecide(`{"series":"高考化学分类汇编",`+base+`}`, in)
	if err != nil {
		t.Fatal(err)
	}
	if d.Series != "" {
		t.Fatalf("invented series %q accepted", d.Series)
	}
	found := false
	for _, p := range problems {
		found = found || (p.Field == "series" && p.Code == "evidence")
	}
	if !found {
		t.Fatalf("no series evidence problem: %+v", problems)
	}

	// A series identical to the pack's own name adds nothing.
	d, _, err = CheckDecide(`{"series":"十年（2012-2021）高考化学真题分项汇编 专题44 水的电离与溶液的pH",`+base+`}`, in)
	if err != nil {
		t.Fatal(err)
	}
	if d.Series != "" {
		t.Fatalf("series equal to exam_name accepted: %q", d.Series)
	}

	// A type label is not a series.
	d, _, err = CheckDecide(`{"series":"专题练习",`+base+`}`, in)
	if err != nil {
		t.Fatal(err)
	}
	if d.Series != "" {
		t.Fatalf("doc type label accepted as series: %q", d.Series)
	}
}
