/*
Copyright 2026 The Clusternet Authors.

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

package deployer

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusternetfake "github.com/clusternet/clusternet/pkg/generated/clientset/versioned/fake"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/known"
)

func TestGetBaseForUpdateFallsBackToLiveGetWhenListerMissesAlreadyExistingBase(t *testing.T) {
	sub := &appsapi.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-subscription",
			Namespace: "test",
			UID:       types.UID("b6422900-42ab-4cb2-b05f-b13047af9f72"),
		},
	}
	existingBase := &appsapi.Base{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-subscription-ivakdtbq0s",
			Namespace: "cls-bbb-cluster-b",
			Labels: map[string]string{
				known.ConfigSubscriptionUIDLabel: string(sub.UID),
			},
		},
		Spec: appsapi.BaseSpec{
			Feeds: []appsapi.Feed{
				{
					APIVersion: "apps.clusternet.io/v1alpha1",
					Kind:       "HelmChart",
					Namespace:  "test",
					Name:       "old",
				},
			},
		},
	}
	client := clusternetfake.NewSimpleClientset(existingBase)

	emptyBaseIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})

	base, err := getBaseForUpdate(context.TODO(), client.AppsV1alpha1(), applisters.NewBaseLister(emptyBaseIndexer),
		"cls-bbb-cluster-b", "demo-subscription-ivakdtbq0s")
	if err != nil {
		t.Fatalf("getBaseForUpdate() returned error: %v", err)
	}

	if base.Spec.Feeds[0].Name != "old" {
		t.Fatalf("getBaseForUpdate() got feed name %q, want %q", base.Spec.Feeds[0].Name, "old")
	}
}

func TestFindBaseUIDsForHelmChartFallsBackToBaseFeedsWhenChartLabelMissing(t *testing.T) {
	chart := &appsapi.HelmChart{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nginx",
			Namespace: "charts",
		},
	}
	base := &appsapi.Base{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-base",
			Namespace: "cluster-a",
			UID:       types.UID("d4d9da23-cd45-4c61-baf7-c39b45795ed7"),
		},
		Spec: appsapi.BaseSpec{
			Feeds: []appsapi.Feed{
				{
					APIVersion: "apps.clusternet.io/v1alpha1",
					Kind:       "HelmChart",
					Namespace:  chart.Namespace,
					Name:       chart.Name,
				},
			},
		},
	}

	baseIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	if err := baseIndexer.Add(base); err != nil {
		t.Fatalf("failed to add Base to indexer: %v", err)
	}
	deployer := &Deployer{
		baseLister: applisters.NewBaseLister(baseIndexer),
	}

	got, err := deployer.findBaseUIDsForHelmChart(chart)
	if err != nil {
		t.Fatalf("findBaseUIDsForHelmChart() returned error: %v", err)
	}
	if len(got) != 1 || got[0] != string(base.UID) {
		t.Fatalf("findBaseUIDsForHelmChart() = %v, want [%s]", got, base.UID)
	}
}
