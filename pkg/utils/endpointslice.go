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
	"context"
	"fmt"

	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	discoverylisterv1 "k8s.io/client-go/listers/discovery/v1"
	"k8s.io/client-go/util/retry"
)

// ApplyEndPointSliceWithRetry create or update existed slices.
func ApplyEndPointSliceWithRetry(client kubernetes.Interface, slice *discoveryv1.EndpointSlice) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		var lastError error
		_, lastError = client.DiscoveryV1().EndpointSlices(slice.GetNamespace()).Create(context.TODO(), slice, metav1.CreateOptions{})
		if lastError == nil {
			return nil
		}
		if !errors.IsAlreadyExists(lastError) {
			return lastError
		}

		curObj, err := client.DiscoveryV1().EndpointSlices(slice.GetNamespace()).Get(context.TODO(), slice.GetName(), metav1.GetOptions{})
		if err != nil {
			return err
		}
		lastError = nil

		if ResourceNeedResync(curObj, slice, false) {
			// try to update slice
			curObj.Ports = slice.Ports
			curObj.Endpoints = slice.Endpoints
			curObj.AddressType = slice.AddressType
			_, lastError = client.DiscoveryV1().EndpointSlices(slice.GetNamespace()).Update(context.TODO(), curObj, metav1.UpdateOptions{})
			if lastError == nil {
				return nil
			}
		}
		return lastError
	})
}

func RemoveNonexistentEndpointslice(
	srcLister discoverylisterv1.EndpointSliceLister,
	srcNamespace string,
	labelMap labels.Set,
	targetClient kubernetes.Interface,
	targetNamespace string,
	dstLabelMap labels.Set,
) ([]*discoveryv1.EndpointSlice, error) {
	srcEndpointSliceList, err := srcLister.EndpointSlices(srcNamespace).List(
		labels.SelectorFromSet(labelMap))
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, err
		}
	}
	// remove endpoint slices exist in delicate ns but not in target ns
	srcEndpointSliceMap := make(map[string]bool, 0)
	for _, item := range srcEndpointSliceList {
		srcEndpointSliceMap[fmt.Sprintf("%s-%s", item.Namespace, item.Name)] = true
	}

	var targetEndpointSliceList *discoveryv1.EndpointSliceList
	targetEndpointSliceList, err = targetClient.DiscoveryV1().EndpointSlices(targetNamespace).List(
		context.TODO(),
		metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(dstLabelMap).String(),
		},
	)
	if err == nil {
		for _, item := range targetEndpointSliceList.Items {
			if !srcEndpointSliceMap[item.Name] {
				if err = targetClient.DiscoveryV1().EndpointSlices(targetNamespace).Delete(context.TODO(), item.Name, metav1.DeleteOptions{}); err != nil {
					utilruntime.HandleError(fmt.Errorf("the endpointclise '%s/%s' in target namespace deleted failed", item.Namespace, item.Name))
					return nil, err
				}
			}
		}
	}
	return srcEndpointSliceList, nil
}
