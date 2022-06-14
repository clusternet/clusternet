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

package scheme

import (
	schedulerconfig "github.com/clusternet/clusternet/pkg/scheduler/apis/config"
	schedulerconfigv1alpha1 "github.com/clusternet/clusternet/pkg/scheduler/apis/config/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
)

var (
	// Scheme is the runtime.Scheme to which all scheduler api types are registered.
	Scheme = runtime.NewScheme()

	// Codecs provides access to encoding and decoding for the scheme.
	Codecs = serializer.NewCodecFactory(Scheme, serializer.EnableStrict)
)

func init() {
	AddToScheme(Scheme)
}

// AddToScheme builds the scheduler scheme using all known versions of the scheduler api.
func AddToScheme(scheme *runtime.Scheme) {
	utilruntime.Must(schedulerconfig.AddToScheme(scheme))
	utilruntime.Must(schedulerconfigv1alpha1.AddToScheme(scheme))
	utilruntime.Must(scheme.SetVersionPriority(schedulerconfigv1alpha1.SchemeGroupVersion))
}
