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

package api

import (
	"fmt"
	"strconv"
	"strings"

	imageutils "github.com/clusternet/clusternet/pkg/kyverno/engine/utils/image"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type ImageInfo struct {
	imageutils.ImageInfo
	// Pointer is the path to the image object in the resource
	Pointer string `json:"-"`
}

var (
	podExtractors               = BuildStandardExtractors("spec")
	podControllerExtractors     = BuildStandardExtractors("spec", "template", "spec")
	cronjobControllerExtractors = BuildStandardExtractors("spec", "jobTemplate", "spec", "template", "spec")
	registeredExtractors        = map[string][]imageExtractor{
		"Pod":                   podExtractors,
		"DaemonSet":             podControllerExtractors,
		"Deployment":            podControllerExtractors,
		"ReplicaSet":            podControllerExtractors,
		"ReplicationController": podControllerExtractors,
		"StatefulSet":           podControllerExtractors,
		"CronJob":               cronjobControllerExtractors,
		"Job":                   podControllerExtractors,
	}
)

type imageExtractor struct {
	Fields []string
	Key    string
	Value  string
	Name   string
}

func (i *imageExtractor) ExtractFromResource(resource interface{}) (map[string]ImageInfo, error) {
	imageInfo := map[string]ImageInfo{}
	if err := extract(resource, []string{}, i.Key, i.Value, i.Fields, &imageInfo); err != nil {
		return nil, err
	}
	return imageInfo, nil
}

func extract(obj interface{}, path []string, keyPath, valuePath string, fields []string, imageInfos *map[string]ImageInfo) error {
	if obj == nil {
		return nil
	}
	if len(fields) > 0 && fields[0] == "*" {
		switch typedObj := obj.(type) {
		case []interface{}:
			for i, v := range typedObj {
				if err := extract(v, append(path, strconv.Itoa(i)), keyPath, valuePath, fields[1:], imageInfos); err != nil {
					return err
				}
			}
		case map[string]interface{}:
			for i, v := range typedObj {
				if err := extract(v, append(path, i), keyPath, valuePath, fields[1:], imageInfos); err != nil {
					return err
				}
			}
		case interface{}:
			return fmt.Errorf("invalid type")
		}
		return nil
	}

	output, ok := obj.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid image config")
	}

	if len(fields) == 0 {
		pointer := fmt.Sprintf("/%s/%s", strings.Join(path, "/"), valuePath)
		key := pointer
		if keyPath != "" {
			key, ok = output[keyPath].(string)
			if !ok {
				return fmt.Errorf("invalid key")
			}
		}
		value, ok := output[valuePath].(string)
		if !ok {
			return fmt.Errorf("invalid value")
		}
		if imageInfo, err := imageutils.GetImageInfo(value); err != nil {
			return fmt.Errorf("invalid image %s", value)
		} else {
			(*imageInfos)[key] = ImageInfo{*imageInfo, pointer}
		}
		return nil
	}

	currentPath := fields[0]
	return extract(output[currentPath], append(path, currentPath), keyPath, valuePath, fields[1:], imageInfos)
}

func BuildStandardExtractors(tags ...string) []imageExtractor {
	var extractors []imageExtractor
	for _, tag := range []string{"initContainers", "containers", "ephemeralContainers"} {
		var t []string
		t = append(t, tags...)
		t = append(t, tag)
		t = append(t, "*")
		extractors = append(extractors, imageExtractor{Fields: t, Key: "name", Value: "image", Name: tag})
	}
	return extractors
}

func lookupImageExtractor(kind string) []imageExtractor {
	return registeredExtractors[kind]
}

func ExtractImagesFromResource(resource unstructured.Unstructured) (map[string]map[string]ImageInfo, error) {
	infos := map[string]map[string]ImageInfo{}

	extractors := lookupImageExtractor(resource.GetKind())
	if extractors != nil && len(extractors) == 0 {
		return nil, fmt.Errorf("no extractors found for %s", resource.GetKind())
	}

	for _, extractor := range extractors {
		if infoMap, err := extractor.ExtractFromResource(resource.Object); err != nil {
			return nil, err
		} else if len(infoMap) > 0 {
			infos[extractor.Name] = infoMap
		}
	}

	return infos, nil
}
