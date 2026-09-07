package capabilities

import (
	"errors"
	"testing"

	securityv1beta1 "thyris-sz/api/v1beta1"
)

func TestCheckCapabilitiesAcceptsWindowedStreaming(t *testing.T) {
	err := CheckCapabilities(securityv1beta1.TSZGuardrailPolicySpec{Streaming: &securityv1beta1.StreamingSpec{Mode: "Windowed"}}, EnvoyGatewayCapabilities)
	if err != nil {
		t.Fatalf("CheckCapabilities() error = %v", err)
	}
}

func TestCheckCapabilitiesRejectsUnsupportedWindowedStreaming(t *testing.T) {
	err := CheckCapabilities(securityv1beta1.TSZGuardrailPolicySpec{
		Streaming: &securityv1beta1.StreamingSpec{Enabled: true, Mode: "Windowed"},
		Response:  &securityv1beta1.ResponsePolicySpec{Enabled: true, PII: securityv1beta1.PolicyActionMask, Secret: securityv1beta1.PolicyActionBlock, UnsafeContent: securityv1beta1.PolicyActionAuditOnly},
	}, EnvoyGatewayCapabilities)
	if !errors.Is(err, ErrUnsupportedCapability) {
		t.Fatalf("CheckCapabilities() error = %v, want ErrUnsupportedCapability", err)
	}
}

func TestCapabilitiesRejectUnsupportedEnforcement(t *testing.T) {
	tests := []struct {
		name     string
		restrict func(*AdapterCapabilities)
		spec     securityv1beta1.TSZGuardrailPolicySpec
	}{
		{"native attachment", func(c *AdapterCapabilities) { c.NativePolicyAttachment = false }, securityv1beta1.TSZGuardrailPolicySpec{}},
		{"headers", func(c *AdapterCapabilities) { c.RequestHeaders = false }, securityv1beta1.TSZGuardrailPolicySpec{}},
		{"request inspection", func(c *AdapterCapabilities) { c.RequestBufferedBody = false }, securityv1beta1.TSZGuardrailPolicySpec{}},
		{"request masking", func(c *AdapterCapabilities) { c.RequestBodyMutation = false }, securityv1beta1.TSZGuardrailPolicySpec{Request: &securityv1beta1.RequestPolicySpec{PII: securityv1beta1.PolicyActionMask}}},
		{"blocking", func(c *AdapterCapabilities) { c.ImmediateResponse = false }, securityv1beta1.TSZGuardrailPolicySpec{Request: &securityv1beta1.RequestPolicySpec{Secret: securityv1beta1.PolicyActionBlock}}},
		{"response inspection", func(c *AdapterCapabilities) { c.ResponseBufferedBody = false }, securityv1beta1.TSZGuardrailPolicySpec{Response: &securityv1beta1.ResponsePolicySpec{Enabled: true}}},
		{"response masking", func(c *AdapterCapabilities) { c.ResponseBodyMutation = false }, securityv1beta1.TSZGuardrailPolicySpec{Response: &securityv1beta1.ResponsePolicySpec{Enabled: true, PII: securityv1beta1.PolicyActionMask}}},
		{"streaming", func(c *AdapterCapabilities) { c.ResponseStreaming = StreamingNone }, securityv1beta1.TSZGuardrailPolicySpec{Streaming: &securityv1beta1.StreamingSpec{Enabled: true, Mode: "Windowed"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			caps := EnvoyGatewayCapabilities
			tt.restrict(&caps)
			if err := CheckCapabilities(tt.spec, caps); !errors.Is(err, ErrUnsupportedCapability) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestAuditOnlyDoesNotRequireMutationOrBlocking(t *testing.T) {
	caps := EnvoyGatewayCapabilities
	caps.RequestBodyMutation, caps.ResponseBodyMutation, caps.ImmediateResponse = false, false, false
	spec := securityv1beta1.TSZGuardrailPolicySpec{
		Request:  &securityv1beta1.RequestPolicySpec{PII: securityv1beta1.PolicyActionAuditOnly},
		Response: &securityv1beta1.ResponsePolicySpec{Enabled: true, PII: securityv1beta1.PolicyActionAuditOnly},
	}
	if err := CheckCapabilities(spec, caps); err != nil {
		t.Fatal(err)
	}
}
