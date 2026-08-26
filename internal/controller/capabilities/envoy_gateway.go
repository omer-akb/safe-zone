package capabilities

import (
	"errors"
	"fmt"

	securityv1alpha1 "thyris-sz/api/v1alpha1"
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

func CheckCapabilities(spec securityv1alpha1.TSZGuardrailPolicySpec, caps AdapterCapabilities) error {
	if spec.Streaming != nil && spec.Streaming.Mode == "Windowed" && caps.ResponseStreaming != StreamingWindowed {
		return fmt.Errorf("%w: adapter %s does not support windowed response streaming", ErrUnsupportedCapability, caps.Name)
	}
	if spec.Streaming != nil && spec.Streaming.Mode == "Windowed" && spec.Response != nil {
		responseActions := []securityv1alpha1.PolicyAction{
			spec.Response.PII,
			spec.Response.Secret,
			spec.Response.UnsafeContent,
		}
		for _, action := range responseActions {
			if action == securityv1alpha1.PolicyActionBlock {
				return fmt.Errorf("%w: windowed response streaming does not support BLOCK actions", ErrUnsupportedCapability)
			}
		}
	}
	return nil
}
