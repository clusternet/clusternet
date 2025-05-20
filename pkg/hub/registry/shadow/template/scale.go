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
	"context"
	"fmt"
	"strings"

	autoscalingapiv1 "k8s.io/api/autoscaling/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/client-go/scale/scheme/appsv1beta1"
	"k8s.io/client-go/scale/scheme/appsv1beta2"
	"k8s.io/client-go/scale/scheme/autoscalingv1"
	"k8s.io/client-go/scale/scheme/extensionsv1beta1"
	"k8s.io/klog/v2"
)

// ScaleREST implements a ScaleREST Storage for Shadow API
type ScaleREST struct {
	store *REST
}

// GroupVersionKind returns GroupVersionKind for Scale object
func (r *ScaleREST) GroupVersionKind(containingGV schema.GroupVersion) schema.GroupVersionKind {
	switch containingGV {
	case extensionsv1beta1.SchemeGroupVersion:
		return extensionsv1beta1.SchemeGroupVersion.WithKind("Scale")
	case appsv1beta1.SchemeGroupVersion:
		return appsv1beta1.SchemeGroupVersion.WithKind("Scale")
	case appsv1beta2.SchemeGroupVersion:
		return appsv1beta2.SchemeGroupVersion.WithKind("Scale")
	default:
		return autoscalingv1.SchemeGroupVersion.WithKind("Scale")
	}
}

// New creates a new Scale object
func (r *ScaleREST) New() runtime.Object {
	return &autoscalingapiv1.Scale{}
}

// Get retrieves object from ScaleREST storage.
func (r *ScaleREST) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	obj, err := r.store.Get(ctx, name, options)
	if err != nil {
		return nil, err
	}
	return scaleFromUnstructuredObject(obj.(*unstructured.Unstructured)), nil
}

// Update alters scale subset of object.
func (r *ScaleREST) Update(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo, createValidation rest.ValidateObjectFunc, updateValidation rest.ValidateObjectUpdateFunc, forceAllowCreate bool, options *metav1.UpdateOptions) (runtime.Object, bool, error) {
	// We are explicitly setting forceAllowCreate to false in the call to the underlying storage because
	// subresources should never allow create on update.
	obj, _, err := r.store.Update(
		ctx,
		name,
		&scaleUpdatedObjectInfo{name, objInfo},
		toScaleCreateValidation(createValidation),
		toScaleUpdateValidation(updateValidation),
		false,
		options,
	)
	if err != nil {
		return nil, false, err
	}
	return scaleFromUnstructuredObject(obj.(*unstructured.Unstructured)), false, nil
}

func (r *ScaleREST) Destroy() {
}

// ScaleREST implements Patcher
var _ = rest.Storage(&ScaleREST{})
var _ = rest.Patcher(&ScaleREST{})
var _ = rest.GroupVersionKindProvider(&ScaleREST{})

// NewScaleREST returns a ScaleREST Storage object that will work against API services.
func NewScaleREST(store *REST) *ScaleREST {
	return &ScaleREST{
		store: store,
	}
}

func toScaleCreateValidation(f rest.ValidateObjectFunc) rest.ValidateObjectFunc {
	return func(ctx context.Context, obj runtime.Object) error {
		scale := scaleFromUnstructuredObject(obj.(*unstructured.Unstructured))
		return f(ctx, scale)
	}
}

func toScaleUpdateValidation(f rest.ValidateObjectUpdateFunc) rest.ValidateObjectUpdateFunc {
	return func(ctx context.Context, obj, old runtime.Object) error {
		newScale := scaleFromUnstructuredObject(obj.(*unstructured.Unstructured))
		oldScale := scaleFromUnstructuredObject(old.(*unstructured.Unstructured))
		return f(ctx, newScale, oldScale)
	}
}

func scaleFromUnstructuredObject(obj *unstructured.Unstructured) *autoscalingapiv1.Scale {
	return &autoscalingapiv1.Scale{
		ObjectMeta: metav1.ObjectMeta{
			Name:              obj.GetName(),
			Namespace:         obj.GetNamespace(),
			UID:               obj.GetUID(),
			ResourceVersion:   obj.GetResourceVersion(),
			CreationTimestamp: obj.GetCreationTimestamp(),
		},
		Spec: autoscalingapiv1.ScaleSpec{
			Replicas: getReplicas(obj, "spec", "replicas"),
		},
		Status: autoscalingapiv1.ScaleStatus{
			Replicas: getReplicas(obj, "status", "replicas"),
		},
	}
}

func getReplicas(obj *unstructured.Unstructured, fields ...string) int32 {
	val, found, err := unstructured.NestedInt64(obj.Object, fields...)
	if err != nil {
		klog.ErrorDepth(5, fmt.Sprintf("failed to get %s: %v", strings.Join(fields, "."), err))
	}
	if !found {
		klog.WarningDepth(5, fmt.Sprintf("unable to get %s", strings.Join(fields, ".")))
	}
	return int32(val)
}

type scaleUpdatedObjectInfo struct {
	name       string
	reqObjInfo rest.UpdatedObjectInfo
}

func (i *scaleUpdatedObjectInfo) Preconditions() *metav1.Preconditions {
	return i.reqObjInfo.Preconditions()
}

// UpdatedObject transforms existing object -> existing scale -> new scale -> new object
func (i *scaleUpdatedObjectInfo) UpdatedObject(ctx context.Context, oldObj runtime.Object) (runtime.Object, error) {
	oldObjUnstructured := oldObj.(*unstructured.Unstructured)
	oldScale := scaleFromUnstructuredObject(oldObjUnstructured)
	// old scale -> new scale
	newScaleObj, err := i.reqObjInfo.UpdatedObject(ctx, oldScale)
	if err != nil {
		return nil, err
	}
	if newScaleObj == nil {
		return nil, errors.NewBadRequest("nil update passed to Scale")
	}

	// validate scale object
	newScale, ok := newScaleObj.(*autoscalingapiv1.Scale)
	if !ok {
		return nil, errors.NewBadRequest(fmt.Sprintf("expected input object type to be Scale, but %T", newScaleObj))
	}

	// perform real update
	newObj := oldObjUnstructured.DeepCopy()
	err = unstructured.SetNestedField(newObj.Object, int64(newScale.Spec.Replicas), "spec", "replicas")
	return newObj, err
}
