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
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"
)

// findServiceIPRange returns the service ip range for the cluster.
func findServiceIPRange(podLister corelisters.PodLister) (string, error) {
	// Try to find the service ip range from the kube-apiserver first
	// and then kube-controller-manager if failed
	labelKeys := []string{"component", "app.kubernetes.io/component"}
	labelValues := []string{"kube-apiserver", "kube-controller-manager"}
	parameter := "--service-cluster-ip-range"
	for _, labelValue := range labelValues {
		for _, labelKey := range labelKeys {
			labelSelector := labels.SelectorFromSet(labels.Set{labelKey: labelValue})
			serviceIPRange := findPodCommandParameter(podLister, labelSelector, parameter)
			if serviceIPRange != "" {
				return serviceIPRange, nil
			}
		}
	}

	// Try to find the service ip range from the env.
	serviceIPRange := os.Getenv("SERVICE_CIDR")
	if serviceIPRange != "" {
		return serviceIPRange, nil
	}
	return "", errors.New("can't get service ip range")
}

// findPodIPRange returns the pod ip range for the cluster.
func findPodIPRange(podLister corelisters.PodLister) (string, error) {
	// Try to find the pod ip range from the kube-controller-manager not including kube-apiserver
	labelKeys := []string{"component", "app.kubernetes.io/component"}
	labelValues := []string{"kube-controller-manager"}
	parameter := "--cluster-cidr"
	for _, labelValue := range labelValues {
		for _, labelKey := range labelKeys {
			labelSelector := labels.SelectorFromSet(labels.Set{labelKey: labelValue})
			podIPRange := findPodCommandParameter(podLister, labelSelector, parameter)
			if podIPRange != "" {
				return podIPRange, nil
			}
		}
	}

	// Try to find the pod ip range from the kube-proxy
	labelKey, labelValue := "k8s-app", "kube-proxy"
	labelSelector := labels.SelectorFromSet(labels.Set{labelKey: labelValue})
	podIPRange := findPodCommandParameter(podLister, labelSelector, parameter)
	if podIPRange != "" {
		return podIPRange, nil
	}

	// Try to find the pod ip range from the env.
	podIPRange = os.Getenv("CLUSTER_CIDR")
	if podIPRange != "" {
		return podIPRange, nil
	}
	return "", errors.New("can't get pod ip range")
}

// findPodCommandParameter returns the pod container command parameter by the given labelSelector.
func findPodCommandParameter(podLister corelisters.PodLister, labelSelector labels.Selector,
	parameter string) string {
	pods, err := findPods(podLister, labelSelector)
	if err != nil {
		klog.Errorf("Failed to find pods by label selector %q: %v", labelSelector, err)
		return ""
	}

	for _, pod := range pods {
		for _, container := range pod.Spec.Containers {
			if val := getParameterValue(container.Command, parameter); val != "" {
				return val
			}
			if val := getParameterValue(container.Args, parameter); val != "" {
				return val
			}
		}
	}

	return ""
}

// findPods returns the pods filter by the given labelSelector.
func findPods(podLister corelisters.PodLister, labelSelector labels.Selector) ([]*corev1.Pod, error) {
	pods, err := podLister.List(labelSelector)
	if err != nil {
		return nil, err
	}

	return pods, nil
}

func getParameterValue(args []string, parameter string) string {
	for _, arg := range args {
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
