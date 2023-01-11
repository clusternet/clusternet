/*
Copyright 2022 The Clusternet Authors.

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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
)

func GetReplicaRequirements(podSpec corev1.PodSpec) appsapi.ReplicaRequirements {
	resourceLimits := map[corev1.ResourceName]resource.Quantity{}
	resourceRequests := map[corev1.ResourceName]resource.Quantity{}
	for _, container := range podSpec.Containers {
		if container.Resources.Limits != nil {
			if container.Resources.Requests == nil {
				container.Resources.Requests = make(corev1.ResourceList)
			}
		}
		// calculate total resource limits
		for resourceName, limit := range container.Resources.Limits {
			// set default request to limit if request is not set
			if _, exists := container.Resources.Requests[resourceName]; !exists {
				container.Resources.Requests[resourceName] = limit.DeepCopy()
			}

			curLimit, ok := resourceLimits[resourceName]
			if !ok {
				resourceLimits[resourceName] = limit
				continue
			}

			curLimit.Add(limit)
			resourceLimits[resourceName] = curLimit
		}

		// calculate total resource requests
		for resourceName, request := range container.Resources.Requests {
			curRequest, ok := resourceRequests[resourceName]
			if !ok {
				resourceRequests[resourceName] = request
				continue
			}

			curRequest.Add(request)
			resourceRequests[resourceName] = curRequest
		}
	}

	// take max_resource(sum_pod, any_init_container)
	for _, container := range podSpec.InitContainers {
		if container.Resources.Limits != nil {
			if container.Resources.Requests == nil {
				container.Resources.Requests = make(corev1.ResourceList)
			}
		}
		// take max resource limits
		for resourceName, limit := range container.Resources.Limits {
			// set default request to limit if request is not set
			if _, exists := container.Resources.Requests[resourceName]; !exists {
				container.Resources.Requests[resourceName] = limit.DeepCopy()
			}

			curLimit, ok := resourceLimits[resourceName]
			if !ok {
				resourceLimits[resourceName] = limit
				continue
			}

			if limit.Cmp(curLimit) == 1 {
				resourceLimits[resourceName] = limit
			}
		}

		// take max resource requests
		for resourceName, request := range container.Resources.Requests {
			curRequest, ok := resourceRequests[resourceName]
			if !ok {
				resourceRequests[resourceName] = request
				continue
			}

			if request.Cmp(curRequest) == 1 {
				resourceRequests[resourceName] = request
			}
		}
	}

	return appsapi.ReplicaRequirements{
		NodeSelector: podSpec.NodeSelector,
		Tolerations:  podSpec.Tolerations,
		Affinity:     podSpec.Affinity,
		Resources: corev1.ResourceRequirements{
			Limits:   resourceLimits,
			Requests: resourceRequests,
		},
	}
}
