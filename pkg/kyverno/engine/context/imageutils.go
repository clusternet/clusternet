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

package context

import (
	"fmt"

	engineutils "github.com/clusternet/clusternet/pkg/kyverno/engine/utils"
)

// MutateResourceWithImageInfo will set images to their canonical form so that they can be compared
// in a predictable manner. This sets the default registry as `docker.io` and the tag as `latest` if
// these are missing.
func MutateResourceWithImageInfo(raw []byte, ctx Interface) error {
	images := ctx.ImageInfo()
	if images == nil {
		return nil
	}
	var patches [][]byte
	buildJSONPatch := func(op, path, value string) []byte {
		p := fmt.Sprintf(`{ "op": "%s", "path": "%s", "value":"%s" }`, op, path, value)
		return []byte(p)
	}
	for _, infoMaps := range images {
		for _, info := range infoMaps {
			patches = append(patches, buildJSONPatch("replace", info.Pointer, info.String()))
		}
	}
	patchedResource, err := engineutils.ApplyPatches(raw, patches)
	if err != nil {
		return err
	}
	return AddResource(ctx, patchedResource)
}
