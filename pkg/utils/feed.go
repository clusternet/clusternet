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

package utils

import (
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/known"
)

func GetLabelsSelectorFromFeed(feed appsapi.Feed) (labels.Selector, error) {
	var gv schema.GroupVersion
	var err error
	if len(feed.APIVersion) > 0 {
		gv, err = schema.ParseGroupVersion(feed.APIVersion)
		if err != nil {
			return nil, err
		}
	}

	labelSet := labels.Set{
		known.ConfigGroupLabel:     gv.Group,
		known.ConfigVersionLabel:   gv.Version,
		known.ConfigKindLabel:      feed.Kind,
		known.ConfigNamespaceLabel: feed.Namespace,
	}

	if len(feed.Name) > 0 {
		labelSet[known.ConfigNameLabel] = feed.Name
	}
	selector := labels.SelectorFromSet(labelSet)

	if feed.FeedSelector != nil {
		feedSelector, err := metav1.LabelSelectorAsSelector(feed.FeedSelector)
		if err != nil {
			return nil, err
		}

		reqs, _ := feedSelector.Requirements()
		for _, r := range reqs {
			selector = selector.Add(r)
		}
	}

	return selector, nil
}

func ListManifestsBySelector(manifestLister applisters.ManifestLister, feed appsapi.Feed) ([]*appsapi.Manifest, error) {
	if manifestLister == nil {
		return nil, errors.New("manifestLister is nil when listing charts by selector")
	}

	selector, err := GetLabelsSelectorFromFeed(feed)
	if err != nil {
		return nil, err
	}
	return manifestLister.Manifests(appsapi.ReservedNamespace).List(selector)
}

func ListChartsBySelector(chartLister applisters.HelmChartLister, feed appsapi.Feed) ([]*appsapi.HelmChart, error) {
	if chartLister == nil {
		return nil, errors.New("chartLister is nil when listing charts by selector")
	}

	if len(feed.Namespace) == 0 {
		return nil, fmt.Errorf("namespace is not set in Feed %s", FormatFeed(feed))
	}
	if len(feed.Name) > 0 {
		chart, err := chartLister.HelmCharts(feed.Namespace).Get(feed.Name)
		if err != nil {
			return nil, err
		}
		return []*appsapi.HelmChart{chart}, nil
	}

	if feed.FeedSelector != nil {
		feedSelector, err := metav1.LabelSelectorAsSelector(feed.FeedSelector)
		if err != nil {
			return nil, err
		}
		return chartLister.HelmCharts(feed.Namespace).List(feedSelector)
	}

	return nil, fmt.Errorf("failed to find matched %s", FormatFeed(feed))
}

func FormatFeed(feed appsapi.Feed) string {
	if len(feed.Name) != 0 {
		return fmt.Sprintf("%s %s/%s", feed.Kind, feed.Namespace, feed.Name)
	}

	return fmt.Sprintf("%s with selector %q", feed.Kind, feed.FeedSelector.String())
}
