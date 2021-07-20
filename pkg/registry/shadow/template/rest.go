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
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/client-go/kubernetes"
	clientgorest "k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusternet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	"github.com/clusternet/clusternet/pkg/known"
)

const (
	category         = "clusternet.shadow"
	CoreGroupPrefix  = "api"
	NamedGroupPrefix = "apis"

	// cluster-scoped objects will be store into Manifest with ReservedNamespace
	ReservedNamespace = "clusternet-reserved"

	// default value for DeleteCollectionWorkers
	DefaultDeleteCollectionWorkers = 2
)

// REST implements a RESTStorage for Shadow API
type REST struct {
	// name is the plural name of the resource.
	name string
	// shortNames is a list of suggested short names of the resource.
	shortNames []string
	// namespaced indicates if a resource is namespaced or not.
	namespaced bool
	// kind is the kind for the resource (e.g. 'Foo' is the kind for a resource 'foo')
	kind string
	// originalGroup is the original group of the resource.
	originalGroup string
	// originalVersion is the original version of the resource.
	originalVersion string

	ParameterCodec runtime.ParameterCodec

	DryRunClient     *kubernetes.Clientset
	ClusternetClient *clusternet.Clientset

	// DeleteCollectionWorkers is the maximum number of workers in a single
	// DeleteCollection call. Delete requests for the items in a collection
	// are issued in parallel.
	DeleteCollectionWorkers int
}

// Create inserts a new item into Manifest according to the unique key from the object.
func (r *REST) Create(ctx context.Context, obj runtime.Object, createValidation rest.ValidateObjectFunc, options *metav1.CreateOptions) (runtime.Object, error) {
	// dry-run
	result, err := r.dryRunCreate(ctx, obj, createValidation, options)
	if err != nil {
		return nil, err
	}

	// next we create manifest to store the result
	manifest := &appsapi.Manifest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      r.generateNameForManifest(result.GetName()),
			Namespace: ReservedNamespace,
			Labels: map[string]string{
				known.ConfigSourceApiVersionLabel: result.GetObjectKind().GroupVersionKind().GroupVersion().String(),
				known.ConfigSourceKindLabel:       r.kind,
				known.ConfigNameLabel:             result.GetName(),
				known.ConfigNamespaceLabel:        result.GetNamespace(),
			},
		},
		Template: runtime.RawExtension{
			Object: result,
		},
	}
	manifest, err = r.ClusternetClient.AppsV1alpha1().Manifests(manifest.Namespace).Create(ctx, manifest, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			return nil, errors.NewAlreadyExists(schema.GroupResource{Group: r.originalGroup, Resource: r.name}, result.GetName())
		}
		return nil, err
	}
	return transformManifest(manifest)
}

// Get retrieves the item from Manifest.
func (r *REST) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	manifest, err := r.ClusternetClient.AppsV1alpha1().Manifests(ReservedNamespace).Get(ctx, r.generateNameForManifest(name), *options)
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, errors.NewNotFound(schema.GroupResource{Group: r.originalGroup, Resource: r.name}, name)
		}
		return nil, errors.NewInternalError(err)
	}

	return transformManifest(manifest)
}

// Update performs an atomic update and set of the object. Returns the result of the update
// or an error. If the registry allows create-on-update, the create flow will be executed.
// A bool is returned along with the object and any errors, to indicate object creation.
func (r *REST) Update(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo, createValidation rest.ValidateObjectFunc, updateValidation rest.ValidateObjectUpdateFunc, forceAllowCreate bool, options *metav1.UpdateOptions) (runtime.Object, bool, error) {
	manifest, err := r.ClusternetClient.AppsV1alpha1().Manifests(ReservedNamespace).Get(ctx, r.generateNameForManifest(name), metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, false, errors.NewNotFound(schema.GroupResource{Group: r.originalGroup, Resource: r.name}, name)
		}
		return nil, false, errors.NewInternalError(err)
	}

	oldObj := &unstructured.Unstructured{}
	if err = json.Unmarshal(manifest.Template.Raw, oldObj); err != nil {
		return nil, false, errors.NewInternalError(err)
	}

	// TODO: validate update
	newObj, err := objInfo.UpdatedObject(ctx, oldObj)
	if err != nil {
		return nil, false, err
	}

	// dry-run
	result, err := r.dryRunCreate(ctx, newObj, createValidation, &metav1.CreateOptions{})
	if err != nil {
		return nil, false, err
	}

	manifest.Template.Reset()
	manifest.Template.Object = result
	manifest, err = r.ClusternetClient.AppsV1alpha1().Manifests(ReservedNamespace).Update(ctx, manifest, *options)
	if err != nil {
		return nil, false, err
	}

	result, err = transformManifest(manifest)
	return result, err != nil, err
}

// Delete removes the item from storage.
// options can be mutated by rest.BeforeDelete due to a graceful deletion strategy.
func (r *REST) Delete(ctx context.Context, name string, deleteValidation rest.ValidateObjectFunc, options *metav1.DeleteOptions) (runtime.Object, bool, error) {
	err := r.ClusternetClient.AppsV1alpha1().Manifests(ReservedNamespace).Delete(ctx, r.generateNameForManifest(name), *options)
	if err != nil {
		if errors.IsNotFound(err) {
			err = errors.NewNotFound(schema.GroupResource{Group: r.originalGroup, Resource: r.name}, name)
		}
	}
	return nil, err == nil, err
}

// DeleteCollection removes all items returned by List with a given ListOptions from storage.
//
// DeleteCollection is currently NOT atomic. It can happen that only subset of objects
// will be deleted from storage, and then an error will be returned.
// In case of success, the list of deleted objects will be returned.
// Copied from k8s.io/apiserver/pkg/registry/generic/registry/store.go and modified.
func (r *REST) DeleteCollection(ctx context.Context, deleteValidation rest.ValidateObjectFunc, options *metav1.DeleteOptions, listOptions *internalversion.ListOptions) (runtime.Object, error) {
	if listOptions == nil {
		listOptions = &internalversion.ListOptions{}
	} else {
		listOptions = listOptions.DeepCopy()
	}

	listObj, err := r.List(ctx, listOptions)
	if err != nil {
		return nil, err
	}
	items, err := meta.ExtractList(listObj)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		// Nothing to delete, return now
		return listObj, nil
	}
	// Spawn a number of goroutines, so that we can issue requests to storage
	// in parallel to speed up deletion.
	// It is proportional to the number of items to delete, up to
	// DeleteCollectionWorkers (it doesn't make much sense to spawn 16
	// workers to delete 10 items).
	workersNumber := r.DeleteCollectionWorkers
	if workersNumber > len(items) {
		workersNumber = len(items)
	}
	if workersNumber < 1 {
		workersNumber = 1
	}
	wg := sync.WaitGroup{}
	toProcess := make(chan int, 2*workersNumber)
	errs := make(chan error, workersNumber+1)

	go func() {
		defer utilruntime.HandleCrash(func(panicReason interface{}) {
			errs <- fmt.Errorf("DeleteCollection distributor panicked: %v", panicReason)
		})
		for i := 0; i < len(items); i++ {
			toProcess <- i
		}
		close(toProcess)
	}()

	wg.Add(workersNumber)
	for i := 0; i < workersNumber; i++ {
		go func() {
			// panics don't cross goroutine boundaries
			defer utilruntime.HandleCrash(func(panicReason interface{}) {
				errs <- fmt.Errorf("DeleteCollection goroutine panicked: %v", panicReason)
			})
			defer wg.Done()

			for index := range toProcess {
				accessor, err := meta.Accessor(items[index])
				if err != nil {
					errs <- err
					return
				}
				// DeepCopy the deletion options because individual graceful deleters communicate changes via a mutating
				// function in the delete strategy called in the delete method.  While that is always ugly, it works
				// when making a single call.  When making multiple calls via delete collection, the mutation applied to
				// pod/A can change the option ultimately used for pod/B.
				if _, _, err := r.Delete(ctx, accessor.GetName(), deleteValidation, options.DeepCopy()); err != nil && !errors.IsNotFound(err) {
					klog.V(4).InfoS("Delete object in DeleteCollection failed", "object", klog.KObj(accessor), "err", err)
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	select {
	case err := <-errs:
		return nil, err
	default:
		return listObj, nil
	}
}

// Watch makes a matcher for the given label and field.
func (r *REST) Watch(ctx context.Context, options *internalversion.ListOptions) (watch.Interface, error) {
	label, err := r.convertListOptionsToLabels(options)
	if err != nil {
		return nil, err
	}

	klog.V(5).Infof("%v", label)
	watcher, err := r.ClusternetClient.AppsV1alpha1().Manifests(ReservedNamespace).Watch(ctx, metav1.ListOptions{
		TypeMeta:             metav1.TypeMeta{},
		LabelSelector:        label.String(),
		FieldSelector:        "",
		Watch:                options.Watch,
		AllowWatchBookmarks:  options.AllowWatchBookmarks,
		ResourceVersion:      options.ResourceVersion,
		ResourceVersionMatch: options.ResourceVersionMatch,
		TimeoutSeconds:       options.TimeoutSeconds,
		Limit:                options.Limit,
		Continue:             options.Continue,
	})
	watchWrapper := NewWatchWrapper(ctx, watcher, func(object runtime.Object) runtime.Object {
		// transform object here
		if _, ok := object.(*metav1.Status); ok {
			return object
		}

		if manifest, ok := object.(*appsapi.Manifest); ok {
			obj, err := transformManifest(manifest)
			if err != nil {
				klog.ErrorDepth(3, fmt.Sprintf("failed to tranform Manifest %s: %v", klog.KObj(manifest), err))
				return manifest
			}
			return obj
		}

		return object
	}, defaultSize)
	if err == nil {
		go watchWrapper.Run()
	}
	return watchWrapper, err
}

// List returns a list of items matching labels.
func (r *REST) List(ctx context.Context, options *internalversion.ListOptions) (runtime.Object, error) {
	label, err := r.convertListOptionsToLabels(options)
	if err != nil {
		return nil, err
	}

	manifests, err := r.ClusternetClient.AppsV1alpha1().Manifests(ReservedNamespace).List(ctx, metav1.ListOptions{
		//TypeMeta:             metav1.TypeMeta{},
		LabelSelector: label.String(),
		//FieldSelector:        "",
		//Watch:                false,
		//AllowWatchBookmarks:  false,
		//ResourceVersion:      "",
		//ResourceVersionMatch: "",
		//TimeoutSeconds:       nil,
		//Limit:                0,
		//Continue:             "",
	})
	if err != nil {
		return nil, err
	}

	result := &unstructured.UnstructuredList{}
	orignalGVK := r.GroupVersionKind(schema.GroupVersion{})
	result.SetAPIVersion(orignalGVK.GroupVersion().String())
	result.SetKind("List")
	if len(manifests.Items) == 0 {
		return result, nil
	}
	for _, manifest := range manifests.Items {
		obj, err := transformManifest(&manifest)
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, *obj)
	}

	return result, err
}

func (r *REST) NewList() runtime.Object {
	newObj := &unstructured.UnstructuredList{}
	orignalGVK := r.GroupVersionKind(schema.GroupVersion{})
	newObj.SetAPIVersion(orignalGVK.GroupVersion().String())
	newObj.SetKind("List")
	return newObj
}

func (r *REST) ConvertToTable(ctx context.Context, object runtime.Object, tableOptions runtime.Object) (*metav1.Table, error) {
	tableConvertor := rest.NewDefaultTableConvertor(schema.GroupResource{Group: r.originalGroup, Resource: r.name})
	return tableConvertor.ConvertToTable(ctx, object, tableOptions)
}

func (r *REST) ShortNames() []string {
	return r.shortNames
}

func (r *REST) SetShortNames(ss []string) {
	r.shortNames = ss
}

func (r *REST) SetName(name string) {
	r.name = name
}

func (r *REST) NamespaceScoped() bool {
	return r.namespaced
}

func (r *REST) SetNamespaceScoped(namespaceScoped bool) {
	r.namespaced = namespaceScoped
}

func (r *REST) Categories() []string {
	return []string{category}
}

func (r *REST) SetOriginalGroup(originalGroup string) {
	r.originalGroup = originalGroup
}

func (r *REST) SetOriginalVersion(originalVersion string) {
	r.originalVersion = originalVersion
}

func (r *REST) SetKind(kind string) {
	r.kind = kind
}

func (r *REST) New() runtime.Object {
	newObj := &unstructured.Unstructured{}
	orignalGVK := r.GroupVersionKind(schema.GroupVersion{})
	newObj.SetAPIVersion(orignalGVK.GroupVersion().String())
	newObj.SetKind(orignalGVK.Kind)
	return newObj
}

func (r *REST) GroupVersionKind(_ schema.GroupVersion) schema.GroupVersionKind {
	// use original GVK
	return schema.GroupVersionKind{
		Group:   r.originalGroup,
		Version: r.originalVersion,
		Kind:    r.kind,
	}
}

func (r *REST) normalizeRequest(req *clientgorest.Request, namespace string) *clientgorest.Request {
	if len(r.originalGroup) == 0 {
		req.Prefix(CoreGroupPrefix, r.originalVersion)
	} else {
		req.Prefix(NamedGroupPrefix, r.originalGroup, r.originalVersion)
	}
	if r.namespaced {
		req.Namespace(namespace)
	}
	return req
}

func (r *REST) generateNameForManifest(name string) string {
	return fmt.Sprintf("%s-%s", strings.ToLower(r.kind), name)
}

func (r *REST) dryRunCreate(ctx context.Context, obj runtime.Object, createValidation rest.ValidateObjectFunc, options *metav1.CreateOptions) (*unstructured.Unstructured, error) {
	objNamespace := request.NamespaceValue(ctx)

	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil, errors.NewBadRequest(fmt.Sprintf("not a Unstructured object: %T", obj))
	}
	labels := u.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[known.ObjectCreatedByLabel] = known.ClusternetHubName
	u.SetLabels(labels)

	if r.kind != "Namespace" && r.namespaced {
		u.SetNamespace(ReservedNamespace)
	}
	dryRunNamespace := ReservedNamespace
	if r.kind == "Namespace" {
		dryRunNamespace = ""
	}

	body, err := u.MarshalJSON()
	if err != nil {
		return nil, errors.NewBadRequest(fmt.Sprintf("failed to marshal to json: %v", u.Object))
	}

	client := r.DryRunClient.RESTClient()
	result := &unstructured.Unstructured{}
	klog.V(7).Infof("creating %s with %s", r.kind, body)
	// first we dry-run the creation
	req := client.Post().
		Resource(r.name).
		Param("dryRun", "All").
		VersionedParams(options, r.ParameterCodec).
		Body(body)
	err = r.normalizeRequest(req, dryRunNamespace).Do(ctx).Into(result)
	if err != nil && errors.IsAlreadyExists(err) {
		// TODO: security risk
		// get existing object
		req := client.Get().
			Name(u.GetName()).
			Resource(r.name).
			VersionedParams(options, r.ParameterCodec).
			Body(body)
		err = r.normalizeRequest(req, dryRunNamespace).Do(ctx).Into(result)
	}
	if err != nil {
		return nil, err
	}

	if r.kind != "Namespace" && r.namespaced {
		result.SetNamespace(objNamespace)
	}
	return result, nil
}

func (r *REST) convertListOptionsToLabels(options *internalversion.ListOptions) (labels.Selector, error) {
	label := labels.Everything()
	if options != nil && options.LabelSelector != nil {
		label = options.LabelSelector
	}
	if options != nil && options.FieldSelector != nil {
		rqmts := options.FieldSelector.Requirements()
		for _, rqmt := range rqmts {
			var selectorKey string
			switch rqmt.Field {
			case "metadata.name":
				selectorKey = known.ConfigNameLabel
			default:
				return nil, errors.NewInternalError(fmt.Errorf("unable to recognize selector key %s", rqmt.Field))
			}
			requirement, err := labels.NewRequirement(selectorKey, rqmt.Operator, []string{rqmt.Value})
			if err != nil {
				return nil, err
			}
			label = label.Add(*requirement)
		}
	}

	// apply default labels
	requirement, err := labels.NewRequirement(known.ConfigSourceKindLabel, selection.Equals, []string{r.kind})
	if err != nil {
		return nil, err
	}
	label = label.Add(*requirement)

	return label, nil
}

// NewREST returns a RESTStorage object that will work against API services.
func NewREST(dryRunClient *kubernetes.Clientset, clusternetclient *clusternet.Clientset, parameterCodec runtime.ParameterCodec) *REST {
	return &REST{
		DryRunClient:     dryRunClient,
		ClusternetClient: clusternetclient,
		ParameterCodec:   parameterCodec,
		// currently we only set a default value for DeleteCollectionWorkers
		// TODO: make it configurable?
		DeleteCollectionWorkers: DefaultDeleteCollectionWorkers,
	}
}

var _ rest.GroupVersionKindProvider = &REST{}
var _ rest.CategoriesProvider = &REST{}
var _ rest.ShortNamesProvider = &REST{}
var _ rest.StandardStorage = &REST{}
