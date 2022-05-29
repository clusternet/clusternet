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
	"fmt"
	"math"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/validation"
)

// Resource is a collection of compute resource.
type Resource struct {
	MilliCPU         int64
	Memory           int64
	EphemeralStorage int64
	AllowedPodNumber int64
	// ScalarResources
	ScalarResources map[corev1.ResourceName]int64
}

// EmptyResource creates a empty resource object and returns.
func EmptyResource() *Resource {
	return &Resource{}
}

func NewResource(rl corev1.ResourceList) *Resource {
	r := &Resource{}
	for rName, rQuant := range rl {
		switch rName {
		case corev1.ResourceCPU:
			r.MilliCPU += rQuant.MilliValue()
		case corev1.ResourceMemory:
			r.Memory += rQuant.Value()
		case corev1.ResourcePods:
			r.AllowedPodNumber += rQuant.Value()
		case corev1.ResourceEphemeralStorage:
			r.EphemeralStorage += rQuant.Value()
		default:
			if IsScalarResourceName(rName) {
				r.AddScalar(rName, rQuant.Value())
			}
		}
	}
	return r
}

// Add is used to add two resources.
func (r *Resource) Add(rl corev1.ResourceList) {
	for rName, rQuant := range rl {
		switch rName {
		case corev1.ResourceCPU:
			r.MilliCPU += rQuant.MilliValue()
		case corev1.ResourceMemory:
			r.Memory += rQuant.Value()
		case corev1.ResourcePods:
			r.AllowedPodNumber += rQuant.Value()
		case corev1.ResourceEphemeralStorage:
			r.EphemeralStorage += rQuant.Value()
		default:
			if IsScalarResourceName(rName) {
				r.AddScalar(rName, rQuant.Value())
			}
		}
	}
}

// AddMulti is used to add resources with scale.
func (r *Resource) AddMulti(rl corev1.ResourceList, ratio int64) {
	for rName, rQuant := range rl {
		switch rName {
		case corev1.ResourceCPU:
			r.MilliCPU += rQuant.MilliValue() * ratio
		case corev1.ResourceMemory:
			r.Memory += rQuant.Value() * ratio
		case corev1.ResourcePods:
			r.AllowedPodNumber += rQuant.Value() * ratio
		case corev1.ResourceEphemeralStorage:
			r.EphemeralStorage += rQuant.Value() * ratio
		default:
			if IsScalarResourceName(rName) {
				r.AddScalar(rName, rQuant.Value()*ratio)
			}
		}
	}
}

// Multi is used to scale a resource.
func (r *Resource) Multi(ratio int64) {
	r.MilliCPU *= ratio
	r.Memory *= ratio
	r.EphemeralStorage *= ratio
	r.AllowedPodNumber *= ratio
	for rName, rQuant := range r.ScalarResources {
		r.ScalarResources[rName] = rQuant * ratio
	}
}

// Sub is used to subtract two resources.
func (r *Resource) Sub(rl corev1.ResourceList) error {
	for rName, rQuant := range rl {
		switch rName {
		case corev1.ResourceCPU:
			cpu := rQuant.MilliValue()
			if r.MilliCPU < cpu {
				return fmt.Errorf("cpu difference is less than 0, remain %d, got %d", r.MilliCPU, cpu)
			}
			r.MilliCPU -= cpu
		case corev1.ResourceMemory:
			mem := rQuant.Value()
			if r.Memory < mem {
				return fmt.Errorf("memory difference is less than 0, remain %d, got %d", r.Memory, mem)
			}
			r.Memory -= mem
		case corev1.ResourcePods:
			pods := rQuant.Value()
			if r.AllowedPodNumber < pods {
				return fmt.Errorf("allowed pod difference is less than 0, remain %d, got %d", r.AllowedPodNumber, pods)
			}
			r.AllowedPodNumber -= pods
		case corev1.ResourceEphemeralStorage:
			ephemeralStorage := rQuant.Value()
			if r.EphemeralStorage < ephemeralStorage {
				return fmt.Errorf("allowed storage number difference is less than 0, remain %d, got %d", r.EphemeralStorage, ephemeralStorage)
			}
			r.EphemeralStorage -= ephemeralStorage
		default:
			if IsScalarResourceName(rName) {
				rScalar, ok := r.ScalarResources[rName]
				scalar := rQuant.Value()
				if !ok && scalar > 0 {
					return fmt.Errorf("scalar resources %s does not exist, got %d", rName, scalar)
				}
				if rScalar < scalar {
					return fmt.Errorf("scalar resources %s difference is less than 0, remain %d, got %d", rName, rScalar, scalar)
				}
				r.ScalarResources[rName] = rScalar - scalar
			}
		}
	}
	return nil
}

// Clone is used to clone a resource type, which is a deep copy function.
func (r *Resource) Clone() *Resource {
	clone := &Resource{
		MilliCPU:         r.MilliCPU,
		Memory:           r.Memory,
		EphemeralStorage: r.EphemeralStorage,
		AllowedPodNumber: r.AllowedPodNumber,
	}

	if r.ScalarResources != nil {
		clone.ScalarResources = make(map[corev1.ResourceName]int64)
		for k, v := range r.ScalarResources {
			clone.ScalarResources[k] = v
		}
	}

	return clone
}

// SetMaxResource compares with ResourceList and takes max value for each Resource.
func (r *Resource) SetMaxResource(rl corev1.ResourceList) {
	for rName, rQuant := range rl {
		switch rName {
		case corev1.ResourceCPU:
			if cpu := rQuant.MilliValue(); cpu > r.MilliCPU {
				r.MilliCPU = cpu
			}
		case corev1.ResourceMemory:
			if mem := rQuant.Value(); mem > r.Memory {
				r.Memory = mem
			}
		case corev1.ResourceEphemeralStorage:
			if ephemeralStorage := rQuant.Value(); ephemeralStorage > r.EphemeralStorage {
				r.EphemeralStorage = ephemeralStorage
			}
		case corev1.ResourcePods:
			if pods := rQuant.Value(); pods > r.AllowedPodNumber {
				r.AllowedPodNumber = pods
			}
		default:
			if IsScalarResourceName(rName) {
				if value := rQuant.Value(); value > r.ScalarResources[rName] {
					r.SetScalar(rName, value)
				}
			}
		}
	}
}

func (r *Resource) MaxReplicaDivided(rl corev1.ResourceList) int64 {
	res := int64(math.MaxInt32)
	for rName, rQuant := range rl {
		switch rName {
		case corev1.ResourceCPU:
			if cpu := rQuant.MilliValue(); cpu > 0 {
				res = MinInt64(res, r.MilliCPU/cpu)
			}
		case corev1.ResourceMemory:
			if mem := rQuant.Value(); mem > 0 {
				res = MinInt64(res, r.Memory/mem)
			}
		case corev1.ResourceEphemeralStorage:
			if ephemeralStorage := rQuant.Value(); ephemeralStorage > 0 {
				res = MinInt64(res, r.EphemeralStorage/ephemeralStorage)
			}
		default:
			if IsScalarResourceName(rName) {
				rScalar := r.ScalarResources[rName]
				if scalar := rQuant.Value(); scalar > 0 {
					res = MinInt64(res, rScalar/scalar)
				}
			}
		}
	}
	res = MinInt64(res, r.AllowedPodNumber)
	return res
}

// AddScalar adds a resource by a scalar value of this resource.
func (r *Resource) AddScalar(name corev1.ResourceName, quantity int64) {
	r.SetScalar(name, r.ScalarResources[name]+quantity)
}

// SetScalar sets a resource by a scalar value of this resource.
func (r *Resource) SetScalar(name corev1.ResourceName, quantity int64) {
	// Lazily allocate scalar resource map.
	if r.ScalarResources == nil {
		r.ScalarResources = map[corev1.ResourceName]int64{}
	}
	r.ScalarResources[name] = quantity
}

// ResourceList returns a resource list of this resource.
func (r *Resource) ResourceList() corev1.ResourceList {
	result := corev1.ResourceList{
		corev1.ResourceCPU:              *resource.NewMilliQuantity(r.MilliCPU, resource.DecimalSI),
		corev1.ResourceMemory:           *resource.NewQuantity(r.Memory, resource.BinarySI),
		corev1.ResourceEphemeralStorage: *resource.NewQuantity(r.EphemeralStorage, resource.BinarySI),
		corev1.ResourcePods:             *resource.NewQuantity(r.AllowedPodNumber, resource.DecimalSI),
	}
	for rName, rQuant := range r.ScalarResources {
		if IsHugePageResourceName(rName) {
			result[rName] = *resource.NewQuantity(rQuant, resource.BinarySI)
		} else {
			result[rName] = *resource.NewQuantity(rQuant, resource.DecimalSI)
		}
	}
	return result
}

func (r *Resource) Less(rr *Resource) bool {
	moreFunc := func(l, r int64) bool {
		return l > r
	}

	if moreFunc(r.MilliCPU, rr.MilliCPU) {
		return false
	}
	if moreFunc(r.Memory, rr.Memory) {
		return false
	}
	if moreFunc(r.EphemeralStorage, rr.EphemeralStorage) {
		return false
	}
	if moreFunc(r.AllowedPodNumber, rr.AllowedPodNumber) {
		return false
	}
	for rrName, rrQuant := range rr.ScalarResources {
		rQuant, _ := r.ScalarResources[rrName]
		if moreFunc(rQuant, rrQuant) {
			return false
		}
	}
	return !reflect.DeepEqual(r, rr)
}

// AddPodRequest add the effective request resource of a pod to the origin resource.
// The Pod's effective request is the higher of:
// - the sum of all app containers(spec.Containers) request for a resource.
// - the effective init containers(spec.InitContainers) request for a resource.
// The effective init containers request is the highest request on all init containers.
func (r *Resource) AddPodRequest(podSpec *corev1.PodSpec) *Resource {
	for _, container := range podSpec.Containers {
		r.Add(container.Resources.Requests)
	}
	for _, container := range podSpec.InitContainers {
		r.SetMaxResource(container.Resources.Requests)
	}
	// If Overhead is being utilized, add to the total requests for the pod.
	// We assume the EnablePodOverhead feature gate of member cluster is set (it is on by default since 1.18).
	if podSpec.Overhead != nil {
		r.Add(podSpec.Overhead)
	}
	return r
}

func (r *Resource) AddResourcePods(pods int64) {
	r.Add(corev1.ResourceList{
		corev1.ResourcePods: *resource.NewQuantity(pods, resource.DecimalSI),
	})
}

// String returns resource details in string format
func (r *Resource) String() string {
	str := fmt.Sprintf("cpu %d, memory %d, ephemeral-storage %d, pods %d", r.MilliCPU, r.Memory, r.EphemeralStorage, r.AllowedPodNumber)
	for rName, rQuant := range r.ScalarResources {
		str = fmt.Sprintf("%s, %s %d", str, rName, rQuant)
	}
	return str
}

func MinInt64(a, b int64) int64 {
	if a <= b {
		return a
	}
	return b
}

// IsScalarResourceName validates the resource for Extended, Hugepages, Native and AttachableVolume resources
func IsScalarResourceName(name corev1.ResourceName) bool {
	return IsExtendedResourceName(name) || IsHugePageResourceName(name) ||
		IsPrefixedNativeResource(name) || IsAttachableVolumeResourceName(name)
}

// IsExtendedResourceName returns true if:
// 1. the resource name is not in the default namespace;
// 2. resource name does not have "requests." prefix,
// to avoid confusion with the convention in quota
// 3. it satisfies the rules in IsQualifiedName() after converted into quota resource name
func IsExtendedResourceName(name corev1.ResourceName) bool {
	if IsNativeResource(name) || strings.HasPrefix(string(name), corev1.DefaultResourceRequestsPrefix) {
		return false
	}
	// Ensure it satisfies the rules in IsQualifiedName() after converted into quota resource name
	nameForQuota := fmt.Sprintf("%s%s", corev1.DefaultResourceRequestsPrefix, string(name))
	if errs := validation.IsQualifiedName(nameForQuota); len(errs) != 0 {
		return false
	}
	return true
}

// IsPrefixedNativeResource returns true if the resource name is in the
// *kubernetes.io/ namespace.
func IsPrefixedNativeResource(name corev1.ResourceName) bool {
	return strings.Contains(string(name), corev1.ResourceDefaultNamespacePrefix)
}

// IsNativeResource returns true if the resource name is in the
// *kubernetes.io/ namespace. Partially-qualified (unprefixed) names are
// implicitly in the kubernetes.io/ namespace.
func IsNativeResource(name corev1.ResourceName) bool {
	return !strings.Contains(string(name), "/") ||
		IsPrefixedNativeResource(name)
}

// IsAttachableVolumeResourceName returns true when the resource name is prefixed in attachable volume
func IsAttachableVolumeResourceName(name corev1.ResourceName) bool {
	return strings.HasPrefix(string(name), corev1.ResourceAttachableVolumesPrefix)
}

// IsHugePageResourceName returns true if the resource name has the huge page
// resource prefix.
func IsHugePageResourceName(name corev1.ResourceName) bool {
	return strings.HasPrefix(string(name), corev1.ResourceHugePagesPrefix)
}
