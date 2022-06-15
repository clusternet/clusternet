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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	framework "github.com/clusternet/clusternet/pkg/predictor/framework/interfaces"
)

func (ps *PredictorServer) HttpServer(ctx context.Context) error {
	http.HandleFunc("/predict", func(w http.ResponseWriter, r *http.Request) {
		requestBody, err := ioutil.ReadAll(r.Body)
		if err != nil {
			klog.Warning("error of read predictor request body : ", err)
		}

		var require appsapi.ReplicaRequirements
		err = json.Unmarshal(requestBody, &require)
		if err != nil {
			klog.Warning("error of Unmarshal predictor request body : ", err)
		}
		replicas, err := ps.MaxAcceptableReplicas(ctx, require)
		if err != nil {
			klog.Warning("error of get max acceptable replicas : ", err)
		}
		if value, isok := replicas[framework.DefaultAcceptableReplicasKey]; isok {
			responseData, err := json.Marshal(value)
			if err != nil {
				klog.Warning("error of marshal replicas to response writer byte : ", err)
			}
			_, err = w.Write(responseData)
			if err != nil {
				klog.Warning("error of write response : ", err)
			}
		}
	})

	// TODO: unschedulable replicas http handler function logic
	http.HandleFunc("/unschedulablereplicas", func(w http.ResponseWriter, r *http.Request) {})

	klog.Infof("Run predictor server with port %d ... ", ps.port)
	err := http.ListenAndServe(fmt.Sprintf("localhost:%d", ps.port), nil)
	return err
}
