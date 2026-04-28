// Copyright Contributors to the Open Cluster Management project

// This file provides an envtest-based integration test for the webhook server.
// It requires kubebuilder binaries (etcd, kube-apiserver) to be installed.
// Set KUBEBUILDER_ASSETS to the path containing the binaries before running.

package v1alpha1

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// TestWebhookServer boots an envtest API server with the webhook registered and
// verifies the webhook server becomes reachable over TLS.
func TestWebhookServer(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS not set, skipping envtest integration test")
	}

	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Bootstrap envtest with CRDs and webhook config
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: false,
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			Paths: []string{filepath.Join("..", "..", "config", "webhook")},
		},
	}

	cfg, err := testEnv.Start()
	require.NoError(t, err, "failed to start envtest")
	require.NotNil(t, cfg)
	defer func() {
		assert.NoError(t, testEnv.Stop(), "failed to stop envtest")
	}()

	// Build scheme
	scheme := runtime.NewScheme()
	require.NoError(t, AddToScheme(scheme))
	require.NoError(t, admissionv1beta1.AddToScheme(scheme))

	// Start manager with webhook server
	webhookOpts := testEnv.WebhookInstallOptions
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		WebhookServer: webhook.NewServer(webhook.Options{
			Host:    webhookOpts.LocalServingHost,
			Port:    webhookOpts.LocalServingPort,
			CertDir: webhookOpts.LocalServingCertDir,
		}),
		LeaderElection: false,
	})
	require.NoError(t, err, "failed to create manager")

	require.NoError(t, (&CollectorConfig{}).SetupWebhookWithManager(mgr), "failed to setup webhook")

	go func() {
		if err := mgr.Start(ctx); err != nil {
			t.Logf("manager exited: %v", err)
		}
	}()

	// Wait for webhook server to accept TLS connections
	dialer := &net.Dialer{Timeout: time.Second}
	addrPort := fmt.Sprintf("%s:%d", webhookOpts.LocalServingHost, webhookOpts.LocalServingPort)

	require.Eventually(t, func() bool {
		conn, err := tls.DialWithDialer(dialer, "tcp", addrPort, &tls.Config{InsecureSkipVerify: true})
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, 10*time.Second, 250*time.Millisecond, "webhook server did not become ready")
}
