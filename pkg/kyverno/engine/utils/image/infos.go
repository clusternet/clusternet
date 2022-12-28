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

package image

import (
	"strings"

	"github.com/distribution/distribution/reference"
	"github.com/pkg/errors"
)

type ImageInfo struct {
	// Registry is the URL address of the image registry e.g. `docker.io`
	Registry string `json:"registry,omitempty"`

	// Name is the image name portion e.g. `busybox`
	Name string `json:"name"`

	// Path is the repository path and image name e.g. `some-repository/busybox`
	Path string `json:"path"`

	// Tag is the image tag e.g. `v2`
	Tag string `json:"tag,omitempty"`

	// Digest is the image digest portion e.g. `sha256:128c6e3534b842a2eec139999b8ce8aa9a2af9907e2b9269550809d18cd832a3`
	Digest string `json:"digest,omitempty"`
}

func (i *ImageInfo) String() string {
	image := i.Registry + "/" + i.Path
	if i.Digest != "" {
		return image + "@" + i.Digest
	} else {
		return image + ":" + i.Tag
	}
}

func (i *ImageInfo) ReferenceWithTag() string {
	return i.Registry + "/" + i.Path + ":" + i.Tag
}

func GetImageInfo(image string) (*ImageInfo, error) {
	image = addDefaultDomain(image)
	ref, err := reference.Parse(image)
	if err != nil {
		return nil, errors.Wrapf(err, "bad image: %s", image)
	}
	var registry, path, name, tag, digest string
	if named, ok := ref.(reference.Named); ok {
		registry = reference.Domain(named)
		path = reference.Path(named)
		name = path[strings.LastIndex(path, "/")+1:]
	}
	if tagged, ok := ref.(reference.Tagged); ok {
		tag = tagged.Tag()
	}
	if digested, ok := ref.(reference.Digested); ok {
		digest = digested.Digest().String()
	}
	// set default tag - the domain is set via addDefaultDomain before parsing
	if digest == "" && tag == "" {
		tag = "latest"
	}
	return &ImageInfo{
		Registry: registry,
		Name:     name,
		Path:     path,
		Tag:      tag,
		Digest:   digest,
	}, nil
}

func addDefaultDomain(name string) string {
	i := strings.IndexRune(name, '/')
	if i == -1 || (!strings.ContainsAny(name[:i], ".:") && name[:i] != "localhost" && strings.ToLower(name[:i]) == name[:i]) {
		return "docker.io/" + name
	}
	return name
}
