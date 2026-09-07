package capabilities

import (
	"errors"
	"fmt"

	securityv1beta1 "thyris-sz/api/v1beta1"
	"thyris-sz/internal/extproc/policy"
)

type StreamingCapability string

const (
	StreamingNone     StreamingCapability = "None"
	StreamingWindowed StreamingCapability = "Windowed"
)

type AdapterCapabilities struct {
	Name                                                                        string
	Version                                                                     string
	RequestHeaders, RequestBufferedBody, RequestBodyMutation, ImmediateResponse bool
	ResponseBufferedBody, ResponseBodyMutation                                  bool
	ResponseStreaming                                                           StreamingCapability
	DynamicMetadata, NativePolicyAttachment                                     bool
}

var EnvoyGatewayCapabilities = AdapterCapabilities{Name: "envoy-gateway", Version: "1.8.3", RequestHeaders: true, RequestBufferedBody: true, RequestBodyMutation: true, ImmediateResponse: true, ResponseBufferedBody: true, ResponseBodyMutation: true, ResponseStreaming: StreamingWindowed, DynamicMetadata: true, NativePolicyAttachment: true}
var ErrUnsupportedCapability = errors.New("unsupported adapter capability")

func CheckCapabilities(spec securityv1beta1.TSZGuardrailPolicySpec, caps AdapterCapabilities) error {
	if !caps.NativePolicyAttachment {
		return unsupported(caps, "native policy attachment")
	}
	if !caps.RequestHeaders {
		return unsupported(caps, "request headers")
	}
	if !caps.RequestBufferedBody {
		return unsupported(caps, "buffered request inspection")
	}
	if spec.Request != nil {
		if err := checkActions(caps, true, spec.Request.PII, spec.Request.Secret, spec.Request.PromptInjection); err != nil {
			return err
		}
	}
	if spec.Response != nil && spec.Response.Enabled {
		if !caps.ResponseBufferedBody {
			return unsupported(caps, "buffered response inspection")
		}
		if err := checkActions(caps, false, spec.Response.PII, spec.Response.Secret, spec.Response.UnsafeContent); err != nil {
			return err
		}
	}

	if spec.Streaming != nil && spec.Streaming.Mode == "Windowed" && caps.ResponseStreaming != StreamingWindowed {
		return fmt.Errorf("%w: adapter %s does not support windowed response streaming", ErrUnsupportedCapability, caps.Name)
	}
	if spec.Streaming != nil && spec.Streaming.Mode == "Windowed" && spec.Response != nil {
		responseActions := []securityv1beta1.PolicyAction{
			spec.Response.PII,
			spec.Response.Secret,
			spec.Response.UnsafeContent,
		}
		for _, action := range responseActions {
			if action == securityv1beta1.PolicyActionBlock {
				return fmt.Errorf("%w: windowed response streaming does not support BLOCK actions", ErrUnsupportedCapability)
			}
		}
	}
	return nil
}

func unsupported(caps AdapterCapabilities, feature string) error {
	return fmt.Errorf("%w: adapter %s does not support %s", ErrUnsupportedCapability, caps.Name, feature)
}

func checkActions(caps AdapterCapabilities, request bool, actions ...securityv1beta1.PolicyAction) error {
	for _, action := range actions {
		if action == securityv1beta1.PolicyActionMask {
			if request && !caps.RequestBodyMutation {
				return unsupported(caps, "request body mutation")
			}
			if !request && !caps.ResponseBodyMutation {
				return unsupported(caps, "response body mutation")
			}
		}
		if action == securityv1beta1.PolicyActionBlock && !caps.ImmediateResponse {
			return unsupported(caps, "blocking with an immediate response")
		}
	}
	return nil
}

// CheckDefinition checks resolved Postgres snapshots as well as Inline policy
// definitions before activation. Checking just the attachment spec would miss
// actions hidden behind policyRef and permit unsupported enforcement.
func CheckDefinition(def policy.PolicyDefinition, caps AdapterCapabilities) error {
	spec := securityv1beta1.TSZGuardrailPolicySpec{
		Request:   &securityv1beta1.RequestPolicySpec{PII: apiAction(def.Request.PII), Secret: apiAction(def.Request.Secret), PromptInjection: apiAction(def.Request.PromptInjection)},
		Response:  &securityv1beta1.ResponsePolicySpec{Enabled: def.Response.Enabled, PII: apiAction(def.Response.PII), Secret: apiAction(def.Response.Secret), UnsafeContent: apiAction(def.Response.UnsafeContent)},
		Streaming: &securityv1beta1.StreamingSpec{Enabled: def.Streaming.Mode == policy.StreamingModeWindowed, Mode: def.Streaming.Mode},
	}
	return CheckCapabilities(spec, caps)
}

func apiAction(action policy.Action) securityv1beta1.PolicyAction {
	switch action {
	case policy.ActionMask:
		return securityv1beta1.PolicyActionMask
	case policy.ActionBlock:
		return securityv1beta1.PolicyActionBlock
	case policy.ActionAuditOnly:
		return securityv1beta1.PolicyActionAuditOnly
	case policy.ActionAllow:
		return securityv1beta1.PolicyActionAllow
	default:
		return securityv1beta1.PolicyAction(action)
	}
}
