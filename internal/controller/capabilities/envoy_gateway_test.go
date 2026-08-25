package capabilities

import (
	"errors"
	"testing"

	securityv1alpha1 "thyris-sz/api/v1alpha1"
)

func TestCheckCapabilitiesAcceptsWindowedStreaming(t *testing.T) {
	err := CheckCapabilities(securityv1alpha1.TSZGuardrailPolicySpec{Streaming: &securityv1alpha1.StreamingSpec{Mode: "Windowed"}}, EnvoyGatewayCapabilities)
	if err != nil {
		t.Fatalf("CheckCapabilities() error = %v", err)
	}
}

func TestCheckCapabilitiesRejectsUnsupportedWindowedStreaming(t *testing.T) {
	err := CheckCapabilities(securityv1alpha1.TSZGuardrailPolicySpec{
		Streaming: &securityv1alpha1.StreamingSpec{Enabled: true, Mode: "Windowed"},
		Response:  &securityv1alpha1.ResponsePolicySpec{Enabled: true, PII: securityv1alpha1.PolicyActionMask, Secret: securityv1alpha1.PolicyActionBlock, UnsafeContent: securityv1alpha1.PolicyActionAuditOnly},
	}, EnvoyGatewayCapabilities)
	if !errors.Is(err, ErrUnsupportedCapability) {
		t.Fatalf("CheckCapabilities() error = %v, want ErrUnsupportedCapability", err)
	}
}
