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
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusternetclientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
)

type MetaOption struct {
	MetaData MetaData `json:"metadata"`
}

type MetaData struct {
	Labels      map[string]*string `json:"labels,omitempty"`
	Annotations map[string]*string `json:"annotations,omitempty"`
}

type JsonPatchOption struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func PatchManifestLabelsAndAnnotations(clusternetClient *clusternetclientset.Clientset, manifest *appsapi.Manifest,
	labels, annotations map[string]*string) error {
	patchData, err := GetPatchDataForLabelsAndAnnotations(labels, annotations)
	if err != nil {
		return err
	}
	if patchData == nil {
		return nil
	}

	_, err = clusternetClient.AppsV1alpha1().Manifests(manifest.Namespace).Patch(context.TODO(),
		manifest.Name,
		types.MergePatchType,
		patchData,
		metav1.PatchOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func PatchHelmChartLabelsAndAnnotations(clusternetClient *clusternetclientset.Clientset, chart *appsapi.HelmChart,
	labels, annotations map[string]*string) error {
	patchData, err := GetPatchDataForLabelsAndAnnotations(labels, annotations)
	if err != nil {
		return err
	}
	if patchData == nil {
		return nil
	}

	_, err = clusternetClient.AppsV1alpha1().HelmCharts(chart.Namespace).Patch(context.TODO(),
		chart.Name,
		types.MergePatchType,
		patchData,
		metav1.PatchOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func GetPatchDataForLabelsAndAnnotations(labels, annotations map[string]*string) ([]byte, error) {
	labelOption := MetaOption{}
	if labels != nil {
		labelOption.MetaData.Labels = labels
	}
	if annotations != nil {
		labelOption.MetaData.Annotations = annotations
	}
	if labels == nil && annotations == nil {
		return nil, nil
	}
	return json.Marshal(labelOption)
}
