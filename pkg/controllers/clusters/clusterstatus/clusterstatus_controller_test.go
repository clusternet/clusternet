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
		}),
		1: BuildNode("node1", map[string]string{
			"node.clusternet.io/k1": "v1",
			"node.clusternet.io/k2": "v2",
			"node.clusternet.io/k3": "v3",
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
	}
}

func (suite *StatusSuite) TestGetCommonNodeLabels() {
	commonLabels := getCommonNodeLabels(suite.nodes)
	suite.Equal(
		map[string]string{"node.clusternet.io/k1": "v1", "node.clusternet.io/k2": "v2"},
		commonLabels, "failed get common labels from nodes")
}
