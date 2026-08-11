package extproc

import (
	"errors"
	"testing"
)

func TestHeaderPolicyResolverAcceptsExactlyOneTrustedPolicy(t *testing.T) {
	tenant := "tenant-a"
	resolved, err := (HeaderPolicyResolver{}).ResolvePolicy(PolicyResolutionInput{
		Headers: map[string][]string{"X-TSZ-Policy": {" route-policy "}},
		Tenant:  &tenant,
	})
	if err != nil {
		t.Fatalf("ResolvePolicy() error = %v", err)
	}
	if resolved.PolicyID != "route-policy" || resolved.Tenant == nil || *resolved.Tenant != tenant {
		t.Fatalf("ResolvePolicy() = %+v", resolved)
	}
}

func TestHeaderPolicyResolverRejectsMissingDuplicateAndEmptyValues(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string][]string
	}{
		{name: "missing", headers: map[string][]string{}},
		{name: "empty", headers: map[string][]string{trustedPolicyHeader: {"  "}}},
		{name: "duplicate values", headers: map[string][]string{trustedPolicyHeader: {"one", "two"}}},
		{name: "duplicate header casing", headers: map[string][]string{"X-TSZ-Policy": {"one"}, "x-tsz-policy": {"two"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := (HeaderPolicyResolver{}).ResolvePolicy(PolicyResolutionInput{Headers: test.headers})
			if !errors.Is(err, ErrInvalidPolicyIdentity) {
				t.Fatalf("ResolvePolicy() error = %v, want ErrInvalidPolicyIdentity", err)
			}
		})
	}
}

func TestHeaderPolicyResolverDoesNotUseGuardrailOverrideHeaders(t *testing.T) {
	resolved, err := (HeaderPolicyResolver{}).ResolvePolicy(PolicyResolutionInput{Headers: map[string][]string{
		trustedPolicyHeader:     {"required-route-policy"},
		"x-tsz-guardrails":      {"none"},
		"x-tsz-guardrails-mode": {"open"},
	}})
	if err != nil {
		t.Fatalf("ResolvePolicy() error = %v", err)
	}
	if resolved.PolicyID != "required-route-policy" {
		t.Fatalf("policy override changed resolved policy: %+v", resolved)
	}
}
