package extproc

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"thyris-sz/internal/extproc/policy"
	"thyris-sz/internal/guardrails"
)

// Processor owns gateway-neutral processing logic. Transport adapters must not
// implement policy or guardrail decisions.
type Processor interface {
	Process(ctx context.Context, request ProcessingRequest) (ProcessingResult, error)
}

// AllowProcessor is the Phase 1 default. It validates the contract stage and
// permits request and response messages without mutation.
type AllowProcessor struct{}

func NewAllowProcessor() Processor {
	return AllowProcessor{}
}

func (AllowProcessor) Process(ctx context.Context, request ProcessingRequest) (ProcessingResult, error) {
	if err := ctx.Err(); err != nil {
		return ProcessingResult{}, err
	}
	if err := request.Stage.Validate(); err != nil {
		return ProcessingResult{}, fmt.Errorf("allow processor: %w", err)
	}
	return ProcessingResult{Action: ActionAllow}, nil
}

// OpenAIRequestProcessor applies a stream-pinned policy only to supported
// OpenAI user message content. It never performs floating policy lookups.
type OpenAIRequestProcessor struct {
	service guardrails.GuardrailService
}

func NewOpenAIRequestProcessor(service guardrails.GuardrailService) (*OpenAIRequestProcessor, error) {
	if service == nil {
		return nil, fmt.Errorf("guardrail service is required")
	}
	return &OpenAIRequestProcessor{service: service}, nil
}

func (p *OpenAIRequestProcessor) Process(ctx context.Context, request ProcessingRequest) (ProcessingResult, error) {
	if err := ctx.Err(); err != nil {
		return ProcessingResult{}, err
	}
	if err := request.Stage.Validate(); err != nil {
		return ProcessingResult{}, err
	}
	// Phase 2 enforces only buffered request bodies. Header and response stages
	// remain protocol ALLOW paths, as do routes without a loaded snapshot.
	if request.Stage != StageRequest || request.Body == nil || request.PolicySnapshot == nil {
		return ProcessingResult{Action: ActionAllow}, nil
	}
	chat, err := ParseChatRequest(request.ContentType, request.Body)
	if err != nil {
		return ProcessingResult{}, err
	}
	rules, err := compiledGuardrailRules(*request.PolicySnapshot)
	if err != nil {
		return ProcessingResult{}, err
	}
	result := ProcessingResult{Action: ActionAllow}
	categorySet := make(map[string]struct{})
	mutations := make([]ChatContentMutation, 0)
	started := time.Now()
	for _, content := range chat.UserContents {
		inspection, err := p.service.Inspect(ctx, guardrails.InspectInput{
			Text: content.Content, RID: request.RID, Policy: rules,
		})
		if err != nil {
			return ProcessingResult{}, fmt.Errorf("inspect user message %d: %w", content.MessageIndex, err)
		}
		action, err := actionFromGuardrail(inspection.Action)
		if err != nil {
			return ProcessingResult{}, err
		}
		result.Action = strongerProcessingAction(result.Action, action)
		result.DetectionCount += inspection.DetectionCount
		for _, category := range inspection.Categories {
			categorySet[category] = struct{}{}
		}
		if action == ActionMask {
			mutations = append(mutations, ChatContentMutation{MessageIndex: content.MessageIndex, Content: inspection.SafeContent})
		}
	}
	result.Metadata = SafeMetadata{
		RequestID: request.EnvoyReqID, RID: request.RID, PolicyID: request.PolicyID,
		PolicyVersion: request.PolicyVersion, Adapter: "openai_chat_completions", Stage: StageRequest,
		Action: result.Action, DetectionCount: result.DetectionCount,
		ProcessorLatencyMS: time.Since(started).Milliseconds(),
	}
	for category := range categorySet {
		result.Metadata.Categories = append(result.Metadata.Categories, category)
	}
	sort.Strings(result.Metadata.Categories)
	// A block prevents upstream forwarding; body mutation is unnecessary. Audit
	// only deliberately leaves the body byte-for-byte unchanged.
	if result.Action != ActionMask || len(mutations) == 0 {
		return result, nil
	}
	body, err := chat.Mutate(mutations)
	if err != nil {
		return ProcessingResult{}, err
	}
	result.Body = body
	result.HeaderMutations = map[string]string{"content-length": strconv.Itoa(len(body))}
	return result, nil
}

func compiledGuardrailRules(snapshot policy.CompiledSnapshot) (*guardrails.CompiledPolicyRules, error) {
	definition := snapshot.Definition.Request
	rules := &guardrails.CompiledPolicyRules{
		PolicyID: snapshot.PolicyID, Version: snapshot.Version,
		PIIAction:             guardrails.RuleAction(definition.PII),
		SecretAction:          guardrails.RuleAction(definition.Secret),
		PromptInjectionAction: guardrails.RuleAction(definition.PromptInjection),
	}
	for _, rule := range definition.CompiledRules.CustomPatterns {
		rules.CustomPatterns = append(rules.CustomPatterns, guardrails.CompiledPatternRule{
			ID: rule.ID, Name: rule.Name, Category: rule.Category, Regex: rule.Regex, Action: guardrails.RuleAction(rule.Action),
		})
	}
	for _, rule := range definition.CompiledRules.Allowlist {
		rules.Allowlist = append(rules.Allowlist, guardrails.CompiledListRule{ID: rule.ID, Value: rule.Value})
	}
	for _, rule := range definition.CompiledRules.Blocklist {
		rules.Blocklist = append(rules.Blocklist, guardrails.CompiledListRule{ID: rule.ID, Value: rule.Value})
	}
	for _, rule := range definition.CompiledRules.Validators {
		rules.Validators = append(rules.Validators, guardrails.CompiledValidatorRule{
			ID: rule.ID, Version: rule.Version, Name: rule.Name, Kind: guardrails.ValidatorKind(rule.Kind),
			Rule: rule.Rule, ExpectedResponse: rule.ExpectedResponse, Action: guardrails.RuleAction(rule.Action),
		})
	}
	return rules, nil
}

func actionFromGuardrail(action guardrails.RuleAction) (Action, error) {
	converted := Action(action)
	if err := converted.Validate(); err != nil {
		return "", fmt.Errorf("guardrail action %q: %w", action, err)
	}
	return converted, nil
}

func strongerProcessingAction(current, candidate Action) Action {
	priority := map[Action]int{ActionAllow: 0, ActionAuditOnly: 1, ActionMask: 2, ActionBlock: 3}
	if priority[candidate] > priority[current] {
		return candidate
	}
	return current
}
