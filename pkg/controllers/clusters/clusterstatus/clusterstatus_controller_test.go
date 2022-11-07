package clusterstatus

import (
	"testing"

	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func BuildNode(name string, labels map[string]string) *corev1.Node {
	node := &corev1.Node{
		TypeMeta: metav1.TypeMeta{Kind: "Node"},
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
	return node
}

type StatusSuite struct {
	nodes []*corev1.Node
	suite.Suite
}

func TestDeployment(t *testing.T) {
	suite.Run(t, new(StatusSuite))
}

func (suite *StatusSuite) SetupTest() {
	suite.nodes = []*corev1.Node{
		0: BuildNode("node0", map[string]string{
			"node.clusternet.io/k1": "v1",
			"node.clusternet.io/k2": "v2",
			"node.clusternet.io/k3": "v3",
			"others.keys.io/k2":     "a1",
		}),
		1: BuildNode("node1", map[string]string{
			"node.clusternet.io/k1": "v1",
			"node.clusternet.io/k2": "v2",
			"node.clusternet.io/k3": "v3",
			"others.keys.io/k2":     "a1",
		}),
		2: BuildNode("node2", map[string]string{
			"node.clusternet.io/k1": "v1",
			"node.clusternet.io/k2": "v2",
			"node.clusternet.io/k4": "v4",
		}),
		3: BuildNode("node3", map[string]string{
			"node.clusternet.io/k1": "v1",
			"node.clusternet.io/k2": "v2",
			"node.clusternet.io/k3": "v3",
			"node.clusternet.io/k4": "v4",
		}),
		4: BuildNode("node4", map[string]string{
			"node.clusternet.io/k1":          "v1",
			"node.clusternet.io/k2":          "v2",
			"node.clusternet.io/k3":          "v3",
			"node.clusternet.io/k4":          "v4",
			"node-role.kubernetes.io/master": "",
		}),
	}
}

func (suite *StatusSuite) TestGetCommonNodeLabels() {
	commonLabels := getCommonNodeLabels(suite.nodes)
	suite.Equal(
		map[string]string{
			"node.clusternet.io/k1": "v1",
			"node.clusternet.io/k2": "v2",
		},
		commonLabels, "failed get common labels from nodes")
}

func (suite *StatusSuite) TestGetSingleNodeCommonNodeLabels() {
	commonLabels := getCommonNodeLabels(suite.nodes[0:1])
	suite.Equal(
		map[string]string{
			"node.clusternet.io/k1": "v1",
			"node.clusternet.io/k2": "v2",
			"node.clusternet.io/k3": "v3",
		},
		commonLabels, "failed get common labels from nodes")
}

func (suite *StatusSuite) TestAggregateLimitedLabels1() {
	commonLabels := aggregateLimitedLabels(suite.nodes, 0.8)
	suite.Equal(
		map[string]string{
			"node.clusternet.io/k1": "v1",
			"node.clusternet.io/k2": "v2",
		},
		commonLabels, "failed to aggregate labels from nodes")
}

func (suite *StatusSuite) TestAggregateLimitedLabels2() {
	commonLabels := aggregateLimitedLabels(suite.nodes, 0.7)
	suite.Equal(
		map[string]string{
			"node.clusternet.io/k1": "v1",
			"node.clusternet.io/k2": "v2",
			"node.clusternet.io/k3": "v3",
		},
		commonLabels, "failed to aggregate labels from nodes")
}

func (suite *StatusSuite) TestAggregateLimitedLabels3() {
	commonLabels := aggregateLimitedLabels(suite.nodes, 0.5)
	suite.Equal(
		map[string]string{
			"node.clusternet.io/k1": "v1",
			"node.clusternet.io/k2": "v2",
			"node.clusternet.io/k3": "v3",
			"node.clusternet.io/k4": "v4",
		},
		commonLabels, "failed to aggregate labels from nodes")
}

func (suite *StatusSuite) TestAggregateLimitedSingleLabels() {
	commonLabels := aggregateLimitedLabels(suite.nodes[0:1], 1)
	suite.Equal(
		map[string]string{
			"node.clusternet.io/k1": "v1",
			"node.clusternet.io/k2": "v2",
			"node.clusternet.io/k3": "v3",
		},
		commonLabels, "failed to aggregate single node")
}
