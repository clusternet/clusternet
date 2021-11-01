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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
)

func trimCommonMetadata(result *unstructured.Unstructured) {
	unstructured.RemoveNestedField(result.Object, "metadata", "uid")
	unstructured.RemoveNestedField(result.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(result.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(result.Object, "metadata", "resourceVersion")
}

func trimCoreService(result *unstructured.Unstructured) {
	serviceType, found, err := unstructured.NestedString(result.Object, "spec", "type")
	if !found || err != nil {
		return
	}

	switch corev1.ServiceType(serviceType) {
	case corev1.ServiceTypeNodePort, corev1.ServiceTypeLoadBalancer:
		// corev1.Service will init node ports when creating NodePort or LoadBalancer
		items, found, err := unstructured.NestedSlice(result.Object, "spec", "ports")
		if !found || err != nil {
			return
		}
		for _, item := range items {
			servicePort, ok := item.(map[string]interface{})
			if !ok {
				return
			}
			unstructured.RemoveNestedField(servicePort, "nodePort")
		}

		err = unstructured.SetNestedSlice(result.Object, items, "spec", "ports")
		if err != nil {
			klog.ErrorDepth(2, fmt.Sprintf("failed to trim Service %s/%s: %v", result.GetNamespace(), result.GetName(), err))
		}
	}
}

func trimBatchJob(result *unstructured.Unstructured) {
	unstructured.RemoveNestedField(result.Object, "spec", "selector", "matchLabels", "controller-uid")
	unstructured.RemoveNestedField(result.Object, "spec", "template", "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(result.Object, "spec", "template", "metadata", "labels", "controller-uid")
}
