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

package v1alpha1

import (
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Important: Run "make generated" to regenerate code after modifying this file

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope="Namespaced",shortName=loc;local,categories=clusternet
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"

// Localization represents the override config for a group of resources.
type Localization struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec LocalizationSpec `json:"spec"`
}

// LocalizationSpec defines the desired state of Localization
type LocalizationSpec struct {
	// OverridePolicy specifies the override policy for this Localization.
	//
	// +optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Enum=ApplyNow;ApplyLater
	// +kubebuilder:default=ApplyLater
	OverridePolicy OverridePolicy `json:"overridePolicy,omitempty"`

	// Overrides holds all the OverrideConfig.
	//
	// +optional
	Overrides []OverrideConfig `json:"overrides,omitempty"`

	// Priority is an integer defining the relative importance of this Localization compared to others.
	// Lower numbers are considered lower priority.
	// And these Localization(s) will be applied by order from lower priority to higher.
	// That means override values in lower Localization will be overridden by those in higher Localization.
	//
	// +optional
	// +kubebuilder:validation:Maximum=1000
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=500
	Priority int32 `json:"priority,omitempty"`

	// Feed holds references to the objects the Localization applies to.
	//
	// +optional
	Feed `json:"feed,omitempty"`
}

type OverridePolicy string

const (
	// Apply overrides for all matched objects immediately, including those already populated
	ApplyNow OverridePolicy = "ApplyNow"

	// Apply overrides for all matched objects on next updates (including updates on Subscription,
	// Manifest, HelmChart, etc) or new created objects.
	ApplyLater OverridePolicy = "ApplyLater"
)

type OverrideType string

const (
	// HelmType applies Helm values for all matched HelmCharts.
	// Note: HelmType only works with HelmChart(s).
	HelmType OverrideType = "Helm"

	// JSONPatchType applies a json patch for all matched objects.
	// Note: JSONPatchType does not work with HelmChart(s).
	JSONPatchType OverrideType = "JSONPatch"

	// MergePatchType applies a json merge patch for all matched objects.
	// Note: MergePatchType does not work with HelmChart(s).
	MergePatchType OverrideType = "MergePatch"

	// StrategicMergePatchType won't be supported, since `patchStrategy`
	// and `patchMergeKey` can not be retrieved.

	// KyvernoPatchType applies a kyverno style patch for all matched objects.
	// Note: KyvernoPatchType does not work with HelmChart(s).
	KyvernoPatchType OverrideType = "KyvernoPatch"
)

// OverrideConfig holds information that describes a override config.
type OverrideConfig struct {
	// Name indicate the OverrideConfig name.
	//
	// +optional
	Name string `json:"name,omitempty"`

	// Value represents override value.
	//
	// +required
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Type=string
	Value string `json:"value"`

	// Type specifies the override type for override value.
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Enum=Helm;JSONPatch;MergePatch;KyvernoPatch
	Type OverrideType `json:"type"`

	// OverrideChart indicates whether the override value for the HelmChart CR.
	//
	// +optional
	OverrideChart bool `json:"overrideChart,omitempty"`

	// KyvernoPatchConfig kyverno style patch config
	//
	// +optional
	KyvernoConfig *KyvernoPatchConfig `json:"kyvernoConfig,omitempty"`
}

// KyvernoVariable defines an arbitrary JMESPath context variable that can be defined inline.
type KyvernoVariable struct {
	// Value is any arbitrary JSON object representable in YAML or JSON form.
	// +optional
	Value *apiextv1.JSON `json:"value,omitempty" yaml:"value,omitempty"`

	// JMESPath is an optional JMESPath Expression that can be used to
	// transform the variable.
	// +optional
	JMESPath string `json:"jmesPath,omitempty" yaml:"jmesPath,omitempty"`

	// Default is an optional arbitrary JSON object that the variable may take if the JMESPath
	// expression evaluates to nil
	// +optional
	Default *apiextv1.JSON `json:"default,omitempty" yaml:"default,omitempty"`
}

// KyvernoContextEntry adds variables to a rule context
type KyvernoContextEntry struct {
	// Name is the variable name.
	Name string `json:"name,omitempty" yaml:"name,omitempty"`

	// Variable defines an arbitrary JMESPath context variable that can be defined inline.
	Variable *KyvernoVariable `json:"variable,omitempty" yaml:"variable,omitempty"`
}

// KyvernoCondition defines variable-based conditional criteria for rule execution.
type KyvernoCondition struct {
	// Key is the context entry (using JMESPath) for conditional rule evaluation.
	RawKey *apiextv1.JSON `json:"key,omitempty" yaml:"key,omitempty"`

	// Operator is the conditional operation to perform. Valid operators are:
	// Equals, NotEquals, In, AnyIn, AllIn, NotIn, AnyNotIn, AllNotIn, GreaterThanOrEquals,
	// GreaterThan, LessThanOrEquals, LessThan, DurationGreaterThanOrEquals, DurationGreaterThan,
	// DurationLessThanOrEquals, DurationLessThan
	Operator ConditionOperator `json:"operator,omitempty" yaml:"operator,omitempty"`

	// Value is the conditional value, or set of values. The values can be fixed set
	// or can be variables declared using JMESPath.
	// +optional
	RawValue *apiextv1.JSON `json:"value,omitempty" yaml:"value,omitempty"`
}

func (c *KyvernoCondition) GetKey() apiextensions.JSON {
	return FromJSON(c.RawKey)
}

func (c *KyvernoCondition) SetKey(in apiextensions.JSON) {
	c.RawKey = ToJSON(in)
}

func (c *KyvernoCondition) GetValue() apiextensions.JSON {
	return FromJSON(c.RawValue)
}

func (c *KyvernoCondition) SetValue(in apiextensions.JSON) {
	c.RawValue = ToJSON(in)
}

// AnyAllConditions consists of conditions wrapped denoting a logical criteria to be fulfilled.
// AnyConditions get fulfilled when at least one of its sub-conditions passes.
// AllConditions get fulfilled only when all of its sub-conditions pass.
type AnyAllConditions struct {
	// AnyConditions enable variable-based conditional rule execution. This is useful for
	// finer control of when an rule is applied. A condition can reference object data
	// using JMESPath notation.
	// Here, at least one of the conditions need to pass
	// +optional
	AnyConditions []KyvernoCondition `json:"any,omitempty" yaml:"any,omitempty"`

	// AllConditions enable variable-based conditional rule execution. This is useful for
	// finer control of when an rule is applied. A condition can reference object data
	// using JMESPath notation.
	// Here, all of the conditions need to pass
	// +optional
	AllConditions []KyvernoCondition `json:"all,omitempty" yaml:"all,omitempty"`
}

// ConditionOperator is the operation performed on condition key and value.
// +kubebuilder:validation:Enum=Equals;NotEquals;In;AnyIn;AllIn;NotIn;AnyNotIn;AllNotIn;GreaterThanOrEquals;GreaterThan;LessThanOrEquals;LessThan;DurationGreaterThanOrEquals;DurationGreaterThan;DurationLessThanOrEquals;DurationLessThan
type ConditionOperator string

// ConditionOperators stores all the valid ConditionOperator types as key-value pairs.
//
// "Equal" evaluates if the key is equal to the value. (Deprecated; Use Equals instead)
// "Equals" evaluates if the key is equal to the value.
// "NotEqual" evaluates if the key is not equal to the value. (Deprecated; Use NotEquals instead)
// "NotEquals" evaluates if the key is not equal to the value.
// "In" evaluates if the key is contained in the set of values.
// "AnyIn" evaluates if any of the keys are contained in the set of values.
// "AllIn" evaluates if all the keys are contained in the set of values.
// "NotIn" evaluates if the key is not contained in the set of values.
// "AnyNotIn" evaluates if any of the keys are not contained in the set of values.
// "AllNotIn" evaluates if all the keys are not contained in the set of values.
// "GreaterThanOrEquals" evaluates if the key (numeric) is greater than or equal to the value (numeric).
// "GreaterThan" evaluates if the key (numeric) is greater than the value (numeric).
// "LessThanOrEquals" evaluates if the key (numeric) is less than or equal to the value (numeric).
// "LessThan" evaluates if the key (numeric) is less than the value (numeric).
// "DurationGreaterThanOrEquals" evaluates if the key (duration) is greater than or equal to the value (duration)
// "DurationGreaterThan" evaluates if the key (duration) is greater than the value (duration)
// "DurationLessThanOrEquals" evaluates if the key (duration) is less than or equal to the value (duration)
// "DurationLessThan" evaluates if the key (duration) is greater than the value (duration)
var ConditionOperators = map[string]ConditionOperator{
	"Equal":                       ConditionOperator("Equal"),
	"Equals":                      ConditionOperator("Equals"),
	"NotEqual":                    ConditionOperator("NotEqual"),
	"NotEquals":                   ConditionOperator("NotEquals"),
	"In":                          ConditionOperator("In"),
	"AnyIn":                       ConditionOperator("AnyIn"),
	"AllIn":                       ConditionOperator("AllIn"),
	"NotIn":                       ConditionOperator("NotIn"),
	"AnyNotIn":                    ConditionOperator("AnyNotIn"),
	"AllNotIn":                    ConditionOperator("AllNotIn"),
	"GreaterThanOrEquals":         ConditionOperator("GreaterThanOrEquals"),
	"GreaterThan":                 ConditionOperator("GreaterThan"),
	"LessThanOrEquals":            ConditionOperator("LessThanOrEquals"),
	"LessThan":                    ConditionOperator("LessThan"),
	"DurationGreaterThanOrEquals": ConditionOperator("DurationGreaterThanOrEquals"),
	"DurationGreaterThan":         ConditionOperator("DurationGreaterThan"),
	"DurationLessThanOrEquals":    ConditionOperator("DurationLessThanOrEquals"),
	"DurationLessThan":            ConditionOperator("DurationLessThan"),
}

// KyvernoForEachMutation applies mutation rules to a list of sub-elements by creating a context for each entry in the list and looping over it to apply the specified logic.
type KyvernoForEachMutation struct {
	// List specifies a JMESPath expression that results in one or more elements
	// to which the validation logic is applied.
	List string `json:"list,omitempty" yaml:"list,omitempty"`

	// Context defines variables and data sources that can be used during rule execution.
	// +optional
	Context []KyvernoContextEntry `json:"context,omitempty" yaml:"context,omitempty"`

	// AnyAllConditions are used to determine if a policy rule should be applied by evaluating a
	// set of conditions. The declaration can contain nested `any` or `all` statements.
	// See: https://kyverno.io/docs/writing-policies/preconditions/
	// +kubebuilder:validation:XPreserveUnknownFields
	// +optional
	AnyAllConditions *AnyAllConditions `json:"preconditions,omitempty" yaml:"preconditions,omitempty"`

	// PatchStrategicMerge is a strategic merge patch used to modify resources.
	// See https://kubernetes.io/docs/tasks/manage-kubernetes-objects/update-api-object-kubectl-patch/
	// and https://kubectl.docs.kubernetes.io/references/kustomize/patchesstrategicmerge/.
	// +optional
	RawPatchStrategicMerge *apiextv1.JSON `json:"patchStrategicMerge,omitempty" yaml:"patchStrategicMerge,omitempty"`

	// PatchesJSON6902 is a list of RFC 6902 JSON Patch declarations used to modify resources.
	// See https://tools.ietf.org/html/rfc6902 and https://kubectl.docs.kubernetes.io/references/kustomize/patchesjson6902/.
	// +optional
	PatchesJSON6902 string `json:"patchesJson6902,omitempty" yaml:"patchesJson6902,omitempty"`
}

func (m *KyvernoForEachMutation) GetPatchStrategicMerge() apiextensions.JSON {
	return FromJSON(m.RawPatchStrategicMerge)
}

func (m *KyvernoForEachMutation) SetPatchStrategicMerge(in apiextensions.JSON) {
	m.RawPatchStrategicMerge = ToJSON(in)
}

// KyvernoMutation defines how resource are modified.
type KyvernoMutation struct {
	// PatchStrategicMerge is a strategic merge patch used to modify resources.
	// See https://kubernetes.io/docs/tasks/manage-kubernetes-objects/update-api-object-kubectl-patch/
	// and https://kubectl.docs.kubernetes.io/references/kustomize/patchesstrategicmerge/.
	// +optional
	RawPatchStrategicMerge *apiextv1.JSON `json:"patchStrategicMerge,omitempty" yaml:"patchStrategicMerge,omitempty"`

	// PatchesJSON6902 is a list of RFC 6902 JSON Patch declarations used to modify resources.
	// See https://tools.ietf.org/html/rfc6902 and https://kubectl.docs.kubernetes.io/references/kustomize/patchesjson6902/.
	// +optional
	PatchesJSON6902 string `json:"patchesJson6902,omitempty" yaml:"patchesJson6902,omitempty"`

	// ForEach applies mutation rules to a list of sub-elements by creating a context for each entry in the list and looping over it to apply the specified logic.
	// +optional
	ForEachMutation []KyvernoForEachMutation `json:"foreach,omitempty" yaml:"foreach,omitempty"`
}

func (m *KyvernoMutation) GetPatchStrategicMerge() apiextensions.JSON {
	return FromJSON(m.RawPatchStrategicMerge)
}

func (m *KyvernoMutation) SetPatchStrategicMerge(in apiextensions.JSON) {
	m.RawPatchStrategicMerge = ToJSON(in)
}

// KyvernoPatchConfig config for a kyverno style patch
type KyvernoPatchConfig struct {
	// Context defines variables and data sources that can be used during rule execution.
	// +optional
	Context []KyvernoContextEntry `json:"context,omitempty" yaml:"context,omitempty"`

	// Preconditions are used to determine if a policy rule should be applied by evaluating a
	// set of conditions. The declaration can contain nested `any` or `all` statements. A direct list
	// of conditions (without `any` or `all` statements is supported for backwards compatibility but
	// will be deprecated in the next major release.
	// See: https://kyverno.io/docs/writing-policies/preconditions/
	// +optional
	RawAnyAllConditions *apiextv1.JSON `json:"preconditions,omitempty" yaml:"preconditions,omitempty"`

	// Mutation is used to modify matching resources.
	// +optional
	Mutation KyvernoMutation `json:"mutate,omitempty" yaml:"mutate,omitempty"`
}

func (kpc *KyvernoPatchConfig) GetAnyAllConditions() apiextensions.JSON {
	return FromJSON(kpc.RawAnyAllConditions)
}

func (kpc *KyvernoPatchConfig) SetAnyAllConditions(in apiextensions.JSON) {
	kpc.RawAnyAllConditions = ToJSON(in)
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// LocalizationList contains a list of Localization
type LocalizationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Localization `json:"items"`
}
