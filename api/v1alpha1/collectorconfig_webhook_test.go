// Copyright Contributors to the Open Cluster Management project

package v1alpha1

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("CollectorConfig Webhook", func() {
	Context("When creating CollectorConfig with invalid spec", func() {
		It("Should reject config with empty apiGroups", func() {
			config := &CollectorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: CollectorConfigSpec{
					CollectionRules: []CollectionRule{
						{
							Action: ActionInclude,
							ResourceSelector: ResourceSelector{
								APIGroups: []string{}, // Invalid: empty
								Kinds:     []string{"Pod"},
							},
						},
					},
				},
			}

			ctx := context.Background()
			_, err := config.ValidateCreate(ctx, config)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must specify at least one apiGroup"))
		})

		It("Should reject config with empty kinds", func() {
			config := &CollectorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: CollectorConfigSpec{
					CollectionRules: []CollectionRule{
						{
							Action: ActionInclude,
							ResourceSelector: ResourceSelector{
								APIGroups: []string{"apps"},
								Kinds:     []string{}, // Invalid: empty
							},
						},
					},
				},
			}

			ctx := context.Background()
			_, err := config.ValidateCreate(ctx, config)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must specify at least one kind"))
		})

		It("Should reject config with fields but multiple kinds", func() {
			config := &CollectorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: CollectorConfigSpec{
					CollectionRules: []CollectionRule{
						{
							Action: ActionInclude,
							ResourceSelector: ResourceSelector{
								APIGroups: []string{"apps"},
								Kinds:     []string{"Deployment", "StatefulSet"}, // Invalid: multiple kinds with fields
							},
							Fields: []Field{
								{
									Name:     "status",
									JSONPath: "{.status.replicas}",
									Type:     DataTypeNumber,
								},
							},
						},
					},
				},
			}

			ctx := context.Background()
			_, err := config.ValidateCreate(ctx, config)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must specify exactly 1 kind when fields are defined"))
		})

		It("Should accept valid config", func() {
			config := &CollectorConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "default",
				},
				Spec: CollectorConfigSpec{
					CollectionRules: []CollectionRule{
						{
							Action: ActionInclude,
							ResourceSelector: ResourceSelector{
								APIGroups: []string{"apps"},
								Kinds:     []string{"Deployment"},
							},
							Fields: []Field{
								{
									Name:     "replicas",
									JSONPath: "{.status.replicas}",
									Type:     DataTypeNumber,
								},
							},
							FieldSuffix: "custom",
						},
					},
				},
			}

			ctx := context.Background()
			_, err := config.ValidateCreate(ctx, config)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
