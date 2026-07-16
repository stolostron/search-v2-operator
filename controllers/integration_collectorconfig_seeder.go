// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"
	"fmt"
	"time"

	searchv1alpha1 "github.com/stolostron/search-v2-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// integrationSeedRetryInterval is how often IntegrationCollectorConfigSeeder retries applying the
// embedded integration CollectorConfigs after a failed attempt (e.g. the API server or the
// CollectorConfig webhook isn't ready yet during a fresh install).
const integrationSeedRetryInterval = 10 * time.Second

// IntegrationCollectorConfigSeeder is a controller-runtime manager.Runnable (see main.go's
// mgr.Add call) that applies the built-in integration CollectorConfigs exactly once per operator
// process, at manager startup — deliberately not on every reconcile. See
// applyIntegrationCollectorConfigs for why running this only at startup (and unconditionally
// overwriting, no diffing) is the intended behavior, not a limitation.
//
// This intentionally isn't a sync.Once: a plain sync.Once marks itself "done" the moment its
// function is called, even if that call failed (e.g. the CollectorConfig webhook's CA bundle
// hasn't been injected yet on a fresh install — a known startup race, see docs/ARCHITECTURE.md).
// Start retries on a short interval until it succeeds once, then returns — giving "run once per
// pod start" semantics without the risk of permanently giving up on a transient early failure.
type IntegrationCollectorConfigSeeder struct {
	Client client.Client

	// Namespace is used if non-empty. Left empty in practice — WATCH_NAMESPACE/POD_NAMESPACE
	// are not reliably set in real deployments (observed empty on a live ACM 5.0 install), so
	// Start falls back to discovering the namespace from the live Search CR (named
	// OperatorName) instead of trusting an env var passed in at construction time. Exposed as a
	// field mainly for tests, which don't have a Search CR to discover from.
	Namespace string

	// RetryInterval overrides integrationSeedRetryInterval. Defaults to it when zero; exposed
	// mainly so tests can exercise the retry-then-succeed path without a real 10s wait.
	RetryInterval time.Duration
}

// Start implements manager.Runnable. It blocks (retrying on failure) until the embedded
// integration CollectorConfigs have been successfully applied once, then returns nil. Returning
// an error here would be treated by controller-runtime as fatal to the whole manager, so this
// only returns when ctx is done (pod shutting down) or once it has fully succeeded.
func (s *IntegrationCollectorConfigSeeder) Start(ctx context.Context) error {
	interval := s.RetryInterval
	if interval <= 0 {
		interval = integrationSeedRetryInterval
	}
	err := wait.PollUntilContextCancel(ctx, interval, true,
		func(ctx context.Context) (bool, error) {
			namespace, paused, err := s.resolveSearch(ctx)
			if err != nil {
				log.Error(err, "Could not resolve namespace for integration CollectorConfig seeding, will retry")
				return false, nil // keep polling — the Search CR may not exist yet on a fresh install.
			}
			if paused {
				// search-pause: true must halt all operator-driven writes, including this one —
				// keep polling (so seeding still happens promptly once unpaused) without writing.
				log.V(2).Info("Search reconciliation is paused, deferring integration CollectorConfig seeding")
				return false, nil
			}
			if err := applyIntegrationCollectorConfigs(ctx, s.Client, namespace); err != nil {
				log.Error(err, "Could not apply built-in integration CollectorConfigs, will retry")
				return false, nil // keep polling — this is expected on a fresh install.
			}
			return true, nil
		})
	if err != nil {
		// Only reachable if ctx was canceled (pod shutting down) before success.
		log.V(2).Info("Stopped retrying integration CollectorConfig seeding", "reason", err)
		return nil
	}
	log.Info("Finished applying built-in integration CollectorConfigs")
	return nil
}

// NeedLeaderElection implements manager.LeaderElectionRunnable. This writes cluster state, so it
// should only run on the elected leader when leader election is enabled — same as the
// reconciler itself.
func (s *IntegrationCollectorConfigSeeder) NeedLeaderElection() bool {
	return true
}

// resolveSearch returns the namespace and search-pause state of the live Search CR. There is
// always supposed to be exactly one, named OperatorName (see search_controller.go) — this
// requires exactly one match rather than returning the first one found, so a duplicate CR (a bug
// elsewhere) causes this to keep retrying with an error instead of silently seeding an arbitrary
// namespace. If s.Namespace is explicitly set (used by tests), it's used as-is and paused is
// always false, since there's no Search CR to check in that case.
//
// The rest of the reconciler already gets its namespace this way (from the reconciled object
// itself, via a cluster-wide watch), not from an env var — WATCH_NAMESPACE/POD_NAMESPACE are not
// reliably set in real deployments.
func (s *IntegrationCollectorConfigSeeder) resolveSearch(
	ctx context.Context,
) (namespace string, paused bool, err error) {
	if s.Namespace != "" {
		return s.Namespace, false, nil
	}
	list := &searchv1alpha1.SearchList{}
	if err := s.Client.List(ctx, list); err != nil {
		return "", false, err
	}
	var match *searchv1alpha1.Search
	for i := range list.Items {
		if list.Items[i].Name == OperatorName {
			if match != nil {
				return "", false, fmt.Errorf(
					"found multiple search CRs named %q (namespaces %q and %q) — refusing to guess which one to use",
					OperatorName, match.Namespace, list.Items[i].Namespace)
			}
			match = &list.Items[i]
		}
	}
	if match == nil {
		return "", false, fmt.Errorf("search CR %q not found in any namespace", OperatorName)
	}
	return match.Namespace, IsPaused(match.GetAnnotations()), nil
}
