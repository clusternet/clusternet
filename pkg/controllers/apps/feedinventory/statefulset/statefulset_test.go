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

package statefulset

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
)

func TestPluginParser(t *testing.T) {
	tests := []struct {
		name string

		rawData      []byte
		inputLimit   map[string]string
		inputRequest map[string]string

		replicas        int32
		requirements    appsapi.ReplicaRequirements
		replicaJsonPath string
	}{
		{
			name: "empty requirements",
			rawData: []byte(`{
  "apiVersion": "apps/v1",
  "kind": "StatefulSet",
  "metadata": {
    "name": "rss-site",
    "labels": {
      "app": "web"
    }
  },
  "spec": {
    "replicas": 2,
    "selector": {
      "matchLabels": {
        "app": "web"
      }
    },
    "template": {
      "metadata": {
        "labels": {
          "app": "web"
        }
      },
      "spec": {
        "containers": [
          {
            "name": "front-end",
            "image": "nginx"
          },
          {
            "name": "rss-reader",
            "image": "rss-php-nginx:v1"
          }
        ]
      }
    }
  }
}`),
			replicas: 2,
			requirements: appsapi.ReplicaRequirements{
				Resources: corev1.ResourceRequirements{
					Limits:   map[corev1.ResourceName]resource.Quantity{},
					Requests: map[corev1.ResourceName]resource.Quantity{},
				},
			},
			replicaJsonPath: "/spec/replicas",
		},

		{
			name: "multiple-qos-with-foo-resource",
			rawData: []byte(`{
  "apiVersion": "apps/v1",
  "kind": "StatefulSet",
  "metadata": {
    "name": "qos-demo",
    "labels": {
      "app": "qos-demo"
    }
  },
  "spec": {
    "replicas": 2,
    "selector": {
      "matchLabels": {
        "app": "qos-demo"
      }
    },
    "template": {
      "metadata": {
        "labels": {
          "app": "qos-demo"
        }
      },
      "spec": {
        "nodeSelector": {
          "disktype": "ssd"
        },
        "tolerations": [
          {
            "key": "example-key",
            "operator": "Exists",
            "effect": "NoSchedule"
          }
        ],
        "containers": [
          {
            "name": "best-effort",
            "image": "nginx"
          },
          {
            "name": "burstable-with-foo-resource",
            "image": "nginx",
            "resources": {
              "requests": {
                "memory": "128Mi",
                "foo": 2
              },
              "limits": {
                "memory": "256Mi",
                "foo": 3
              }
            }
          },
          {
            "name": "guaranteed",
            "image": "nginx",
            "resources": {
              "requests": {
                "memory": "128Mi",
                "cpu": "500m"
              },
              "limits": {
                "memory": "128Mi",
                "cpu": "500m"
              }
            }
          }
        ],
        "affinity": {
          "nodeAffinity": {
            "requiredDuringSchedulingIgnoredDuringExecution": {
              "nodeSelectorTerms": [
                {
                  "matchExpressions": [
                    {
                      "key": "kubernetes.io/os",
                      "operator": "In",
                      "values": [
                        "linux"
                      ]
                    }
                  ]
                }
              ]
            }
          }
        }
      }
    }
  }
}`),
			inputRequest: map[string]string{
				"cpu":    "500m",
				"memory": "256Mi",
				"foo":    "2",
			},
			inputLimit: map[string]string{
				"cpu":    "500m",
				"memory": "384Mi",
				"foo":    "3",
			},
			replicas: 2,
			requirements: appsapi.ReplicaRequirements{
				NodeSelector: map[string]string{
					"disktype": "ssd",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "example-key",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
				Affinity: &corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      "kubernetes.io/os",
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{"linux"},
										},
									},
								},
							},
						},
					},
				},
				Resources: corev1.ResourceRequirements{
					Limits:   map[corev1.ResourceName]resource.Quantity{},
					Requests: map[corev1.ResourceName]resource.Quantity{},
				},
			},
			replicaJsonPath: "/spec/replicas",
		},

		{
			name: "init-containers-small-quota",
			rawData: []byte(`{
  "apiVersion": "apps/v1",
  "kind": "StatefulSet",
  "metadata": {
    "name": "qos-demo",
    "labels": {
      "app": "qos-demo"
    }
  },
  "spec": {
    "replicas": 2,
    "selector": {
      "matchLabels": {
        "app": "qos-demo"
      }
    },
    "template": {
      "metadata": {
        "labels": {
          "app": "qos-demo"
        }
      },
      "spec": {
        "nodeSelector": {
          "disktype": "ssd"
        },
        "tolerations": [
          {
            "key": "example-key",
            "operator": "Exists",
            "effect": "NoSchedule"
          }
        ],
        "initContainers": [
          {
            "name": "init-container-1",
            "image": "image-test:tag1"
          },
          {
            "name": "init-container-2",
            "image": "image-test:tag2",
            "resources": {
              "requests": {
                "memory": "128Mi",
                "bar": 1
              },
              "limits": {
                "memory": "256Mi",
                "bar": 2
              }
            }
          }
        ],
        "containers": [
          {
            "name": "best-effort",
            "image": "nginx"
          },
          {
            "name": "burstable-with-foo-resource",
            "image": "nginx",
            "resources": {
              "requests": {
                "memory": "128Mi",
                "foo": 2
              },
              "limits": {
                "memory": "256Mi",
                "foo": 3
              }
            }
          },
          {
            "name": "guaranteed",
            "image": "nginx",
            "resources": {
              "requests": {
                "memory": "128Mi",
                "cpu": "500m"
              },
              "limits": {
                "memory": "128Mi",
                "cpu": "500m"
              }
            }
          }
        ],
        "affinity": {
          "nodeAffinity": {
            "requiredDuringSchedulingIgnoredDuringExecution": {
              "nodeSelectorTerms": [
                {
                  "matchExpressions": [
                    {
                      "key": "kubernetes.io/os",
                      "operator": "In",
                      "values": [
                        "linux"
                      ]
                    }
                  ]
                }
              ]
            }
          }
        }
      }
    }
  }
}`),
			inputRequest: map[string]string{
				"cpu":    "500m",
				"memory": "256Mi",
				"foo":    "2",
				"bar":    "1",
			},
			inputLimit: map[string]string{
				"cpu":    "500m",
				"memory": "384Mi",
				"foo":    "3",
				"bar":    "2",
			},
			replicas: 2,
			requirements: appsapi.ReplicaRequirements{
				NodeSelector: map[string]string{
					"disktype": "ssd",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "example-key",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
				Affinity: &corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      "kubernetes.io/os",
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{"linux"},
										},
									},
								},
							},
						},
					},
				},
				Resources: corev1.ResourceRequirements{
					Limits:   map[corev1.ResourceName]resource.Quantity{},
					Requests: map[corev1.ResourceName]resource.Quantity{},
				},
			},
			replicaJsonPath: "/spec/replicas",
		},

		{
			name: "init-containers-large-quota",
			rawData: []byte(`{
  "apiVersion": "apps/v1",
  "kind": "StatefulSet",
  "metadata": {
    "name": "qos-demo",
    "labels": {
      "app": "qos-demo"
    }
  },
  "spec": {
    "replicas": 2,
    "selector": {
      "matchLabels": {
        "app": "qos-demo"
      }
    },
    "template": {
      "metadata": {
        "labels": {
          "app": "qos-demo"
        }
      },
      "spec": {
        "nodeSelector": {
          "disktype": "ssd"
        },
        "tolerations": [
          {
            "key": "example-key",
            "operator": "Exists",
            "effect": "NoSchedule"
          }
        ],
        "initContainers": [
          {
            "name": "init-container-1",
            "image": "image-test:tag1"
          },
          {
            "name": "init-container-2",
            "image": "image-test:tag2",
            "resources": {
              "requests": {
                "memory": "512Mi",
                "foo": 3
              },
              "limits": {
                "memory": "512Mi",
                "foo": 4
              }
            }
          }
        ],
        "containers": [
          {
            "name": "best-effort",
            "image": "nginx"
          },
          {
            "name": "burstable-with-foo-resource",
            "image": "nginx",
            "resources": {
              "requests": {
                "memory": "128Mi",
                "foo": 2
              },
              "limits": {
                "memory": "256Mi",
                "foo": 3
              }
            }
          },
          {
            "name": "guaranteed",
            "image": "nginx",
            "resources": {
              "requests": {
                "memory": "128Mi",
                "cpu": "500m"
              },
              "limits": {
                "memory": "128Mi",
                "cpu": "500m"
              }
            }
          }
        ],
        "affinity": {
          "nodeAffinity": {
            "requiredDuringSchedulingIgnoredDuringExecution": {
              "nodeSelectorTerms": [
                {
                  "matchExpressions": [
                    {
                      "key": "kubernetes.io/os",
                      "operator": "In",
                      "values": [
                        "linux"
                      ]
                    }
                  ]
                }
              ]
            }
          }
        }
      }
    }
  }
}`),
			inputRequest: map[string]string{
				"cpu":    "500m",
				"memory": "512Mi",
				"foo":    "3",
			},
			inputLimit: map[string]string{
				"cpu":    "500m",
				"memory": "512Mi",
				"foo":    "4",
			},
			replicas: 2,
			requirements: appsapi.ReplicaRequirements{
				NodeSelector: map[string]string{
					"disktype": "ssd",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "example-key",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
				Affinity: &corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      "kubernetes.io/os",
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{"linux"},
										},
									},
								},
							},
						},
					},
				},
				Resources: corev1.ResourceRequirements{
					Limits:   map[corev1.ResourceName]resource.Quantity{},
					Requests: map[corev1.ResourceName]resource.Quantity{},
				},
			},
			replicaJsonPath: "/spec/replicas",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for resourceName, value := range tt.inputRequest {
				quantity, err := resource.ParseQuantity(value)
				if err != nil {
					t.Fatalf("failed to parse quantity %s: %v", value, err)
				}
				tt.requirements.Resources.Requests[corev1.ResourceName(resourceName)] = quantity
			}

			for resourceName, value := range tt.inputLimit {
				quantity, err := resource.ParseQuantity(value)
				if err != nil {
					t.Fatalf("failed to parse quantity %s: %v", value, err)
				}
				tt.requirements.Resources.Limits[corev1.ResourceName(resourceName)] = quantity
			}

			pl := &Plugin{
				name: tt.name,
			}
			replicas, requirements, replicaJsonPath, err := pl.Parser(tt.rawData)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if *replicas != tt.replicas {
				t.Errorf("Parser() replicas = %d, replicas %d", *replicas, tt.replicas)
			}

			if !reflect.DeepEqual(requirements, tt.requirements) {
				t.Errorf("Parser() requirements = %#v\n, requirements %#v", requirements, tt.requirements)
			}

			if replicaJsonPath != tt.replicaJsonPath {
				t.Errorf("Parser() replicaJsonPath = %s, replicaJsonPath %s", replicaJsonPath, tt.replicaJsonPath)
			}
		})
	}
}
