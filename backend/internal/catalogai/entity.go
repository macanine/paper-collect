package catalogai

import (
	"encoding/json"
	"strings"

	"github.com/nic/paper-collect/backend/internal/ai"
)

// DedupeEntity is a dictionary entry with a few sets that use it, so the
// model can tell two similarly named schools apart.
type DedupeEntity struct {
	EntityCandidate
	Samples []string `json:"samples,omitempty"`
}

// DedupeInput asks whether a newly created entry duplicates an existing one.
type DedupeInput struct {
	Kind       string         `json:"kind"`
	Entity     DedupeEntity   `json:"entity"`
	Candidates []DedupeEntity `json:"candidates"`
}

// DedupeReply names the entry the new one is the same as, or nothing.
type DedupeReply struct {
	SameAs string `json:"same_as"`
	Reason string `json:"reason,omitempty"`
}

const dedupeSystem = `你负责维护中学资料库的名称词典，判断一个新建条目是否与已有条目指同一个对象（同一所学校、同一个联盟或同一个标签）。只输出一个 JSON 对象：{"same_as":"已有条目id或空","reason":"一句话依据"}。
规则：
- 简称、全称、旧称、带不带“省/市/区”前缀、是否写“第”字，只要指同一所学校/同一个机构就算同一个。例如“杭二中”“杭州二中”“杭州市第二中学”是同一所。
- 校名相似但城市或编号不同的是不同学校，例如“杭州第二中学”和“宁波第二中学”、“杭州二中”和“杭州十四中”。
- 联盟与其成员学校不是同一个。
- 标签只有含义完全相同时才算同一个（如“一轮复习”和“一轮”），上下位概念不算。
- 拿不准时留空。只能填写候选中的 id。`

// DedupeMessages builds the entity clean-up prompt.
func DedupeMessages(in DedupeInput) []ai.ChatMessage {
	raw, _ := json.Marshal(in)
	return []ai.ChatMessage{ai.TextMessage("system", dedupeSystem), ai.TextMessage("user", "待判断：\n"+string(raw))}
}

// CheckDedupe verifies that a reply only names a candidate.
func CheckDedupe(content string, in DedupeInput) (DedupeReply, []Problem, error) {
	var raw DedupeReply
	if err := decodeObject(content, &raw); err != nil {
		return DedupeReply{}, nil, err
	}
	out := DedupeReply{SameAs: strings.TrimSpace(raw.SameAs), Reason: clip(raw.Reason, 200)}
	if out.SameAs == "" {
		return out, nil, nil
	}
	for _, c := range in.Candidates {
		if c.ID == out.SameAs {
			return out, nil, nil
		}
	}
	return DedupeReply{}, []Problem{critical("unknown_entity", "same_as", "same_as %q 不在候选中", out.SameAs)}, nil
}
