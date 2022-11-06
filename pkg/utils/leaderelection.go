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

package utils

import (
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

func GenerateIdentity() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", err
	}

	// add a uniquifier so that two processes on the same host don't accidentally both become active
	identity := hostname + "_" + string(uuid.NewUUID())
	return identity, nil
}

func NewLeaderElectionConfigWithDefaultValue(identity, leaseName, leaseNamespace string,
	leaseDuration time.Duration, renewDeadline time.Duration, retryPeriod time.Duration,
	clientset kubernetes.Interface, callbacks leaderelection.LeaderCallbacks) *leaderelection.LeaderElectionConfig {
	return &leaderelection.LeaderElectionConfig{
		Lock: &resourcelock.LeaseLock{
			LeaseMeta: metav1.ObjectMeta{
				Name:      leaseName,
				Namespace: leaseNamespace,
			},
			Client: clientset.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{
				Identity: identity,
			},
		},
		// IMPORTANT: you MUST ensure that any code you have that
		// is protected by the lease must terminate **before**
		// you call cancel. Otherwise, you could have a background
		// loop still running and another process could
		// get elected before your background loop finished, violating
		// the stated goal of the lease.
		ReleaseOnCancel: true,
		LeaseDuration:   leaseDuration,
		RenewDeadline:   renewDeadline,
		RetryPeriod:     retryPeriod,
		Callbacks:       callbacks,
	}
}
