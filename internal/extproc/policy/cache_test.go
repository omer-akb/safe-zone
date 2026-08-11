package policy

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type cacheRepositoryStub struct {
	Repository
	mu        sync.Mutex
	snapshots []PolicySnapshot
	err       error
}

func (r *cacheRepositoryStub) ActiveSnapshots(context.Context) ([]PolicySnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]PolicySnapshot(nil), r.snapshots...), r.err
}

func (r *cacheRepositoryStub) ActiveSnapshot(_ context.Context, policyName string, tenant *string) (PolicySnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	wanted := PolicyIdentifier(policyName, tenant)
	for _, snapshot := range r.snapshots {
		if PolicyIdentifier(snapshot.PolicyName, snapshot.Tenant) == wanted && snapshot.Status == StatusActive {
			return snapshot, nil
		}
	}
	return PolicySnapshot{}, ErrNotFound
}

func (r *cacheRepositoryStub) setSnapshots(snapshots ...PolicySnapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snapshots = snapshots
}

func TestCacheReadinessAndImmutableGet(t *testing.T) {
	repository := &cacheRepositoryStub{err: errors.New("PostgreSQL unavailable")}
	redisClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	defer redisClient.Close()
	cache, err := NewCache(repository, redisClient, time.Minute)
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	if cache.Ready() {
		t.Fatal("new cache is ready before first full load")
	}
	if err := cache.Reconcile(context.Background()); err == nil {
		t.Fatal("Reconcile() error = nil while repository is unavailable")
	}
	if cache.Ready() {
		t.Fatal("cache became ready after failed full load")
	}

	snapshot := completeActiveSnapshot("default", 1)
	repository.mu.Lock()
	repository.err = nil
	repository.snapshots = []PolicySnapshot{snapshot}
	repository.mu.Unlock()
	if err := cache.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !cache.Ready() {
		t.Fatal("cache is not ready after successful full load")
	}

	first, found := cache.Get("default")
	if !found {
		t.Fatal("Get(default) did not find active snapshot")
	}
	first.Definition.Request.CustomPatternIDs[0] = "mutated-by-request"
	second, found := cache.Get("default")
	if !found {
		t.Fatal("second Get(default) did not find active snapshot")
	}
	if second.Definition.Request.CustomPatternIDs[0] != "1" {
		t.Fatalf("request mutation changed cached snapshot: %+v", second.Definition.Request.CustomPatternIDs)
	}

	newSnapshot := completeActiveSnapshot("default", 2)
	repository.setSnapshots(newSnapshot)
	if err := cache.handleActivationMessage(context.Background(), `{"policy_id":"default","version":2}`); err != nil {
		t.Fatalf("handleActivationMessage() error = %v", err)
	}
	latest, found := cache.Get("default")
	if !found || latest.Version != 2 {
		t.Fatalf("latest cached snapshot = %+v, want version 2", latest)
	}
	if first.Version != 1 {
		t.Fatalf("request-held snapshot changed during swap: %+v", first)
	}
}

func TestPolicyIdentifierPreservesTenant(t *testing.T) {
	tenant := "tenant/a"
	identifier := PolicyIdentifier("policy/b", &tenant)
	policyName, gotTenant, err := parsePolicyIdentifier(identifier)
	if err != nil {
		t.Fatalf("parsePolicyIdentifier() error = %v", err)
	}
	if policyName != "policy/b" || gotTenant == nil || *gotTenant != tenant {
		t.Fatalf("parsed identifier name=%q tenant=%v", policyName, gotTenant)
	}
}

func TestCachePeriodicallyReconcilesFromPostgreSQL(t *testing.T) {
	repository := &cacheRepositoryStub{snapshots: []PolicySnapshot{completeActiveSnapshot("initial", 1)}}
	redisClient := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: time.Millisecond,
	})
	defer redisClient.Close()
	cache, err := NewCache(repository, redisClient, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := cache.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	repository.setSnapshots(
		completeActiveSnapshot("initial", 1),
		completeActiveSnapshot("periodic", 1),
	)
	deadline := time.After(time.Second)
	for {
		if snapshot, found := cache.Get("periodic"); found && snapshot.Version == 1 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("periodic reconciliation did not load active snapshot")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func completeActiveSnapshot(policyName string, version int) PolicySnapshot {
	now := time.Now().UTC()
	return PolicySnapshot{
		ID:         int64(version),
		PolicyID:   1,
		PolicyName: policyName,
		Version:    &version,
		Status:     StatusActive,
		Definition: PolicyDefinition{
			Request: RequestPolicy{
				PII:              ActionMask,
				Secret:           ActionBlock,
				PromptInjection:  ActionAuditOnly,
				CustomPatternIDs: []string{"1"},
			},
		},
		IntegrityHash: "sha256:test",
		CompiledAt:    &now,
		ActivatedAt:   &now,
	}
}
