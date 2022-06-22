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

package hub

import (
	"fmt"
	"reflect"
	"testing"

	coordinationapi "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilpointer "k8s.io/utils/pointer"
)

func TestGetPeerInfo(t *testing.T) {
	tests := []struct {
		name  string
		lease coordinationapi.Lease
		pi    peerInfo
	}{
		{
			lease: coordinationapi.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-lease",
					Namespace: "some-namespace",
				},
			},
			pi: peerInfo{},
		},
		{
			lease: coordinationapi.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-dw7c7", peerLeasePrefix),
					Namespace: "some-namespace",
				},
				Spec: coordinationapi.LeaseSpec{
					HolderIdentity:       nil,
					LeaseDurationSeconds: nil,
					AcquireTime:          nil,
					RenewTime:            nil,
					LeaseTransitions:     nil,
				},
			},
			pi: peerInfo{ID: "dw7c7"},
		},
		{
			lease: coordinationapi.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-dw7c7", peerLeasePrefix),
					Namespace: "some-namespace",
				},
				Spec: coordinationapi.LeaseSpec{
					HolderIdentity:       utilpointer.StringPtr(`{"identity":"some-where","id":"dw7c7","host":"192.168.10.4","port":8124,"token":"Cheugy"}`),
					LeaseDurationSeconds: nil,
					AcquireTime:          nil,
					RenewTime:            nil,
					LeaseTransitions:     nil,
				},
			},
			pi: peerInfo{
				ID:       "dw7c7",
				Identity: "some-where",
				Host:     "192.168.10.4",
				Port:     8124,
				Token:    "Cheugy",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pi, err := getPeerInfo(&tt.lease)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var gotPeerInfo peerInfo
			if pi == nil {
				gotPeerInfo = peerInfo{}
			} else {
				gotPeerInfo = *pi
			}

			if !reflect.DeepEqual(gotPeerInfo, tt.pi) {
				t.Errorf("got %#v, but want %#v", gotPeerInfo, tt.pi)
			}
		})
	}
}
