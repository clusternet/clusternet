package clusterstatus

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/clusternet/clusternet/pkg/known"
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
	commonLabels := aggregateLimitedLabels(suite.nodes, 0.8, []string{known.NodeLabelsKeyPrefix})
	suite.Equal(
		map[string]string{
			"node.clusternet.io/k1": "v1",
			"node.clusternet.io/k2": "v2",
		},
		commonLabels, "failed to aggregate labels from nodes")
}

func (suite *StatusSuite) TestAggregateLimitedLabels2() {
	commonLabels := aggregateLimitedLabels(suite.nodes, 0.7, []string{known.NodeLabelsKeyPrefix})
	suite.Equal(
		map[string]string{
			"node.clusternet.io/k1": "v1",
			"node.clusternet.io/k2": "v2",
			"node.clusternet.io/k3": "v3",
		},
		commonLabels, "failed to aggregate labels from nodes")
}

func (suite *StatusSuite) TestAggregateLimitedLabels3() {
	commonLabels := aggregateLimitedLabels(suite.nodes, 0.5, []string{known.NodeLabelsKeyPrefix})
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
	commonLabels := aggregateLimitedLabels(suite.nodes[0:1], 1, []string{known.NodeLabelsKeyPrefix})
	suite.Equal(
		map[string]string{
			"node.clusternet.io/k1": "v1",
			"node.clusternet.io/k2": "v2",
			"node.clusternet.io/k3": "v3",
		},
		commonLabels, "failed to aggregate single node")
}

func TestAggregateLimitedLabels(t *testing.T) {
	type arg struct {
		nodes         []*corev1.Node
		threshold     float32
		labelPrefixes []string
	}
	var tests = []struct {
		name string
		args arg
		want map[string]string
	}{
		{
			name: "aggregate one",
			args: arg{
				nodes:         []*corev1.Node{BuildNode("n0", map[string]string{"foo": "bar", "name": "n0"})},
				threshold:     0,
				labelPrefixes: []string{"foo"},
			},
			want: map[string]string{"foo": "bar"},
		},
		{
			name: "aggretate with 100% threshold case 1",
			args: arg{
				nodes: []*corev1.Node{
					BuildNode("n0", map[string]string{"foo": "bar", "name": "n0"}),
					BuildNode("n1", map[string]string{"foo": "bar", "name": "n1"}),
					BuildNode("n3", map[string]string{"name": "n2"}),
				},
				threshold:     1,
				labelPrefixes: []string{"foo"},
			},
			want: map[string]string{},
		},
		{
			name: "aggretate with 100% threshold case 2",
			args: arg{
				nodes: []*corev1.Node{
					BuildNode("n0", map[string]string{"foo": "bar", "name": "n0"}),
					BuildNode("n1", map[string]string{"foo": "bar", "name": "n1"}),
					BuildNode("n3", map[string]string{"foo": "bar", "name": "n2"}),
				},
				threshold:     1,
				labelPrefixes: []string{"foo"},
			},
			want: map[string]string{"foo": "bar"},
		},
		{
			name: "aggretate with mutil prefix case 1",
			args: arg{
				nodes: []*corev1.Node{
					BuildNode("n0", map[string]string{"foo": "bar", "zone": "zone0"}),
					BuildNode("n1", map[string]string{"foo": "bar", "zone": "zone0"}),
					BuildNode("n3", map[string]string{"foo": "bar", "zone": "zone1"}),
				},
				threshold:     0.5,
				labelPrefixes: []string{"foo", "zone"},
			},
			want: map[string]string{"foo": "bar", "zone": "zone0"},
		},
		{
			name: "aggretate with mutil prefix case 2",
			args: arg{
				nodes: []*corev1.Node{
					BuildNode("n0", map[string]string{"foo": "bar", "zone": "zone0"}),
					BuildNode("n1", map[string]string{"foo": "bar", "zone": "zone0"}),
					BuildNode("n3", map[string]string{"foo": "bar", "zone": "zone1"}),
				},
				threshold:     0.1,
				labelPrefixes: []string{"foo", "zone"},
			},
			want: map[string]string{"foo": "bar", "zone": "zone0"},
		},
		{
			name: "aggretate with mutil prefix case 3",
			args: arg{
				nodes: []*corev1.Node{
					BuildNode("n0", map[string]string{"foo/abc": "bar", "zone": "zone0"}),
					BuildNode("n1", map[string]string{"foo/def": "bar", "zone": "zone1"}),
					BuildNode("n3", map[string]string{"zone": "zone1"}),
				},
				threshold:     0.1,
				labelPrefixes: []string{"foo", "zone"},
			},
			want: map[string]string{"foo/abc": "bar", "foo/def": "bar", "zone": "zone1"},
		},
	}
	for _, tt := range tests {
		newMap := aggregateLimitedLabels(tt.args.nodes, tt.args.threshold, tt.args.labelPrefixes)
		if !reflect.DeepEqual(newMap, tt.want) {
			t.Errorf("error of aggregateLimitedLabels %s,\n want %v\n  got %v", tt.name, tt.want, newMap)
		}
	}
}
