/*
Copyright 2022 The Clusternet Authors.

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

package predictor

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	restclient "k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	utilpointer "k8s.io/utils/pointer"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	proxiesapi "github.com/clusternet/clusternet/pkg/apis/proxies/v1alpha1"
	schedulerapi "github.com/clusternet/clusternet/pkg/apis/scheduler"
	"github.com/clusternet/clusternet/pkg/features"
	framework "github.com/clusternet/clusternet/pkg/scheduler/framework/interfaces"
	"github.com/clusternet/clusternet/pkg/scheduler/framework/plugins/names"
	"github.com/clusternet/clusternet/pkg/utils"
)

const (
	// Name is the name of the plugin used in the plugin registry and configurations.
	Name = names.Predictor

	// DefaultTimeout is the default timeout for a predictor request.
	DefaultTimeout = time.Second * 20
)

var _ framework.PredictPlugin = &Predictor{}

// Predictor is a plugin than checks if a subscription need resources
type Predictor struct {
	handle framework.Handle
}

// New initializes a new plugin and returns it.
func New(_ runtime.Object, h framework.Handle) (framework.Plugin, error) {
	return &Predictor{handle: h}, nil
}

// Name returns name of the plugin. It is used in logs, etc.
func (pl *Predictor) Name() string {
	return Name
}

// Predict invoked by scheduler predictor plugin
func (pl *Predictor) Predict(
	ctx context.Context,
	_ *framework.CycleState,
	_ *appsapi.Subscription,
	finv *appsapi.FeedInventory,
	mcls *clusterapi.ManagedCluster,
) (feedReplicas framework.FeedReplicas, s *framework.Status) {
	if !mcls.Status.PredictorEnabled {
		return nil, framework.NewStatus(framework.Skip, "predictor is disabled")
	}

	predictorAddress := mcls.Status.PredictorAddress
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	var err error
	if !mcls.Status.PredictorDirectAccess {
		klog.V(5).Infof("the predictor of cluster %s can not be accessed directly", mcls.Spec.ClusterID)
		if !mcls.Status.UseSocket {
			return nil, framework.AsStatus(fmt.Errorf("cluster %s does not enable feature gate %s",
				mcls.Spec.ClusterID, features.SocketConnection))
		}

		httpClient, err = restclient.HTTPClientFor(pl.handle.KubeConfig())
		if err != nil {
			return nil, framework.AsStatus(err)
		}

		predictorAddress = strings.Replace(predictorAddress, "http://", "http/", 1)
		predictorAddress = strings.Replace(predictorAddress, "https://", "https/", 1)
		predictorAddress = strings.Join([]string{
			strings.TrimRight(pl.handle.KubeConfig().Host, "/"),
			fmt.Sprintf(
				"apis/%s/sockets/%s/proxy/%s",
				proxiesapi.SchemeGroupVersion.String(),
				mcls.Spec.ClusterID,
				predictorAddress,
			),
		}, "/")
	}
	httpClient.Timeout = DefaultTimeout

	for _, feedOrder := range finv.Spec.Feeds {
		if feedOrder.DesiredReplicas == nil {
			feedReplicas = append(feedReplicas, nil)
			continue
		}

		replica, err2 := predictMaxAcceptableReplicas(
			ctx,
			httpClient,
			mcls.Spec.ClusterID,
			predictorAddress,
			feedOrder.ReplicaRequirements)
		if err2 != nil {
			return nil, framework.AsStatus(err2)
		}
		// Todo : support topology-aware replicas
		feedReplicas = append(feedReplicas, utilpointer.Int32(replica[schedulerapi.DefaultAcceptableReplicasKey]))
	}
	return
}

func predictMaxAcceptableReplicas(
	ctx context.Context,
	httpClient *http.Client,
	clusterID types.UID,
	address string,
	require appsapi.ReplicaRequirements,
) (map[string]int32, error) {
	payload, err := utils.Marshal(require)
	if err != nil {
		return nil, err
	}

	urlForReplicasPredicting, err := url.JoinPath(address, schedulerapi.RootPathReplicas, schedulerapi.SubPathPredict)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", urlForReplicasPredicting, bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(schedulerapi.ClusterIDHeader, string(clusterID))

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	predictorResults := &schedulerapi.PredictorResults{}
	err = utils.Unmarshal(data, predictorResults)
	if err != nil {
		return nil, err
	}

	return predictorResults.Replicas, nil
}
