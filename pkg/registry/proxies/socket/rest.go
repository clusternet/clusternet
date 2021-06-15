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
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/registry/rest"

	proxies "github.com/clusternet/clusternet/pkg/apis/proxies/v1alpha1"
	"github.com/clusternet/clusternet/pkg/exchanger"
	"github.com/clusternet/clusternet/pkg/features"
	clusterInformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions/clusters/v1beta1"
)

const (
	category = "clusternet"
)

// REST implements a RESTStorage for Proxies API
type REST struct {
	exchanger        *exchanger.Exchanger
	socketConnection bool
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

// TODO: constraint proxy methods
var proxyMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

// ConnectMethods returns the list of HTTP methods that can be proxied
func (r *REST) ConnectMethods() []string {
	return proxyMethods
}

// NewConnectOptions returns versioned resource that represents proxy parameters
func (r *REST) NewConnectOptions() (runtime.Object, bool, string) {
	return &proxies.Socket{}, true, ""
}

// Connect returns a handler for the websocket connection
func (r *REST) Connect(ctx context.Context, id string, opts runtime.Object, responder rest.Responder) (http.Handler, error) {
	if !r.socketConnection {
		return nil, apierrors.NewServiceUnavailable(fmt.Sprintf("featuregate %s has not been enabled on the server side", features.SocketConnection))
	}
	socket, ok := opts.(*proxies.Socket)
	if !ok {
		return nil, fmt.Errorf("invalid options object: %#v", opts)
	}

	return r.exchanger.Connect(ctx, id, socket, responder)
}

// NewREST returns a RESTStorage object that will work against API services.
func NewREST(tunnelLogging, socketConnection bool, mclsInformer clusterInformers.ManagedClusterInformer) *REST {
	var ec *exchanger.Exchanger
	if socketConnection {
		ec = exchanger.NewExchanger(tunnelLogging, mclsInformer)
	}
	return &REST{
		exchanger:        ec,
		socketConnection: socketConnection,
	}
}

var _ rest.CategoriesProvider = &REST{}
var _ rest.ShortNamesProvider = &REST{}
var _ rest.Connecter = &REST{}
