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

package clusterstatus

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	corev1Lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"
)

// findClusterIPRange returns the cluster IP range for the cluster.
// copied from submariner.io/submariner-operator/pkg/discovery/network/generic.go and modified
func findClusterIPRange(podLister corev1Lister.PodLister) (string, error) {
	clusterIPRange, err := findPodCommandParameter(podLister, "component", "kube-apiserver", "--service-cluster-ip-range")
	if err != nil || clusterIPRange != "" {
		klog.Errorf("Failed to find cluster IP range from kube-apiserver: %v", err)
		return clusterIPRange, err
	}
	return "", nil
}

// findPodIpRange returns the pod IP range for the cluster.
// copied from submariner.io/submariner-operator/pkg/discovery/network/generic.go
func findPodIPRange(nodeLister corev1Lister.NodeLister, podLister corev1Lister.PodLister) (string, error) {
	// Try to find the pod IP range from the kube-controller-manager.
	podIPRange, err := findPodIPRangeKubeController(podLister)
	if err != nil || podIPRange != "" {
		klog.Errorf("Failed to find pod IP range from kube-controller-manager: %v", err)
		return podIPRange, err
	}

	// Try to find the pod IP range from the kube-proxy.
	podIPRange, err = findPodIPRangeKubeProxy(podLister)
	if err != nil || podIPRange != "" {
		klog.Errorf("Failed to find pod IP range from kube-proxy: %v", err)
		return podIPRange, err
	}

	// Try to find the pod IP range from the node spec.
	podIPRange, err = findPodIPRangeFromNodeSpec(nodeLister)
	if err != nil || podIPRange != "" {
		klog.Errorf("Failed to find pod IP range from node spec: %v", err)
		return podIPRange, err
	}

	return "", nil
}

// copied from submariner.io/submariner-operator/pkg/discovery/network/generic.go
func findPodIPRangeKubeController(podLister corev1Lister.PodLister) (string, error) {
	return findPodCommandParameter(podLister, "component", "kube-controller-manager", "--cluster-cidr")
}

// copied from submariner.io/submariner-operator/pkg/discovery/network/generic.go
func findPodIPRangeKubeProxy(podLister corev1Lister.PodLister) (string, error) {
	return findPodCommandParameter(podLister, "component", "kube-proxy", "--cluster-cidr")
}

// copied from submariner.io/submariner-operator/pkg/discovery/network/generic.go
func findPodIPRangeFromNodeSpec(nodeLister corev1Lister.NodeLister) (string, error) {
	nodes, err := nodeLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("Failed to list nodes: %v", err)
		return "", err
	}

	for _, node := range nodes {
		if node.Spec.PodCIDR != "" {
			return node.Spec.PodCIDR, nil
		}
	}

	return "", nil
}

// findPodCommandParameter returns the pod container command parameter for the given pod.
// copied from submariner.io/submariner-operator/pkg/discovery/network/pods.go
func findPodCommandParameter(podLister corev1Lister.PodLister, labelSelectorKey, labelSelectorValue, parameter string) (string, error) {
	pod, err := findPod(podLister, labelSelectorKey, labelSelectorValue)

	if err != nil || pod == nil {
		return "", err
	}
	for _, container := range pod.Spec.Containers {
		for _, arg := range container.Command {
			if strings.HasPrefix(arg, parameter) {
				return strings.Split(arg, "=")[1], nil
			}
			// Handling the case where the command is in the form of /bin/sh -c exec ....
			if strings.Contains(arg, " ") {
				for _, subArg := range strings.Split(arg, " ") {
					if strings.HasPrefix(subArg, parameter) {
						return strings.Split(subArg, "=")[1], nil
					}
				}
			}
		}
	}
	return "", nil
}

// findPod returns the pods filter by the given labelSelector.
// copied from submariner.io/submariner-operator/pkg/discovery/network/pods.go
func findPod(podLister corev1Lister.PodLister, labelSelectorKey, labelSelectorValue string) (*corev1.Pod, error) {
	labelSelector := labels.NewSelector()
	requirement, err := labels.NewRequirement(labelSelectorKey, selection.Equals, []string{labelSelectorValue})
	if err != nil {
		return nil, err
	}
	labelSelector = labelSelector.Add(*requirement)

	pods, err := podLister.List(labelSelector)
	if err != nil {
		klog.Errorf("Failed to list pods by label selector %q: %v", labelSelector, err)
		return nil, err
	}

	if len(pods) == 0 {
		return nil, nil
	}

	return pods[0], nil
}
