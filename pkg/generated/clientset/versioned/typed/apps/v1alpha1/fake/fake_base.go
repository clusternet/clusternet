/*
Copyright The Clusternet Authors.

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
// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	"context"
	json "encoding/json"
	"fmt"

	v1alpha1 "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	appsv1alpha1 "github.com/clusternet/clusternet/pkg/generated/applyconfiguration/apps/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeBases implements BaseInterface
type FakeBases struct {
	Fake *FakeAppsV1alpha1
	ns   string
}

var basesResource = v1alpha1.SchemeGroupVersion.WithResource("bases")

var basesKind = v1alpha1.SchemeGroupVersion.WithKind("Base")

// Get takes name of the base, and returns the corresponding base object, and an error if there is any.
func (c *FakeBases) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.Base, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(basesResource, c.ns, name), &v1alpha1.Base{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.Base), err
}

// List takes label and field selectors, and returns the list of Bases that match those selectors.
func (c *FakeBases) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.BaseList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(basesResource, basesKind, c.ns, opts), &v1alpha1.BaseList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.BaseList{ListMeta: obj.(*v1alpha1.BaseList).ListMeta}
	for _, item := range obj.(*v1alpha1.BaseList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested bases.
func (c *FakeBases) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(basesResource, c.ns, opts))

}

// Create takes the representation of a base and creates it.  Returns the server's representation of the base, and an error, if there is any.
func (c *FakeBases) Create(ctx context.Context, base *v1alpha1.Base, opts v1.CreateOptions) (result *v1alpha1.Base, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(basesResource, c.ns, base), &v1alpha1.Base{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.Base), err
}

// Update takes the representation of a base and updates it. Returns the server's representation of the base, and an error, if there is any.
func (c *FakeBases) Update(ctx context.Context, base *v1alpha1.Base, opts v1.UpdateOptions) (result *v1alpha1.Base, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(basesResource, c.ns, base), &v1alpha1.Base{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.Base), err
}

// Delete takes name of the base and deletes it. Returns an error if one occurs.
func (c *FakeBases) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteActionWithOptions(basesResource, c.ns, name, opts), &v1alpha1.Base{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeBases) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(basesResource, c.ns, listOpts)

	_, err := c.Fake.Invokes(action, &v1alpha1.BaseList{})
	return err
}

// Patch applies the patch and returns the patched base.
func (c *FakeBases) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.Base, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(basesResource, c.ns, name, pt, data, subresources...), &v1alpha1.Base{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.Base), err
}

// Apply takes the given apply declarative configuration, applies it and returns the applied base.
func (c *FakeBases) Apply(ctx context.Context, base *appsv1alpha1.BaseApplyConfiguration, opts v1.ApplyOptions) (result *v1alpha1.Base, err error) {
	if base == nil {
		return nil, fmt.Errorf("base provided to Apply must not be nil")
	}
	data, err := json.Marshal(base)
	if err != nil {
		return nil, err
	}
	name := base.Name
	if name == nil {
		return nil, fmt.Errorf("base.Name must be provided to Apply")
	}
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(basesResource, c.ns, *name, types.ApplyPatchType, data), &v1alpha1.Base{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.Base), err
}
