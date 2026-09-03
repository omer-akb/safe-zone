package handlers

import (
	"context"
	"testing"

	"thyris-sz/internal/guardrails"
)

type gatewayInspectFunc func(context.Context, guardrails.InspectInput) (guardrails.InspectResult, error)

func (fn gatewayInspectFunc) Inspect(ctx context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
	return fn(ctx, input)
}

func TestApplyInputGuardrailsScansSystemAndUserMessages(t *testing.T) {
	var inspected []string
	service := gatewayInspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		inspected = append(inspected, input.Text)
		return guardrails.InspectResult{Action: guardrails.RuleActionMask, SafeContent: "[MASKED]", ContainsSensitive: true}, nil
	})
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": "system secret"},
		map[string]interface{}{"role": "assistant", "content": "assistant history"},
		map[string]interface{}{"role": "user", "content": "user secret"},
	}

	got, blocked, _, responses := applyInputGuardrails(context.Background(), service, messages, "rid-system", nil)
	if blocked {
		t.Fatal("applyInputGuardrails() unexpectedly blocked")
	}
	if len(inspected) != 2 || inspected[0] != "system secret" || inspected[1] != "user secret" {
		t.Fatalf("inspected = %#v, want system and user content", inspected)
	}
	if len(responses) != 2 {
		t.Fatalf("responses = %d, want 2", len(responses))
	}
	if got[0].(map[string]interface{})["content"] != "[MASKED]" || got[2].(map[string]interface{})["content"] != "[MASKED]" {
		t.Fatalf("sanitized messages = %#v", got)
	}
	if got[1].(map[string]interface{})["content"] != "assistant history" {
		t.Fatalf("assistant message changed: %#v", got[1])
	}
}
