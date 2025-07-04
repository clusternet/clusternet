/*
Copyright The Clusternet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
// Code generated by client-gen. DO NOT EDIT.

package v1alpha1

import (
	context "context"

	appsv1alpha1 "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	applyconfigurationappsv1alpha1 "github.com/clusternet/clusternet/pkg/generated/applyconfiguration/apps/v1alpha1"
	scheme "github.com/clusternet/clusternet/pkg/generated/clientset/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	gentype "k8s.io/client-go/gentype"
)

// SubscriptionsGetter has a method to return a SubscriptionInterface.
// A group's client should implement this interface.
type SubscriptionsGetter interface {
	Subscriptions(namespace string) SubscriptionInterface
}

// SubscriptionInterface has methods to work with Subscription resources.
type SubscriptionInterface interface {
	Create(ctx context.Context, subscription *appsv1alpha1.Subscription, opts v1.CreateOptions) (*appsv1alpha1.Subscription, error)
	Update(ctx context.Context, subscription *appsv1alpha1.Subscription, opts v1.UpdateOptions) (*appsv1alpha1.Subscription, error)
	// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
	UpdateStatus(ctx context.Context, subscription *appsv1alpha1.Subscription, opts v1.UpdateOptions) (*appsv1alpha1.Subscription, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*appsv1alpha1.Subscription, error)
	List(ctx context.Context, opts v1.ListOptions) (*appsv1alpha1.SubscriptionList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *appsv1alpha1.Subscription, err error)
	Apply(ctx context.Context, subscription *applyconfigurationappsv1alpha1.SubscriptionApplyConfiguration, opts v1.ApplyOptions) (result *appsv1alpha1.Subscription, err error)
	// Add a +genclient:noStatus comment above the type to avoid generating ApplyStatus().
	ApplyStatus(ctx context.Context, subscription *applyconfigurationappsv1alpha1.SubscriptionApplyConfiguration, opts v1.ApplyOptions) (result *appsv1alpha1.Subscription, err error)
	SubscriptionExpansion
}

// subscriptions implements SubscriptionInterface
type subscriptions struct {
	*gentype.ClientWithListAndApply[*appsv1alpha1.Subscription, *appsv1alpha1.SubscriptionList, *applyconfigurationappsv1alpha1.SubscriptionApplyConfiguration]
}

// newSubscriptions returns a Subscriptions
func newSubscriptions(c *AppsV1alpha1Client, namespace string) *subscriptions {
	return &subscriptions{
		gentype.NewClientWithListAndApply[*appsv1alpha1.Subscription, *appsv1alpha1.SubscriptionList, *applyconfigurationappsv1alpha1.SubscriptionApplyConfiguration](
			"subscriptions",
			c.RESTClient(),
			scheme.ParameterCodec,
			namespace,
			func() *appsv1alpha1.Subscription { return &appsv1alpha1.Subscription{} },
			func() *appsv1alpha1.SubscriptionList { return &appsv1alpha1.SubscriptionList{} },
		),
	}
}
