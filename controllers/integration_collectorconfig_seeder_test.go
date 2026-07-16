// Copyright Contributors to the Open Cluster Management project
package controllers

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestIntegrationCollectorConfigSeeder_AppliesConfigsAndReturns(t *testing.T) {
	r := setupReconciler()
	seeder := &IntegrationCollectorConfigSeeder{Client: r.Client, Namespace: testNamespace}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start should succeed on the first attempt and return promptly — it must not block for the
	// full retry interval when there's nothing to retry.
	done := make(chan error, 1)
	go func() { done <- seeder.Start(ctx) }()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return promptly after a successful first attempt")
	}

	cc := &searchv1alpha1.CollectorConfig{}
	require.NoError(t, r.Get(context.TODO(), types.NamespacedName{
		Name: "cnv-integration-collector-config", Namespace: testNamespace,
	}, cc))
	assert.NotEmpty(t, cc.Spec.CollectionRules)
}

func TestIntegrationCollectorConfigSeeder_StopsRetryingWhenContextCanceled(t *testing.T) {
	// An empty namespace makes every Create fail (CollectorConfig is namespaced), so Start must
	// keep retrying until the context is canceled, then return nil rather than blocking forever
	// or returning an error (which controller-runtime would treat as fatal to the manager).
	r := setupReconciler()
	seeder := &IntegrationCollectorConfigSeeder{Client: r.Client, Namespace: ""}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- seeder.Start(ctx) }()

	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err, "Start should return nil, not an error, on context cancellation")
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not stop after context cancellation")
	}
}

func TestIntegrationCollectorConfigSeeder_NeedsLeaderElection(t *testing.T) {
	seeder := &IntegrationCollectorConfigSeeder{}
	assert.True(t, seeder.NeedLeaderElection())
}

// Regression test for a real bug caught during live cluster testing: WATCH_NAMESPACE/POD_NAMESPACE
// are empty in a real ACM 5.0 deployment (the manager watches cluster-wide instead), so the
// seeder must discover its namespace from the live Search CR rather than trusting those env vars.
func TestIntegrationCollectorConfigSeeder_DiscoversNamespaceFromSearchCR(t *testing.T) {
	instance := newSearchInstance() // named OperatorName, namespace testNamespace.
	r := setupReconciler(instance)
	// Namespace intentionally left empty, exactly as main.go now constructs it.
	seeder := &IntegrationCollectorConfigSeeder{Client: r.Client, RetryInterval: 20 * time.Millisecond}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- seeder.Start(ctx) }()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not discover the namespace and finish")
	}

	cc := &searchv1alpha1.CollectorConfig{}
	require.NoError(t, r.Get(context.TODO(), types.NamespacedName{
		Name: "cnv-integration-collector-config", Namespace: testNamespace,
	}, cc), "config should be created in the Search CR's namespace, discovered dynamically")
}

// If the Search CR doesn't exist yet (very early in a fresh install), Start must keep retrying
// rather than erroring out or creating configs in the wrong (empty) namespace.
func TestIntegrationCollectorConfigSeeder_RetriesUntilSearchCRExists(t *testing.T) {
	r := setupReconciler() // no Search CR yet.
	seeder := &IntegrationCollectorConfigSeeder{Client: r.Client, RetryInterval: 20 * time.Millisecond}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- seeder.Start(ctx) }()

	// Confirm it's still retrying (not stuck/errored) while the CR is absent.
	select {
	case err := <-done:
		t.Fatalf("Start should still be retrying with no Search CR present, got: %v", err)
	case <-time.After(150 * time.Millisecond):
		// Expected — still polling.
	}

	// Now create the CR; the next poll should discover it and succeed.
	instance := newSearchInstance()
	require.NoError(t, r.Create(context.TODO(), instance))

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not recover after the Search CR was created")
	}

	// Assert on the actual side effect, not just "Start returned nil" — cancellation and
	// success both return nil, so this is what actually proves it succeeded rather than timing out.
	cc := &searchv1alpha1.CollectorConfig{}
	require.NoError(t, r.Get(context.TODO(), types.NamespacedName{
		Name: "cnv-integration-collector-config", Namespace: testNamespace,
	}, cc))
}

// Simulates the exact race this design is meant to survive: the first attempt fails (e.g. the
// CollectorConfig webhook's CA bundle isn't injected yet), and a later attempt succeeds. Start
// must retry rather than giving up after the first failure (the sync.Once flaw it was built to
// avoid), and must return promptly once it does succeed rather than blocking for the full
// interval afterward.
func TestIntegrationCollectorConfigSeeder_RetriesUntilSuccess(t *testing.T) {
	var attempts atomic.Int32
	base := setupReconciler().Client.(client.WithWatch)
	flakyClient := interceptor.NewClient(base, interceptor.Funcs{
		Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			if attempts.Add(1) == 1 {
				return errors.New("simulated transient failure on first attempt")
			}
			return c.Create(ctx, obj, opts...)
		},
	})
	seeder := &IntegrationCollectorConfigSeeder{
		Client:        flakyClient,
		Namespace:     testNamespace,
		RetryInterval: 20 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- seeder.Start(ctx) }()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Start never recovered from the first transient failure")
	}
	assert.GreaterOrEqual(t, attempts.Load(), int32(2), "must have retried after the first failure")
}
