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
	"errors"
	"fmt"
	"net/http"
	"path"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/emicklei/go-restful/v3"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	apiextensionshelpers "k8s.io/apiextensions-apiserver/pkg/apihelpers"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/authorization/authorizer"
	"k8s.io/apiserver/pkg/endpoints"
	apiserverdiscovery "k8s.io/apiserver/pkg/endpoints/discovery"
	"k8s.io/apiserver/pkg/endpoints/handlers"
	"k8s.io/apiserver/pkg/endpoints/handlers/negotiation"
	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"
	apirequest "k8s.io/apiserver/pkg/endpoints/request"
	genericapiserver "k8s.io/apiserver/pkg/server"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	apiservicelisters "k8s.io/kube-aggregator/pkg/client/listers/apiregistration/v1"

	shadowapi "github.com/clusternet/clusternet/pkg/apis/shadow/v1alpha1"
	clusternet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/hub/registry/shadow/template"
	"github.com/clusternet/clusternet/pkg/known"
)

type crdHandler struct {
	lock sync.RWMutex

	minRequestTimeout   time.Duration
	maxRequestBodyBytes int64
	admissionControl    admission.Interface
	authorizer          authorizer.Authorizer
	serializer          runtime.NegotiatedSerializer

	kubeRESTClient   restclient.Interface
	clusternetClient *clusternet.Clientset

	manifestLister   applisters.ManifestLister
	crdInformer      apiextensionsinformers.CustomResourceDefinitionInformer
	apiserviceLister apiservicelisters.APIServiceLister

	rootPrefix string

	ws *restful.WebService
	// Storage per CRD
	storages map[string]*template.REST
	// Request scope per CRD
	requestScopes           map[string]*handlers.RequestScope
	versionDiscoveryHandler *versionDiscoveryHandler
	nonCRDAPIResources      []metav1.APIResource

	// namespace where Manifests are created
	reservedNamespace string
}

func NewCRDHandler(kubeRESTClient restclient.Interface, clusternetClient *clusternet.Clientset,
	manifestLister applisters.ManifestLister, apiserviceLister apiservicelisters.APIServiceLister,
	crdInformer apiextensionsinformers.CustomResourceDefinitionInformer,
	minRequestTimeout int, maxRequestBodyBytes int64,
	admissionControl admission.Interface, authorizer authorizer.Authorizer, serializer runtime.NegotiatedSerializer,
	reservedNamespace string) *crdHandler {
	r := &crdHandler{
		rootPrefix:          path.Join(genericapiserver.APIGroupPrefix, shadowapi.SchemeGroupVersion.String()),
		kubeRESTClient:      kubeRESTClient,
		clusternetClient:    clusternetClient,
		manifestLister:      manifestLister,
		crdInformer:         crdInformer,
		apiserviceLister:    apiserviceLister,
		minRequestTimeout:   time.Duration(minRequestTimeout) * time.Second,
		maxRequestBodyBytes: maxRequestBodyBytes,
		admissionControl:    admissionControl,
		authorizer:          authorizer,
		serializer:          serializer,
		storages:            map[string]*template.REST{},
		requestScopes:       map[string]*handlers.RequestScope{},
		reservedNamespace:   reservedNamespace,
	}
	return r
}

func (r *crdHandler) AddNonCRDAPIResource(apiResource metav1.APIResource) {
	r.lock.Lock()
	defer r.lock.Unlock()
	apiResource.Categories = []string{known.Category}
	r.nonCRDAPIResources = append(r.nonCRDAPIResources, apiResource)
}

// SetRootWebService should only called once
func (r *crdHandler) SetRootWebService(ws *restful.WebService) {
	r.lock.Lock()
	defer r.lock.Unlock()

	if r.ws != nil {
		panic("root WebService has already been set for CRD")
	}
	r.ws = ws

	r.versionDiscoveryHandler = newVersionDiscoveryHandler(
		r.serializer,
		shadowapi.SchemeGroupVersion,
		r.nonCRDAPIResources,
	)

	// update version discovery route
	versionDiscoveryPath := fmt.Sprintf("%s/%s/", genericapiserver.APIGroupPrefix, shadowapi.SchemeGroupVersion.String())
	// before we remove routes, always set dynamicRoutes as true
	r.ws.SetDynamicRoutes(true)
	defer r.ws.SetDynamicRoutes(false)
	if err := r.ws.RemoveRoute(versionDiscoveryPath, "GET"); err != nil {
		klog.Errorf("failed to remove route for %s", versionDiscoveryPath)
		return
	}
	mediaTypes, _ := negotiation.MediaTypesForSerializer(r.serializer)
	r.ws.Route(new(restful.WebService).Path(versionDiscoveryPath).GET("/").To(r.versionDiscoveryHandler.handle).
		Doc("get available resources").
		Operation("getAPIResources").
		Produces(mediaTypes...).
		Consumes(mediaTypes...).
		Writes(metav1.APIResourceList{}))

	// start event handler after ws is set
	_, err := r.crdInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    r.addCustomResourceDefinition,
		UpdateFunc: r.updateCustomResourceDefinition,
		DeleteFunc: r.deleteCustomResourceDefinition,
	})
	if err != nil {
		klog.Fatalf("failed to add event handler for crd: %v", err)
		return
	}
}

func (r *crdHandler) addCustomResourceDefinition(obj interface{}) {
	crd := obj.(*apiextensionsv1.CustomResourceDefinition)
	if strings.HasSuffix(crd.Spec.Group, clusternetGroupSuffix) {
		klog.V(4).Infof("skip syncing Clusternet related CustomResourceDefinition %q", klog.KObj(crd))
		return
	}

	klog.V(4).Infof("adding CustomResourceDefinition %q", klog.KObj(crd))
	err := r.addStorage(crd)
	if err != nil {
		klog.ErrorDepth(2, err)
	}
}

func (r *crdHandler) updateCustomResourceDefinition(old, cur interface{}) {
	oldCRD := old.(*apiextensionsv1.CustomResourceDefinition)
	newCRD := cur.(*apiextensionsv1.CustomResourceDefinition)

	if newCRD.DeletionTimestamp != nil {
		return
	}
	if reflect.DeepEqual(oldCRD.Spec, newCRD.Spec) {
		klog.V(4).Infof("no updates on the spec of CustomResourceDefinition %s, skipping syncing", klog.KObj(oldCRD))
		return
	}
	if strings.HasSuffix(newCRD.Spec.Group, clusternetGroupSuffix) {
		klog.V(4).Infof("skip syncing Clusternet related CustomResourceDefinition %s", klog.KObj(newCRD))
		return
	}
	klog.V(4).Infof("updating CustomResourceDefinition %q", klog.KObj(newCRD))
	r.removeStorage(oldCRD)
	err := r.addStorage(newCRD)
	if err != nil {
		klog.ErrorDepth(2, err)
	}
}

func (r *crdHandler) deleteCustomResourceDefinition(obj interface{}) {
	crd, ok := obj.(*apiextensionsv1.CustomResourceDefinition)
	if !ok {
		tombstone, ok2 := obj.(cache.DeletedFinalStateUnknown)
		if !ok2 {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		crd, ok2 = tombstone.Obj.(*apiextensionsv1.CustomResourceDefinition)
		if !ok2 {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a CustomResourceDefinition %#v", obj))
			return
		}
	}
	klog.V(5).Infof("deleting CustomResourceDefinition %q", klog.KObj(crd))
	r.removeStorage(crd)

	// TODO: clean up all CRs
	// Deleting a CRD will not bring in cascading deletion for shadow CRs. And these shadow CRs may be referenced as
	// feeds by multiple subscriptions.
}

func (r *crdHandler) removeStorage(crd *apiextensionsv1.CustomResourceDefinition) {
	r.versionDiscoveryHandler.removeCRD(crd)

	r.lock.Lock()
	delete(r.storages, crd.Spec.Names.Plural)
	delete(r.requestScopes, crd.Spec.Names.Plural)
	r.lock.Unlock()

	if r.ws == nil {
		klog.Error("nil root WebService for crdHandler")
		return
	}

	itemPath := path.Join(r.rootPrefix, crd.Spec.Names.Plural)
	if crd.Spec.Scope == apiextensionsv1.NamespaceScoped {
		itemPath = path.Join(r.rootPrefix, "namespaces/{namespace}", crd.Spec.Names.Plural)
	}

	var routesToBeRemoved []restful.Route
	for _, route := range r.ws.Routes() {
		if strings.HasPrefix(route.Path, itemPath) {
			routesToBeRemoved = append(routesToBeRemoved, route)
		}
	}

	// before we remove routes, always set dynamicRoutes as true
	r.ws.SetDynamicRoutes(true)
	defer r.ws.SetDynamicRoutes(false)
	for _, route := range routesToBeRemoved {
		if err := r.ws.RemoveRoute(route.Path, route.Method); err != nil {
			klog.ErrorDepth(5, fmt.Sprintf("failed to remove route %s", route.Path), err)
			continue
		}
	}
}

func (r *crdHandler) addStorage(crd *apiextensionsv1.CustomResourceDefinition) error {
	if crd.DeletionTimestamp != nil {
		return nil
	}
	if r.ws == nil {
		return errors.New("nil root WebService for crdHandler")
	}

	storageVersion, err := apiextensionshelpers.GetCRDStorageVersion(crd)
	if err != nil {
		return nil
	}
	if !apiextensionshelpers.HasServedCRDVersion(crd, storageVersion) {
		klog.WarningDepth(4, fmt.Sprintf("no served version found for CustomResourceDefinition %s. skip adding serving info.", klog.KObj(crd)))
		return nil
	}

	// register this resource to storage if the priority is the highest
	if !canBeAddedToStorage(crd.Spec.Group, storageVersion, crd.Spec.Names.Plural, r.apiserviceLister) {
		return nil
	}
	metav1.AddToGroupVersion(Scheme, schema.GroupVersion{Group: crd.Spec.Group, Version: storageVersion})

	r.versionDiscoveryHandler.updateCRD(crd)

	crdGroupInfo := genericapiserver.NewDefaultAPIGroupInfo(shadowapi.GroupName, Scheme, runtime.NewParameterCodec(Scheme), Codecs)
	var standardSerializers []runtime.SerializerInfo
	for _, s := range crdGroupInfo.NegotiatedSerializer.SupportedMediaTypes() {
		if s.MediaType == runtime.ContentTypeProtobuf {
			continue
		}
		standardSerializers = append(standardSerializers, s)
	}

	kind := crd.Spec.Names.Kind
	resource := crd.Spec.Names.Plural

	restStorage := template.NewREST(r.kubeRESTClient, r.clusternetClient, runtime.NewParameterCodec(Scheme), r.manifestLister, r.reservedNamespace)
	restStorage.SetNamespaceScoped(crd.Spec.Scope == apiextensionsv1.NamespaceScoped)
	restStorage.SetName(resource)
	restStorage.SetShortNames(crd.Spec.Names.ShortNames)
	restStorage.SetKind(crd.Spec.Names.Kind)
	restStorage.SetGroup(crd.Spec.Group)
	restStorage.SetVersion(storageVersion)
	restStorage.SetCRD(crd)

	groupVersionKind := restStorage.GroupVersionKind(schema.GroupVersion{})
	groupVersionResource := groupVersionKind.GroupVersion().WithResource(resource)
	equivalentResourceRegistry := runtime.NewEquivalentResourceRegistry()
	equivalentResourceRegistry.RegisterKindFor(groupVersionResource, "", groupVersionKind)
	subResources, err := apiextensionshelpers.GetSubresourcesForVersion(crd, storageVersion)
	if err != nil {
		return err
	}
	if subResources != nil {
		if subResources.Status != nil {
			equivalentResourceRegistry.RegisterKindFor(groupVersionResource, "status", shadowapi.SchemeGroupVersion.WithKind(kind))
		}
		if subResources.Scale != nil {
			equivalentResourceRegistry.RegisterKindFor(groupVersionResource, "scale", autoscalingv1.SchemeGroupVersion.WithKind("Scale"))
		}
	}

	r.lock.Lock()
	r.storages[resource] = restStorage
	r.requestScopes[resource] = &handlers.RequestScope{
		Namer: handlers.ContextBasedNaming{
			Namer:         meta.NewAccessor(),
			ClusterScoped: crd.Spec.Scope == apiextensionsv1.ClusterScoped,
		},
		Serializer:               crdGroupInfo.NegotiatedSerializer,
		ParameterCodec:           crdGroupInfo.ParameterCodec,
		StandardSerializers:      standardSerializers,
		Creater:                  crdGroupInfo.Scheme, //nolint:misspell
		Convertor:                crdGroupInfo.Scheme,
		Defaulter:                crdGroupInfo.Scheme,
		Typer:                    crdGroupInfo.Scheme,
		UnsafeConvertor:          runtime.UnsafeObjectConvertor(crdGroupInfo.Scheme),
		EquivalentResourceMapper: equivalentResourceRegistry,
		Resource:                 groupVersionResource,
		Kind:                     groupVersionKind,
		HubGroupVersion:          groupVersionKind.GroupVersion(),
		MetaGroupVersion:         metav1.SchemeGroupVersion,
		TableConvertor:           restStorage,
		Authorizer:               r.authorizer,
		MaxRequestBodyBytes:      r.maxRequestBodyBytes,
	}
	r.lock.Unlock()

	var resourcePath string
	var namespaced string
	switch crd.Spec.Scope {
	case apiextensionsv1.ClusterScoped:
		// handle cluster scoped resources
		resourcePath = resource
	default:
		resourcePath = "namespaces/{namespace}/" + resource
		namespaced = "Namespaced"
	}

	namespaceParam := restful.PathParameter("namespace", "object name and auth scope, such as for teams and projects").DataType("string")
	nameParam := restful.PathParameter("name", "name of the "+kind).DataType("string")

	// GET: Get a resource.
	func() {
		ws := r.newWebService()
		route := ws.GET(resourcePath + "/{name}").
			Doc("read the specified " + kind).
			Param(nameParam).
			Operation("read" + namespaced + kind).
			To(r.handle)
		if len(namespaced) > 0 {
			route.Param(namespaceParam)
		}
		r.ws.Route(route)
	}()

	// LIST: List all resources of a kind.
	func() {
		ws := r.newWebService()
		route := ws.GET(resourcePath).
			Doc("list or watch objects of kind " + kind).
			Operation("list" + namespaced + kind).
			To(r.handle)
		if len(namespaced) > 0 {
			route.Param(namespaceParam)
		}
		r.ws.Route(route)
	}()

	// LIST: List resources for all namespaces
	func() {
		if crd.Spec.Scope == apiextensionsv1.ClusterScoped {
			return
		}
		ws := r.newWebService()
		route := ws.GET(resource).
			Doc("list or watch objects of kind " + kind + "for all namespaces").
			Operation("list" + namespaced + kind + "ForAllNamespaces").
			To(r.handle)
		r.ws.Route(route)
	}()

	// PUT: Update a resource.
	func() {
		ws := r.newWebService()
		route := ws.PUT(resourcePath + "/{name}").
			Doc("replace the specified " + kind).
			Param(nameParam).
			Operation("replace" + namespaced + kind).
			To(r.handle)
		if len(namespaced) > 0 {
			route.Param(namespaceParam)
		}
		r.ws.Route(route)
	}()

	// PATCH: Partially update a resource
	func() {
		ws := r.newWebService()
		route := ws.PATCH(resourcePath + "/{name}").
			Doc("partially update the specified " + kind).
			Param(nameParam).
			Operation("patch" + namespaced + kind).
			To(r.handle)
		if len(namespaced) > 0 {
			route.Param(namespaceParam)
		}
		r.ws.Route(route)
	}()

	// POST: Create a resource
	func() {
		article := endpoints.GetArticleForNoun(kind, " ")
		ws := r.newWebService()
		route := ws.POST(resourcePath).
			Doc("create" + article + kind).
			Operation("create" + namespaced + kind).
			To(r.handle)
		if len(namespaced) > 0 {
			route.Param(namespaceParam)
		}
		r.ws.Route(route)
	}()

	// DELETE: Delete a resource.
	func() {
		article := endpoints.GetArticleForNoun(kind, " ")
		ws := r.newWebService()
		route := ws.DELETE(resourcePath + "/{name}").
			Doc("delete" + article + kind).
			Operation("delete" + namespaced + kind).
			To(r.handle)
		if len(namespaced) > 0 {
			route.Param(namespaceParam)
		}
		r.ws.Route(route)
	}()

	// DELETECOLLECTION
	func() {
		ws := r.newWebService()
		route := ws.PATCH(resourcePath).
			Doc("delete collection of " + kind).
			Operation("deletecollection" + namespaced + kind).
			To(r.handle)
		if len(namespaced) > 0 {
			route.Param(namespaceParam)
		}
		r.ws.Route(route)
	}()

	// status subresource
	if subResources != nil && subResources.Status != nil {
		routeFunction := restful.RouteFunction(func(request *restful.Request, response *restful.Response) {
			http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				http.Error(writer,
					fmt.Sprintf("Clusternet does not allow to %s status of %s", request.Method, kind),
					http.StatusMethodNotAllowed)
			})(response.ResponseWriter, request.Request)
		})

		// GET: Get subresource status.
		func() {
			ws := r.newWebService()
			route := ws.GET(resourcePath + "/{name}/status").
				Doc("read status of the specified " + kind).
				Param(nameParam).
				Operation("read" + namespaced + kind + "Status").
				To(routeFunction)
			if len(namespaced) > 0 {
				route.Param(namespaceParam)
			}
			r.ws.Route(route)
		}()

		// PUT: Update subresource status.
		func() {
			ws := r.newWebService()
			route := ws.PUT(resourcePath + "/{name}/status").
				Doc("replace status of the specified " + kind).
				Param(nameParam).
				Operation("replace" + namespaced + kind + "Status").
				To(routeFunction)
			if len(namespaced) > 0 {
				route.Param(namespaceParam)
			}
			r.ws.Route(route)
		}()

		// PATCH: Partially update subresource status
		func() {
			ws := r.newWebService()
			route := ws.PATCH(resourcePath + "/{name}/status").
				Doc("partially update status of the specified " + kind).
				Param(nameParam).
				Operation("patch" + namespaced + kind + "Status").
				To(routeFunction)
			if len(namespaced) > 0 {
				route.Param(namespaceParam)
			}
			r.ws.Route(route)
		}()
	}

	// scale subresource
	if subResources != nil && subResources.Scale != nil {
		// GET: Get subresource scale.
		func() {
			ws := r.newWebService()
			route := ws.GET(resourcePath + "/{name}/scale").
				Doc("read scale of the specified " + kind).
				Param(nameParam).
				Operation("read" + namespaced + kind + "Scale").
				To(r.handle)
			if len(namespaced) > 0 {
				route.Param(namespaceParam)
			}
			r.ws.Route(route)
		}()

		// PUT: Update subresource scale.
		func() {
			ws := r.newWebService()
			route := ws.PUT(resourcePath + "/{name}/scale").
				Doc("replace scale of the specified " + kind).
				Param(nameParam).
				Operation("replace" + namespaced + kind + "Scale").
				To(r.handle)
			if len(namespaced) > 0 {
				route.Param(namespaceParam)
			}
			r.ws.Route(route)
		}()

		// PATCH: Partially update subresource scale
		func() {
			ws := r.newWebService()
			route := ws.PATCH(resourcePath + "/{name}/scale").
				Doc("partially update scale of the specified " + kind).
				Param(nameParam).
				Operation("patch" + namespaced + kind + "Scale").
				To(r.handle)
			if len(namespaced) > 0 {
				route.Param(namespaceParam)
			}
			r.ws.Route(route)
		}()
	}
	return nil
}

// newWebService creates a new restful webservice with the api installer's prefix and version.
func (r *crdHandler) newWebService() *restful.WebService {
	mediaTypes, streamMediaTypes := negotiation.MediaTypesForSerializer(r.serializer)

	// Backwards compatibility, we accepted objects with empty content-type at V1.
	// If we stop using go-restful, we can default empty content-type to application/json on an
	// endpoint by endpoint basis

	// prefix contains "prefix/group/version"
	ws := new(restful.WebService)
	ws.Path(r.rootPrefix).Doc("API at " + r.rootPrefix).
		Consumes("*/*").
		Produces(append(mediaTypes, streamMediaTypes...)...).
		Param(restful.QueryParameter("pretty", "If 'true', then the output is pretty printed."))
	return ws
}

// handle returns a handler which will return the api.VersionAndVersion of the group.
func (r *crdHandler) handle(req *restful.Request, resp *restful.Response) {
	r.ServeHTTP(resp.ResponseWriter, req.Request)
}

// excerpts from k8s.io/apiextensions-apiserver/pkg/apiserver/customresource_handler.go and modified
func (r *crdHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	requestInfo, ok := apirequest.RequestInfoFrom(ctx)
	if !ok {
		responsewriters.ErrorNegotiated(
			apierrors.NewInternalError(fmt.Errorf("no RequestInfo found in the context")),
			Codecs, schema.GroupVersion{Group: requestInfo.APIGroup, Version: requestInfo.APIVersion}, w, req,
		)
		return
	}

	// for subresource, only allow "get", "update", "patch"
	if len(requestInfo.Subresource) > 0 {
		switch requestInfo.Verb {
		case "get", "update", "patch":
		default:
			responsewriters.ErrorNegotiated(
				apierrors.NewMethodNotSupported(schema.GroupResource{Group: requestInfo.APIGroup, Resource: requestInfo.Resource}, requestInfo.Verb),
				Codecs, schema.GroupVersion{Group: requestInfo.APIGroup, Version: requestInfo.APIVersion}, w, req,
			)
			return
		}
	}

	r.lock.RLock()
	requestScope := r.requestScopes[requestInfo.Resource]
	storage := r.storages[requestInfo.Resource]
	r.lock.RUnlock()
	switch requestInfo.Verb {
	case "get":
		handlers.GetResource(storage, requestScope).ServeHTTP(w, req)
	case "list":
		handlers.ListResource(storage, storage, requestScope, false, r.minRequestTimeout).ServeHTTP(w, req)
	case "watch":
		handlers.ListResource(storage, storage, requestScope, true, r.minRequestTimeout).ServeHTTP(w, req)
	case "create":
		handlers.CreateResource(storage, requestScope, r.admissionControl).ServeHTTP(w, req)
	case "update":
		handlers.UpdateResource(storage, requestScope, r.admissionControl).ServeHTTP(w, req)
	case "patch":
		handlers.PatchResource(storage, requestScope, r.admissionControl, []string{
			string(types.JSONPatchType),
			string(types.MergePatchType),
		}).ServeHTTP(w, req)
	case "delete":
		handlers.DeleteResource(storage, true, requestScope, r.admissionControl).ServeHTTP(w, req)
	case "deletecollection":
		handlers.DeleteCollection(storage, true, requestScope, r.admissionControl).ServeHTTP(w, req)
	default:
		responsewriters.ErrorNegotiated(
			apierrors.NewMethodNotSupported(schema.GroupResource{Group: requestInfo.APIGroup, Resource: requestInfo.Resource}, requestInfo.Verb),
			Codecs, schema.GroupVersion{Group: requestInfo.APIGroup, Version: requestInfo.APIVersion}, w, req,
		)
	}
}

type versionDiscoveryHandler struct {
	lock sync.RWMutex

	apiVersionHandler  *apiserverdiscovery.APIVersionHandler
	nonCRDAPIResources []metav1.APIResource

	crdAPIResources []metav1.APIResource
}

func newVersionDiscoveryHandler(serializer runtime.NegotiatedSerializer, groupVersion schema.GroupVersion, nonCRDAPIResources []metav1.APIResource) *versionDiscoveryHandler {
	s := &versionDiscoveryHandler{
		nonCRDAPIResources: nonCRDAPIResources,
		crdAPIResources:    []metav1.APIResource{},
	}
	s.apiVersionHandler = apiserverdiscovery.NewAPIVersionHandler(serializer, groupVersion, s)
	return s
}

func (h *versionDiscoveryHandler) updateCRDAPIResource(apiResource metav1.APIResource) {
	h.lock.Lock()
	defer h.lock.Unlock()
	apiResource.Categories = []string{known.Category}

	var index *int
	for idx, resource := range h.crdAPIResources {
		if resource.Name == apiResource.Name {
			index = &idx
			break
		}
	}
	if index == nil {
		h.crdAPIResources = append(h.crdAPIResources, apiResource)
	} else {
		h.crdAPIResources[*index] = apiResource
	}
}

func (h *versionDiscoveryHandler) updateCRD(crd *apiextensionsv1.CustomResourceDefinition) {
	var servedVersion string
	var subResourceScale bool
	for _, version := range crd.Spec.Versions {
		if version.Storage && version.Served {
			servedVersion = version.Name
			subResourceScale = version.Subresources != nil && version.Subresources.Scale != nil
			break
		}
	}

	apiResource := &metav1.APIResource{
		Name:         crd.Spec.Names.Plural,
		SingularName: crd.Spec.Names.Singular,
		Namespaced:   crd.Spec.Scope == apiextensionsv1.NamespaceScoped,
		Kind:         crd.Spec.Names.Kind,
		Verbs: []string{
			"delete",
			"deletecollection",
			"get",
			"list",
			"patch",
			"create",
			"update",
			"watch",
		},
		ShortNames:         crd.Spec.Names.ShortNames,
		StorageVersionHash: apiserverdiscovery.StorageVersionHash(crd.Spec.Group, servedVersion, crd.Spec.Names.Kind),
	}
	h.updateCRDAPIResource(*apiResource)

	// all these shadow apis are considered as templates, updating subresources, such as 'status' makes no sense.
	// so we only handle "scale" subresource
	if subResourceScale {
		scaleAPIResource := apiResource.DeepCopy()
		scaleAPIResource.Name = fmt.Sprintf("%s/scale", apiResource.Name)
		scaleAPIResource.Group = autoscalingv1.GroupName
		scaleAPIResource.Version = autoscalingv1.SchemeGroupVersion.Version
		scaleAPIResource.Kind = "Scale"
		scaleAPIResource.Verbs = []string{"get", "patch", "update"}
		scaleAPIResource.StorageVersionHash = ""
		h.updateCRDAPIResource(*scaleAPIResource)
	}
}

func (h *versionDiscoveryHandler) removeCRD(crd *apiextensionsv1.CustomResourceDefinition) {
	h.lock.Lock()
	defer h.lock.Unlock()
	var crdRDAPIResources []metav1.APIResource
	for _, apiResource := range h.crdAPIResources {
		if apiResource.Name == crd.Spec.Names.Plural {
			continue
		}
		if strings.HasPrefix(apiResource.Name, crd.Spec.Names.Plural+"/") {
			continue
		}
		crdRDAPIResources = append(crdRDAPIResources, apiResource)
	}
	h.crdAPIResources = crdRDAPIResources
}

// handle returns a handler which will return the api.VersionAndVersion of the group.
func (h *versionDiscoveryHandler) handle(req *restful.Request, resp *restful.Response) {
	h.ServeHTTP(resp.ResponseWriter, req.Request)
}

func (h *versionDiscoveryHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	h.lock.RLock()
	defer h.lock.RUnlock()
	h.apiVersionHandler.ServeHTTP(w, req)
}

func (h *versionDiscoveryHandler) ListAPIResources() []metav1.APIResource {
	return append(h.nonCRDAPIResources, h.crdAPIResources...)
}
