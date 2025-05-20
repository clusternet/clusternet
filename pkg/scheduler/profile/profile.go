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

// This file was copied from k8s.io/kubernetes/pkg/scheduler/profile/profile.go

// Package profile holds the definition of a scheduling Profile.
package profile

import (
	"errors"
	"fmt"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"

	"github.com/clusternet/clusternet/pkg/scheduler/apis"
	framework "github.com/clusternet/clusternet/pkg/scheduler/framework/interfaces"
	frameworkruntime "github.com/clusternet/clusternet/pkg/scheduler/framework/runtime"
)

// RecorderFactory builds an EventRecorder for a given scheduler name.
type RecorderFactory func(string) events.EventRecorder

// newProfile builds a Profile for the given configuration.
func newProfile(cfg apis.SchedulerProfile, r frameworkruntime.Registry,
	opts ...frameworkruntime.Option) (framework.Framework, error) {
	fwk, err := frameworkruntime.NewFramework(r, &cfg, opts...)
	if err != nil {
		return nil, err
	}
	return fwk, nil
}

// Map holds frameworks indexed by scheduler name.
type Map map[string]framework.Framework

// NewMap builds the frameworks given by the configuration, indexed by name.
func NewMap(cfgs []apis.SchedulerProfile, r frameworkruntime.Registry,
	opts ...frameworkruntime.Option) (Map, error) {
	m := make(Map)
	v := cfgValidator{m: m}

	for _, cfg := range cfgs {
		p, err := newProfile(cfg, r, opts...)
		if err != nil {
			return nil, fmt.Errorf("creating profile for scheduler name %s: %v", cfg.SchedulerName, err)
		}
		if err = v.validate(cfg, p); err != nil {
			return nil, err
		}
		m[cfg.SchedulerName] = p
	}
	return m, nil
}

// HandlesSchedulerName returns whether a profile handles the given scheduler name.
func (m Map) HandlesSchedulerName(name string) bool {
	_, ok := m[name]
	return ok
}

// NewRecorderFactory returns a RecorderFactory for the broadcaster.
func NewRecorderFactory(b events.EventBroadcaster) RecorderFactory {
	return func(name string) events.EventRecorder {
		return b.NewRecorder(scheme.Scheme, name)
	}
}

type cfgValidator struct {
	m Map
}

func (v *cfgValidator) validate(cfg apis.SchedulerProfile, f framework.Framework) error {
	if len(f.ProfileName()) == 0 {
		return errors.New("scheduler name is needed")
	}
	if cfg.Plugins == nil {
		return fmt.Errorf("plugins required for profile with scheduler name %q", f.ProfileName())
	}
	if v.m[f.ProfileName()] != nil {
		return fmt.Errorf("duplicate profile with scheduler name %q", f.ProfileName())
	}

	return nil
}
