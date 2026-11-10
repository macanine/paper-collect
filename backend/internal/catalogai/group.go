package catalogai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nic/paper-collect/backend/internal/ai"
)

// GroupFile is one file to be placed, described by its card.
type GroupFile struct {
	ID   string `json:"id"`
	Path string `json:"path"`
	Card Card   `json:"card"`
	// CurrentSet is the set the file sits in today (re-grouping an existing
	// library); empty for new uploads.
	CurrentSet string `json:"current_set_id,omitempty"`
}

// GroupCandidate is an existing set a file may join.
type GroupCandidate struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	ExamName     string   `json:"exam_name,omitempty"`
	DocType      string   `json:"doc_type,omitempty"`
	Subjects     []string `json:"subjects,omitempty"`
	Grade        string   `json:"grade,omitempty"`
	Cohort       int      `json:"cohort,omitempty"`
	Organization string   `json:"organization,omitempty"`
	ExamDate     string   `json:"exam_date,omitempty"`
	Roles        []string `json:"roles"`
	Fingerprint  []string `json:"fingerprint,omitempty"`
}

// GroupInput is one grouping request.
type GroupInput struct {
	Files      []GroupFile      `json:"files"`
	Candidates []GroupCandidate `json:"candidates"`
}

// Group is one set in a grouping reply.
type Group struct {
	Files    []string `json:"files"`
	Existing string   `json:"existing_set_id,omitempty"`
	Reason   string   `json:"reason,omitempty"`
}

// GroupReply is the verified grouping.
type GroupReply struct {
	Groups []Group `json:"groups"`
}

const groupSystem = `你是中学资料编目员，负责把文件分成“套卷”。一套是同一场考试（或同一份资料）的全部部件，例如同一场考试的试题卷、答案、答题卡、听力。只输出一个 JSON 对象，不要输出解释。
格式：{"groups":[{"files":["文件id",...],"existing_set_id":"已有套卷id或空","reason":"一句话依据"}]}
规则：
- 每个文件必须且只能出现在一个组里。
- 只有当文件与某个已有套卷确实是同一场考试/同一份资料时，才填写 existing_set_id，且只能使用候选列表中的 id；否则留空表示新建一套。
- 判断是否同一套的依据：标题、考试名称、学校/机构、年级、届别、考试年月、学科一致，并且部件互补（例如一份试题卷配一份答案）。只是年级和学科相同不能算同一套。
- 同一套里试题卷、答案、前题后答(full)、教师版、答题卡各最多一份。
- 不同学科永远不能是同一套。
- 文件 card 中的 metadata_only=true 表示只能依据文件名判断，证据较弱，宁可单独成套也不要勉强合并。
- current_set_id 表示文件目前所在的套卷；如果仍然正确，就把该 id 填到 existing_set_id。`

// GroupMessages builds the GROUP prompt.
func GroupMessages(in GroupInput) []ai.ChatMessage {
	files, _ := json.Marshal(in.Files)
	candidates, _ := json.Marshal(in.Candidates)
	text := "待分组的文件：\n" + string(files) + "\n\n可加入的已有套卷候选：\n" + string(candidates)
	return []ai.ChatMessage{ai.TextMessage("system", groupSystem), ai.TextMessage("user", text)}
}

// CheckGroup verifies that a grouping covers every file exactly once, only
// uses candidate sets, and never puts two copies of one exam role into a set.
func CheckGroup(content string, in GroupInput) (GroupReply, []Problem, error) {
	var raw GroupReply
	if err := decodeObject(content, &raw); err != nil {
		return GroupReply{}, nil, err
	}
	files := map[string]GroupFile{}
	for _, f := range in.Files {
		files[f.ID] = f
	}
	candidates := map[string]GroupCandidate{}
	for _, c := range in.Candidates {
		candidates[c.ID] = c
	}
	var problems []Problem
	placed := map[string]bool{}
	byExisting := map[string]int{}
	out := GroupReply{}
	for gi, g := range raw.Groups {
		group := Group{Existing: strings.TrimSpace(g.Existing), Reason: clip(g.Reason, 200)}
		for _, id := range g.Files {
			if _, ok := files[id]; !ok {
				problems = append(problems, critical("unknown_file", "files", "第 %d 组包含不存在的文件 id %q", gi+1, id))
				continue
			}
			if placed[id] {
				problems = append(problems, critical("duplicate_file", "files", "文件 %s 出现在多个组中", id))
				continue
			}
			placed[id] = true
			group.Files = append(group.Files, id)
		}
		if len(group.Files) == 0 {
			continue
		}
		var existingRoles []string
		if group.Existing != "" {
			c, ok := candidates[group.Existing]
			if !ok {
				problems = append(problems, critical("unknown_set", "existing_set_id", "第 %d 组的 existing_set_id %q 不在候选列表中", gi+1, group.Existing))
				group.Existing = ""
			} else {
				existingRoles = c.Roles
				if n := byExisting[group.Existing]; n > 0 {
					problems = append(problems, critical("split_set", "existing_set_id", "已有套卷 %s 被分配给了多个组，请合并为一组", group.Existing))
				}
				byExisting[group.Existing]++
			}
		}
		if p := roleConflict(gi, group, files, existingRoles); p != nil {
			problems = append(problems, *p)
		}
		if p := subjectConflict(gi, group, files, candidates[group.Existing]); p != nil {
			problems = append(problems, *p)
		}
		out.Groups = append(out.Groups, group)
	}
	for _, f := range in.Files {
		if !placed[f.ID] {
			problems = append(problems, critical("missing_file", "files", "文件 %s（%s）没有被分到任何组", f.ID, f.Path))
		}
	}
	return out, problems, nil
}

func roleConflict(index int, g Group, files map[string]GroupFile, existing []string) *Problem {
	count := map[string]int{}
	for _, r := range existing {
		count[r]++
	}
	for _, id := range g.Files {
		role := files[id].Card.Role
		if files[id].CurrentSet != "" && files[id].CurrentSet == g.Existing {
			// Already a member: its role is part of existing.
			continue
		}
		count[role]++
	}
	for role, n := range count {
		if uniqueRoles[role] && n > 1 {
			p := critical("duplicate_role", "files", "第 %d 组有 %d 份「%s」，同一套最多一份，请把不同考试拆开",
				index+1, n, RoleLabels[role])
			return &p
		}
	}
	return nil
}

func subjectConflict(index int, g Group, files map[string]GroupFile, existing GroupCandidate) *Problem {
	subjects := map[string]bool{}
	add := func(list []string) {
		if len(list) == 1 {
			subjects[list[0]] = true
		}
	}
	add(existing.Subjects)
	for _, id := range g.Files {
		add(files[id].Card.Subjects)
	}
	if len(subjects) > 1 {
		keys := make([]string, 0, len(subjects))
		for k := range subjects {
			keys = append(keys, k)
		}
		p := critical("mixed_subjects", "files", "第 %d 组混入了不同学科（%s），不同学科不能是同一套", index+1, strings.Join(keys, "、"))
		return &p
	}
	return nil
}

// Singletons is the safe fallback when no model produced a valid grouping:
// every file keeps its current set or starts its own, nothing is merged.
func Singletons(in GroupInput) GroupReply {
	out := GroupReply{}
	for _, f := range in.Files {
		out.Groups = append(out.Groups, Group{Files: []string{f.ID}, Existing: f.CurrentSet, Reason: "未能得到可靠的分组结果，暂时单独成套"})
	}
	return out
}

// String renders a short summary for job logs.
func (r GroupReply) String() string {
	merged := 0
	for _, g := range r.Groups {
		if g.Existing != "" {
			merged++
		}
	}
	return fmt.Sprintf("%d 组，其中 %d 组并入已有套卷", len(r.Groups), merged)
}
