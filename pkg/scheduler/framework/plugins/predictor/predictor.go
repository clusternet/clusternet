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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clsapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/apis/scheduler"
	framework "github.com/clusternet/clusternet/pkg/scheduler/framework/interfaces"
	"github.com/clusternet/clusternet/pkg/scheduler/framework/plugins/names"
)

// Predictor is a plugin than checks if a subscription need resources
type Predictor struct {
	handle framework.Handle

	Address string
}

var _ framework.PredictPlugin = &Predictor{}

// Name returns name of the plugin. It is used in logs, etc.
func (pl *Predictor) Name() string {
	return names.Predictor
}

// Predict invoked by scheduler predictor plugin
func (pl *Predictor) Predict(ctx context.Context, state *framework.CycleState, sub *appsapi.Subscription, finv *appsapi.FeedInventory, cluster *clsapi.ManagedCluster) (feedReplicas framework.FeedReplicas, s *framework.Status) {
	var pp scheduler.PredictorProvider

	if cluster.Status.PredictorEnabled {
		// TODO: if clusters.status.predictorAddress is http://localhost, use agent built-in predictor
		if matched, _ := regexp.MatchString("http://localstatus:[0-9]+", cluster.Status.PredictorAddress); matched {
			return nil, framework.NewStatus(framework.Skip, "use built-in predictor")
		}
	} else {
		return nil, framework.NewStatus(framework.Skip, "predictor is disabled")
	}

	for _, feed := range finv.Spec.Feeds {
		if feed.ReplicaRequirements.Resources.Size() == 0 {
			feedReplicas = append(feedReplicas, 0)
		} else {
			pl.Address = cluster.Status.PredictorAddress
			replica, err := pl.MaxAcceptableReplicas(ctx, feed.ReplicaRequirements)
			if err != nil {
				return nil, framework.AsStatus(err)
			}
			var keys string
			for k, v := range feed.ReplicaRequirements.NodeSelector {
				keys = strings.Join([]string{fmt.Sprintf("%s=%s", k, v)}, ",")
			}
			feedReplicas = append(feedReplicas, replica[keys])
		}
	}
	return
}

// New initializes a new plugin and returns it.
func New(_ runtime.Object, h framework.Handle) (framework.Plugin, error) {
	return &Predictor{handle: h}, nil
}

func (pl *Predictor) MaxAcceptableReplicas(ctx context.Context, require appsapi.ReplicaRequirements) (map[string]int32, error) {
	replicas := make(map[string]int32)
	url := pl.Address + "/accept"
	jsonData, err := json.Marshal(require)
	if err != nil {
		return nil, err
	}
	response, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	data, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	replica, err := strconv.Atoi(string(data))
	if err != nil {
		return nil, err
	}
	var keys string
	for k, v := range require.NodeSelector {
		keys = strings.Join([]string{fmt.Sprintf("%s=%s", k, v)}, ",")
	}
	replicas[keys] = int32(replica)
	return replicas, nil
}
