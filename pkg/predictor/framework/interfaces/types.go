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

package interfaces

import (
	v1 "k8s.io/api/core/v1"
)

// NodeInfo is node level aggregated information.
type NodeInfo struct {
	// Overall node information.
	node *v1.Node

	// Pods running on the node.
	Pods []*v1.Pod
}

// NewNodeInfo new a NodeInfo
func NewNodeInfo(node *v1.Node, pods []*v1.Pod) *NodeInfo {
	return &NodeInfo{
		node: node,
		Pods: pods,
	}
}

// Node returns overall information about this node.
func (n *NodeInfo) Node() *v1.Node {
	if n == nil {
		return nil
	}
	return n.node
}
