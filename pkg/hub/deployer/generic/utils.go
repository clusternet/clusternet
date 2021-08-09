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

package generic

import (
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// copyied from k8s.io/apimachinery/pkg/apis/meta/v1/unstructured
func getNestedString(obj map[string]interface{}, fields ...string) string {
	val, found, err := unstructured.NestedString(obj, fields...)
	if !found || err != nil {
		return ""
	}
	return val
}

// copyied from k8s.io/apimachinery/pkg/apis/meta/v1/unstructured
// and modified
func setNestedField(u *unstructured.Unstructured, value interface{}, fields ...string) {
	if u.Object == nil {
		u.Object = make(map[string]interface{})
	}
	unstructured.SetNestedField(u.Object, value, fields...)
}

// getStatusCause returns the named cause from the provided error if it exists and
// the error is of the type APIStatus. Otherwise it returns false.
func getStatusCause(err error) ([]metav1.StatusCause, bool) {
	apierr, ok := err.(apierrors.APIStatus)
	if !ok || apierr == nil || apierr.Status().Details == nil {
		return nil, false
	}
	return apierr.Status().Details.Causes, true
}
