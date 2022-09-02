/*
Copyright 2021 The Clusternet Authors.

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

package defaultbinder

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/generated/clientset/versioned/fake"
	framework "github.com/clusternet/clusternet/pkg/scheduler/framework/interfaces"
	frameworkruntime "github.com/clusternet/clusternet/pkg/scheduler/framework/runtime"
)

func TestDefaultBinder(t *testing.T) {
	testSubscription := &appsapi.Subscription{
		ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "ns"},
	}
	testClusters := framework.TargetClusters{
		BindingClusters: []string{
			"cluster-ns-03/xyz",
			"cluster-ns-01/def",
		},
	}
	tests := []struct {
		name           string
		injectErr      error
		wantedBindings []string
	}{
		{
			name: "successful",
			wantedBindings: []string{
				"cluster-ns-01/def",
				"cluster-ns-03/xyz",
			},
		}, {
			name:      "binding error",
			injectErr: errors.New("binding error"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var boundClusters []string
			client := fake.NewSimpleClientset(testSubscription)
			client.PrependReactor("patch", "subscriptions", func(action clienttesting.Action) (bool, runtime.Object, error) {
				if action.GetSubresource() != "status" {
					return false, nil, nil
				}
				if tt.injectErr != nil {
					return true, nil, tt.injectErr
				}
				return false, nil, nil
			})

			fh, err := frameworkruntime.NewFramework(nil, nil, frameworkruntime.WithClientSet(client))
			if err != nil {
				t.Fatal(err)
			}
			binder := &DefaultBinder{handle: fh}
			status := binder.Bind(context.Background(), nil, testSubscription, testClusters)
			gotSubscription, err := client.AppsV1alpha1().Subscriptions(testSubscription.Namespace).Get(context.Background(), testSubscription.Name, metav1.GetOptions{})
			if err != nil {
				t.Errorf("failed to get subscription: %v", err)
			}
			boundClusters = gotSubscription.Status.BindingClusters
			if got := status.AsError(); (tt.injectErr != nil) != (got != nil) {
				t.Errorf("got error %q, want %q", got, tt.injectErr)
			}
			if diff := cmp.Diff(tt.wantedBindings, boundClusters); diff != "" {
				t.Errorf("got different binding (-want, +got): %s", diff)
			}
		})
	}
}
