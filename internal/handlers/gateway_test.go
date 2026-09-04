package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"thyris-sz/internal/config"
	"thyris-sz/internal/guardrails"
)

type gatewayInspectFunc func(context.Context, guardrails.InspectInput) (guardrails.InspectResult, error)

func (fn gatewayInspectFunc) Inspect(ctx context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
	return fn(ctx, input)
}

func TestApplyInputGuardrailsScansSystemUserAndAssistantMessages(t *testing.T) {
	var inspected []string
	service := gatewayInspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		inspected = append(inspected, input.Text)
		return guardrails.InspectResult{Action: guardrails.RuleActionMask, SafeContent: "[MASKED]", ContainsSensitive: true}, nil
	})
	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": "system secret"},
		map[string]interface{}{"role": "assistant", "content": "assistant history", "tool_calls": []interface{}{
			map[string]interface{}{"id": "call_1", "type": "function", "function": map[string]interface{}{"name": "lookup", "arguments": "tool arguments"}},
		}},
		map[string]interface{}{"role": "tool", "tool_call_id": "call_1", "content": "tool result"},
		map[string]interface{}{"role": "user", "content": "user secret"},
	}

	got, blocked, _, responses := applyInputGuardrails(context.Background(), service, messages, "rid-system", nil)
	if blocked {
		t.Fatal("applyInputGuardrails() unexpectedly blocked")
	}
	wantInspected := []string{"system secret", "assistant history", "tool arguments", "tool result", "user secret"}
	if len(inspected) != len(wantInspected) {
		t.Fatalf("inspected = %#v, want %#v", inspected, wantInspected)
	}
	for index := range wantInspected {
		if inspected[index] != wantInspected[index] {
			t.Fatalf("inspected = %#v, want %#v", inspected, wantInspected)
		}
	}
	if len(responses) != 5 {
		t.Fatalf("responses = %d, want 5", len(responses))
	}
	toolArguments := got[1].(map[string]interface{})["tool_calls"].([]interface{})[0].(map[string]interface{})["function"].(map[string]interface{})["arguments"]
	if got[0].(map[string]interface{})["content"] != "[MASKED]" || got[1].(map[string]interface{})["content"] != "[MASKED]" || toolArguments != "[MASKED]" || got[2].(map[string]interface{})["content"] != "[MASKED]" || got[3].(map[string]interface{})["content"] != "[MASKED]" {
		t.Fatalf("sanitized messages = %#v", got)
	}
}

func TestProcessNonStreamResponseScansToolCallArguments(t *testing.T) {
	originalConfig := config.AppConfig
	config.AppConfig = &config.Config{GatewayBlockMode: "MASK"}
	t.Cleanup(func() { config.AppConfig = originalConfig })
	service := gatewayInspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		return guardrails.InspectResult{Action: guardrails.RuleActionMask, SafeContent: "{\"email\":\"[MASKED]\"}", ContainsSensitive: true}, nil
	})
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"email\":\"secret@example.com\"}"}}]}}]}`)
	upstream := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}
	recorder := httptest.NewRecorder()

	processNonStreamResponse(context.Background(), service, "rid-tool", nil, upstream, recorder, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	choices := payload["choices"].([]interface{})
	message := choices[0].(map[string]interface{})["message"].(map[string]interface{})
	toolCalls := message["tool_calls"].([]interface{})
	arguments := toolCalls[0].(map[string]interface{})["function"].(map[string]interface{})["arguments"]
	if arguments != `{"email":"[MASKED]"}` {
		t.Fatalf("tool arguments = %q", arguments)
	}
}
