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

package apiserver

import (
	"reflect"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	apiregistrationapis "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	apiservicelisters "k8s.io/kube-aggregator/pkg/client/listers/apiregistration/v1"
)

var (
	priorityLock sync.Mutex

	resourcePriorityMap = make(map[string]groupPriority)

	apiServiceGVR = schema.GroupVersionResource{
		Group:    apiregistrationapis.GroupName,
		Version:  apiregistrationapis.SchemeGroupVersion.Version,
		Resource: "apiservices",
	}

	apiServiceStatusGVR = schema.GroupVersionResource{
		Group:    apiregistrationapis.GroupName,
		Version:  apiregistrationapis.SchemeGroupVersion.Version,
		Resource: "apiservices/status",
	}
)

type groupPriority struct {
	groupVersionName     string
	groupPriorityMinimum int32
}

func getAPIService(group, version string, apiserviceLister apiservicelisters.APIServiceLister) (*apiregistrationapis.APIService, error) {
	return apiserviceLister.Get(strings.Join([]string{version, group}, "."))
}

func canBeAddedToStorage(group, version, resourceName string, apiserviceLister apiservicelisters.APIServiceLister) bool {
	priorityLock.Lock()
	defer priorityLock.Unlock()

	if reflect.DeepEqual(schema.GroupVersionResource{Group: group, Version: version, Resource: resourceName}, apiServiceGVR) {
		return false
	}

	if reflect.DeepEqual(schema.GroupVersionResource{Group: group, Version: version, Resource: resourceName}, apiServiceStatusGVR) {
		return false
	}

	apiservice, err := getAPIService(group, version, apiserviceLister)
	if err != nil {
		klog.Errorf("failed to get APIService %q: %v", strings.Join([]string{version, group}, "."), err)
		// ignore the error, just add it
		return true
	}

	if gp, ok := resourcePriorityMap[resourceName]; ok && gp.groupPriorityMinimum > apiservice.Spec.GroupPriorityMinimum {
		return false
	}

	resourcePriorityMap[resourceName] = groupPriority{
		groupVersionName:     apiservice.Name,
		groupPriorityMinimum: apiservice.Spec.GroupPriorityMinimum,
	}
	return true
}
