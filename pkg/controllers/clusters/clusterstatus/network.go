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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	corev1Lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"
)

// findClusterIPRange returns the cluster IP range for the cluster.
func findClusterIPRange(nodeLister corev1Lister.NodeLister, podLister corev1Lister.PodLister) (string, error) {
	clusterIPRange, err := findPodCommandParameter(nodeLister, podLister, "component", "kube-apiserver", "--service-cluster-ip-range")
	if err != nil || clusterIPRange != "" {
		return clusterIPRange, err
	}
	return "", nil
}

// findPodIpRange returns the pod IP range for the cluster.
func findPodIPRange(nodeLister corev1Lister.NodeLister, podLister corev1Lister.PodLister) (string, error) {
	// Try to find the pod IP range from the kube-controller-manager.
	podIPRange, err := findPodIPRangeKubeController(nodeLister, podLister)
	if err != nil || podIPRange != "" {
		return podIPRange, err
	}

	// Try to find the pod IP range from the kube-proxy.
	podIPRange, err = findPodIPRangeKubeProxy(nodeLister, podLister)
	if err != nil || podIPRange != "" {
		return podIPRange, err
	}

	// Try to find the pod IP range from the node spec.
	podIPRange, err = findPodIPRangeFromNodeSpec(nodeLister)
	if err != nil || podIPRange != "" {
		return podIPRange, err
	}

	return "", nil
}

func findPodIPRangeKubeController(nodeLister corev1Lister.NodeLister, podLister corev1Lister.PodLister) (string, error) {
	return findPodCommandParameter(nodeLister, podLister, "component", "kube-controller-manager", "--cluster-cidr")
}

func findPodIPRangeKubeProxy(nodeLister corev1Lister.NodeLister, podLister corev1Lister.PodLister) (string, error) {
	return findPodCommandParameter(nodeLister, podLister, "component", "kube-proxy", "--cluster-cidr")
}

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
func findPodCommandParameter(nodeLister corev1Lister.NodeLister, podLister corev1Lister.PodLister, labelSelectorKey, labelSelectorValue, parameter string) (string, error) {
	parameterValue := ""
	pod, err := findPod(nodeLister, podLister, labelSelectorKey, labelSelectorValue)
	if err != nil || pod == nil {
		if errors.IsNotFound(err) {
			return parameterValue, nil
		}
		return parameterValue, err
	}
	for _, container := range pod.Spec.Containers {
		klog.V(7).Infof("Container Command: %+v", container.Command)
		for _, arg := range container.Command {
			if strings.HasPrefix(arg, parameter) {
				parameterValue = strings.Split(arg, "=")[1]
				klog.V(7).Infof("Found parameter %s=%s", parameter, parameterValue)
				return parameterValue, nil
			}
			// Handling the case where the command is in the form of /bin/sh -c exec ....
			if strings.Contains(arg, " ") {
				for _, subArg := range strings.Split(arg, " ") {
					if strings.HasPrefix(subArg, parameter) {
						parameterValue = strings.Split(subArg, "=")[1]
						klog.V(7).Infof("Found parameter %s=%s", parameter, parameterValue)
						return parameterValue, nil
					}
				}
			}
		}
		if parameterValue == "" {
			klog.V(7).Infof("Container Args: %+v", container.Args)
			for _, arg := range container.Args {
				if strings.HasPrefix(arg, parameter) {
					parameterValue = strings.Split(arg, "=")[1]
					return parameterValue, nil
				}
			}
		}
	}
	return parameterValue, nil
}

// findPod returns the pods filter by the given labelSelector.
func findPod(nodeLister corev1Lister.NodeLister, podLister corev1Lister.PodLister, labelSelectorKey, labelSelectorValue string) (*corev1.Pod, error) {
	labelSelector := labels.NewSelector()
	requirement, err := labels.NewRequirement(labelSelectorKey, selection.Equals, []string{labelSelectorValue})
	if err != nil {
		return nil, err
	}
	labelSelector = labelSelector.Add(*requirement)

	pods, err := podLister.List(labelSelector)
	if err != nil && !errors.IsNotFound(err) {
		klog.Errorf("Failed to list pods by label selector %v: %v", labelSelector, err)
		return nil, err
	}
	klog.V(7).Infof("Found %d pods by label selector %v", len(pods), labelSelector)
	if len(pods) == 0 {
		master, err := findMasterNode(nodeLister)
		if master == nil || err != nil {
			return nil, err
		}
		klog.V(7).Infof("Master node found: %v", master)
		pod, err := podLister.Pods(metav1.NamespaceSystem).Get(labelSelectorValue + "-" + master.Name)
		if err != nil {
			klog.Errorf("Failed to find pod %s/%s: %v", metav1.NamespaceSystem, labelSelectorValue+"-"+master.Name, err)
			return nil, err
		}
		return pod, nil
	}

	return pods[0], nil
}

// findMasterNode returns the master node for the cluster.
func findMasterNode(nodeLister corev1Lister.NodeLister) (*corev1.Node, error) {
	labelSelector := labels.NewSelector()
	requirement, err := labels.NewRequirement("node-role.kubernetes.io/master", selection.In, []string{"", "true"})
	if err != nil {
		klog.Errorf("Failed to create node label selector: %v", err)
		return nil, err
	}
	labelSelector = labelSelector.Add(*requirement)
	nodes, err := nodeLister.List(labelSelector)
	if err != nil {
		klog.Errorf("Failed to list nodes: %v", err)
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, nil
	}
	return nodes[0], nil
}
