package extproc

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"thyris-sz/internal/extproc/policy"
	"thyris-sz/internal/guardrails"
)

type inspectFunc func(context.Context, guardrails.InspectInput) (guardrails.InspectResult, error)

func (fn inspectFunc) Inspect(ctx context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
	return fn(ctx, input)
}

func TestOpenAIRequestProcessorMasksOnlyUserContentAndUpdatesLength(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		if input.Text == "secret user value" {
			return guardrails.InspectResult{Action: guardrails.RuleActionMask, SafeContent: "[MASKED]", DetectionCount: 1, Categories: []string{"PII"}}, nil
		}
		return guardrails.InspectResult{Action: guardrails.RuleActionAllow, SafeContent: input.Text}, nil
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	body := []byte(`{"model":"kept","unknown":{"x":1},"messages":[{"role":"system","content":"keep system"},{"role":"user","content":"secret user value"},{"role":"assistant","content":"keep assistant"},{"role":"user","content":"safe user value"}]}`)
	result, err := processor.Process(context.Background(), ProcessingRequest{
		RID: "rid-mask", EnvoyReqID: "envoy-mask", Stage: StageRequest, ContentType: "application/json", Body: body,
		PolicyID: "default", PolicyVersion: 3, PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 3, Definition: requestPolicyDefinition()},
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Action != ActionMask || result.DetectionCount != 1 || string(result.Body) == string(body) {
		t.Fatalf("result = %+v", result)
	}
	want := `{"model":"kept","unknown":{"x":1},"messages":[{"role":"system","content":"keep system"},{"role":"user","content":"[MASKED]"},{"role":"assistant","content":"keep assistant"},{"role":"user","content":"safe user value"}]}`
	if got := string(result.Body); got != want {
		t.Fatalf("mutated body = %s\nwant = %s", got, want)
	}
	if result.HeaderMutations["content-length"] != strconv.Itoa(len(result.Body)) {
		t.Fatalf("content-length mutation = %q, want %d", result.HeaderMutations["content-length"], len(result.Body))
	}
	if result.Metadata.PolicyID != "default" || result.Metadata.PolicyVersion != 3 || result.Metadata.Action != ActionMask {
		t.Fatalf("metadata = %+v", result.Metadata)
	}
}

func TestOpenAIRequestProcessorMasksStreamingWindow(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		if input.Text == "secret@example.test" {
			return guardrails.InspectResult{Action: guardrails.RuleActionMask, SafeContent: "[MASKED]", DetectionCount: 1, Categories: []string{"PII"}}, nil
		}
		return guardrails.InspectResult{Action: guardrails.RuleActionAllow, SafeContent: input.Text}, nil
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	parser := &OpenAISSEParser{}
	events, err := parser.Feed([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"secret@example.test\"}}]}\n\ndata: [DONE]\n\n"))
	if err != nil {
		t.Fatalf("parse SSE: %v", err)
	}
	result, mutated, err := processor.ProcessSSEWindow(context.Background(), ProcessingRequest{RID: "rid", EnvoyReqID: "envoy", Stage: StageResponse, PolicyID: "default", PolicyVersion: 1, PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: policy.PolicyDefinition{Response: policy.ResponsePolicy{Enabled: true, PII: policy.ActionMask, Secret: policy.ActionMask, UnsafeContent: policy.ActionMask}}}}, events)
	if err != nil {
		t.Fatalf("ProcessSSEWindow() error = %v", err)
	}
	if result.Action != ActionMask || result.DetectionCount != 1 || !strings.Contains(string(mutated[0].Raw), "[MASKED]") || !mutated[1].Done {
		t.Fatalf("window result = %+v, events = %+v", result, mutated)
	}
}

func TestOpenAIRequestProcessorStreamingWindowUsesCrossEventContext(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		if input.Text != "secret@example.test" {
			t.Fatalf("Inspect text = %q, want concatenated SSE deltas", input.Text)
		}
		return guardrails.InspectResult{Action: guardrails.RuleActionMask, SafeContent: "[MASKED]", DetectionCount: 1, Categories: []string{"PII"}}, nil
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	parser := &OpenAISSEParser{}
	events, err := parser.Feed([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"secret@\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"example.test\"}}]}\n\n"))
	if err != nil {
		t.Fatalf("parse SSE: %v", err)
	}
	_, mutated, err := processor.ProcessSSEWindow(context.Background(), ProcessingRequest{Stage: StageResponse, PolicySnapshot: &policy.CompiledSnapshot{Definition: policy.PolicyDefinition{Response: policy.ResponsePolicy{Enabled: true, PII: policy.ActionMask, Secret: policy.ActionMask, UnsafeContent: policy.ActionMask}}}}, events)
	if err != nil {
		t.Fatalf("ProcessSSEWindow() error = %v", err)
	}
	if !strings.Contains(string(mutated[0].Raw), "[MASKED]") || strings.Contains(string(mutated[1].Raw), "example.test") {
		t.Fatalf("cross-event mutation = %q %q", mutated[0].Raw, mutated[1].Raw)
	}
}

func TestOpenAIRequestProcessorUsesStrongestActionWithoutMutatingAuditOrBlock(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		switch input.Text {
		case "audit":
			return guardrails.InspectResult{Action: guardrails.RuleActionAuditOnly, SafeContent: "changed", DetectionCount: 1, Categories: []string{"PROMPT_INJECTION"}}, nil
		case "mask":
			return guardrails.InspectResult{Action: guardrails.RuleActionMask, SafeContent: "[MASKED]", DetectionCount: 1, Categories: []string{"PII"}}, nil
		default:
			return guardrails.InspectResult{Action: guardrails.RuleActionBlock, SafeContent: "[BLOCKED]", DetectionCount: 1, Categories: []string{"SECRET"}}, nil
		}
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	body := []byte(`{"messages":[{"role":"user","content":"audit"},{"role":"user","content":"mask"},{"role":"user","content":"block"}]}`)
	result, err := processor.Process(context.Background(), ProcessingRequest{
		Stage: StageRequest, ContentType: "application/json", Body: body, PolicyID: "default", PolicyVersion: 1,
		PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: requestPolicyDefinition()},
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Action != ActionBlock || result.DetectionCount != 3 {
		t.Fatalf("strongest result = %+v", result)
	}
	if result.Body != nil || result.HeaderMutations != nil {
		t.Fatalf("BLOCK must not mutate upstream body/header: %+v", result)
	}
	if got := result.Metadata.Categories; len(got) != 3 || got[0] != "PII" || got[1] != "PROMPT_INJECTION" || got[2] != "SECRET" {
		t.Fatalf("metadata categories = %v", got)
	}
}

func TestOpenAIRequestProcessorLeavesAuditOnlyBodyUnchanged(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		return guardrails.InspectResult{Action: guardrails.RuleActionAuditOnly, SafeContent: "must not be used", DetectionCount: 1, Categories: []string{"PROMPT_INJECTION"}}, nil
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	body := []byte(`{"messages":[{"role":"user","content":"audit"}]}`)
	result, err := processor.Process(context.Background(), ProcessingRequest{
		Stage: StageRequest, ContentType: "application/json", Body: body,
		PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: requestPolicyDefinition()},
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Action != ActionAuditOnly || result.DetectionCount != 1 || result.Body != nil || result.HeaderMutations != nil {
		t.Fatalf("AUDIT_ONLY result = %+v", result)
	}
}

func TestOpenAIRequestProcessorForwardsSafeBodyByteForByte(t *testing.T) {
	processor := compiledPolicyProcessor(t)
	body := []byte(`{"model":"kept","messages":[{"role":"user","content":"ordinary request"}]}`)
	result, err := processor.Process(context.Background(), ProcessingRequest{
		RID: "rid-safe", EnvoyReqID: "envoy-safe", Stage: StageRequest, ContentType: "application/json", Body: body,
		PolicyID: "default", PolicyVersion: 1, PolicySnapshot: compiledSnapshot("default", 1, policy.RequestPolicy{
			PII: policy.ActionMask, Secret: policy.ActionBlock, PromptInjection: policy.ActionAuditOnly,
			CompiledRules: policy.CompiledRequestRules{CustomPatterns: []policy.CompiledPattern{
				{ID: "email", Name: "EMAIL", Category: "PII", Regex: `[[:alnum:]._%+-]+@[[:alnum:].-]+\.[[:alpha:]]{2,}`, Action: policy.ActionMask},
			}},
		}),
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Action != ActionAllow || result.Body != nil || result.HeaderMutations != nil {
		t.Fatalf("safe result = %+v", result)
	}
	if upstream := bodyForMockUpstream(body, result); string(upstream) != string(body) {
		t.Fatalf("safe upstream body = %q, want byte-for-byte %q", upstream, body)
	}
}

func TestOpenAIRequestProcessorMasksPIIForMockUpstreamWithConsistentContentLength(t *testing.T) {
	processor := compiledPolicyProcessor(t)
	body := []byte(`{"messages":[{"role":"user","content":"email alice@example.com"}]}`)
	result, err := processor.Process(context.Background(), ProcessingRequest{
		RID: "rid-pii", Stage: StageRequest, ContentType: "application/json", Body: body,
		PolicyID: "default", PolicyVersion: 2, PolicySnapshot: compiledSnapshot("default", 2, policy.RequestPolicy{
			PII: policy.ActionMask, Secret: policy.ActionBlock, PromptInjection: policy.ActionAuditOnly,
			CompiledRules: policy.CompiledRequestRules{CustomPatterns: []policy.CompiledPattern{
				{ID: "email", Name: "EMAIL", Category: "PII", Regex: `[[:alnum:]._%+-]+@[[:alnum:].-]+\.[[:alpha:]]{2,}`, Action: policy.ActionMask},
			}},
		}),
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	upstream := bodyForMockUpstream(body, result)
	if result.Action != ActionMask || string(upstream) == string(body) || string(upstream) == "" {
		t.Fatalf("masked upstream body = %q, result = %+v", upstream, result)
	}
	if result.HeaderMutations["content-length"] != strconv.Itoa(len(upstream)) {
		t.Fatalf("content-length = %q, want %d", result.HeaderMutations["content-length"], len(upstream))
	}
}

func TestOpenAIRequestProcessorMasksBuiltinPIIWithoutCustomPolicyPattern(t *testing.T) {
	processor := compiledPolicyProcessor(t)
	body := []byte(`{"messages":[{"role":"user","content":"contact alice@example.com"}]}`)
	result, err := processor.Process(context.Background(), ProcessingRequest{
		RID: "rid-builtin-pii", Stage: StageRequest, ContentType: "application/json", Body: body,
		PolicyID: "default", PolicyVersion: 2, PolicySnapshot: compiledSnapshot("default", 2, policy.RequestPolicy{
			PII: policy.ActionMask, Secret: policy.ActionBlock, PromptInjection: policy.ActionAuditOnly,
		}),
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Action != ActionMask || result.DetectionCount != 1 || result.Body == nil || strings.Contains(string(result.Body), "alice@example.com") {
		t.Fatalf("built-in PII result = %+v body=%q", result, result.Body)
	}
}

func TestOpenAIRequestProcessorSecretBlockOverridesPIIAuditOnly(t *testing.T) {
	processor := compiledPolicyProcessor(t)
	body := []byte(`{"messages":[{"role":"user","content":"token secret-42"}]}`)
	result, err := processor.Process(context.Background(), ProcessingRequest{
		RID: "rid-secret", Stage: StageRequest, ContentType: "application/json", Body: body,
		PolicyID: "default", PolicyVersion: 3, PolicySnapshot: compiledSnapshot("default", 3, policy.RequestPolicy{
			PII: policy.ActionAuditOnly, Secret: policy.ActionBlock, PromptInjection: policy.ActionAuditOnly,
			CompiledRules: policy.CompiledRequestRules{CustomPatterns: []policy.CompiledPattern{
				{ID: "pii", Name: "PII", Category: "PII", Regex: `token`, Action: policy.ActionAuditOnly},
				{ID: "secret", Name: "SECRET", Category: "SECRET", Regex: `secret-[0-9]+`, Action: policy.ActionBlock},
			}},
		}),
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Action != ActionBlock || result.DetectionCount != 2 || result.Body != nil {
		t.Fatalf("secret must block despite PII audit: %+v", result)
	}
}

func TestOpenAIRequestProcessorMutatesEveryMatchingUserMessage(t *testing.T) {
	processor := compiledPolicyProcessor(t)
	body := []byte(`{"messages":[{"role":"user","content":"first@example.com"},{"role":"assistant","content":"assistant@example.com"},{"role":"user","content":"second@example.com"}]}`)
	result, err := processor.Process(context.Background(), ProcessingRequest{
		RID: "rid-multiple", Stage: StageRequest, ContentType: "application/json", Body: body,
		PolicyID: "default", PolicyVersion: 4, PolicySnapshot: compiledSnapshot("default", 4, policy.RequestPolicy{
			PII: policy.ActionMask, Secret: policy.ActionBlock, PromptInjection: policy.ActionAuditOnly,
			CompiledRules: policy.CompiledRequestRules{CustomPatterns: []policy.CompiledPattern{
				{ID: "email", Name: "EMAIL", Category: "PII", Regex: `[[:alnum:]._%+-]+@[[:alnum:].-]+\.[[:alpha:]]{2,}`, Action: policy.ActionMask},
			}},
		}),
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	mutated := string(result.Body)
	if result.Action != ActionMask || result.DetectionCount != 2 || containsAny(mutated, "first@example.com", "second@example.com") || !containsAny(mutated, "assistant@example.com") {
		t.Fatalf("multiple user mutation = %q, result = %+v", mutated, result)
	}
}

func TestOpenAIRequestProcessorMasksAssistantResponseAndUpdatesLength(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		if input.Text == "secret assistant value" {
			return guardrails.InspectResult{Action: guardrails.RuleActionMask, SafeContent: "", DetectionCount: 1, Categories: []string{"PII"}}, nil
		}
		return guardrails.InspectResult{Action: guardrails.RuleActionAllow, SafeContent: input.Text}, nil
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	body := []byte(`{"id":"kept","choices":[{"message":{"role":"assistant","content":"secret assistant value"}},{"message":{"role":"assistant","content":"safe answer"}}],"usage":{"total_tokens":9}}`)
	result, err := processor.Process(context.Background(), ProcessingRequest{
		RID: "rid-response-mask", EnvoyReqID: "envoy-response-mask", Stage: StageResponse, ContentType: "application/json", Body: body,
		PolicyID: "default", PolicyVersion: 4, PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 4, Definition: responsePolicyDefinition()},
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Action != ActionMask || result.DetectionCount != 1 || result.Metadata.Stage != StageResponse {
		t.Fatalf("result = %+v", result)
	}
	want := `{"id":"kept","choices":[{"message":{"role":"assistant","content":""}},{"message":{"role":"assistant","content":"safe answer"}}],"usage":{"total_tokens":9}}`
	if string(result.Body) != want || result.HeaderMutations["content-length"] != strconv.Itoa(len(result.Body)) {
		t.Fatalf("masked response = %q, headers = %+v", result.Body, result.HeaderMutations)
	}
}

func TestOpenAIRequestProcessorBlocksAssistantResponseWithForbiddenStatus(t *testing.T) {
	processor, err := NewOpenAIRequestProcessor(inspectFunc(func(_ context.Context, input guardrails.InspectInput) (guardrails.InspectResult, error) {
		return guardrails.InspectResult{Action: guardrails.RuleActionBlock, SafeContent: "must not be used", DetectionCount: 1, Categories: []string{"SECRET"}}, nil
	}))
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	result, err := processor.Process(context.Background(), ProcessingRequest{
		RID: "rid-response-block", EnvoyReqID: "envoy-response-block", Stage: StageResponse, ContentType: "application/json",
		Body:     []byte(`{"choices":[{"message":{"role":"assistant","content":"secret response"}}]}`),
		PolicyID: "default", PolicyVersion: 5, PolicySnapshot: &policy.CompiledSnapshot{PolicyID: "default", Version: 5, Definition: responsePolicyDefinition()},
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if result.Action != ActionBlock || result.ImmediateStatus != 403 || result.Body != nil || result.HeaderMutations != nil || result.Metadata.Stage != StageResponse {
		t.Fatalf("blocked response result = %+v", result)
	}
}

func compiledPolicyProcessor(t *testing.T) *OpenAIRequestProcessor {
	t.Helper()
	service, err := guardrails.NewGuardrailService(&guardrails.Detector{})
	if err != nil {
		t.Fatalf("NewGuardrailService() error = %v", err)
	}
	processor, err := NewOpenAIRequestProcessor(service)
	if err != nil {
		t.Fatalf("NewOpenAIRequestProcessor() error = %v", err)
	}
	return processor
}

func compiledSnapshot(policyID string, version int, request policy.RequestPolicy) *policy.CompiledSnapshot {
	return &policy.CompiledSnapshot{PolicyID: policyID, Version: version, Definition: policy.PolicyDefinition{Request: request}}
}

// bodyForMockUpstream models Envoy's request-body forwarding decision: a nil
// mutation preserves the original bytes, otherwise the supplied mutation wins.
func bodyForMockUpstream(original []byte, result ProcessingResult) []byte {
	if result.Body == nil {
		return original
	}
	return result.Body
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if len(candidate) > 0 && len(value) >= len(candidate) {
			for index := 0; index+len(candidate) <= len(value); index++ {
				if value[index:index+len(candidate)] == candidate {
					return true
				}
			}
		}
	}
	return false
}

func requestPolicyDefinition() policy.PolicyDefinition {
	return policy.PolicyDefinition{Request: policy.RequestPolicy{
		PII: policy.ActionMask, Secret: policy.ActionBlock, PromptInjection: policy.ActionAuditOnly,
	}}
}

func responsePolicyDefinition() policy.PolicyDefinition {
	return policy.PolicyDefinition{Response: policy.ResponsePolicy{
		Enabled: true, PII: policy.ActionMask, Secret: policy.ActionBlock, UnsafeContent: policy.ActionAuditOnly,
	}}
}
