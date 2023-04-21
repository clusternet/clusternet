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

package discovery

import (
	"bytes"
	"fmt"
	"text/template"
)

const (
	// ClusternetAgentDeployment is the clusternet-agent Deployment manifest
	ClusternetAgentDeployment = `
---
apiVersion: v1
kind: Namespace
metadata:
  name: {{ .Namespace }}

---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: clusternet-agent
  namespace: {{ .Namespace }}

---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: clusternet-app-deployer
  namespace: {{ .Namespace }}

---
apiVersion: v1
kind: Secret
metadata:
  name: clusternet-agent-cluster-registration
  namespace: {{ .Namespace }}
type: Opaque
stringData:
  parentURL: {{ .ParentURL }}
  regToken: {{ .Token }}

---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: clusternet-agent
  namespace: {{ .Namespace }}
  labels:
    app: clusternet-agent
spec:
  replicas: 3
  selector:
    matchLabels:
      app: clusternet-agent
  template:
    metadata:
      labels:
        app: clusternet-agent
    spec:
      serviceAccountName: clusternet-agent
      tolerations:
        - key: node-role.kubernetes.io/master
          operator: Exists
      containers:
        - name: clusternet-agent
          image: {{ .Image }}
          imagePullPolicy: IfNotPresent
          env:
            - name: PARENT_URL
              valueFrom:
                secretKeyRef:
                  name: clusternet-agent-cluster-registration
                  key: parentURL
            - name: REG_TOKEN
              valueFrom:
                secretKeyRef:
                  name: clusternet-agent-cluster-registration
                  key: regToken
            - name: AGENT_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
          command:
            - /usr/local/bin/clusternet-agent
            - --cluster-reg-token=$(REG_TOKEN)
            - --cluster-reg-parent-url=$(PARENT_URL)
            - --cluster-sync-mode=Dual
            - --use-metrics-server=false
            - --feature-gates=SocketConnection=true,AppPusher=true,Recovery=true,Predictor=true
            - --leader-elect-resource-namespace=$(AGENT_NAMESPACE)
            - --predictor-direct-access=false
            - --predictor-port=8080
`

	// ClusternetAgentRBACRules is the clusternet-agent RBAC rules manifest
	ClusternetAgentRBACRules = `
---
kind: Role
apiVersion: rbac.authorization.k8s.io/v1
metadata:
  name: clusternet:agent:admin
  namespace: {{ .Namespace }}
rules:
  - apiGroups: [ "*" ]
    resources: [ "*" ]
    verbs: [ "*" ]

---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: clusternet:agent:admin
  namespace: {{ .Namespace }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: clusternet:agent:admin
subjects:
  - kind: ServiceAccount
    name: clusternet-agent
    namespace: {{ .Namespace }}

---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: clusternet:agent:admin
rules:
  - apiGroups: [ "" ]
    resources: [ "pods", "nodes" ]
    verbs: [ "get", "list", "watch" ]
  - apiGroups: ["metrics.k8s.io"]
    resources: ["pods", "nodes"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["discovery.k8s.io"]
    resources: [ "endpointslices" ]
    verbs: [ "get", "list", "watch" ]
  - apiGroups: [ "multicluster.x-k8s.io" ]
    resources: [ "serviceexports", "serviceimports" ]
    verbs: [ "get", "list", "watch" ]
  - nonResourceURLs: [ "*" ]
    verbs: [ "*" ]

---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: clusternet:agent:admin
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: clusternet:agent:admin
subjects:
  - kind: ServiceAccount
    name: clusternet-agent
    namespace: {{ .Namespace }}

---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: clusternet:app:deployer
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
  - kind: ServiceAccount
    name: clusternet-app-deployer
    namespace: {{ .Namespace }}
`
)

func parseManifest(tmplName, tmplStr string, obj interface{}) (string, error) {
	var buf bytes.Buffer
	tmpl, err := template.New(tmplName).Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template %s: %v", tmplName, err)
	}
	err = tmpl.Execute(&buf, obj)
	if err != nil {
		return "", fmt.Errorf("failed to render template %s: %v", tmplName, err)
	}
	return buf.String(), nil
}
