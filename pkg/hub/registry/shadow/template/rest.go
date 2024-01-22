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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionshelpers "k8s.io/apiextensions-apiserver/pkg/apihelpers"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiextensions-apiserver/pkg/registry/customresource/tableconvertor"
	"k8s.io/apimachinery/pkg/api/errors"
	apimachineryvalidation "k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/apiserver/pkg/registry/rest"
	clientgorest "k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusternet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/hub/registry/shadow/printers"
	printersinternal "github.com/clusternet/clusternet/pkg/hub/registry/shadow/printers/internalversion"
	printerstorage "github.com/clusternet/clusternet/pkg/hub/registry/shadow/printers/storage"
	"github.com/clusternet/clusternet/pkg/hub/registry/shadow/printers/util"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

const (
	CoreGroupPrefix  = "api"
	NamedGroupPrefix = "apis"

	// DefaultDeleteCollectionWorkers defines the default value for deleteCollectionWorkers
	DefaultDeleteCollectionWorkers = 2

	crdKind = "CustomResourceDefinition"
)

// REST implements a RESTStorage for Shadow API
type REST struct {
	// name is the plural name of the resource.
	name string
	// singularName is the singular name of the resource.
	singularName string
	// shortNames is a list of suggested short names of the resource.
	shortNames []string
	// namespaced indicates if a resource is namespaced or not.
	namespaced bool
	// kind is the Kind for the resource (e.g. 'Foo' is the kind for a resource 'foo')
	kind string
	// group is the Group of the resource.
	group string
	// version is the Version of the resource.
	version string

	parameterCodec runtime.ParameterCodec

	dryRunClient     clientgorest.Interface
	clusternetClient *clusternet.Clientset
	manifestLister   applisters.ManifestLister

	// deleteCollectionWorkers is the maximum number of workers in a single
	// DeleteCollection call. Delete requests for the items in a collection
	// are issued in parallel.
	deleteCollectionWorkers int

	// namespace where Manifests are created
	reservedNamespace string

	// is this Rest for CRD resource.
	CRD *apiextensionsv1.CustomResourceDefinition
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
			Name:      r.getNormalizedManifestName(result.GetNamespace(), result.GetName()),
			Namespace: r.reservedNamespace,
			Labels:    result.GetLabels(), // reuse labels from original object, which is useful for label selector
		},
		Template: runtime.RawExtension{
			Object: result,
		},
	}
	// append Clusternet labels
	if manifest.Labels == nil {
		manifest.Labels = map[string]string{}
	}
	manifest.Labels[known.ConfigGroupLabel] = r.group
	manifest.Labels[known.ConfigVersionLabel] = r.version
	manifest.Labels[known.ConfigKindLabel] = r.kind
	manifest.Labels[known.ConfigNameLabel] = result.GetName()
	manifest.Labels[known.ConfigNamespaceLabel] = result.GetNamespace()
	manifest, err = r.clusternetClient.AppsV1alpha1().Manifests(manifest.Namespace).Create(ctx, manifest, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			return nil, errors.NewAlreadyExists(schema.GroupResource{Group: r.group, Resource: r.name}, result.GetName())
		}
		return nil, err
	}
	return transformManifest(manifest)
}

// Get retrieves the item from Manifest.
func (r *REST) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	var manifest *appsapi.Manifest
	var err error
	if len(options.ResourceVersion) == 0 {
		manifest, err = r.manifestLister.Manifests(r.reservedNamespace).Get(r.getNormalizedManifestName(request.NamespaceValue(ctx), name))
	} else {
		manifest, err = r.clusternetClient.AppsV1alpha1().Manifests(r.reservedNamespace).
			Get(ctx, r.getNormalizedManifestName(request.NamespaceValue(ctx), name), *options)
	}
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, errors.NewNotFound(schema.GroupResource{Group: r.group, Resource: r.name}, name)
		}
		return nil, errors.NewInternalError(err)
	}

	return transformManifest(manifest)
}

// Update performs an atomic update and set of the object. Returns the result of the update
// or an error. If the registry allows create-on-update, the create flow will be executed.
// A bool is returned along with the object and any errors, to indicate object creation.
func (r *REST) Update(ctx context.Context, name string, objInfo rest.UpdatedObjectInfo,
	createValidation rest.ValidateObjectFunc, updateValidation rest.ValidateObjectUpdateFunc,
	forceAllowCreate bool, options *metav1.UpdateOptions) (runtime.Object, bool, error) {
	// We are explicitly taking forceAllowCreate as false.
	// TODO: forceAllowCreate could be true
	resource, subresource := r.getResourceName()
	if len(subresource) > 0 && !supportedSubresources.Has(subresource) {
		// all these shadow apis are considered as templates, updating subresources, such as 'status' makes no sense.
		err := errors.NewMethodNotSupported(schema.GroupResource{Group: r.group, Resource: r.name}, "")
		err.ErrStatus.Message = fmt.Sprintf("%s are considered as templates, which make no sense to update templates' %s",
			resource, subresource)
		return nil, false, err
	}

	manifest, err := r.manifestLister.Manifests(r.reservedNamespace).Get(r.getNormalizedManifestName(request.NamespaceValue(ctx), name))
	if err != nil {
		if errors.IsNotFound(err) {
			return nil, false, errors.NewNotFound(schema.GroupResource{Group: r.group, Resource: r.name}, name)
		}
		return nil, false, errors.NewInternalError(err)
	}

	oldObj := &unstructured.Unstructured{}
	if err = utils.Unmarshal(manifest.Template.Raw, oldObj); err != nil {
		return nil, false, errors.NewInternalError(err)
	}

	newObj, err := objInfo.UpdatedObject(ctx, oldObj)
	if err != nil {
		return nil, false, err
	}
	// Now we've got a fully formed object. Validators that apiserver handling chain wants to enforce can be called.
	if updateValidation != nil {
		if err = updateValidation(ctx, newObj.DeepCopyObject(), oldObj.DeepCopyObject()); err != nil {
			return nil, false, err
		}
	}
	result := newObj.(*unstructured.Unstructured)
	r.trimResult(result)

	// in case labels get changed
	manifestCopy := manifest.DeepCopy()
	if manifestCopy.Labels == nil {
		manifestCopy.Labels = map[string]string{}
	}
	for k, v := range result.GetLabels() {
		manifestCopy.Labels[k] = v
	}
	manifestCopy.Labels[known.ConfigGroupLabel] = r.group
	manifestCopy.Labels[known.ConfigVersionLabel] = r.version
	if r.kind != "Scale" {
		manifestCopy.Labels[known.ConfigKindLabel] = r.kind
	}
	manifestCopy.Labels[known.ConfigNameLabel] = result.GetName()
	manifestCopy.Labels[known.ConfigNamespaceLabel] = result.GetNamespace()
	manifestCopy.Template.Reset()
	manifestCopy.Template.Object = result
	// save the updates
	manifestCopy, err = r.clusternetClient.AppsV1alpha1().Manifests(r.reservedNamespace).Update(ctx, manifestCopy, *options)
	if err != nil {
		return nil, false, err
	}

	result, err = transformManifest(manifestCopy)
	return result, err != nil, err
}

// Delete removes the item from storage.
// options can be mutated by rest.BeforeDelete due to a graceful deletion strategy.
func (r *REST) Delete(ctx context.Context, name string, deleteValidation rest.ValidateObjectFunc, options *metav1.DeleteOptions) (runtime.Object, bool, error) {
	err := r.clusternetClient.AppsV1alpha1().Manifests(r.reservedNamespace).
		Delete(ctx, r.getNormalizedManifestName(request.NamespaceValue(ctx), name), *options)
	if err != nil {
		if errors.IsNotFound(err) {
			err = errors.NewNotFound(schema.GroupResource{Group: r.group, Resource: r.name}, name)
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
	// Related issue: https://github.com/clusternet/clusternet/issues/528
	// When clusternet-agent runs in parent cluster, if we delete a Subscription, then corresponding manifest feeds are deleted as well except namespace.
	// That's because when namespace were deleted, all namespaced API group include `shadow` group were deleted triggered by a Terminating namespace.
	// Then namespace-controller will delete all content ForGroupVersionResource.
	return nil, nil
}

// Watch makes a matcher for the given label and field.
func (r *REST) Watch(ctx context.Context, options *internalversion.ListOptions) (watch.Interface, error) {
	label, err := r.convertListOptionsToLabels(ctx, options)
	if err != nil {
		return nil, err
	}

	watcher, err := r.clusternetClient.AppsV1alpha1().Manifests(r.reservedNamespace).Watch(ctx, metav1.ListOptions{
		LabelSelector:        label.String(),
		FieldSelector:        "", // explicitly set FieldSelector to an empty string
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
			obj, err2 := transformManifest(manifest)
			if err2 != nil {
				klog.ErrorDepth(3, fmt.Sprintf("failed to transform Manifest %s: %v", klog.KObj(manifest), err2))
				return manifest
			}
			return obj
		}

		return object
	}, r.GroupVersion().WithKind(r.kind), defaultSize)
	if err == nil {
		go watchWrapper.Run()
	}
	return watchWrapper, err
}

// List returns a list of items matching labels.
func (r *REST) List(ctx context.Context, options *internalversion.ListOptions) (runtime.Object, error) {
	label, err := r.convertListOptionsToLabels(ctx, options)
	if err != nil {
		return nil, err
	}

	manifests, err := r.clusternetClient.AppsV1alpha1().Manifests(r.reservedNamespace).List(ctx, metav1.ListOptions{
		LabelSelector:        label.String(),
		FieldSelector:        "", // explicitly set FieldSelector to an empty string
		Watch:                options.Watch,
		AllowWatchBookmarks:  options.AllowWatchBookmarks,
		ResourceVersion:      options.ResourceVersion,
		ResourceVersionMatch: options.ResourceVersionMatch,
		TimeoutSeconds:       options.TimeoutSeconds,
		Limit:                options.Limit,
		Continue:             options.Continue,
	})
	if err != nil {
		return nil, err
	}

	result := &unstructured.UnstructuredList{}
	orignalGVK := r.GroupVersionKind(schema.GroupVersion{})
	result.SetAPIVersion(orignalGVK.GroupVersion().String())
	result.SetKind(r.getListKind())
	result.SetResourceVersion(manifests.ResourceVersion)
	result.SetContinue(manifests.Continue)
	// remainingItemCount will always be nil, since we're using non-empty label selectors.
	// This is a limitation on Kubernetes side.
	if len(manifests.Items) == 0 {
		return result, nil
	}
	for _, manifest := range manifests.Items {
		obj, err2 := transformManifest(&manifest)
		if err2 != nil {
			return nil, err2
		}
		result.Items = append(result.Items, *obj)
	}
	return result, nil
}

func (r *REST) NewList() runtime.Object {
	// Here the list GVK "meta.k8s.io/v1 List" is just a symbol,
	// since the real GVK will be set when List()
	newObj := &unstructured.UnstructuredList{}
	newObj.SetAPIVersion(metav1.SchemeGroupVersion.String())
	newObj.SetKind("List")
	return newObj
}

func (r *REST) ConvertToTable(ctx context.Context, object runtime.Object, tableOptions runtime.Object) (*metav1.Table, error) {
	if r.group == apiextensionsv1.GroupName && r.kind == crdKind {
		return rest.NewDefaultTableConvertor(schema.GroupResource{
			Group:    apiextensionsv1.GroupName,
			Resource: r.name,
		}).ConvertToTable(ctx, object, tableOptions)
	}

	if r.CRD != nil {
		storageVersion, _ := apiextensionshelpers.GetCRDStorageVersion(r.CRD)
		columns, _ := util.GetColumnsForVersion(r.CRD, storageVersion)
		tableConvertor, _ := tableconvertor.New(columns)
		return tableConvertor.ConvertToTable(ctx, object, tableOptions)
	} else {
		tableConvertor := printerstorage.TableConvertor{TableGenerator: printers.NewTableGenerator().With(printersinternal.AddHandlers)}
		return tableConvertor.ConvertToTable(ctx, object, tableOptions)
	}
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

func (r *REST) SetSingularName(name string) {
	r.singularName = name
}

func (r *REST) GetSingularName() string {
	return r.singularName
}

func (r *REST) NamespaceScoped() bool {
	return r.namespaced
}

func (r *REST) SetNamespaceScoped(namespaceScoped bool) {
	r.namespaced = namespaceScoped
}

func (r *REST) Categories() []string {
	return []string{known.Category}
}

func (r *REST) SetGroup(group string) {
	r.group = group
}

func (r *REST) SetVersion(version string) {
	r.version = version
}

func (r *REST) SetKind(kind string) {
	r.kind = kind
}

func (r *REST) SetCRD(crd *apiextensionsv1.CustomResourceDefinition) {
	r.CRD = crd
}

func (r *REST) New() runtime.Object {
	newObj := &unstructured.Unstructured{}
	orignalGVK := r.GroupVersionKind(schema.GroupVersion{})
	newObj.SetGroupVersionKind(orignalGVK)
	return newObj
}

func (r *REST) GroupVersionKind(_ schema.GroupVersion) schema.GroupVersionKind {
	// use original GVK
	return r.GroupVersion().WithKind(r.kind)
}

func (r *REST) GroupVersion() schema.GroupVersion {
	return schema.GroupVersion{
		Group:   r.group,
		Version: r.version,
	}
}

func (r *REST) Destroy() {
}

func (r *REST) normalizeRequest(req *clientgorest.Request, namespace string) *clientgorest.Request {
	if len(r.group) == 0 {
		req.Prefix(CoreGroupPrefix, r.version)
	} else {
		req.Prefix(NamedGroupPrefix, r.group, r.version)
	}
	if r.namespaced {
		req.Namespace(namespace)
	}
	return req
}

// getNormalizedManifestName will converge generateLegacyNameForManifest and generateNameForManifest
func (r *REST) getNormalizedManifestName(namespace, name string) string {
	legacyManifestName := r.generateLegacyNameForManifest(namespace, name)
	// backward compatible
	_, err := r.manifestLister.Manifests(r.reservedNamespace).Get(legacyManifestName)
	if err == nil {
		return legacyManifestName
	}

	return r.generateNameForManifest(namespace, name)
}

func (r *REST) generateNameForManifest(namespace, name string) string {
	resource, _ := r.getResourceName()
	// resource is a word ("[a-z]([-a-z0-9]*[a-z0-9])?") without "."
	// namespace is a word ("[a-z]([-a-z0-9]*[a-z0-9])?") without "."
	// so we use "." for concatenation
	if r.namespaced {
		return fmt.Sprintf("%s.%s.%s", resource, namespace, name)
	}
	return fmt.Sprintf("%s.%s", resource, name)
}

// DEPRECATED
// use hyphens for concatenation may lead to conflicts, such as
// - resource "foos" with name "lovely-panda" in namespace "bar"
// - resource "foos" with name "panda" in namespace "bar-lovely"
// will map a same Manifest object with name "foos-bar-lovely-panda"
func (r *REST) generateLegacyNameForManifest(namespace, name string) string {
	resource, _ := r.getResourceName()
	if r.namespaced {
		return fmt.Sprintf("%s-%s-%s", resource, namespace, name)
	}
	return fmt.Sprintf("%s-%s", resource, name)
}

func (r *REST) dryRunCreate(ctx context.Context, obj runtime.Object, _ rest.ValidateObjectFunc, options *metav1.CreateOptions) (*unstructured.Unstructured, error) {
	objNamespace := request.NamespaceValue(ctx)

	u, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil, errors.NewBadRequest(fmt.Sprintf("not a Unstructured object: %T", obj))
	}

	// check whether the given namespace name is valid
	if r.namespaced || r.kind == "Namespace" {
		fieldPath := field.NewPath("metadata", "namespace")
		if r.kind == "Namespace" {
			fieldPath = field.NewPath("metadata", "name")
			objNamespace = u.GetName()
		}
		if errs := apimachineryvalidation.ValidateNamespaceName(objNamespace, false); len(errs) > 0 {
			allErrs := field.ErrorList{field.Invalid(fieldPath, objNamespace, strings.Join(errs, ","))}
			return nil, errors.NewInvalid(r.GroupVersionKind(schema.GroupVersion{}).GroupKind(), u.GetName(), allErrs)
		}
	}

	labels := u.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[known.ObjectCreatedByLabel] = known.ClusternetHubName
	u.SetLabels(labels)

	annotations := u.GetAnnotations()
	// skip validating when SkipValidatingAnnotation is true
	if annotations != nil && annotations[known.SkipValidatingAnnotation] == "true" {
		if len(u.GetUID()) == 0 {
			// fixes https://github.com/clusternet/clusternet/issues/468
			u.SetUID(uuid.NewUUID())
		}
		return u, nil
	}

	if r.kind != "Namespace" && r.namespaced {
		u.SetNamespace(r.reservedNamespace)
	}
	// use reserved namespace (default to be "clusternet-reserved") to avoid error "namespaces not found"
	dryRunNamespace := r.reservedNamespace
	if r.kind == "Namespace" {
		dryRunNamespace = ""
	}

	body, err := u.MarshalJSON()
	if err != nil {
		return nil, errors.NewBadRequest(fmt.Sprintf("failed to marshal to json: %v", u.Object))
	}

	result := &unstructured.Unstructured{}
	klog.V(7).Infof("creating %s with %s", r.kind, body)
	resource, _ := r.getResourceName()
	// first we dry-run the creation
	req := r.dryRunClient.Post().
		Resource(resource).
		Param("dryRun", "All").
		VersionedParams(options, r.parameterCodec).
		Body(body)
	err = r.normalizeRequest(req, dryRunNamespace).Do(ctx).Into(result)
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			return nil, err
		}

		// already exists (only for CRD)
		if r.group == apiextensionsv1.GroupName && r.kind == crdKind {
			err = r.normalizeRequest(r.dryRunClient.Get().Resource(resource).Name(u.GetName()), dryRunNamespace).Do(ctx).Into(result)
			if err == nil {
				r.trimResult(result)
			}
			return result, err
		}

		// already exists, create a separate clean obj
		// we don't want to expose sensitive data of original obj
		uCopy := u.DeepCopy()
		uCopy.SetName(fmt.Sprintf("%s%s", u.GetName(), utilrand.String(3)))
		result, err = r.dryRunCreate(ctx, uCopy, nil, options)
		if err == nil {
			result.SetName(u.GetName())
		}
		return result, err
	}

	if r.kind != "Namespace" && r.namespaced {
		// set original namespace back
		result.SetNamespace(objNamespace)
	}

	// trim metadata
	r.trimResult(result)
	return result, nil
}

func (r *REST) trimResult(result *unstructured.Unstructured) {
	// trim common metadata
	trimCommonMetadata(result)

	switch r.GroupVersionKind(schema.GroupVersion{}).GroupKind() {
	case schema.GroupKind{Kind: "Job", Group: batchv1.GroupName}:
		trimBatchJob(result)
	case schema.GroupKind{Kind: "Service", Group: corev1.GroupName}:
		trimCoreService(result)
	}

	// trim status
	trimStatus(result)
}

func (r *REST) convertListOptionsToLabels(ctx context.Context, options *internalversion.ListOptions) (labels.Selector, error) {
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

	// apply default kind label
	kindRequirement, err := labels.NewRequirement(known.ConfigKindLabel, selection.Equals, []string{r.kind})
	if err != nil {
		return nil, err
	}
	label = label.Add(*kindRequirement)

	// apply default namespace label
	namespace := request.NamespaceValue(ctx)
	if len(namespace) > 0 {
		nsRequirement, err2 := labels.NewRequirement(known.ConfigNamespaceLabel, selection.Equals, []string{namespace})
		if err2 != nil {
			return nil, err2
		}
		label = label.Add(*nsRequirement)
	}

	return label, nil
}

func (r *REST) getResourceName() (string, string) {
	// is subresource
	if strings.Contains(r.name, "/") {
		resources := strings.Split(r.name, "/")
		return resources[0], resources[1]
	}

	return r.name, ""
}

func (r *REST) getListKind() string {
	if strings.Contains(r.name, "/") {
		return r.kind
	}
	return fmt.Sprintf("%sList", r.kind)
}

// NewREST returns a RESTStorage object that will work against API services.
func NewREST(dryRunClient clientgorest.Interface, clusternetclient *clusternet.Clientset, parameterCodec runtime.ParameterCodec,
	manifestLister applisters.ManifestLister, reservedNamespace string) *REST {
	return &REST{
		dryRunClient:            dryRunClient,
		clusternetClient:        clusternetclient,
		manifestLister:          manifestLister,
		parameterCodec:          parameterCodec,
		deleteCollectionWorkers: DefaultDeleteCollectionWorkers, // currently we only set a default value for deleteCollectionWorkers
		reservedNamespace:       reservedNamespace,
	}
}

var _ rest.SingularNameProvider = &REST{}
var _ rest.GroupVersionKindProvider = &REST{}
var _ rest.CategoriesProvider = &REST{}
var _ rest.ShortNamesProvider = &REST{}
var _ rest.StandardStorage = &REST{}
var _ rest.Storage = &REST{}
var supportedSubresources = sets.NewString(
	"scale",
)
