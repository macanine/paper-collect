package catalogai

import (
	"fmt"
	"strings"

	"github.com/nic/paper-collect/backend/internal/ai"
)

// Problem is one failed check on a model reply. Critical problems mean the
// reply cannot be used as it is; the others only clear the offending field.
type Problem struct {
	Code     string `json:"code"`
	Field    string `json:"field,omitempty"`
	Message  string `json:"message"`
	Critical bool   `json:"critical,omitempty"`
}

func critical(code, field, format string, args ...any) Problem {
	return Problem{Code: code, Field: field, Message: fmt.Sprintf(format, args...), Critical: true}
}

func warn(code, field, format string, args ...any) Problem {
	return Problem{Code: code, Field: field, Message: fmt.Sprintf(format, args...)}
}

// NotJSON is the problem recorded for a reply without a JSON object.
func NotJSON() Problem {
	return critical("not_json", "", "回答不是合法的 JSON 对象")
}

// HasCritical reports whether any problem is critical.
func HasCritical(problems []Problem) bool {
	for _, p := range problems {
		if p.Critical {
			return true
		}
	}
	return false
}

// Worse reports whether a is a worse result than b: more critical problems,
// then more problems overall.
func Worse(a, b []Problem) bool {
	ca, cb := countCritical(a), countCritical(b)
	if ca != cb {
		return ca > cb
	}
	return len(a) > len(b)
}

func countCritical(problems []Problem) int {
	n := 0
	for _, p := range problems {
		if p.Critical {
			n++
		}
	}
	return n
}

// RepairMessage asks the model to fix the listed problems in a full new reply.
func RepairMessage(problems []Problem) ai.ChatMessage {
	lines := make([]string, 0, len(problems))
	for _, p := range problems {
		lines = append(lines, "- "+p.Message)
	}
	return ai.TextMessage("user", "你的上一次回答没有通过校验：\n"+strings.Join(lines, "\n")+
		"\n请依据提供的材料逐条修正，重新输出完整的 JSON 对象，不要输出其他内容。证据必须原样摘自材料。")
}

// Codes is a compact list of problem codes for storage and display.
func Codes(problems []Problem) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, p := range problems {
		key := p.Code
		if p.Field != "" {
			key += ":" + p.Field
		}
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	return out
}
