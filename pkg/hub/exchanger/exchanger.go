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

package exchanger

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/rancher/remotedialer"
	"github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/proxy"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/klog/v2"

	proxies "github.com/clusternet/clusternet/pkg/apis/proxies/v1alpha1"
	clusterlisters "github.com/clusternet/clusternet/pkg/generated/listers/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/known"
)

type Exchanger struct {
	// use cachedTransports to speed up networking connections
	cachedTransports map[string]*http.Transport

	lock sync.Mutex

	// dialerServer is used for serving websocket connection
	dialerServer *remotedialer.Server

	mcLister clusterlisters.ManagedClusterLister
}

var (
	urlPrefix = fmt.Sprintf("/apis/%s/sockets/", proxies.SchemeGroupVersion.String())
)

func authorizer(req *http.Request) (string, bool, error) {
	if strings.Contains(req.URL.Path, urlPrefix) {
		clusterID := strings.TrimPrefix(strings.TrimRight(req.URL.Path, "/"), urlPrefix)
		return clusterID, clusterID != "", nil
	}
	return "", false, fmt.Errorf("illegal request %s", req.URL.Path)
}

func NewExchanger(peerID, peerToken string, tunnelLogging bool, mcLister clusterlisters.ManagedClusterLister) *Exchanger {
	if tunnelLogging {
		logrus.SetLevel(logrus.DebugLevel)
		remotedialer.PrintTunnelData = true
	}

	e := &Exchanger{
		cachedTransports: map[string]*http.Transport{},
		dialerServer:     remotedialer.New(authorizer, remotedialer.DefaultErrorWriter),
		mcLister:         mcLister,
	}
	e.dialerServer.PeerID = peerID
	e.dialerServer.PeerToken = peerToken
	return e
}

func (e *Exchanger) getClonedTransport(clusterID string) *http.Transport {
	// return cloned transport to avoid being changed outside
	e.lock.Lock()
	defer e.lock.Unlock()

	transport := e.cachedTransports[clusterID]
	if transport != nil {
		return transport.Clone()
	}

	dialer := e.dialerServer.Dialer(clusterID)
	transport = &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
		DialContext: dialer,
		// apply default settings from http.DefaultTransport
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	e.cachedTransports[clusterID] = transport
	return transport.Clone()
}

func (e *Exchanger) Connect(ctx context.Context, id string, opts *proxies.Socket, responder rest.Responder) (http.Handler, error) {
	return e.GetDialerHandler(), nil
}

func (e *Exchanger) GetDialerHandler() *remotedialer.Server {
	return e.dialerServer
}

func (e *Exchanger) ProxyConnect(ctx context.Context, id string, opts *proxies.Socket, responder rest.Responder, extraHeaderPrefixes []string) (http.Handler, error) {
	location, _, err := e.ClusterLocation(id, opts)
	if err != nil {
		return nil, err
	}

	// TODO: add metrics

	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		location.RawQuery = request.URL.RawQuery
		// proxy and re-dial through websocket connection
		klog.V(4).Infof("Request to %q will be redialed from cluster %q", location.String(), id)

		extra := getExtraFromHeaders(request.Header, extraHeaderPrefixes)
		for key, vals := range extra {
			if !strings.HasPrefix(key, known.HeaderPrefixKey) {
				continue
			}

			switch key {
			case known.TokenHeaderKey, known.CertificateHeaderKey, known.PrivateKeyHeaderKey:
				continue
			default:
				for _, val := range vals {
					request.Header.Add(strings.TrimPrefix(key, known.HeaderPrefixKey), val)
				}
			}
		}

		if token, ok := extra[strings.ToLower(known.TokenHeaderKey)]; ok && len(token) > 0 {
			request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token[0]))
		}

		transport := &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
			DialContext: e.dialerServer.Dialer(id),
			// apply default settings from http.DefaultTransport
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}

		publicCertificate := extra[strings.ToLower(known.CertificateHeaderKey)]
		privateKey := extra[strings.ToLower(known.PrivateKeyHeaderKey)]
		if len(publicCertificate) > 0 && len(privateKey) > 0 {
			certPEMBlock, err2 := base64.StdEncoding.DecodeString(publicCertificate[0])
			if err2 != nil {
				responder.Error(apierrors.NewBadRequest(fmt.Sprintf("invalid certificate in header %s: %v", known.CertificateHeaderKey, err2)))
				return
			}
			keyPEMBlock, err2 := base64.StdEncoding.DecodeString(privateKey[0])
			if err2 != nil {
				responder.Error(apierrors.NewBadRequest(fmt.Sprintf("invalid private key in header %s: %v", known.PrivateKeyHeaderKey, err2)))
				return
			}
			cert, err2 := tls.X509KeyPair(certPEMBlock, keyPEMBlock)
			if err2 != nil {
				responder.Error(apierrors.NewBadRequest(fmt.Sprintf("invalid key pair in header: %v", err2)))
				return
			}

			transport.TLSClientConfig.Certificates = []tls.Certificate{cert}
		}

		for k := range extra {
			for _, prefix := range extraHeaderPrefixes {
				request.Header.Del(prefix + k)
			}
		}

		handler := newThrottledUpgradeAwareProxyHandler(location, transport, false, false, true, responder)
		handler.ServeHTTP(writer, request)
	})

	return handler, nil
}

func (e *Exchanger) ClusterLocation(id string, opts *proxies.Socket) (*url.URL, http.RoundTripper, error) {
	var transport *http.Transport

	mcls, err := e.mcLister.List(labels.SelectorFromSet(labels.Set{
		known.ClusterIDLabel: id,
	}))
	if err != nil {
		return nil, nil, apierrors.NewServiceUnavailable(err.Error())
	}
	if mcls == nil {
		return nil, nil, apierrors.NewBadRequest(fmt.Sprintf("no cluster id is %s", id))
	}
	if len(mcls) > 1 {
		klog.Warningf("found multiple ManagedCluster dedicated for cluster %s !!!", id)
	}

	reqPath := strings.TrimLeft(opts.Path, "/")
	parts := strings.Split(reqPath, "/")
	if len(parts) == 0 {
		return nil, nil, apierrors.NewBadRequest(fmt.Sprintf("unexpected error: invalid request path %s", reqPath))
	}
	var isShortPath bool
	if parts[0] == "direct" {
		isShortPath = true
	}

	location := new(url.URL)
	if isShortPath {
		apiserverURL := mcls[0].Status.APIServerURL
		if len(apiserverURL) == 0 {
			return nil, nil, apierrors.NewServiceUnavailable(fmt.Sprintf("cannot retrieve valid apiserver url for cluster %s", id))
		}

		loc, err2 := url.Parse(apiserverURL)
		if err2 != nil {
			return nil, nil, apierrors.NewInternalError(err2)
		}

		location.Scheme = loc.Scheme
		location.Host = loc.Host

		paths := []string{loc.Path}
		if len(parts) > 1 {
			paths = append(paths, parts[1:]...)
		}
		location.Path = path.Join(paths...)
	} else {
		location.Scheme = parts[0]
		if len(parts) > 1 {
			location.Host = parts[1]
		}
		if len(parts) > 2 {
			location.Path = path.Join(parts[2:]...)
		}
	}

	if mcls[0].Status.UseSocket {
		if !e.dialerServer.HasSession(id) {
			return nil, nil, apierrors.NewBadRequest(fmt.Sprintf("cannot proxy through cluster %s, whose agent is disconnected", id))
		}
		transport = e.getClonedTransport(id)
	}

	return location, transport, nil
}

func newThrottledUpgradeAwareProxyHandler(location *url.URL, transport http.RoundTripper, wrapTransport, upgradeRequired, useLocationHost bool, responder rest.Responder) *proxy.UpgradeAwareHandler {
	// if location.Path is empty, a status code 301 will be returned by below handler with a new location
	// ends with a '/'. This is essentially a hack for http://issue.k8s.io/4958.
	handler := proxy.NewUpgradeAwareHandler(location, transport, wrapTransport, upgradeRequired, proxy.NewErrorResponder(responder))
	handler.UseLocationHost = useLocationHost
	return handler
}

// copied from k8s.io/apiserver/pkg/authentication/request/headerrequest/requestheader.go
func unescapeExtraKey(encodedKey string) string {
	key, err := url.PathUnescape(encodedKey) // Decode %-encoded bytes.
	if err != nil {
		return encodedKey // Always record extra strings, even if malformed/unencoded.
	}
	return key
}

// copied from k8s.io/apiserver/pkg/authentication/request/headerrequest/requestheader.go
func getExtraFromHeaders(h http.Header, headerPrefixes []string) map[string][]string {
	ret := map[string][]string{}

	// we have to iterate over prefixes first in order to have proper ordering inside the value slices
	for _, prefix := range headerPrefixes {
		for headerName, vv := range h {
			if !strings.HasPrefix(strings.ToLower(headerName), strings.ToLower(prefix)) {
				continue
			}

			extraKey := unescapeExtraKey(strings.ToLower(headerName[len(prefix):]))
			ret[extraKey] = append(ret[extraKey], vv...)
		}
	}

	return ret
}
