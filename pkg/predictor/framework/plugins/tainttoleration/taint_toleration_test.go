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

// This file was copied from k8s.io/kubernetes/pkg/scheduler/framework/plugins/tainttoleration/taint_toleration_test.go and modified.

package tainttoleration

import (
	"context"
	"reflect"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	framework "github.com/clusternet/clusternet/pkg/predictor/framework/interfaces"
	"github.com/clusternet/clusternet/pkg/predictor/framework/runtime"
)

func nodeWithTaints(nodeName string, taints []v1.Taint) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
		},
		Spec: v1.NodeSpec{
			Taints: taints,
		},
	}
}

func requirementsWithTolerations(tolerations []v1.Toleration) *appsapi.ReplicaRequirements {
	return &appsapi.ReplicaRequirements{
		Tolerations: tolerations,
	}
}

func TestTaintTolerationScore(t *testing.T) {
	tests := []struct {
		name         string
		requirements *appsapi.ReplicaRequirements
		nodes        []*v1.Node
		expectedList framework.NodeScoreList
	}{
		// basic test case
		{
			name: "node with taints tolerated by the requirements, gets a higher score than those node with intolerable taints",
			requirements: requirementsWithTolerations([]v1.Toleration{{
				Key:      "foo",
				Operator: v1.TolerationOpEqual,
				Value:    "bar",
				Effect:   v1.TaintEffectPreferNoSchedule,
			}}),
			nodes: []*v1.Node{
				nodeWithTaints("nodeA", []v1.Taint{{
					Key:    "foo",
					Value:  "bar",
					Effect: v1.TaintEffectPreferNoSchedule,
				}}),
				nodeWithTaints("nodeB", []v1.Taint{{
					Key:    "foo",
					Value:  "blah",
					Effect: v1.TaintEffectPreferNoSchedule,
				}}),
			},
			expectedList: []framework.NodeScore{
				{Name: "nodeA", Score: framework.MaxNodeScore},
				{Name: "nodeB", Score: 0},
			},
		},
		// the count of taints that are tolerated by requirement, does not matter.
		{
			name: "the nodes that all of their taints are tolerated by the requirement, get the same score, no matter how many tolerable taints a node has",
			requirements: requirementsWithTolerations([]v1.Toleration{
				{
					Key:      "cpu-type",
					Operator: v1.TolerationOpEqual,
					Value:    "arm64",
					Effect:   v1.TaintEffectPreferNoSchedule,
				}, {
					Key:      "disk-type",
					Operator: v1.TolerationOpEqual,
					Value:    "ssd",
					Effect:   v1.TaintEffectPreferNoSchedule,
				},
			}),
			nodes: []*v1.Node{
				nodeWithTaints("nodeA", []v1.Taint{}),
				nodeWithTaints("nodeB", []v1.Taint{
					{
						Key:    "cpu-type",
						Value:  "arm64",
						Effect: v1.TaintEffectPreferNoSchedule,
					},
				}),
				nodeWithTaints("nodeC", []v1.Taint{
					{
						Key:    "cpu-type",
						Value:  "arm64",
						Effect: v1.TaintEffectPreferNoSchedule,
					}, {
						Key:    "disk-type",
						Value:  "ssd",
						Effect: v1.TaintEffectPreferNoSchedule,
					},
				}),
			},
			expectedList: []framework.NodeScore{
				{Name: "nodeA", Score: framework.MaxNodeScore},
				{Name: "nodeB", Score: framework.MaxNodeScore},
				{Name: "nodeC", Score: framework.MaxNodeScore},
			},
		},
		// the count of taints on a node that are not tolerated by requirement, matters.
		{
			name: "the more intolerable taints a node has, the lower score it gets.",
			requirements: requirementsWithTolerations([]v1.Toleration{{
				Key:      "foo",
				Operator: v1.TolerationOpEqual,
				Value:    "bar",
				Effect:   v1.TaintEffectPreferNoSchedule,
			}}),
			nodes: []*v1.Node{
				nodeWithTaints("nodeA", []v1.Taint{}),
				nodeWithTaints("nodeB", []v1.Taint{
					{
						Key:    "cpu-type",
						Value:  "arm64",
						Effect: v1.TaintEffectPreferNoSchedule,
					},
				}),
				nodeWithTaints("nodeC", []v1.Taint{
					{
						Key:    "cpu-type",
						Value:  "arm64",
						Effect: v1.TaintEffectPreferNoSchedule,
					}, {
						Key:    "disk-type",
						Value:  "ssd",
						Effect: v1.TaintEffectPreferNoSchedule,
					},
				}),
			},
			expectedList: []framework.NodeScore{
				{Name: "nodeA", Score: framework.MaxNodeScore},
				{Name: "nodeB", Score: 50},
				{Name: "nodeC", Score: 0},
			},
		},
		// taints-tolerations priority only takes care about the taints and tolerations that have effect PreferNoSchedule
		{
			name: "only taints and tolerations that have effect PreferNoSchedule are checked by taints-tolerations priority function",
			requirements: requirementsWithTolerations([]v1.Toleration{
				{
					Key:      "cpu-type",
					Operator: v1.TolerationOpEqual,
					Value:    "arm64",
					Effect:   v1.TaintEffectNoSchedule,
				}, {
					Key:      "disk-type",
					Operator: v1.TolerationOpEqual,
					Value:    "ssd",
					Effect:   v1.TaintEffectNoSchedule,
				},
			}),
			nodes: []*v1.Node{
				nodeWithTaints("nodeA", []v1.Taint{}),
				nodeWithTaints("nodeB", []v1.Taint{
					{
						Key:    "cpu-type",
						Value:  "arm64",
						Effect: v1.TaintEffectNoSchedule,
					},
				}),
				nodeWithTaints("nodeC", []v1.Taint{
					{
						Key:    "cpu-type",
						Value:  "arm64",
						Effect: v1.TaintEffectPreferNoSchedule,
					}, {
						Key:    "disk-type",
						Value:  "ssd",
						Effect: v1.TaintEffectPreferNoSchedule,
					},
				}),
			},
			expectedList: []framework.NodeScore{
				{Name: "nodeA", Score: framework.MaxNodeScore},
				{Name: "nodeB", Score: framework.MaxNodeScore},
				{Name: "nodeC", Score: 0},
			},
		},
		{
			name: "Default behaviour No taints and tolerations, lands on node with no taints",
			//requirements without tolerations
			requirements: requirementsWithTolerations([]v1.Toleration{}),
			nodes: []*v1.Node{
				//Node without taints
				nodeWithTaints("nodeA", []v1.Taint{}),
				nodeWithTaints("nodeB", []v1.Taint{
					{
						Key:    "cpu-type",
						Value:  "arm64",
						Effect: v1.TaintEffectPreferNoSchedule,
					},
				}),
			},
			expectedList: []framework.NodeScore{
				{Name: "nodeA", Score: framework.MaxNodeScore},
				{Name: "nodeB", Score: 0},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fakeInformerFactory := informers.NewSharedInformerFactory(&fake.Clientset{}, 0*time.Second)
			for _, node := range test.nodes {
				if err := fakeInformerFactory.Core().V1().Nodes().Informer().GetStore().Add(node); err != nil {
					t.Fatal(err)
				}
			}
			fh, err := runtime.NewFramework(nil, nil, runtime.WithInformerFactory(fakeInformerFactory))
			if err != nil {
				t.Fatal(err)
			}
			p, _ := New(nil, fh)
			var gotList framework.NodeScoreList
			for _, n := range test.nodes {
				nodeName := n.ObjectMeta.Name
				score, status := p.(framework.ScorePlugin).Score(context.Background(), test.requirements, nodeName)
				if !status.IsSuccess() {
					t.Errorf("unexpected error: %v", status)
				}
				gotList = append(gotList, framework.NodeScore{Name: nodeName, Score: score})
			}
			status := p.(framework.ScorePlugin).ScoreExtensions().NormalizeScore(context.Background(), test.requirements, gotList)
			if !status.IsSuccess() {
				t.Errorf("unexpected error: %v", status)
			}

			if !reflect.DeepEqual(test.expectedList, gotList) {
				t.Errorf("expected:\n\t%+v,\ngot:\n\t%+v", test.expectedList, gotList)
			}
		})
	}
}

func TestTaintTolerationFilter(t *testing.T) {
	tests := []struct {
		name         string
		requirements *appsapi.ReplicaRequirements
		node         *v1.Node
		wantStatus   *framework.Status
	}{
		{
			name:         "A requirement having no tolerations can't be scheduled onto a node with nonempty taints",
			requirements: requirementsWithTolerations([]v1.Toleration{}),
			node:         nodeWithTaints("nodeA", []v1.Taint{{Key: "dedicated", Value: "user1", Effect: "NoSchedule"}}),
			wantStatus: framework.NewStatus(framework.Unpredictable,
				"node(s) had untolerated taint {dedicated: user1}"),
		},
		{
			name:         "A requirement which can be scheduled on a dedicated node assigned to user1 with effect NoSchedule",
			requirements: requirementsWithTolerations([]v1.Toleration{{Key: "dedicated", Value: "user1", Effect: "NoSchedule"}}),
			node:         nodeWithTaints("nodeA", []v1.Taint{{Key: "dedicated", Value: "user1", Effect: "NoSchedule"}}),
		},
		{
			name:         "A requirement which can't be scheduled on a dedicated node assigned to user2 with effect NoSchedule",
			requirements: requirementsWithTolerations([]v1.Toleration{{Key: "dedicated", Operator: "Equal", Value: "user2", Effect: "NoSchedule"}}),
			node:         nodeWithTaints("nodeA", []v1.Taint{{Key: "dedicated", Value: "user1", Effect: "NoSchedule"}}),
			wantStatus: framework.NewStatus(framework.Unpredictable,
				"node(s) had untolerated taint {dedicated: user1}"),
		},
		{
			name:         "A requirement can be scheduled onto the node, with a toleration uses operator Exists that tolerates the taints on the node",
			requirements: requirementsWithTolerations([]v1.Toleration{{Key: "foo", Operator: "Exists", Effect: "NoSchedule"}}),
			node:         nodeWithTaints("nodeA", []v1.Taint{{Key: "foo", Value: "bar", Effect: "NoSchedule"}}),
		},
		{
			name: "A requirement has multiple tolerations, node has multiple taints, all the taints are tolerated, requirement can be scheduled onto the node",
			requirements: requirementsWithTolerations([]v1.Toleration{
				{Key: "dedicated", Operator: "Equal", Value: "user2", Effect: "NoSchedule"},
				{Key: "foo", Operator: "Exists", Effect: "NoSchedule"},
			}),
			node: nodeWithTaints("nodeA", []v1.Taint{
				{Key: "dedicated", Value: "user2", Effect: "NoSchedule"},
				{Key: "foo", Value: "bar", Effect: "NoSchedule"},
			}),
		},
		{
			name: "A requirement has a toleration that keys and values match the taint on the node, but (non-empty) effect doesn't match, " +
				"can't be scheduled onto the node",
			requirements: requirementsWithTolerations([]v1.Toleration{{Key: "foo", Operator: "Equal", Value: "bar", Effect: "PreferNoSchedule"}}),
			node:         nodeWithTaints("nodeA", []v1.Taint{{Key: "foo", Value: "bar", Effect: "NoSchedule"}}),
			wantStatus: framework.NewStatus(framework.Unpredictable,
				"node(s) had untolerated taint {foo: bar}"),
		},
		{
			name: "The requirement has a toleration that keys and values match the taint on the node, the effect of toleration is empty, " +
				"and the effect of taint is NoSchedule. Requirement can be scheduled onto the node",
			requirements: requirementsWithTolerations([]v1.Toleration{{Key: "foo", Operator: "Equal", Value: "bar"}}),
			node:         nodeWithTaints("nodeA", []v1.Taint{{Key: "foo", Value: "bar", Effect: "NoSchedule"}}),
		},
		{
			name: "The requirement has a toleration that key and value don't match the taint on the node, " +
				"but the effect of taint on node is PreferNoSchedule. Requirement can be scheduled onto the node",
			requirements: requirementsWithTolerations([]v1.Toleration{{Key: "dedicated", Operator: "Equal", Value: "user2", Effect: "NoSchedule"}}),
			node:         nodeWithTaints("nodeA", []v1.Taint{{Key: "dedicated", Value: "user1", Effect: "PreferNoSchedule"}}),
		},
		{
			name: "The requirement has no toleration, " +
				"but the effect of taint on node is PreferNoSchedule. Requirement can be scheduled onto the node",
			requirements: requirementsWithTolerations([]v1.Toleration{}),
			node:         nodeWithTaints("nodeA", []v1.Taint{{Key: "dedicated", Value: "user1", Effect: "PreferNoSchedule"}}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			nodeInfo := framework.NewNodeInfo(test.node, nil)
			p, _ := New(nil, nil)
			gotStatus := p.(framework.FilterPlugin).Filter(context.Background(), test.requirements, nodeInfo)
			if !reflect.DeepEqual(gotStatus, test.wantStatus) {
				t.Errorf("status does not match: %v, want: %v", gotStatus, test.wantStatus)
			}
		})
	}
}
