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
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
)

func TestGetProviderConfigFromSecret(t *testing.T) {
	tests := []struct {
		name    string
		secret  *v1.Secret
		want    RegistrationConfig
		wantErr bool
	}{
		{
			secret: &v1.Secret{
				Data: map[string][]byte{
					"parentURL": []byte("http://a-url"),
					"regToken":  []byte("some-token"),
					"image":     []byte("myimage:tag"),
				},
			},
			want: RegistrationConfig{
				Image:             []byte("myimage:tag"),
				ParentURL:         []byte("http://a-url"),
				RegistrationToken: []byte("some-token"),
			},
		},
		{
			secret: &v1.Secret{
				Data: map[string][]byte{},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getRegistrationConfigFromSecret(tt.secret)
			if (err != nil) != tt.wantErr {
				t.Errorf("getRegistrationConfigFromSecret() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if diff := cmp.Diff(*got, tt.want); diff != "" {
				t.Errorf("Unexpected result (-want, +got):\n%s", diff)
			}
		})
	}
}
