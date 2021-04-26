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

package socket

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/rest"

	"github.com/clusternet/clusternet/pkg/apis/proxies"
	"github.com/clusternet/clusternet/pkg/apis/proxies/validation"
)

const (
	category = "clusternet"
)

// REST implements a RESTStorage for Proxies API
type REST struct {
	// TODO
}

func (r *REST) ShortNames() []string {
	return []string{"ss"}
}

func (r *REST) NamespaceScoped() bool {
	return false
}

func (r *REST) Categories() []string {
	return []string{category}
}

func (r *REST) New() runtime.Object {
	return &proxies.Socket{}
}

func (r *REST) Create(ctx context.Context, obj runtime.Object, createValidation rest.ValidateObjectFunc, options *metav1.CreateOptions) (runtime.Object, error) {
	socket, ok := obj.(*proxies.Socket)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("not a Socket object: %#v", obj))
	}

	if createValidation != nil {
		if err := createValidation(ctx, obj.DeepCopyObject()); err != nil {
			return nil, err
		}
	}

	if allErrs := validation.ValidateSocket(socket); len(allErrs) > 0 {
		return nil, allErrs.ToAggregate()
	}

	// TODO

	return socket, nil
}

// NewREST returns a RESTStorage object that will work against API services.
func NewREST() *REST {
	return &REST{}
}

var _ rest.CategoriesProvider = &REST{}
var _ rest.ShortNamesProvider = &REST{}
