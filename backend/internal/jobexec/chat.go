package jobexec

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nic/paper-collect/backend/internal/ai"
	"github.com/nic/paper-collect/backend/internal/catalogai"
)

const (
	aiCallTimeout = 120 * time.Second
	// repairRounds is how many times one model may correct a reply that
	// failed the checks before the next model is tried.
	repairRounds = 1
)

// chatOutcome is the best reply a model produced and what its checks found.
type chatOutcome struct {
	ProviderID string
	Model      string
	Content    string
	Problems   []catalogai.Problem
}

// checkFunc verifies a reply; a decode error means no JSON object was found.
type checkFunc func(content string) ([]catalogai.Problem, error)

// chatChecked asks the endpoints in priority order. Each reply is checked;
// a failing reply goes back to the same model once with the list of
// problems. The first clean reply wins. Otherwise the best reply without
// critical problems wins, then the best reply overall, so the caller can
// record exactly what was wrong. An error is returned only when no endpoint
// produced any reply; its class says whether to wait, retry or give up.
func chatChecked(ctx context.Context, env Env, models []ModelConfig, label string,
	build func(vision bool) []ai.ChatMessage, hasImages bool, check checkFunc) (chatOutcome, error) {
	if len(models) == 0 {
		return chatOutcome{}, classed(ClassConfig, errors.New("没有启用的 AI 模型端点"))
	}
	var best *chatOutcome
	var errs []error
	tried := 0
	for index, m := range models {
		modelName := strings.TrimSpace(m.Model)
		vision := hasImages && strings.TrimSpace(m.VisionModel) != ""
		if vision {
			modelName = strings.TrimSpace(m.VisionModel)
		}
		if modelName == "" {
			continue
		}
		tried++
		client, err := ai.NewOpenAI(m.BaseURL, m.APIKey, nil)
		if err != nil {
			errs = append(errs, classed(ClassConfig, err))
			continue
		}
		messages := build(vision)
		for round := 0; round <= repairRounds; round++ {
			env.Progress(ctx, fmt.Sprintf("%s：第 %d / %d 个模型服务（第 %d 次）", label, index+1, len(models), round+1), index, len(models), nil)
			callCtx, cancel := context.WithTimeout(ctx, aiCallTimeout)
			started := time.Now()
			content, err := client.Chat(callCtx, modelName, messages)
			cancel()
			if err != nil {
				if ctx.Err() != nil {
					return chatOutcome{}, ctx.Err()
				}
				env.Logf("%s provider %s failed: %s", label, m.Name, ai.Sanitize(err.Error()))
				errs = append(errs, err)
				break
			}
			problems, perr := check(content)
			if perr != nil {
				problems = []catalogai.Problem{catalogai.NotJSON()}
			}
			env.Progress(ctx, fmt.Sprintf("%s：模型 %s 已回复，%d 个校验问题", label, m.Name, len(problems)), index+1, len(models),
				map[string]any{"provider": m.Name, "model": modelName, "duration_ms": time.Since(started).Milliseconds(), "problems": len(problems)})
			current := chatOutcome{ProviderID: m.ID, Model: modelName, Content: content, Problems: problems}
			if best == nil || catalogai.Worse(best.Problems, problems) {
				best = &current
			}
			if len(problems) == 0 {
				return current, nil
			}
			if round < repairRounds {
				messages = append(messages, ai.TextMessage("assistant", content), catalogai.RepairMessage(problems))
			}
		}
		if best != nil && !catalogai.HasCritical(best.Problems) {
			return *best, nil
		}
	}
	if best != nil {
		return *best, nil
	}
	if tried == 0 {
		return chatOutcome{}, classed(ClassConfig, errors.New("没有可用模型：请配置启用的模型端点及默认/视觉模型"))
	}
	return chatOutcome{}, aggregate(errs)
}

// aggregate turns per-endpoint errors into one classified error. Any
// transient failure makes the whole call retryable; only when every endpoint
// failed for configuration reasons does the file wait for AI.
func aggregate(errs []error) error {
	if len(errs) == 0 {
		return classed(ClassPermanent, errors.New("模型没有返回结果"))
	}
	last := errs[len(errs)-1]
	class := ClassConfig
	for _, err := range errs {
		switch Classify(err) {
		case ClassTransient:
			class = ClassTransient
		case ClassPermanent:
			if class == ClassConfig {
				class = ClassPermanent
			}
		}
	}
	return classed(class, errors.New(ai.Sanitize(last.Error())))
}
