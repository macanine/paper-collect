package jobexec

import (
	"context"
	"fmt"

	"github.com/nic/paper-collect/backend/internal/ai"
	"github.com/nic/paper-collect/backend/internal/catalogai"
)

// GroupInput is one GROUP call. Group, decide and entity jobs only run on the
// master, so their inputs never travel to a node.
type GroupInput struct {
	Request catalogai.GroupInput `json:"request"`
	Models  []ModelConfig        `json:"models"`
}

// GroupResult is a verified grouping. Fallback is set when no model produced
// a usable grouping and every file was kept on its own.
type GroupResult struct {
	ProviderID string               `json:"provider_id"`
	Model      string               `json:"model"`
	Reply      catalogai.GroupReply `json:"reply"`
	Problems   []catalogai.Problem  `json:"problems,omitempty"`
	Fallback   bool                 `json:"fallback,omitempty"`
}

func runGroup(ctx context.Context, env Env, in GroupInput) (*GroupResult, error) {
	outcome, err := chatChecked(ctx, env, in.Models, fmt.Sprintf("分组 %d 个文件", len(in.Request.Files)),
		func(bool) []ai.ChatMessage { return catalogai.GroupMessages(in.Request) }, false,
		func(content string) ([]catalogai.Problem, error) {
			_, problems, err := catalogai.CheckGroup(content, in.Request)
			return problems, err
		})
	if err != nil {
		return nil, err
	}
	out := &GroupResult{ProviderID: outcome.ProviderID, Model: outcome.Model, Problems: outcome.Problems}
	reply, problems, err := catalogai.CheckGroup(outcome.Content, in.Request)
	if err != nil || catalogai.HasCritical(problems) {
		out.Reply, out.Fallback = catalogai.Singletons(in.Request), true
		return out, nil
	}
	out.Reply = reply
	return out, nil
}

// DecideInput is one DECIDE call.
type DecideInput struct {
	Request catalogai.DecideInput `json:"request"`
	Models  []ModelConfig         `json:"models"`
}

// DecideResult is a verified set catalogue. Problems that remain explain
// which values were rejected; critical ones mean the set needs attention.
type DecideResult struct {
	ProviderID string              `json:"provider_id"`
	Model      string              `json:"model"`
	Decision   catalogai.Decision  `json:"decision"`
	Problems   []catalogai.Problem `json:"problems,omitempty"`
	Usable     bool                `json:"usable"`
}

func runDecide(ctx context.Context, env Env, in DecideInput) (*DecideResult, error) {
	outcome, err := chatChecked(ctx, env, in.Models, "确定套卷编目",
		func(bool) []ai.ChatMessage { return catalogai.DecideMessages(in.Request) }, false,
		func(content string) ([]catalogai.Problem, error) {
			_, problems, err := catalogai.CheckDecide(content, in.Request)
			return problems, err
		})
	if err != nil {
		return nil, err
	}
	out := &DecideResult{ProviderID: outcome.ProviderID, Model: outcome.Model}
	decision, problems, err := catalogai.CheckDecide(outcome.Content, in.Request)
	if err != nil {
		out.Problems = []catalogai.Problem{catalogai.NotJSON()}
		return out, nil
	}
	out.Decision, out.Problems, out.Usable = decision, problems, true
	return out, nil
}

// EntityInput is one dictionary clean-up call.
type EntityInput struct {
	Request catalogai.DedupeInput `json:"request"`
	Models  []ModelConfig         `json:"models"`
}

// EntityResult names the entry a new one duplicates, if any.
type EntityResult struct {
	ProviderID string                `json:"provider_id"`
	Model      string                `json:"model"`
	Reply      catalogai.DedupeReply `json:"reply"`
}

func runEntity(ctx context.Context, env Env, in EntityInput) (*EntityResult, error) {
	outcome, err := chatChecked(ctx, env, in.Models, "检查名称是否重复",
		func(bool) []ai.ChatMessage { return catalogai.DedupeMessages(in.Request) }, false,
		func(content string) ([]catalogai.Problem, error) {
			_, problems, err := catalogai.CheckDedupe(content, in.Request)
			return problems, err
		})
	if err != nil {
		return nil, err
	}
	reply, problems, err := catalogai.CheckDedupe(outcome.Content, in.Request)
	if err != nil || len(problems) > 0 {
		// Unsure means keep both entries; a person can still merge them.
		reply = catalogai.DedupeReply{}
	}
	return &EntityResult{ProviderID: outcome.ProviderID, Model: outcome.Model, Reply: reply}, nil
}
