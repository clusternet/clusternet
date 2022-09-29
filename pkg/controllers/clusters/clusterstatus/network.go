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
	"errors"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	corev1Lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"
)

// findServiceIPRange returns the cluster IP range for the cluster service.
// copied from submariner.io/submariner-operator/pkg/discovery/network/generic.go and modified
func findServiceIPRange(podLister corev1Lister.PodLister) (string, error) {
	clusterIPRange := findPodCommandParameter(podLister, "kube-apiserver", "--service-cluster-ip-range")
	if clusterIPRange != "" {
		return clusterIPRange, nil
	}
	// Try to find the pod IP range from the env.
	clusterIPRange = os.Getenv("SERVICE_CIDR")
	if clusterIPRange != "" {
		return clusterIPRange, nil
	}
	return "", errors.New("can't get ServiceIPRange")
}

// findPodIpRange returns the pod IP range for the cluster.
// copied from submariner.io/submariner-operator/pkg/discovery/network/generic.go
func findPodIPRange(nodeLister corev1Lister.NodeLister, podLister corev1Lister.PodLister) (string, error) {
	// Try to find the pod IP range from the kube-controller-manager.
	podIPRange := findPodIPRangeKubeController(podLister)
	if podIPRange != "" {
		return podIPRange, nil
	}

	// Try to find the pod IP range from the kube-proxy.
	podIPRange = findPodIPRangeKubeProxy(podLister)
	if podIPRange != "" {
		return podIPRange, nil
	}

	// Try to find the pod IP range from the node spec.
	podIPRange = findPodIPRangeFromNodeSpec(nodeLister)
	if podIPRange != "" {
		return podIPRange, nil
	}
	// Try to find the pod IP range from the env.
	podIPRange = os.Getenv("CLUSTER_CIDR")
	if podIPRange != "" {
		return podIPRange, nil
	}
	return "", errors.New("can't get PodIPRange")
}

// copied from submariner.io/submariner-operator/pkg/discovery/network/generic.go
func findPodIPRangeKubeController(podLister corev1Lister.PodLister) string {
	return findPodCommandParameter(podLister, "kube-controller-manager", "--cluster-cidr")
}

// copied from submariner.io/submariner-operator/pkg/discovery/network/generic.go
func findPodIPRangeKubeProxy(podLister corev1Lister.PodLister) string {
	return findPodCommandParameter(podLister, "kube-proxy", "--cluster-cidr")
}

// copied from submariner.io/submariner-operator/pkg/discovery/network/generic.go
func findPodIPRangeFromNodeSpec(nodeLister corev1Lister.NodeLister) string {
	nodes, err := nodeLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("Failed to list nodes: %v", err)
		return ""
	}

	for _, node := range nodes {
		if node.Spec.PodCIDR != "" {
			return node.Spec.PodCIDR
		}
	}

	return ""
}

// findPodCommandParameter returns the pod container command parameter for the given pod.
// copied from submariner.io/submariner-operator/pkg/discovery/network/pods.go
func findPodCommandParameter(podLister corev1Lister.PodLister, labelSelectorValue, parameter string) string {
	pod := findPodWithComponentLabel(podLister, labelSelectorValue)

	if pod == nil {
		pod = findPodWithK8sComponentLabel(podLister, labelSelectorValue)
	}

	if pod == nil {
		return ""
	}
	for _, container := range pod.Spec.Containers {
		if val := getParaValue(container.Command, parameter); val != "" {
			return val
		}

		if val := getParaValue(container.Args, parameter); val != "" {
			return val
		}
	}
	return ""
}

func findPodWithComponentLabel(podLister corev1Lister.PodLister, labelSelectorValue string) *corev1.Pod {
	pod, err := findPod(podLister, "component", labelSelectorValue)

	if err != nil {
		return nil
	}
	return pod
}

func findPodWithK8sComponentLabel(podLister corev1Lister.PodLister, labelSelectorValue string) *corev1.Pod {
	pod, err := findPod(podLister, "app.kubernetes.io/component", labelSelectorValue)

	if err != nil {
		return nil
	}
	return pod
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

func getParaValue(lists []string, parameter string) string {
	for _, arg := range lists {
		if strings.HasPrefix(arg, parameter) {
			return strings.Split(arg, "=")[1]
		}
		// Handling the case where the command is in the form of /bin/sh -c exec ....
		if strings.Contains(arg, " ") {
			for _, subArg := range strings.Split(arg, " ") {
				if strings.HasPrefix(subArg, parameter) {
					return strings.Split(subArg, "=")[1]
				}
			}
		}
	}
	return ""
}
