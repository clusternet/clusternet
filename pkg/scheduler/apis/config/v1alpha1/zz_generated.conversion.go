//go:build !ignore_autogenerated
// +build !ignore_autogenerated

/*
Copyright The Clusternet Authors.

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
// Code generated by conversion-gen. DO NOT EDIT.

package v1alpha1

import (
	unsafe "unsafe"

	config "github.com/clusternet/clusternet/pkg/scheduler/apis/config"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	conversion "k8s.io/apimachinery/pkg/conversion"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

func init() {
	localSchemeBuilder.Register(RegisterConversions)
}

// RegisterConversions adds conversion functions to the given scheme.
// Public to allow building arbitrary schemes.
func RegisterConversions(s *runtime.Scheme) error {
	if err := s.AddGeneratedConversionFunc((*Plugin)(nil), (*config.Plugin)(nil), func(a, b interface{}, scope conversion.Scope) error {
		return Convert_v1alpha1_Plugin_To_config_Plugin(a.(*Plugin), b.(*config.Plugin), scope)
	}); err != nil {
		return err
	}
	if err := s.AddGeneratedConversionFunc((*config.Plugin)(nil), (*Plugin)(nil), func(a, b interface{}, scope conversion.Scope) error {
		return Convert_config_Plugin_To_v1alpha1_Plugin(a.(*config.Plugin), b.(*Plugin), scope)
	}); err != nil {
		return err
	}
	if err := s.AddGeneratedConversionFunc((*PluginConfig)(nil), (*config.PluginConfig)(nil), func(a, b interface{}, scope conversion.Scope) error {
		return Convert_v1alpha1_PluginConfig_To_config_PluginConfig(a.(*PluginConfig), b.(*config.PluginConfig), scope)
	}); err != nil {
		return err
	}
	if err := s.AddGeneratedConversionFunc((*config.PluginConfig)(nil), (*PluginConfig)(nil), func(a, b interface{}, scope conversion.Scope) error {
		return Convert_config_PluginConfig_To_v1alpha1_PluginConfig(a.(*config.PluginConfig), b.(*PluginConfig), scope)
	}); err != nil {
		return err
	}
	if err := s.AddGeneratedConversionFunc((*PluginSet)(nil), (*config.PluginSet)(nil), func(a, b interface{}, scope conversion.Scope) error {
		return Convert_v1alpha1_PluginSet_To_config_PluginSet(a.(*PluginSet), b.(*config.PluginSet), scope)
	}); err != nil {
		return err
	}
	if err := s.AddGeneratedConversionFunc((*config.PluginSet)(nil), (*PluginSet)(nil), func(a, b interface{}, scope conversion.Scope) error {
		return Convert_config_PluginSet_To_v1alpha1_PluginSet(a.(*config.PluginSet), b.(*PluginSet), scope)
	}); err != nil {
		return err
	}
	if err := s.AddGeneratedConversionFunc((*Plugins)(nil), (*config.Plugins)(nil), func(a, b interface{}, scope conversion.Scope) error {
		return Convert_v1alpha1_Plugins_To_config_Plugins(a.(*Plugins), b.(*config.Plugins), scope)
	}); err != nil {
		return err
	}
	if err := s.AddGeneratedConversionFunc((*config.Plugins)(nil), (*Plugins)(nil), func(a, b interface{}, scope conversion.Scope) error {
		return Convert_config_Plugins_To_v1alpha1_Plugins(a.(*config.Plugins), b.(*Plugins), scope)
	}); err != nil {
		return err
	}
	if err := s.AddGeneratedConversionFunc((*SchedulerProfile)(nil), (*config.SchedulerProfile)(nil), func(a, b interface{}, scope conversion.Scope) error {
		return Convert_v1alpha1_SchedulerProfile_To_config_SchedulerProfile(a.(*SchedulerProfile), b.(*config.SchedulerProfile), scope)
	}); err != nil {
		return err
	}
	if err := s.AddGeneratedConversionFunc((*config.SchedulerProfile)(nil), (*SchedulerProfile)(nil), func(a, b interface{}, scope conversion.Scope) error {
		return Convert_config_SchedulerProfile_To_v1alpha1_SchedulerProfile(a.(*config.SchedulerProfile), b.(*SchedulerProfile), scope)
	}); err != nil {
		return err
	}
	if err := s.AddConversionFunc((*config.SchedulerConfiguration)(nil), (*SchedulerConfiguration)(nil), func(a, b interface{}, scope conversion.Scope) error {
		return Convert_config_SchedulerConfiguration_To_v1alpha1_SchedulerConfiguration(a.(*config.SchedulerConfiguration), b.(*SchedulerConfiguration), scope)
	}); err != nil {
		return err
	}
	if err := s.AddConversionFunc((*SchedulerConfiguration)(nil), (*config.SchedulerConfiguration)(nil), func(a, b interface{}, scope conversion.Scope) error {
		return Convert_v1alpha1_SchedulerConfiguration_To_config_SchedulerConfiguration(a.(*SchedulerConfiguration), b.(*config.SchedulerConfiguration), scope)
	}); err != nil {
		return err
	}
	return nil
}

func autoConvert_v1alpha1_Plugin_To_config_Plugin(in *Plugin, out *config.Plugin, s conversion.Scope) error {
	out.Name = in.Name
	out.Weight = in.Weight
	return nil
}

// Convert_v1alpha1_Plugin_To_config_Plugin is an autogenerated conversion function.
func Convert_v1alpha1_Plugin_To_config_Plugin(in *Plugin, out *config.Plugin, s conversion.Scope) error {
	return autoConvert_v1alpha1_Plugin_To_config_Plugin(in, out, s)
}

func autoConvert_config_Plugin_To_v1alpha1_Plugin(in *config.Plugin, out *Plugin, s conversion.Scope) error {
	out.Name = in.Name
	out.Weight = in.Weight
	return nil
}

// Convert_config_Plugin_To_v1alpha1_Plugin is an autogenerated conversion function.
func Convert_config_Plugin_To_v1alpha1_Plugin(in *config.Plugin, out *Plugin, s conversion.Scope) error {
	return autoConvert_config_Plugin_To_v1alpha1_Plugin(in, out, s)
}

func autoConvert_v1alpha1_PluginConfig_To_config_PluginConfig(in *PluginConfig, out *config.PluginConfig, s conversion.Scope) error {
	out.Name = in.Name
	if err := runtime.Convert_runtime_RawExtension_To_runtime_Object(&in.Args, &out.Args, s); err != nil {
		return err
	}
	return nil
}

// Convert_v1alpha1_PluginConfig_To_config_PluginConfig is an autogenerated conversion function.
func Convert_v1alpha1_PluginConfig_To_config_PluginConfig(in *PluginConfig, out *config.PluginConfig, s conversion.Scope) error {
	return autoConvert_v1alpha1_PluginConfig_To_config_PluginConfig(in, out, s)
}

func autoConvert_config_PluginConfig_To_v1alpha1_PluginConfig(in *config.PluginConfig, out *PluginConfig, s conversion.Scope) error {
	out.Name = in.Name
	if err := runtime.Convert_runtime_Object_To_runtime_RawExtension(&in.Args, &out.Args, s); err != nil {
		return err
	}
	return nil
}

// Convert_config_PluginConfig_To_v1alpha1_PluginConfig is an autogenerated conversion function.
func Convert_config_PluginConfig_To_v1alpha1_PluginConfig(in *config.PluginConfig, out *PluginConfig, s conversion.Scope) error {
	return autoConvert_config_PluginConfig_To_v1alpha1_PluginConfig(in, out, s)
}

func autoConvert_v1alpha1_PluginSet_To_config_PluginSet(in *PluginSet, out *config.PluginSet, s conversion.Scope) error {
	out.Enabled = *(*[]config.Plugin)(unsafe.Pointer(&in.Enabled))
	out.Disabled = *(*[]config.Plugin)(unsafe.Pointer(&in.Disabled))
	return nil
}

// Convert_v1alpha1_PluginSet_To_config_PluginSet is an autogenerated conversion function.
func Convert_v1alpha1_PluginSet_To_config_PluginSet(in *PluginSet, out *config.PluginSet, s conversion.Scope) error {
	return autoConvert_v1alpha1_PluginSet_To_config_PluginSet(in, out, s)
}

func autoConvert_config_PluginSet_To_v1alpha1_PluginSet(in *config.PluginSet, out *PluginSet, s conversion.Scope) error {
	out.Enabled = *(*[]Plugin)(unsafe.Pointer(&in.Enabled))
	out.Disabled = *(*[]Plugin)(unsafe.Pointer(&in.Disabled))
	return nil
}

// Convert_config_PluginSet_To_v1alpha1_PluginSet is an autogenerated conversion function.
func Convert_config_PluginSet_To_v1alpha1_PluginSet(in *config.PluginSet, out *PluginSet, s conversion.Scope) error {
	return autoConvert_config_PluginSet_To_v1alpha1_PluginSet(in, out, s)
}

func autoConvert_v1alpha1_Plugins_To_config_Plugins(in *Plugins, out *config.Plugins, s conversion.Scope) error {
	if err := Convert_v1alpha1_PluginSet_To_config_PluginSet(&in.PreFilter, &out.PreFilter, s); err != nil {
		return err
	}
	if err := Convert_v1alpha1_PluginSet_To_config_PluginSet(&in.Filter, &out.Filter, s); err != nil {
		return err
	}
	if err := Convert_v1alpha1_PluginSet_To_config_PluginSet(&in.PostFilter, &out.PostFilter, s); err != nil {
		return err
	}
	if err := Convert_v1alpha1_PluginSet_To_config_PluginSet(&in.PrePredict, &out.PrePredict, s); err != nil {
		return err
	}
	if err := Convert_v1alpha1_PluginSet_To_config_PluginSet(&in.Predict, &out.Predict, s); err != nil {
		return err
	}
	if err := Convert_v1alpha1_PluginSet_To_config_PluginSet(&in.PreScore, &out.PreScore, s); err != nil {
		return err
	}
	if err := Convert_v1alpha1_PluginSet_To_config_PluginSet(&in.Score, &out.Score, s); err != nil {
		return err
	}
	if err := Convert_v1alpha1_PluginSet_To_config_PluginSet(&in.PreAssign, &out.PreAssign, s); err != nil {
		return err
	}
	if err := Convert_v1alpha1_PluginSet_To_config_PluginSet(&in.Assign, &out.Assign, s); err != nil {
		return err
	}
	if err := Convert_v1alpha1_PluginSet_To_config_PluginSet(&in.Reserve, &out.Reserve, s); err != nil {
		return err
	}
	if err := Convert_v1alpha1_PluginSet_To_config_PluginSet(&in.Permit, &out.Permit, s); err != nil {
		return err
	}
	if err := Convert_v1alpha1_PluginSet_To_config_PluginSet(&in.PreBind, &out.PreBind, s); err != nil {
		return err
	}
	if err := Convert_v1alpha1_PluginSet_To_config_PluginSet(&in.Bind, &out.Bind, s); err != nil {
		return err
	}
	if err := Convert_v1alpha1_PluginSet_To_config_PluginSet(&in.PostBind, &out.PostBind, s); err != nil {
		return err
	}
	return nil
}

// Convert_v1alpha1_Plugins_To_config_Plugins is an autogenerated conversion function.
func Convert_v1alpha1_Plugins_To_config_Plugins(in *Plugins, out *config.Plugins, s conversion.Scope) error {
	return autoConvert_v1alpha1_Plugins_To_config_Plugins(in, out, s)
}

func autoConvert_config_Plugins_To_v1alpha1_Plugins(in *config.Plugins, out *Plugins, s conversion.Scope) error {
	if err := Convert_config_PluginSet_To_v1alpha1_PluginSet(&in.PreFilter, &out.PreFilter, s); err != nil {
		return err
	}
	if err := Convert_config_PluginSet_To_v1alpha1_PluginSet(&in.Filter, &out.Filter, s); err != nil {
		return err
	}
	if err := Convert_config_PluginSet_To_v1alpha1_PluginSet(&in.PostFilter, &out.PostFilter, s); err != nil {
		return err
	}
	if err := Convert_config_PluginSet_To_v1alpha1_PluginSet(&in.PrePredict, &out.PrePredict, s); err != nil {
		return err
	}
	if err := Convert_config_PluginSet_To_v1alpha1_PluginSet(&in.Predict, &out.Predict, s); err != nil {
		return err
	}
	if err := Convert_config_PluginSet_To_v1alpha1_PluginSet(&in.PreScore, &out.PreScore, s); err != nil {
		return err
	}
	if err := Convert_config_PluginSet_To_v1alpha1_PluginSet(&in.Score, &out.Score, s); err != nil {
		return err
	}
	if err := Convert_config_PluginSet_To_v1alpha1_PluginSet(&in.PreAssign, &out.PreAssign, s); err != nil {
		return err
	}
	if err := Convert_config_PluginSet_To_v1alpha1_PluginSet(&in.Assign, &out.Assign, s); err != nil {
		return err
	}
	if err := Convert_config_PluginSet_To_v1alpha1_PluginSet(&in.Reserve, &out.Reserve, s); err != nil {
		return err
	}
	if err := Convert_config_PluginSet_To_v1alpha1_PluginSet(&in.Permit, &out.Permit, s); err != nil {
		return err
	}
	if err := Convert_config_PluginSet_To_v1alpha1_PluginSet(&in.PreBind, &out.PreBind, s); err != nil {
		return err
	}
	if err := Convert_config_PluginSet_To_v1alpha1_PluginSet(&in.Bind, &out.Bind, s); err != nil {
		return err
	}
	if err := Convert_config_PluginSet_To_v1alpha1_PluginSet(&in.PostBind, &out.PostBind, s); err != nil {
		return err
	}
	return nil
}

// Convert_config_Plugins_To_v1alpha1_Plugins is an autogenerated conversion function.
func Convert_config_Plugins_To_v1alpha1_Plugins(in *config.Plugins, out *Plugins, s conversion.Scope) error {
	return autoConvert_config_Plugins_To_v1alpha1_Plugins(in, out, s)
}

func autoConvert_v1alpha1_SchedulerConfiguration_To_config_SchedulerConfiguration(in *SchedulerConfiguration, out *config.SchedulerConfiguration, s conversion.Scope) error {
	if in.Profiles != nil {
		in, out := &in.Profiles, &out.Profiles
		*out = make([]config.SchedulerProfile, len(*in))
		for i := range *in {
			if err := Convert_v1alpha1_SchedulerProfile_To_config_SchedulerProfile(&(*in)[i], &(*out)[i], s); err != nil {
				return err
			}
		}
	} else {
		out.Profiles = nil
	}
	return nil
}

func autoConvert_config_SchedulerConfiguration_To_v1alpha1_SchedulerConfiguration(in *config.SchedulerConfiguration, out *SchedulerConfiguration, s conversion.Scope) error {
	if in.Profiles != nil {
		in, out := &in.Profiles, &out.Profiles
		*out = make([]SchedulerProfile, len(*in))
		for i := range *in {
			if err := Convert_config_SchedulerProfile_To_v1alpha1_SchedulerProfile(&(*in)[i], &(*out)[i], s); err != nil {
				return err
			}
		}
	} else {
		out.Profiles = nil
	}
	return nil
}

func autoConvert_v1alpha1_SchedulerProfile_To_config_SchedulerProfile(in *SchedulerProfile, out *config.SchedulerProfile, s conversion.Scope) error {
	if err := v1.Convert_Pointer_string_To_string(&in.SchedulerName, &out.SchedulerName, s); err != nil {
		return err
	}
	out.Plugins = (*config.Plugins)(unsafe.Pointer(in.Plugins))
	if in.PluginConfig != nil {
		in, out := &in.PluginConfig, &out.PluginConfig
		*out = make([]config.PluginConfig, len(*in))
		for i := range *in {
			if err := Convert_v1alpha1_PluginConfig_To_config_PluginConfig(&(*in)[i], &(*out)[i], s); err != nil {
				return err
			}
		}
	} else {
		out.PluginConfig = nil
	}
	return nil
}

// Convert_v1alpha1_SchedulerProfile_To_config_SchedulerProfile is an autogenerated conversion function.
func Convert_v1alpha1_SchedulerProfile_To_config_SchedulerProfile(in *SchedulerProfile, out *config.SchedulerProfile, s conversion.Scope) error {
	return autoConvert_v1alpha1_SchedulerProfile_To_config_SchedulerProfile(in, out, s)
}

func autoConvert_config_SchedulerProfile_To_v1alpha1_SchedulerProfile(in *config.SchedulerProfile, out *SchedulerProfile, s conversion.Scope) error {
	if err := v1.Convert_string_To_Pointer_string(&in.SchedulerName, &out.SchedulerName, s); err != nil {
		return err
	}
	out.Plugins = (*Plugins)(unsafe.Pointer(in.Plugins))
	if in.PluginConfig != nil {
		in, out := &in.PluginConfig, &out.PluginConfig
		*out = make([]PluginConfig, len(*in))
		for i := range *in {
			if err := Convert_config_PluginConfig_To_v1alpha1_PluginConfig(&(*in)[i], &(*out)[i], s); err != nil {
				return err
			}
		}
	} else {
		out.PluginConfig = nil
	}
	return nil
}

// Convert_config_SchedulerProfile_To_v1alpha1_SchedulerProfile is an autogenerated conversion function.
func Convert_config_SchedulerProfile_To_v1alpha1_SchedulerProfile(in *config.SchedulerProfile, out *SchedulerProfile, s conversion.Scope) error {
	return autoConvert_config_SchedulerProfile_To_v1alpha1_SchedulerProfile(in, out, s)
}