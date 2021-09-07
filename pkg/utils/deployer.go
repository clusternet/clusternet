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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	clusternetclientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	clusterlisters "github.com/clusternet/clusternet/pkg/generated/listers/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/known"
)

// DeployableByHub checks whether HelmRelease/Description should be deployed from hub
func DeployableByHub(clusterLister clusterlisters.ManagedClusterLister, clusterID, dedicatedNamespace string) (bool, error) {
	if len(clusterID) == 0 {
		return false, fmt.Errorf("empty clusterID from label %s", known.ClusterIDLabel)
	}
	if len(dedicatedNamespace) == 0 {
		return false, errors.New("namesapce is empty")
	}

	mcls, err := clusterLister.ManagedClusters(dedicatedNamespace).List(
		labels.SelectorFromSet(labels.Set{
			known.ClusterIDLabel: clusterID,
		}))
	if err != nil {
		return false, err
	}
	if len(mcls) == 0 {
		return false, fmt.Errorf("failed to find a ManagedCluster declaration in namespace %s", dedicatedNamespace)
	}

	if mcls[0].Spec.SyncMode == clusterapi.Pull || !mcls[0].Status.AppPusher {
		msg := "set syncMode as Pull"
		if !mcls[0].Status.AppPusher {
			msg = "disabled AppPusher"
		}
		klog.V(5).Infof("ManagedCluster %s with uid=%s has %s", klog.KObj(mcls[0]), mcls[0].UID, msg)
		return false, nil
	}
	return true, nil
}

func DeployableByAgent(syncMode clusterapi.ClusterSyncMode, appPusherEnabled bool) bool {
	switch syncMode {
	case clusterapi.Push:
		return false
	case clusterapi.Pull:
		return true
	case clusterapi.Dual:
		if appPusherEnabled {
			return false
		}
		return true
	default:
		klog.Errorf("unknown syncMode %s", syncMode)
		return false
	}
}

func ReconcileHelmRelease(ctx context.Context, deployCtx *DeployContext, clusternetClient *clusternetclientset.Clientset,
	hrLister applisters.HelmReleaseLister, descLister applisters.DescriptionLister,
	hr *appsapi.HelmRelease, recorder record.EventRecorder) error {
	klog.V(5).Infof("handle HelmRelease %s", klog.KObj(hr))

	cfg := new(action.Configuration)
	err := cfg.Init(deployCtx, hr.Spec.TargetNamespace, "secret", klog.V(5).Infof)
	if err != nil {
		return err
	}
	cfg.Releases.MaxHistory = 5

	// delete helm release
	if hr.DeletionTimestamp != nil {
		err := UninstallRelease(cfg, hr)
		if err != nil {
			return err
		}

		hr.Finalizers = RemoveString(hr.Finalizers, known.AppFinalizer)
		_, err = clusternetClient.AppsV1alpha1().HelmReleases(hr.Namespace).Update(context.TODO(), hr, metav1.UpdateOptions{})
		return err
	}

	// install or upgrade helm release
	chart, err := LocateHelmChart(hr.Spec.Repository, hr.Spec.Chart, hr.Spec.ChartVersion)
	if err != nil {
		recorder.Event(hr, corev1.EventTypeWarning, "ChartLocateFailure", err.Error())
		return err
	}

	overrideValues, err := GetOverrides(descLister, hr, recorder)
	if err != nil {
		return err
	}

	var rel *release.Release
	// check whether the release is deployed
	rel, err = cfg.Releases.Deployed(hr.Name)
	if err != nil {
		if strings.Contains(err.Error(), driver.ErrNoDeployedReleases.Error()) {
			rel, err = InstallRelease(cfg, hr, chart, overrideValues)
		}
	} else {
		// verify the release is changed or not
		if ReleaseNeedsUpgrade(rel, hr, chart, overrideValues) {
			rel, err = UpgradeRelease(cfg, hr, chart, overrideValues)
		} else {
			klog.V(5).Infof("HelmRelease %s is already updated. No need upgrading.", klog.KObj(hr))
		}
	}

	var hrStatus *appsapi.HelmReleaseStatus
	if err != nil {
		// repo update
		if strings.Contains(err.Error(), "helm repo update") {
			return UpdateRepo(hr.Spec.Repository)
		}
		hrStatus = &appsapi.HelmReleaseStatus{
			Phase: release.StatusFailed,
			Notes: err.Error(),
		}
	}

	if rel != nil {
		hrStatus = &appsapi.HelmReleaseStatus{
			Version: rel.Version,
		}
		if rel.Info != nil {
			hrStatus.FirstDeployed = rel.Info.FirstDeployed.String()
			hrStatus.LastDeployed = rel.Info.LastDeployed.String()
			hrStatus.Description = rel.Info.Description
			hrStatus.Phase = rel.Info.Status
			hrStatus.Notes = rel.Info.Notes
		}
	}
	return UpdateHelmReleaseStatus(ctx, clusternetClient,
		hrLister, descLister, hr, hrStatus)
}

func GetOverrides(descLister applisters.DescriptionLister, hr *appsapi.HelmRelease, recorder record.EventRecorder) (map[string]interface{}, error) {
	var overrideValues map[string]interface{}
	if hr.DeletionTimestamp != nil {
		return overrideValues, nil
	}

	// get overrides
	controllerRef := metav1.GetControllerOf(hr)
	if controllerRef != nil {
		desc := resolveControllerRef(descLister, hr.Namespace, controllerRef)
		if desc == nil {
			return overrideValues, nil
		}

		var found bool
		var index int
		for idx, chart := range desc.Spec.Charts {
			if chart.Namespace == hr.Namespace && chart.Name == hr.Name {
				found = true
				index = idx
				break
			}
		}
		if !found {
			msg := fmt.Sprintf("Description %s has no connection with HelmRelease %s", klog.KObj(desc), klog.KObj(hr))
			klog.WarningDepth(5, msg)
			recorder.Event(desc, corev1.EventTypeWarning, "DescriptionNotRelated", msg)
			return overrideValues, nil
		}
		if len(desc.Spec.Raw) < index {
			msg := fmt.Sprintf("unequal lengths of Spec.Raw and Spec.Charts in Description %s", klog.KObj(desc))
			klog.ErrorDepth(5, msg)
			recorder.Event(desc, corev1.EventTypeWarning, "UnequalLengths", msg)
			return nil, errors.New(msg)
		}
		err := json.Unmarshal(desc.Spec.Raw[index], &overrideValues)
		return overrideValues, err
	}
	return overrideValues, nil
}

func UpdateHelmReleaseStatus(ctx context.Context, clusternetClient *clusternetclientset.Clientset,
	hrLister applisters.HelmReleaseLister, descLister applisters.DescriptionLister,
	hr *appsapi.HelmRelease, status *appsapi.HelmReleaseStatus) error {
	// NEVER modify objects from the store. It's a read-only, local cache.
	// You can use DeepCopy() to make a deep copy of original object and modify this copy
	// Or create a copy manually for better performance

	klog.V(5).Infof("try to update HelmRelease %q status", hr.Name)

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		hr.Status = *status
		_, err := clusternetClient.AppsV1alpha1().HelmReleases(hr.Namespace).UpdateStatus(ctx, hr, metav1.UpdateOptions{})
		if err == nil {
			return nil
		}

		if updated, err := hrLister.HelmReleases(hr.Namespace).Get(hr.Name); err == nil {
			// make a copy so we don't mutate the shared cache
			hr = updated.DeepCopy()
		} else {
			utilruntime.HandleError(fmt.Errorf("error getting updated HelmRelease %q from lister: %v", hr.Name, err))
		}
		return err
	})

	if err != nil {
		return err
	}

	klog.V(5).Infof("try to update HelmRelease %q owner Description status", hr.Name)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		controllerRef := metav1.GetControllerOf(hr)
		if controllerRef == nil {
			// No controller should care about orphans being deleted.
			return nil
		}
		desc := resolveControllerRef(descLister, hr.Namespace, controllerRef)
		if desc == nil {
			return nil
		}
		if status.Phase == release.StatusDeployed {
			desc.Status.Phase = appsapi.DescriptionPhaseSuccess
			desc.Status.Reason = ""
		} else {
			desc.Status.Phase = appsapi.DescriptionPhaseFailure
			desc.Status.Reason = status.Notes
		}
		_, err := clusternetClient.AppsV1alpha1().Descriptions(desc.Namespace).UpdateStatus(ctx, desc, metav1.UpdateOptions{})
		if err == nil {
			return nil
		}

		utilruntime.HandleError(fmt.Errorf("error updating status for Description %q: %v", klog.KObj(desc), err))
		return err
	})
}

// resolveControllerRef returns the controller referenced by a ControllerRef,
// or nil if the ControllerRef could not be resolved to a matching controller
// of the correct Kind.
func resolveControllerRef(descLister applisters.DescriptionLister, namespace string, controllerRef *metav1.OwnerReference) *appsapi.Description {
	// We can't look up by UID, so look up by Name and then verify UID.
	// Don't even try to look up by Name if it's the wrong Kind.
	if controllerRef.Kind != "Description" {
		return nil
	}
	desc, err := descLister.Descriptions(namespace).Get(controllerRef.Name)
	if err != nil {
		return nil
	}
	if desc.UID != controllerRef.UID {
		// The controller we found with this Name is not the same one that the
		// ControllerRef points to.
		return nil
	}
	return desc
}

func ApplyDescription(ctx context.Context, clusternetClient *clusternetclientset.Clientset, dynamicClient dynamic.Interface,
	discoveryRESTMapper meta.RESTMapper, desc *appsapi.Description, recorder record.EventRecorder) error {
	var allErrs []error
	wg := sync.WaitGroup{}
	objectsToBeDeployed := desc.Spec.Raw
	errCh := make(chan error, len(objectsToBeDeployed))
	for _, object := range objectsToBeDeployed {
		resource := &unstructured.Unstructured{}
		err := resource.UnmarshalJSON(object)
		if err != nil {
			allErrs = append(allErrs, err)
			msg := fmt.Sprintf("failed to unmarshal resource: %v", err)
			klog.ErrorDepth(5, msg)
			recorder.Event(desc, corev1.EventTypeWarning, "FailedMarshalingResource", msg)
		} else {
			wg.Add(1)
			go func(resource *unstructured.Unstructured) {
				defer wg.Done()

				err := applyResourceWithRetry(ctx, dynamicClient, discoveryRESTMapper, resource)
				if err != nil {
					errCh <- err
				}
			}(resource)
		}
	}
	wg.Wait()

	// collect errors
	close(errCh)
	for err := range errCh {
		allErrs = append(allErrs, err)
	}

	var statusPhase appsapi.DescriptionPhase
	var reason string
	if len(allErrs) > 0 {
		statusPhase = appsapi.DescriptionPhaseFailure
		reason = utilerrors.NewAggregate(allErrs).Error()

		msg := fmt.Sprintf("failed to deploying Description %s: %s", klog.KObj(desc), reason)
		klog.ErrorDepth(5, msg)
		recorder.Event(desc, corev1.EventTypeWarning, "UnSuccessfullyDeployed", msg)
	} else {
		statusPhase = appsapi.DescriptionPhaseSuccess
		reason = ""

		msg := fmt.Sprintf("Description %s is deployed successfully", klog.KObj(desc))
		klog.V(5).Info(msg)
		recorder.Event(desc, corev1.EventTypeNormal, "SuccessfullyDeployed", msg)
	}

	// update status
	desc.Status.Phase = statusPhase
	desc.Status.Reason = reason
	_, err := clusternetClient.AppsV1alpha1().Descriptions(desc.Namespace).UpdateStatus(context.TODO(), desc, metav1.UpdateOptions{})
	return err
}

func OffloadDescription(ctx context.Context, clusternetClient *clusternetclientset.Clientset, dynamicClient dynamic.Interface,
	discoveryRESTMapper meta.RESTMapper, desc *appsapi.Description, recorder record.EventRecorder) error {
	var err error
	var allErrs []error
	wg := sync.WaitGroup{}
	objectsToBeDeleted := desc.Spec.Raw
	errCh := make(chan error, len(objectsToBeDeleted))
	for _, object := range objectsToBeDeleted {
		resource := &unstructured.Unstructured{}
		err := resource.UnmarshalJSON(object)
		if err != nil {
			allErrs = append(allErrs, err)
			msg := fmt.Sprintf("failed to unmarshal resource: %v", err)
			klog.ErrorDepth(5, msg)
			recorder.Event(desc, corev1.EventTypeWarning, "FailedMarshalingResource", msg)
		} else {
			wg.Add(1)
			go func(resource *unstructured.Unstructured) {
				defer wg.Done()
				klog.V(5).Infof("deleting %s %s defined in Description %s", resource.GetKind(),
					klog.KObj(resource), klog.KObj(desc))
				err := deleteResourceWithRetry(ctx, dynamicClient, discoveryRESTMapper, resource)
				if err != nil {
					errCh <- err
				}
			}(resource)
		}
	}
	wg.Wait()

	// collect errors
	close(errCh)
	for err := range errCh {
		allErrs = append(allErrs, err)
	}

	err = utilerrors.NewAggregate(allErrs)
	if err != nil {
		msg := fmt.Sprintf("failed to deleting Description %s: %v", klog.KObj(desc), err)
		klog.ErrorDepth(5, msg)
		recorder.Event(desc, corev1.EventTypeWarning, "FailedDeletingDescription", msg)
	} else {
		klog.V(5).Infof("Description %s is deleted successfully", klog.KObj(desc))
		desc.Finalizers = RemoveString(desc.Finalizers, known.AppFinalizer)
		_, err = clusternetClient.AppsV1alpha1().Descriptions(desc.Namespace).Update(context.TODO(), desc, metav1.UpdateOptions{})
		if err != nil {
			klog.WarningDepth(4,
				fmt.Sprintf("failed to remove finalizer %s from Description %s: %v", known.AppFinalizer, klog.KObj(desc), err))

		}
	}
	return err
}

func applyResourceWithRetry(ctx context.Context, dynamicClient dynamic.Interface, restMapper meta.RESTMapper, resource *unstructured.Unstructured) error {
	// set UID as empty
	resource.SetUID("")

	return wait.ExponentialBackoffWithContext(ctx, retry.DefaultBackoff, func() (bool, error) {
		restMapping, err := restMapper.RESTMapping(resource.GroupVersionKind().GroupKind(), resource.GroupVersionKind().Version)
		if err != nil {
			klog.ErrorDepth(5, fmt.Sprintf("failed to get RESTMapping: %v", err))
			return false, nil
		}

		_, err = dynamicClient.Resource(restMapping.Resource).Namespace(resource.GetNamespace()).
			Create(context.TODO(), resource, metav1.CreateOptions{})
		if err == nil {
			return true, nil
		}
		if !apierrors.IsAlreadyExists(err) {
			klog.ErrorDepth(5, fmt.Sprintf("failed to create %s %s: %v", resource.GetKind(), klog.KObj(resource), err))
			return false, nil
		}

		// try  to update resource
		_, err = dynamicClient.Resource(restMapping.Resource).Namespace(resource.GetNamespace()).
			Update(context.TODO(), resource, metav1.UpdateOptions{})
		if err == nil {
			return true, nil
		}
		statusCauses, ok := getStatusCause(err)
		if !ok {
			klog.ErrorDepth(5, fmt.Sprintf("failed to get StatusCause for %s %s: %v", resource.GetKind(), klog.KObj(resource), err))
			return false, nil
		}

		curObj, err := dynamicClient.Resource(restMapping.Resource).Namespace(resource.GetNamespace()).
			Get(context.TODO(), resource.GetName(), metav1.GetOptions{})
		if err != nil {
			klog.ErrorDepth(5, fmt.Sprintf("failed to get %s %s: %v", resource.GetKind(), klog.KObj(resource), err))
			return false, nil
		}
		resourceCopy := resource.DeepCopy()
		for _, cause := range statusCauses {
			if cause.Type != metav1.CauseTypeFieldValueInvalid {
				continue
			}
			// apply immutable value
			fields := strings.Split(cause.Field, ".")
			setNestedField(resourceCopy, getNestedString(curObj.Object, fields...), fields...)
		}
		// update with immutable values applied
		_, err = dynamicClient.Resource(restMapping.Resource).Namespace(resourceCopy.GetNamespace()).
			Update(context.TODO(), resourceCopy, metav1.UpdateOptions{})
		if err == nil {
			return true, nil
		}
		klog.ErrorDepth(5, fmt.Sprintf("failed to update %s %s: %v", resource.GetKind(), klog.KObj(resource), err))
		return false, nil
	})
}

func deleteResourceWithRetry(ctx context.Context, dynamicClient dynamic.Interface, restMapper meta.RESTMapper, resource *unstructured.Unstructured) error {
	deletePropagationBackground := metav1.DeletePropagationBackground
	return wait.ExponentialBackoffWithContext(ctx, retry.DefaultBackoff, func() (bool, error) {
		restMapping, err := restMapper.RESTMapping(resource.GroupVersionKind().GroupKind(), resource.GroupVersionKind().Version)
		if err != nil {
			msg := fmt.Sprintf("%v. Please check whether the advertised apiserver of current child cluster is accessible.", err)
			klog.WarningDepth(5, msg)
			return false, errors.New(msg)
		}

		err = dynamicClient.Resource(restMapping.Resource).Namespace(resource.GetNamespace()).
			Delete(context.TODO(), resource.GetName(), metav1.DeleteOptions{PropagationPolicy: &deletePropagationBackground})
		if err == nil || (err != nil && apierrors.IsNotFound(err)) {
			return true, nil
		}
		klog.ErrorDepth(5, "failed to delete %s %s: %v", resource.GetKind(), klog.KObj(resource), err)
		return false, nil
	})
}

// copied from k8s.io/apimachinery/pkg/apis/meta/v1/unstructured
func getNestedString(obj map[string]interface{}, fields ...string) string {
	val, found, err := unstructured.NestedString(obj, fields...)
	if !found || err != nil {
		return ""
	}
	return val
}

// copied from k8s.io/apimachinery/pkg/apis/meta/v1/unstructured
// and modified
func setNestedField(u *unstructured.Unstructured, value interface{}, fields ...string) {
	if u.Object == nil {
		u.Object = make(map[string]interface{})
	}
	err := unstructured.SetNestedField(u.Object, value, fields...)
	if err != nil {
		klog.Warningf("failed to set nested field: %v", err)
	}
}

// getStatusCause returns the named cause from the provided error if it exists and
// the error is of the type APIStatus. Otherwise it returns false.
func getStatusCause(err error) ([]metav1.StatusCause, bool) {
	apierr, ok := err.(apierrors.APIStatus)
	if !ok || apierr == nil || apierr.Status().Details == nil {
		return nil, false
	}
	return apierr.Status().Details.Causes, true
}

func GetDeployerCredentials(ctx context.Context, childKubeClientSet kubernetes.Interface) *corev1.Secret {
	var secret *corev1.Secret
	localCtx, cancel := context.WithCancel(ctx)

	klog.V(4).Infof("get ServiceAccount %s/%s", known.ClusternetSystemNamespace, known.ClusternetAppSA)
	wait.JitterUntilWithContext(localCtx, func(ctx context.Context) {
		sa, err := childKubeClientSet.CoreV1().ServiceAccounts(known.ClusternetSystemNamespace).Get(ctx, known.ClusternetAppSA, metav1.GetOptions{})
		if err != nil {
			klog.ErrorDepth(5, fmt.Errorf("failed to get ServiceAccount %s/%s: %v", known.ClusternetSystemNamespace, known.ClusternetAppSA, err))
			return
		}

		if len(sa.Secrets) == 0 {
			klog.ErrorDepth(5, fmt.Errorf("no secrets found in ServiceAccount %s/%s", known.ClusternetSystemNamespace, known.ClusternetAppSA))
			return
		}

		secret, err = childKubeClientSet.CoreV1().Secrets(known.ClusternetSystemNamespace).Get(ctx, sa.Secrets[0].Name, metav1.GetOptions{})
		if err != nil {
			klog.ErrorDepth(5, fmt.Errorf("failed to get Secret %s/%s: %v", known.ClusternetSystemNamespace, sa.Secrets[0].Name, err))
			return
		}

		cancel()
	}, known.DefaultRetryPeriod, 0.4, true)

	klog.V(4).Info("successfully get credentials populated for deployer")
	return secret
}
