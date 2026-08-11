package extproc

import (
	"errors"
	"fmt"
	"strings"
)

const trustedPolicyHeader = "x-tsz-policy"

var ErrInvalidPolicyIdentity = errors.New("invalid trusted policy identity")

// PolicyResolutionInput is gateway-neutral. Future policy controllers may
// resolve a route policy from any source without introducing Envoy or
// Kubernetes types into processing code.
type PolicyResolutionInput struct {
	Headers map[string][]string
	Gateway string
	Route   string
	Tenant  *string
}

// ResolvedPolicy is the immutable identity that is pinned to a stream. Tenant
// remains nullable for the Phase 2 MVP.
type ResolvedPolicy struct {
	PolicyID string
	Tenant   *string
}

// PolicyResolver resolves a trusted route policy before a stream is pinned.
type PolicyResolver interface {
	ResolvePolicy(PolicyResolutionInput) (ResolvedPolicy, error)
}

// HeaderPolicyResolver accepts exactly one policy header. The header must have
// been removed and overwritten by trusted route configuration before Envoy
// sends it to ext-proc; this resolver never treats a downstream source as
// authoritative by itself.
type HeaderPolicyResolver struct{}

func (HeaderPolicyResolver) ResolvePolicy(input PolicyResolutionInput) (ResolvedPolicy, error) {
	var values []string
	for name, candidates := range input.Headers {
		if strings.EqualFold(name, trustedPolicyHeader) {
			values = append(values, candidates...)
		}
	}
	if len(values) == 0 {
		return ResolvedPolicy{}, fmt.Errorf("%w: %s header is required", ErrInvalidPolicyIdentity, trustedPolicyHeader)
	}
	if len(values) != 1 {
		return ResolvedPolicy{}, fmt.Errorf("%w: %s header must occur exactly once", ErrInvalidPolicyIdentity, trustedPolicyHeader)
	}
	policyID := strings.TrimSpace(values[0])
	if policyID == "" {
		return ResolvedPolicy{}, fmt.Errorf("%w: %s header must not be empty", ErrInvalidPolicyIdentity, trustedPolicyHeader)
	}
	return ResolvedPolicy{PolicyID: policyID, Tenant: input.Tenant}, nil
}
