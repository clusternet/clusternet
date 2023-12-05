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
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"

	"github.com/mattbaird/jsonpatch"
	"github.com/pkg/errors"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	pkgruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/jsonmergepatch"
	"k8s.io/apimachinery/pkg/util/mergepatch"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apiserver/pkg/registry/rest"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	kubectlscheme "k8s.io/kubectl/pkg/scheme"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/features"
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
		return false, errors.New("namespace is empty")
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

	if mcls[0].Spec.SyncMode == clusterapi.Pull {
		klog.V(5).Infof("ManagedCluster %s with uid=%s has %s", klog.KObj(mcls[0]), mcls[0].UID, "set syncMode as Pull")
		return false, nil
	}
	if mcls[0].Status.AppPusher == nil {
		return false, fmt.Errorf("unknown AppPusher")
	}
	if !*mcls[0].Status.AppPusher {
		klog.V(5).Infof("ManagedCluster %s with uid=%s has %s", klog.KObj(mcls[0]), mcls[0].UID, "disabled AppPusher")
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

func ReconcileHelmRelease(ctx context.Context, deployCtx *DeployContext, kubeClient *kubernetes.Clientset,
	clusternetClient *clusternetclientset.Clientset,
	hrLister applisters.HelmReleaseLister, descLister applisters.DescriptionLister,
	hr *appsapi.HelmRelease, recorder record.EventRecorder) error {
	if deployCtx == nil {
		return fmt.Errorf("found nil DeployContext")
	}

	klog.V(5).Infof("handle HelmRelease %s", klog.KObj(hr))

	registryClient, err := registry.NewClient(
		registry.ClientOptDebug(Settings.Debug),
		registry.ClientOptWriter(os.Stdout),
		registry.ClientOptCredentialsFile(Settings.RegistryConfig),
	)
	if err != nil {
		return err
	}

	cfg := new(action.Configuration)
	err = cfg.Init(deployCtx, hr.Spec.TargetNamespace, "secret", klog.V(5).Infof)
	if err != nil {
		return err
	}
	cfg.Releases.MaxHistory = 5
	cfg.RegistryClient = registryClient

	hrStatus := &appsapi.HelmReleaseStatus{}
	// delete helm release
	if hr.DeletionTimestamp != nil {
		hrStatus.Phase = release.StatusUninstalling
		if err = UpdateHelmReleaseStatus(ctx, clusternetClient, hrLister, descLister, hr, hrStatus); err != nil {
			return err
		}
		err2 := UninstallRelease(cfg, hr)
		if err2 != nil {
			return err2
		}

		hrCopy := hr.DeepCopy()
		hrCopy.Finalizers = RemoveString(hrCopy.Finalizers, known.AppFinalizer)
		_, err2 = clusternetClient.AppsV1alpha1().HelmReleases(hrCopy.Namespace).Update(context.TODO(), hrCopy, metav1.UpdateOptions{})
		return err2
	}

	// install or upgrade helm release
	var (
		chart    *chart.Chart
		username string
		password string
	)
	if hr.Spec.ChartPullSecret.Name != "" {
		username, password, err = GetHelmRepoCredentials(kubeClient, hr.Spec.ChartPullSecret.Name, hr.Spec.ChartPullSecret.Namespace)
		if err != nil {
			return err
		}
	}
	chart, err = LocateAuthHelmChart(cfg, hr.Spec.Repository, username, password, hr.Spec.Chart, hr.Spec.ChartVersion)
	if err != nil {
		recorder.Event(hr, corev1.EventTypeWarning, "ChartLocateFailure", err.Error())
		hrStatus = &appsapi.HelmReleaseStatus{
			Phase: release.StatusFailed,
			Notes: err.Error(),
		}
		if err = UpdateHelmReleaseStatus(ctx, clusternetClient, hrLister, descLister, hr, hrStatus); err != nil {
			return err
		}
		return err
	}

	releaseName := getReleaseName(hr)
	var overrideValues map[string]interface{}
	if len(strings.TrimSpace(string(hr.Spec.Overrides))) > 0 {
		if err = json.Unmarshal(hr.Spec.Overrides, &overrideValues); err != nil {
			return err
		}
	}

	var rel *release.Release
	rel, err = cfg.Releases.Last(releaseName)
	if err != nil && !strings.Contains(err.Error(), driver.ErrReleaseNotFound.Error()) {
		return err
	}

	// Install or upgrade failed and atomic is set, uninstalling release
	if hr.Spec.Atomic != nil && *hr.Spec.Atomic && rel != nil && rel.Info.Status != release.StatusDeployed {
		klog.V(5).Infof("Uninstalling undeployed HelmRelease %s", klog.KObj(hr))
		hrStatus.Phase = release.StatusUninstalling
		if err = UpdateHelmReleaseStatus(ctx, clusternetClient, hrLister, descLister, hr, hrStatus); err != nil {
			return err
		}
		err = UninstallRelease(cfg, hr)
		if err != nil {
			return err
		}
		rel = nil
	}

	if rel == nil {
		klog.V(5).Infof("Installing HelmRelease %s", klog.KObj(hr))
		hrStatus.Phase = release.StatusPendingInstall
		if err = UpdateHelmReleaseStatus(ctx, clusternetClient, hrLister, descLister, hr, hrStatus); err != nil {
			return err
		}
		rel, err = InstallRelease(cfg, hr, chart, overrideValues)
	} else {
		// verify the release is changed or not
		if ReleaseNeedsUpgrade(rel, hr, chart, overrideValues) {
			klog.V(5).Infof("Upgrading HelmRelease %s", klog.KObj(hr))
			hrStatus.Phase = release.StatusPendingUpgrade
			if err = UpdateHelmReleaseStatus(ctx, clusternetClient, hrLister, descLister, hr, hrStatus); err != nil {
				return err
			}
			rel, err = UpgradeRelease(cfg, hr, chart, overrideValues)
		} else {
			klog.V(5).Infof("HelmRelease %s is already updated. No need upgrading.", klog.KObj(hr))
		}
	}

	if err != nil {
		// repo update
		if strings.Contains(err.Error(), "helm repo update") {
			err2 := UpdateRepo(hr.Spec.Repository)
			if err2 == nil {
				// return an error to let it reconcile
				err2 = fmt.Errorf("[Helm Repo Update] requeue HelmRelease %s", klog.KObj(hr))
			}
			return err2
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

	err3 := UpdateHelmReleaseStatus(ctx, clusternetClient,
		hrLister, descLister, hr, hrStatus)
	if err3 != nil {
		// above "err" may not be nil, but we still need to update HelmRelease status
		// "err" will aggregate "err3" as well
		err = err3
	}
	return err
}

func GenerateHelmReleaseName(descName string, chartRef appsapi.ChartReference) string {
	return fmt.Sprintf("%s-%s-%s", descName, chartRef.Namespace, chartRef.Name)
}

func UpdateHelmReleaseStatus(ctx context.Context, clusternetClient *clusternetclientset.Clientset,
	hrLister applisters.HelmReleaseLister, descLister applisters.DescriptionLister,
	hr *appsapi.HelmRelease, status *appsapi.HelmReleaseStatus) error {
	if hr == nil || status == nil {
		return nil
	}

	// NEVER modify objects from the store. It's a read-only, local cache.
	// You can use DeepCopy() to make a deep copy of original object and modify this copy
	// Or create a copy manually for better performance
	hr = hr.DeepCopy()
	err := wait.ExponentialBackoffWithContext(ctx, retry.DefaultRetry, func(ctx context.Context) (done bool, err error) {
		klog.V(5).Infof("try to update HelmRelease %q status", hr.Name)
		hr.Status = *status
		_, err = clusternetClient.AppsV1alpha1().HelmReleases(hr.Namespace).UpdateStatus(ctx, hr, metav1.UpdateOptions{})
		if err == nil {
			return true, nil
		}

		updated, err2 := hrLister.HelmReleases(hr.Namespace).Get(hr.Name)
		if err2 == nil {
			// make a copy, so we don't mutate the shared cache
			hr = updated.DeepCopy()
			return false, nil
		}
		utilruntime.HandleError(fmt.Errorf("error getting updated HelmRelease %q from lister: %v", hr.Name, err2))
		return false, err
	})

	if err != nil {
		return err
	}

	// update status of controller owner Description
	controllerRef := metav1.GetControllerOf(hr)
	if controllerRef == nil {
		// No controller should care about orphans being deleted.
		return nil
	}
	desc := resolveControllerRef(descLister, hr.Namespace, controllerRef)
	if desc == nil {
		return nil
	}
	descStatus := desc.Status.DeepCopy()
	switch status.Phase {
	case release.StatusDeployed:
		descStatus.Phase = appsapi.DescriptionPhaseSuccess
		descStatus.Reason = ""
	case release.StatusPendingInstall:
		descStatus.Phase = appsapi.DescriptionPhaseInstalling
	case release.StatusPendingUpgrade:
		descStatus.Phase = appsapi.DescriptionPhaseUpgrading
	case release.StatusUninstalling:
		descStatus.Phase = appsapi.DescriptionPhaseUninstalling
	case release.StatusSuperseded:
		descStatus.Phase = appsapi.DescriptionPhaseSuperseded
		descStatus.Reason = status.Notes
	case release.StatusFailed:
		descStatus.Phase = appsapi.DescriptionPhaseFailure
		descStatus.Reason = status.Notes
	default:
		descStatus.Phase = appsapi.DescriptionPhaseUnknown
		descStatus.Reason = status.Notes
	}

	klog.V(5).Infof("try to update HelmRelease %q owner Description status", hr.Name)
	return UpdateDescriptionStatus(ctx, desc, descStatus, clusternetClient, true)
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
	return desc.DeepCopy()
}

type ResourceCallbackHandler func(resource *unstructured.Unstructured) error

func ApplyDescription(ctx context.Context, clusternetClient *clusternetclientset.Clientset, dynamicClient dynamic.Interface,
	discoveryRESTMapper meta.RESTMapper, desc *appsapi.Description, recorder record.EventRecorder, dryApply bool,
	callbackHandler ResourceCallbackHandler, ignoreAdd bool) error {
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
			continue
		}

		// erases fields that are managed by the system on ObjectMeta when deploying to child clusters
		rest.WipeObjectMetaSystemFields(resource)

		annotations := resource.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		// remove kubectl last-applied-config annotation
		delete(annotations, corev1.LastAppliedConfigAnnotation)
		annotations[known.ObjectOwnedByDescriptionAnnotation] = desc.Namespace + "." + desc.Name
		resource.SetAnnotations(annotations)

		trimedObject, err := resource.MarshalJSON()
		if err != nil {
			allErrs = append(allErrs, err)
			msg := fmt.Sprintf("failed to unmarshal resource: %v", err)
			klog.ErrorDepth(5, msg)
			recorder.Event(desc, corev1.EventTypeWarning, "FailedMarshalingResource", msg)
			continue
		}
		// add clusternet-agent last-applied-config annotation
		annotations[known.LastAppliedConfigAnnotation] = string(trimedObject)
		resource.SetAnnotations(annotations)
		wg.Add(1)
		go func(resource *unstructured.Unstructured) {
			defer wg.Done()

			// dryApply means do not apply resources, just add sub resource watcher.
			if !dryApply {
				retryErr := ApplyResourceWithRetry(ctx, dynamicClient, discoveryRESTMapper, resource, ignoreAdd)
				if retryErr != nil {
					errCh <- retryErr
					return
				}
			}

			if utilfeature.DefaultFeatureGate.Enabled(features.Recovery) && callbackHandler != nil {
				// callbackHandler cannot block, otherwise the subsequent events of the description cannot be processed
				callbackErr := callbackHandler(resource)
				if callbackErr != nil {
					errCh <- callbackErr
					return
				}
			}
		}(resource)

	}
	wg.Wait()

	// collect errors
	close(errCh)
	for err := range errCh {
		allErrs = append(allErrs, err)
	}

	// in dry apply case no need to update description status.
	if dryApply {
		if len(allErrs) > 0 {
			return utilerrors.NewAggregate(allErrs)
		}
		return nil
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
	descStatus := desc.Status.DeepCopy()
	descStatus.Phase = statusPhase
	descStatus.Reason = reason

	var err error
	if !reflect.DeepEqual(desc.Status.Phase, descStatus.Phase) || !reflect.DeepEqual(desc.Status.Reason, descStatus.Reason) {
		err = UpdateDescriptionStatus(ctx, desc, descStatus, clusternetClient, true)
		klog.V(5).Infof("ApplyDescription phaseStatus has changed, UpdateStatus. err: %s", err)
	}

	if len(allErrs) > 0 {
		return utilerrors.NewAggregate(allErrs)
	}
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
		err = resource.UnmarshalJSON(object)
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
				err = DeleteResourceWithRetry(ctx, dynamicClient, discoveryRESTMapper, resource)
				if err != nil {
					errCh <- err
				}
			}(resource)
		}
	}
	wg.Wait()

	// collect errors
	close(errCh)
	for err = range errCh {
		allErrs = append(allErrs, err)
	}

	err = utilerrors.NewAggregate(allErrs)
	if err != nil {
		msg := fmt.Sprintf("failed to deleting Description %s: %v", klog.KObj(desc), err)
		klog.ErrorDepth(5, msg)
		recorder.Event(desc, corev1.EventTypeWarning, "FailedDeletingDescription", msg)
	} else {
		klog.V(5).Infof("Description %s is deleted successfully", klog.KObj(desc))
		descCopy := desc.DeepCopy()
		descCopy.Finalizers = RemoveString(descCopy.Finalizers, known.AppFinalizer)
		_, err = clusternetClient.AppsV1alpha1().Descriptions(descCopy.Namespace).Update(context.TODO(), descCopy, metav1.UpdateOptions{})
		if err != nil {
			klog.WarningDepth(4,
				fmt.Sprintf("failed to remove finalizer %s from Description %s: %v", known.AppFinalizer, klog.KObj(descCopy), err))

		}
	}
	return err
}

func ApplyResourceWithRetry(ctx context.Context, dynamicClient dynamic.Interface, restMapper meta.RESTMapper,
	resource *unstructured.Unstructured, ignoreAdd bool) error {
	var lastError error
	err := wait.ExponentialBackoffWithContext(ctx, retry.DefaultBackoff, func(ctx context.Context) (bool, error) {
		restMapping, err := restMapper.RESTMapping(resource.GroupVersionKind().GroupKind(), resource.GroupVersionKind().Version)
		if err != nil {
			lastError = fmt.Errorf("please check whether the advertised apiserver of current child cluster is accessible. %v", err)
			return false, nil
		}

		_, lastError = dynamicClient.Resource(restMapping.Resource).Namespace(resource.GetNamespace()).
			Create(context.TODO(), resource, metav1.CreateOptions{})
		if lastError == nil {
			return true, nil
		}
		if !apierrors.IsAlreadyExists(lastError) {
			return false, nil
		}

		curObj, err := dynamicClient.Resource(restMapping.Resource).Namespace(resource.GetNamespace()).
			Get(context.TODO(), resource.GetName(), metav1.GetOptions{})
		if err != nil {
			lastError = err
			return false, nil
		} else {
			lastError = nil
		}
		if ResourceNeedResync(resource, curObj, ignoreAdd) {
			// try to update resource
			lastError = doApplyPatch(ctx, dynamicClient, restMapper, resource, curObj)
			if lastError == nil {
				return true, nil
			}
			statusCauses, ok := getStatusCause(lastError)
			if !ok {
				lastError = fmt.Errorf("failed to get StatusCause for %s %s", resource.GetKind(), klog.KObj(resource))
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
			// try to update resource
			lastError = doApplyPatch(ctx, dynamicClient, restMapper, resource, curObj)
			if lastError == nil {
				return true, nil
			}
		}
		return false, nil
	})

	if err == nil {
		return nil
	}
	return lastError
}

func getOriginalConfiguration(current *unstructured.Unstructured) []byte {
	annots := current.GetAnnotations()
	if annots == nil {
		return nil
	}

	original, ok := annots[known.LastAppliedConfigAnnotation]
	if !ok {
		return nil
	}
	return []byte(original)
}

func doApplyPatch(
	ctx context.Context,
	dynamicClient dynamic.Interface, restMapper meta.RESTMapper,
	target, current *unstructured.Unstructured) error {
	curData, err := json.Marshal(current)
	if err != nil {
		return errors.Wrap(err, "serializing current configuration")
	}
	newData, err := json.Marshal(target.Object)
	if err != nil {
		return errors.Wrap(err, "serializing target configuration")
	}
	originalData := getOriginalConfiguration(current)
	restMapping, err := restMapper.RESTMapping(target.GroupVersionKind().GroupKind(), target.GroupVersionKind().Version)
	if err != nil {
		return errors.Wrap(err, "please check whether the advertised apiserver of current child cluster is accessible")
	}

	// Refer to the implementation of kubectl patcher
	var patchType types.PatchType
	var patch []byte
	var lookupPatchMeta strategicpatch.LookupPatchMeta

	versionedObject, err := kubectlscheme.Scheme.New(restMapping.GroupVersionKind)
	switch {
	case pkgruntime.IsNotRegisteredError(err):
		// fall back to generic JSON merge patch
		patchType = types.MergePatchType
		preconditions := []mergepatch.PreconditionFunc{mergepatch.RequireKeyUnchanged("apiVersion"),
			mergepatch.RequireKeyUnchanged("kind"), mergepatch.RequireMetadataKeyUnchanged("name")}
		patch, err = jsonmergepatch.CreateThreeWayJSONMergePatch(originalData, newData, curData, preconditions...)
		if err != nil {
			if mergepatch.IsPreconditionFailed(err) {
				return fmt.Errorf("%s", "At least one of apiVersion, kind and name was changed")
			}
			klog.Errorf("create jsonmergepath failed for gvk %s resource %s/%s, err %s",
				restMapping.GroupVersionKind, target.GetNamespace(), target.GetName(), err.Error())
			return err
		}
	case err != nil:
		klog.Errorf("getting instance of versioned object for %v, err %s", restMapping.GroupVersionKind, err.Error())
		return fmt.Errorf("getting instance of versioned object for %v, err %s",
			restMapping.GroupVersionKind, err.Error())
	case err == nil:
		// Compute a three way strategic merge patch to send to server.
		// TODO: Try to use openapi first if the openapi spec is available
		patchType = types.StrategicMergePatchType
		lookupPatchMeta, err = strategicpatch.NewPatchMetaFromStruct(versionedObject)
		if err != nil {
			klog.Errorf("get patch meta failed for gvk %s resource %s/%s, err %s",
				restMapping.GroupVersionKind, target.GetNamespace(), target.GetName(), err.Error())
			return err
		}
		patch, err = strategicpatch.CreateThreeWayMergePatch(originalData, newData, curData, lookupPatchMeta, true)
		if err != nil {
			klog.Errorf("create three way merge patch failed for gvk %s resource %s/%s, err %s",
				restMapping.GroupVersionKind, target.GetNamespace(), target.GetName(), err.Error())
			return err
		}
	}

	if string(patch) == "{}" {
		return nil
	}
	// try to update resource
	_, err = dynamicClient.Resource(restMapping.Resource).Namespace(target.GetNamespace()).
		Patch(ctx, target.GetName(), patchType, patch, metav1.PatchOptions{})
	if err != nil {
		klog.Errorf("call patch resource %s/%s failed, patch type %s, err %s",
			target.GetNamespace(), target.GetName(), patchType, err.Error())
		// return original error
		return err
	}
	return nil
}

func DeleteResourceWithRetry(ctx context.Context, dynamicClient dynamic.Interface, restMapper meta.RESTMapper, resource *unstructured.Unstructured) error {
	deletePropagationBackground := metav1.DeletePropagationBackground

	var lastError error
	err := wait.ExponentialBackoffWithContext(ctx, retry.DefaultBackoff, func(ctx context.Context) (bool, error) {
		restMapping, err := restMapper.RESTMapping(resource.GroupVersionKind().GroupKind(), resource.GroupVersionKind().Version)
		if err != nil {
			lastError = fmt.Errorf("please check whether the advertised apiserver of current child cluster is accessible. %v", err)
			return false, nil
		}

		lastError = dynamicClient.Resource(restMapping.Resource).Namespace(resource.GetNamespace()).
			Delete(context.TODO(), resource.GetName(), metav1.DeleteOptions{PropagationPolicy: &deletePropagationBackground})
		if lastError == nil || (lastError != nil && apierrors.IsNotFound(lastError)) {
			return true, nil
		}
		return false, nil
	})
	if err == nil {
		return nil
	}
	return lastError
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

func GetDeployerCredentials(ctx context.Context, childKubeClientSet kubernetes.Interface, systemNamespace string, saTokenAutoGen bool) *corev1.Secret {
	var secret *corev1.Secret
	localCtx, cancel := context.WithCancel(ctx)

	klog.V(4).Infof("get ServiceAccount %s/%s", systemNamespace, known.ClusternetAppSA)
	wait.JitterUntilWithContext(localCtx, func(ctx context.Context) {
		secretName := known.ClusternetAppSA
		if saTokenAutoGen {
			sa, err := childKubeClientSet.CoreV1().ServiceAccounts(systemNamespace).Get(ctx, known.ClusternetAppSA, metav1.GetOptions{})
			if err != nil {
				klog.ErrorDepth(5, fmt.Errorf("failed to get ServiceAccount %s/%s: %v", systemNamespace, known.ClusternetAppSA, err))
				return
			}
			if len(sa.Secrets) == 0 {
				klog.ErrorDepth(5, fmt.Errorf("no secrets found in ServiceAccount %s/%s", systemNamespace, known.ClusternetAppSA))
				return
			}
			secretName = sa.Secrets[0].Name
		}

		var err error
		secret, err = childKubeClientSet.CoreV1().Secrets(systemNamespace).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			klog.ErrorDepth(5, fmt.Errorf("failed to get Secret %s/%s: %v", systemNamespace, secretName, err))
			return
		}

		cancel()
	}, known.DefaultRetryPeriod, 0.4, true)

	klog.V(4).Info("successfully get credentials populated for deployer")
	return secret
}

func IsClusterLost(clusterID, namespace string, clusterLister clusterlisters.ManagedClusterLister) bool {
	labelSet := labels.Set{}
	if len(clusterID) > 0 {
		labelSet[known.ClusterIDLabel] = clusterID
	}
	mcls, err := clusterLister.ManagedClusters(namespace).List(
		labels.SelectorFromSet(labelSet))
	if err != nil {
		klog.ErrorDepth(4, fmt.Sprintf("failed to list ManagedCluster in namespace %s: %v", namespace, err))
		return false
	}
	if len(mcls) == 0 {
		klog.WarningDepth(4, fmt.Sprintf("no ManagedCluster found in namespace %s", namespace))
		return true
	}

	// short-circuit
	// in case the conditions are not updated yet
	if len(mcls[0].Status.Conditions) == 0 {
		return false
	}

	return !ClusterHasReadyCondition(mcls[0])
}

func ClusterHasReadyCondition(mc *clusterapi.ManagedCluster) bool {
	// in case the conditions are not updated yet
	if len(mc.Status.Conditions) == 0 {
		return false
	}

	for _, condition := range mc.Status.Conditions {
		if condition.Type == clusterapi.ClusterReady {
			return condition.Status == metav1.ConditionTrue
		}
	}

	return false
}

func fieldsToBeIgnored() []string {
	return []string{
		known.MetaGeneration,
		known.CreationTimestamp,
		known.ManagedFields,
		known.MetaUID,
		known.MetaSelflink,
		known.MetaResourceVersion,
	}
}

func sectionToBeIgnored() []string {
	return []string{
		known.SectionStatus,
	}
}

// shouldPatchBeIgnored used to decide if this patch operation should be ignored.
func shouldPatchBeIgnored(operation jsonpatch.JsonPatchOperation) bool {
	// some fields need to be ignore like meta.selfLink, meta.resourceVersion.
	if ContainsString(fieldsToBeIgnored(), operation.Path) {
		return true
	}
	// some sections like status section need to be ignored.
	if ContainsPrefix(sectionToBeIgnored(), operation.Path) {
		return true
	}

	return false
}

// ResourceNeedResync will compare fields and decide whether to sync back the current object.
//
// current is deployed resource, modified is changed resource.
// ignoreAdd is true if you want to ignore add action.
// The function will return the bool value to indicate whether to sync back the current object.
func ResourceNeedResync(current pkgruntime.Object, modified pkgruntime.Object, ignoreAdd bool) bool {
	currentBytes, err := json.Marshal(current)
	if err != nil {
		klog.ErrorDepth(5, fmt.Sprintf("Error marshal json: %v", err))
		return false
	}

	modifiedBytes, err := json.Marshal(modified)
	if err != nil {
		klog.ErrorDepth(5, fmt.Sprintf("Error marshal json: %v", err))
		return false
	}

	patch, err := jsonpatch.CreatePatch(currentBytes, modifiedBytes)
	if err != nil {
		klog.ErrorDepth(5, fmt.Sprintf("Error creating JSON patch: %v", err))
		return false
	}
	for _, operation := range patch {
		// filter ignored paths
		if shouldPatchBeIgnored(operation) {
			continue
		}

		switch operation.Operation {
		case "add":
			if ignoreAdd {
				continue
			} else {
				return true
			}
		case "remove", "replace":
			return true
		default:
			// skip other operations, like "copy", "move" and "test"
			continue
		}
	}

	return false
}

func UpdateDescriptionStatus(
	ctx context.Context,
	desc *appsapi.Description,
	status *appsapi.DescriptionStatus,
	clusternetClient *clusternetclientset.Clientset,
	deployerTrigger bool,
) error {
	if desc == nil || status == nil {
		return nil
	}

	if !deployerTrigger && reflect.DeepEqual(desc.Status, appsapi.DescriptionStatus{}) {
		return fmt.Errorf("waiting for the deployer to update description status first")
	}

	// NEVER modify objects from the store. It's a read-only, local cache.
	// You can use DeepCopy() to make a deep copy of original object and modify this copy
	// Or create a copy manually for better performance
	desc = desc.DeepCopy()
	return wait.ExponentialBackoffWithContext(ctx, retry.DefaultRetry, func(ctx context.Context) (done bool, err error) {
		klog.V(5).Infof("try to update Description %q status", desc.Name)
		desc.Status = *status
		_, err = clusternetClient.AppsV1alpha1().Descriptions(desc.Namespace).UpdateStatus(ctx, desc, metav1.UpdateOptions{})
		if err == nil {
			//TODO
			return true, nil
		}

		updated, err2 := clusternetClient.AppsV1alpha1().Descriptions(desc.Namespace).Get(ctx, desc.Name, metav1.GetOptions{})
		if err2 == nil {
			// make a copy, so we don't mutate the shared cache
			desc = updated.DeepCopy()
			return false, nil
		}
		utilruntime.HandleError(fmt.Errorf("error getting updated Description %q: %v", desc.Name, err2))
		return false, err
	})
}

func BaseUidIndexFunc(obj interface{}) ([]string, error) {
	base, ok := obj.(*appsapi.Base)
	if !ok {
		return nil, fmt.Errorf("object is not a Base %#v", obj)
	}
	return []string{string(base.UID)}, nil
}

func BaseSubUidIndexFunc(obj interface{}) ([]string, error) {
	base, ok := obj.(*appsapi.Base)
	if !ok {
		return nil, fmt.Errorf("object is not a Base %#v", obj)
	}

	subUid, ok := base.Labels[known.ConfigSubscriptionUIDLabel]
	if !ok {
		return nil, fmt.Errorf("no subUid found for Base %#v", obj)
	}
	return []string{subUid}, nil
}

func SubUidIndexFunc(obj interface{}) ([]string, error) {
	sub, ok := obj.(*appsapi.Subscription)
	if !ok {
		return nil, fmt.Errorf("object is not a Subscription %#v", obj)
	}
	return []string{string(sub.UID)}, nil
}
