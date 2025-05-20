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

package template

import (
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

func transformManifest(manifest *appsapi.Manifest) (*unstructured.Unstructured, error) {
	result := &unstructured.Unstructured{}
	if err := utils.Unmarshal(manifest.Template.Raw, result); err != nil {
		return nil, errors.NewInternalError(err)
	}
	result.SetGeneration(manifest.Generation)
	result.SetCreationTimestamp(manifest.CreationTimestamp)
	result.SetResourceVersion(manifest.ResourceVersion)
	result.SetUID(manifest.UID)
	result.SetDeletionGracePeriodSeconds(manifest.DeletionGracePeriodSeconds)
	result.SetDeletionTimestamp(manifest.DeletionTimestamp)

	annotations := result.GetAnnotations()
	if val, ok := manifest.Annotations[known.FeedProtectionAnnotation]; ok {
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations[known.FeedProtectionAnnotation] = val
	}
	result.SetAnnotations(annotations)

	return result, nil
}
