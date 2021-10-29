/*
Copyright 2021 The Clusternet Authors.

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

package template

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestTrimCoreService(t *testing.T) {
	tests := []struct {
		name          string
		resultRaw     *unstructured.Unstructured
		resultDesired *unstructured.Unstructured
	}{
		{
			name: "ServiceType NodePort",
			resultRaw: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Service",
					"metadata": map[string]interface{}{
						"creationTimestamp": "2021-11-01T08:12:43Z",
						"labels": map[string]string{
							"clusternet.io/created-by": "clusternet-hub",
						},
						"name":            "my-test-nodeport-svc",
						"namespace":       "nginx-test",
						"resourceVersion": "4457294",
						"uid":             "28f5ae38-9eea-431c-918b-68ffdf263c24",
					},
					"spec": map[string]interface{}{
						"clusterIP": "10.98.177.115",
						"clusterIPs": []string{
							"10.98.177.115",
						},
						"externalTrafficPolicy": "Cluster",
						"ipFamilies": []string{
							"IPv4",
						},
						"ipFamilyPolicy": "SingleStack",
						"ports": []interface{}{
							map[string]interface{}{
								"name":       "tcp-80-80",
								"nodePort":   int64(30597),
								"port":       int64(80),
								"protocol":   "TCP",
								"targetPort": int64(80),
							},
							map[string]interface{}{
								"name":       "tcp-443-443",
								"nodePort":   int64(30598),
								"port":       int64(443),
								"protocol":   "TCP",
								"targetPort": int64(443),
							},
						},
						"sessionAffinity": "None",
						"type":            "NodePort",
					},
					"status": map[string]interface{}{},
				},
			},
			resultDesired: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Service",
					"metadata": map[string]interface{}{
						"labels": map[string]string{
							"clusternet.io/created-by": "clusternet-hub",
						},
						"name":      "my-test-nodeport-svc",
						"namespace": "nginx-test",
					},
					"spec": map[string]interface{}{
						"clusterIP": "10.98.177.115",
						"clusterIPs": []string{
							"10.98.177.115",
						},
						"externalTrafficPolicy": "Cluster",
						"ipFamilies": []string{
							"IPv4",
						},
						"ipFamilyPolicy": "SingleStack",
						"ports": []interface{}{
							map[string]interface{}{
								"name":       "tcp-80-80",
								"port":       int64(80),
								"protocol":   "TCP",
								"targetPort": int64(80),
							},
							map[string]interface{}{
								"name":       "tcp-443-443",
								"port":       int64(443),
								"protocol":   "TCP",
								"targetPort": int64(443),
							},
						},
						"sessionAffinity": "None",
						"type":            "NodePort",
					},
					"status": map[string]interface{}{},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trimCommonMetadata(tt.resultRaw)
			trimCoreService(tt.resultRaw)
			if !reflect.DeepEqual(tt.resultRaw, tt.resultDesired) {
				t.Errorf("got: %v\n, but want: %v\n", tt.resultRaw.UnstructuredContent(), tt.resultDesired.UnstructuredContent())
			}
		})
	}
}

func TestTrimBatchJob(t *testing.T) {
	tests := []struct {
		name          string
		resultRaw     *unstructured.Unstructured
		resultDesired *unstructured.Unstructured
	}{
		{
			name: "trim batch job",
			resultRaw: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "batch/v1",
					"kind":       "Job",
					"metadata": map[string]interface{}{
						"creationTimestamp": "2021-11-01T07:01:18Z",
						"generation":        1,
						"labels": map[string]string{
							"clusternet.io/created-by": "clusternet-hub",
						},
						"name":            "pi",
						"namespace":       "default",
						"resourceVersion": "4450181",
						"uid":             "dc8ca8aa-268c-4634-b1f7-6bf67e3af6cb",
					},
					"spec": map[string]interface{}{
						"backoffLimit": 4,
						"completions":  1,
						"parallelism":  1,
						"selector": map[string]interface{}{
							"matchLabels": map[string]interface{}{
								"controller-uid": "a2d92553-a472-4ab5-bf4b-81b654f3db39",
							},
						},
						"template": map[string]interface{}{
							"metadata": map[string]interface{}{
								"creationTimestamp": nil,
								"labels": map[string]interface{}{
									"controller-uid": "a2d92553-a472-4ab5-bf4b-81b654f3db39",
									"job-name":       "pi",
								},
							},
							"spec": map[string]interface{}{
								"containers": []map[string]interface{}{
									{
										"command": []string{
											"perl",
											"-Mbignum=bpi",
											"-wle",
											"print bpi(2000)",
										},
										"image":                    "perl",
										"imagePullPolicy":          "Always",
										"name":                     "pi",
										"terminationMessagePath":   "/dev/termination-log",
										"terminationMessagePolicy": "File",
									},
								},
								"dnsPolicy":                     "ClusterFirst",
								"restartPolicy":                 "Never",
								"schedulerName":                 "default-scheduler",
								"terminationGracePeriodSeconds": 30,
							},
						},
					},
					"status": map[string]interface{}{},
				},
			},
			resultDesired: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "batch/v1",
					"kind":       "Job",
					"metadata": map[string]interface{}{
						"generation": 1,
						"labels": map[string]string{
							"clusternet.io/created-by": "clusternet-hub",
						},
						"name":      "pi",
						"namespace": "default",
					},
					"spec": map[string]interface{}{
						"backoffLimit": 4,
						"completions":  1,
						"parallelism":  1,
						"selector": map[string]interface{}{
							"matchLabels": map[string]interface{}{},
						},
						"template": map[string]interface{}{
							"metadata": map[string]interface{}{
								"labels": map[string]interface{}{
									"job-name": "pi",
								},
							},
							"spec": map[string]interface{}{
								"containers": []map[string]interface{}{
									{
										"command": []string{
											"perl",
											"-Mbignum=bpi",
											"-wle",
											"print bpi(2000)",
										},
										"image":                    "perl",
										"imagePullPolicy":          "Always",
										"name":                     "pi",
										"terminationMessagePath":   "/dev/termination-log",
										"terminationMessagePolicy": "File",
									},
								},
								"dnsPolicy":                     "ClusterFirst",
								"restartPolicy":                 "Never",
								"schedulerName":                 "default-scheduler",
								"terminationGracePeriodSeconds": 30,
							},
						},
					},
					"status": map[string]interface{}{},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trimCommonMetadata(tt.resultRaw)
			trimBatchJob(tt.resultRaw)
			if !reflect.DeepEqual(tt.resultRaw, tt.resultDesired) {
				t.Errorf("got: %v\n, but want: %v\n", tt.resultRaw.UnstructuredContent(), tt.resultDesired.UnstructuredContent())
			}
		})
	}
}
