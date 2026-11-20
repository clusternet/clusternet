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

package localizer

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	clusterlisters "github.com/clusternet/clusternet/pkg/generated/listers/clusters/v1beta1"
)

func TestApplyOverridesToDescriptionFallsBackToFeedWhenLocalizationUIDLabelMissing(t *testing.T) {
	chart := &appsapi.HelmChart{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nginx",
			Namespace: "charts",
			UID:       types.UID("0230fe0b-c23a-49fe-b02b-829d76b98a25"),
		},
	}
	loc := &appsapi.Localization{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "nginx-values",
			Namespace: "cluster-a",
		},
		Spec: appsapi.LocalizationSpec{
			Feed: appsapi.Feed{
				Kind:       chartKind.Kind,
				APIVersion: chartKind.GroupVersion().String(),
				Namespace:  chart.Namespace,
				Name:       chart.Name,
			},
			Overrides: []appsapi.OverrideConfig{
				{
					Name:  "set replica count",
					Type:  appsapi.HelmType,
					Value: `{"replicaCount":3}`,
				},
			},
		},
	}

	chartIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	if err := chartIndexer.Add(chart); err != nil {
		t.Fatalf("failed to add HelmChart to indexer: %v", err)
	}
	locIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	if err := locIndexer.Add(loc); err != nil {
		t.Fatalf("failed to add Localization to indexer: %v", err)
	}

	l := &Localizer{
		chartLister: applisters.NewHelmChartLister(chartIndexer),
		locLister:   applisters.NewLocalizationLister(locIndexer),
		globLister:  applisters.NewGlobalizationLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})),
		mclsLister:  clusterlisters.NewManagedClusterLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})),
	}

	desc := &appsapi.Description{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: "cluster-a",
		},
		Spec: appsapi.DescriptionSpec{
			Deployer: appsapi.DescriptionHelmDeployer,
			Charts: []appsapi.ChartReference{
				{
					Namespace: chart.Namespace,
					Name:      chart.Name,
				},
			},
			ChartRaw: [][]byte{[]byte(`{"kind":"HelmChart","metadata":{"name":"nginx","namespace":"charts"}}`)},
		},
	}

	if err := l.ApplyOverridesToDescription(desc); err != nil {
		t.Fatalf("ApplyOverridesToDescription() returned error: %v", err)
	}
	if got, want := string(desc.Spec.Raw[0]), `{"replicaCount":3}`; got != want {
		t.Fatalf("ApplyOverridesToDescription() raw override = %q, want %q", got, want)
	}
}
