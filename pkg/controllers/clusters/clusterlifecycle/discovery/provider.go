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
	"context"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/klog/v2"
	utilpointer "k8s.io/utils/pointer"

	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

const (
	clusterAPISecret = "clusternet-cluster-api"
)

type RegistrationConfig struct {
	Image             []byte `json:"image,omitempty"`
	ParentURL         []byte `json:"parentURL,omitempty"`
	RegistrationToken []byte `json:"regToken,omitempty"`
}

func fetchRegistrationConfig(ctx context.Context, secretInterface corev1.SecretInterface) (*RegistrationConfig, error) {
	getCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var secret *v1.Secret
	var err error
	wait.JitterUntilWithContext(getCtx, func(ctx context.Context) {
		secret, err = secretInterface.Get(ctx, clusterAPISecret, metav1.GetOptions{})
		if err == nil {
			// success on getting the secret
			cancel()
			return
		}
		klog.ErrorDepth(4,
			fmt.Sprintf("failed to get secret %s in cluster-api management cluster: %v", clusterAPISecret, err))
	}, known.DefaultRetryPeriod, 0.3, true)

	return getRegistrationConfigFromSecret(secret)
}

// getRegistrationConfigFromSecret parses RegistrationConfig from secret data
func getRegistrationConfigFromSecret(secret *v1.Secret) (*RegistrationConfig, error) {
	data, err := utils.Marshal(secret.Data)
	if err != nil {
		return nil, err
	}

	regConfig := &RegistrationConfig{}
	err = utils.Unmarshal(data, regConfig)
	if err != nil {
		return nil, err
	}

	return regConfig, nil
}

func getClusterMetadata(obj *unstructured.Unstructured) *ClusterMetadata {
	return &ClusterMetadata{
		Name:              obj.GetName(),
		Namespace:         obj.GetNamespace(),
		UID:               obj.GetUID(),
		CreationTimestamp: obj.GetCreationTimestamp(),
		DeletionTimestamp: obj.GetDeletionTimestamp(),
		Labels:            obj.GetLabels(),
		Annotations:       obj.GetAnnotations(),
		FailureMessage:    utilpointer.String(getFieldString(obj, "status", "failureMessage")),
		Phase:             getFieldString(obj, "status", "phase"),
	}
}

func getFieldString(u *unstructured.Unstructured, fields ...string) string {
	val, found, err := unstructured.NestedString(u.Object, fields...)
	if !found || err != nil {
		klog.WarningDepth(3, fmt.Sprintf("failed to find value of %s from cluster %s/%s", strings.Join(fields, "."), u.GetNamespace(), u.GetName()))
		return ""
	}
	return val
}

type ClusterMetadata struct {
	Name      string
	Namespace string

	UID types.UID

	CreationTimestamp metav1.Time
	DeletionTimestamp *metav1.Time

	Labels      map[string]string
	Annotations map[string]string

	// FailureMessage indicates that there is a fatal problem reconciling the
	// state, and will be set to a descriptive error message.
	FailureMessage *string

	// Phase represents the current phase of cluster actuation.
	Phase string
}
