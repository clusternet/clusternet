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
	"context"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clsapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	scheduler "github.com/clusternet/clusternet/pkg/apis/scheduler"
	framework "github.com/clusternet/clusternet/pkg/scheduler/framework/interfaces"
	"github.com/clusternet/clusternet/pkg/scheduler/framework/plugins/names"
	"k8s.io/apimachinery/pkg/runtime"
)

// Predictor is a plugin than checks if a subscription need resources
type Predictor struct {
	handle framework.Handle
}

var _ framework.PredictPlugin = &Predictor{}

// Name returns name of the plugin. It is used in logs, etc.
func (pl *Predictor) Name() string {
	return names.Predictor
}

// Predict invoked by scheduler predictor plugin
func (pl *Predictor) Predict(ctx context.Context,
	sub *appsapi.Subscription,
	finv *appsapi.FeedInventory,
	clusters *clsapi.ManagedCluster) (feedReplicas framework.FeedReplicas, s *framework.Status) {
	var pp scheduler.PredictorProvider

	if clusters.Status.PredictorEnabled {
		// TODO: use pkg/predictor create a new predictor provider
	} else {
		return nil, framework.NewStatus(framework.Skip, "predictor is disabled")
	}

	for _, feed := range finv.Spec.Feeds {
		if feed.ReplicaRequirements.Resources.Size() == 0 {
			feedReplicas = append(feedReplicas, 0)
		} else {
			replica, err := pp.MaxAcceptableReplicas(ctx, feed.ReplicaRequirements)
			if err != nil {
				return nil, framework.AsStatus(err)
			}
			feedReplicas = append(feedReplicas, replica[clusters.Name])
		}
	}
	return
}

// New initializes a new plugin and returns it.
func New(_ runtime.Object, h framework.Handle) (framework.Plugin, error) {
	return &Predictor{handle: h}, nil
}
