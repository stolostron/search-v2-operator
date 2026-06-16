// Copyright Contributors to the Open Cluster Management project

package controllers

import (
	"context"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	webhookConfigName     = "search-v2-operator-validating-webhook-configuration"
	caInjectionAnnotation = "service.beta.openshift.io/inject-cabundle"
)

// ensureWebhookCAInjection ensures the ValidatingWebhookConfiguration has the
// OpenShift service-ca CA injection annotation. OLM creates the VWC from the
// CSV webhookdefinitions, which doesn't support custom annotations. Without
// this annotation, the service-ca controller won't inject the CA bundle and
// the webhook TLS handshake will fail.
func (r *SearchReconciler) ensureWebhookCAInjection(ctx context.Context) error {
	vwc := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	err := r.Client.Get(ctx, types.NamespacedName{Name: webhookConfigName}, vwc)
	if err != nil {
		return err
	}

	if vwc.Annotations[caInjectionAnnotation] == "true" {
		log.V(2).Info("Webhook CA injection annotation already present")
		return nil
	}

	if vwc.Annotations == nil {
		vwc.Annotations = map[string]string{}
	}
	vwc.Annotations[caInjectionAnnotation] = "true"

	log.Info("Adding webhook CA injection annotation to ValidatingWebhookConfiguration")
	return r.Client.Update(ctx, vwc, &client.UpdateOptions{})
}
