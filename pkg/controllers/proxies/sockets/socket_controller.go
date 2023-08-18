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

package sockets

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"

	"github.com/gorilla/websocket"
	"github.com/rancher/remotedialer"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
	"k8s.io/klog/v2"

	proxiesapi "github.com/clusternet/clusternet/pkg/apis/proxies/v1alpha1"
	"github.com/clusternet/clusternet/pkg/known"
)

// Controller is a controller that helps setup/maintain websocket connection
type Controller struct {
	baseURL    string
	headers    http.Header
	dialer     *websocket.Dialer
	kubeConfig *rest.Config
}

func NewController(kubeConfig *rest.Config, tunnelLogging bool) (*Controller, error) {
	if tunnelLogging {
		logrus.SetLevel(logrus.DebugLevel)
	}

	tlsConfig, err := rest.TLSConfigFor(kubeConfig)
	if err != nil {
		return nil, err
	}
	// TODO: check CA
	tlsConfig.InsecureSkipVerify = true
	dialer := &websocket.Dialer{
		TLSClientConfig: tlsConfig,
	}

	headers := http.Header{}
	bearerToken := kubeConfig.BearerToken
	if len(kubeConfig.BearerTokenFile) > 0 {
		source := transport.NewCachedFileTokenSource(kubeConfig.BearerTokenFile)
		token, err2 := source.Token()
		if err2 != nil {
			return nil, err2
		}
		bearerToken = token.AccessToken
	}
	if len(bearerToken) > 0 {
		headers.Set("Authorization", "Bearer "+bearerToken)
	}

	u, err := url.Parse(kubeConfig.Host)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "http" {
		u.Scheme = "ws"
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	}
	u.Path = path.Join(u.Path, "apis", proxiesapi.SchemeGroupVersion.String(), "sockets")

	return &Controller{
		kubeConfig: kubeConfig,
		dialer:     dialer,
		headers:    headers,
		baseURL:    u.String(),
	}, nil
}

func (c *Controller) Run(ctx context.Context, clusterID *types.UID) {
	wsURL := fmt.Sprintf("%s/%s", c.baseURL, string(*clusterID))
	klog.V(4).Infof("setting up websocket connection to %s", wsURL)

	wait.JitterUntilWithContext(ctx, func(ctx context.Context) {
		err := remotedialer.ClientConnect(ctx, wsURL, c.headers, c.dialer, func(string, string) bool { return true }, nil)
		if err != nil {
			klog.Errorf("websocket connection error: %v", err)
		}
	}, known.DefaultRetryPeriod, 0.3, true)
}
