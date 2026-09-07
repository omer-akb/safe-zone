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
