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

package helm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/repo"
	"helm.sh/helm/v3/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	utilpointer "k8s.io/utils/pointer"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/controllers/apps/description"
	"github.com/clusternet/clusternet/pkg/controllers/apps/helmchart"
	"github.com/clusternet/clusternet/pkg/controllers/apps/helmrelease"
	"github.com/clusternet/clusternet/pkg/controllers/misc/secret"
	clusternetclientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	clusternetinformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	clusterlisters "github.com/clusternet/clusternet/pkg/generated/listers/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

var (
	descriptionKind = appsapi.SchemeGroupVersion.WithKind("Description")
)

type Deployer struct {
	ctx context.Context

	helmChartController   *helmchart.Controller
	helmReleaseController *helmrelease.Controller
	descriptionController *description.Controller

	secretController *secret.Controller

	clusternetClient *clusternetclientset.Clientset
	kubeClient       *kubernetes.Clientset

	chartLister   applisters.HelmChartLister
	chartSynced   cache.InformerSynced
	hrLister      applisters.HelmReleaseLister
	hrSynced      cache.InformerSynced
	descLister    applisters.DescriptionLister
	descSynced    cache.InformerSynced
	clusterLister clusterlisters.ManagedClusterLister
	clusterSynced cache.InformerSynced

	secretLister corev1lister.SecretLister
	secretSynced cache.InformerSynced

	recorder record.EventRecorder
}

func NewDeployer(ctx context.Context,
	clusternetClient *clusternetclientset.Clientset, kubeClient *kubernetes.Clientset,
	clusternetInformerFactory clusternetinformers.SharedInformerFactory,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	recorder record.EventRecorder) (*Deployer, error) {

	deployer := &Deployer{
		ctx:              ctx,
		clusternetClient: clusternetClient,
		kubeClient:       kubeClient,
		chartLister:      clusternetInformerFactory.Apps().V1alpha1().HelmCharts().Lister(),
		chartSynced:      clusternetInformerFactory.Apps().V1alpha1().HelmCharts().Informer().HasSynced,
		hrLister:         clusternetInformerFactory.Apps().V1alpha1().HelmReleases().Lister(),
		hrSynced:         clusternetInformerFactory.Apps().V1alpha1().HelmReleases().Informer().HasSynced,
		descLister:       clusternetInformerFactory.Apps().V1alpha1().Descriptions().Lister(),
		descSynced:       clusternetInformerFactory.Apps().V1alpha1().Descriptions().Informer().HasSynced,
		clusterLister:    clusternetInformerFactory.Clusters().V1beta1().ManagedClusters().Lister(),
		clusterSynced:    clusternetInformerFactory.Clusters().V1beta1().ManagedClusters().Informer().HasSynced,
		secretLister:     kubeInformerFactory.Core().V1().Secrets().Lister(),
		secretSynced:     kubeInformerFactory.Core().V1().Secrets().Informer().HasSynced,
		recorder:         recorder,
	}

	helmChartController, err := helmchart.NewController(ctx, clusternetClient,
		clusternetInformerFactory.Apps().V1alpha1().HelmCharts(),
		deployer.recorder, deployer.handleHelmChart)
	if err != nil {
		return nil, err
	}
	deployer.helmChartController = helmChartController

	hrController, err := helmrelease.NewController(ctx,
		clusternetClient,
		clusternetInformerFactory.Apps().V1alpha1().Descriptions(),
		clusternetInformerFactory.Apps().V1alpha1().HelmReleases(),
		deployer.handleHelmRelease)
	if err != nil {
		return nil, err
	}
	deployer.helmReleaseController = hrController

	secretController, err := secret.NewController(ctx,
		kubeClient,
		kubeInformerFactory.Core().V1().Secrets(),
		deployer.handleSecret)
	if err != nil {
		return nil, err
	}
	deployer.secretController = secretController

	descController, err := description.NewController(ctx,
		clusternetClient,
		clusternetInformerFactory.Apps().V1alpha1().Descriptions(),
		clusternetInformerFactory.Apps().V1alpha1().HelmReleases(),
		deployer.recorder,
		deployer.handleDescription)
	if err != nil {
		return nil, err
	}
	deployer.descriptionController = descController

	return deployer, nil
}

func (deployer *Deployer) Run(workers int) {
	klog.Info("starting helm deployer...")
	defer klog.Info("shutting helm deployer")

	// Wait for the caches to be synced before starting workers
	klog.V(5).Info("waiting for informer caches to sync")
	if !cache.WaitForCacheSync(deployer.ctx.Done(),
		deployer.chartSynced,
		deployer.hrSynced,
		deployer.descSynced,
		deployer.clusterSynced,
		deployer.secretSynced) {
		return
	}

	go deployer.helmChartController.Run(workers, deployer.ctx.Done())
	go deployer.helmReleaseController.Run(workers, deployer.ctx.Done())
	go deployer.descriptionController.Run(workers, deployer.ctx.Done())
	// 1 worker may get hang up, so we set minimum 2 workers here
	go deployer.secretController.Run(2, deployer.ctx.Done())

	<-deployer.ctx.Done()
}

func (deployer *Deployer) handleDescription(desc *appsapi.Description) error {
	if desc.Spec.Deployer != appsapi.DescriptionHelmDeployer {
		return nil
	}

	// TODO: may ignore checking AppPusher for helm charts?
	// check whether ManagedCluster will enable deploying Description with Pusher/Dual mode
	labelSet := labels.Set{}
	if len(desc.Labels[known.ClusterIDLabel]) > 0 {
		labelSet[known.ClusterIDLabel] = desc.Labels[known.ClusterIDLabel]
	}
	mcls, err := deployer.clusterLister.ManagedClusters(desc.Namespace).List(
		labels.SelectorFromSet(labelSet))
	if err != nil {
		return err
	}
	if mcls == nil {
		deployer.recorder.Event(desc, corev1.EventTypeWarning, "ManagedClusterNotFound",
			fmt.Sprintf("can not find a ManagedCluster with uid=%s in current namespace", desc.Labels[known.ClusterIDLabel]))
		return fmt.Errorf("failed to find a ManagedCluster declaration in namespace %s", desc.Namespace)
	}
	if !mcls[0].Status.AppPusher {
		deployer.recorder.Event(desc, corev1.EventTypeNormal, "", "target cluster has disabled AppPusher")
		klog.V(5).Infof("ManagedCluster with uid=%s has disabled AppPusher", mcls[0].UID)
		return nil
	}

	if desc.DeletionTimestamp != nil {
		// make sure all controllees have been deleted
		hrs, err := deployer.hrLister.HelmReleases(desc.Namespace).List(labels.SelectorFromSet(labels.Set{
			known.ConfigKindLabel:      descriptionKind.Kind,
			known.ConfigNameLabel:      desc.Name,
			known.ConfigNamespaceLabel: desc.Namespace,
			known.ConfigUIDLabel:       string(desc.UID),
		}))
		if err != nil {
			return err
		}

		var allErrs []error
		for _, hr := range hrs {
			if hr.DeletionTimestamp != nil {
				continue
			}

			if err := deployer.deleteHelmRelease(context.TODO(), klog.KObj(hr).String()); err != nil {
				klog.ErrorDepth(5, err)
				allErrs = append(allErrs, err)
				continue
			}
		}

		if hrs != nil || len(allErrs) > 0 {
			return fmt.Errorf("waiting for HelmRelease belongs to Description %s getting deleted", klog.KObj(desc))
		}

		desc.Finalizers = utils.RemoveString(desc.Finalizers, known.AppFinalizer)
		_, err = deployer.clusternetClient.AppsV1alpha1().Descriptions(desc.Namespace).Update(context.TODO(), desc, metav1.UpdateOptions{})
		if err != nil {
			klog.WarningDepth(4,
				fmt.Sprintf("failed to remove finalizer %s from Description %s: %v", known.AppFinalizer, klog.KObj(desc), err))
		}
		return err
	}

	if err := deployer.PopulateHelmRelease(desc); err != nil {
		return err
	}

	return nil
}

func (deployer *Deployer) handleHelmChart(chart *appsapi.HelmChart) error {
	if chart.DeletionTimestamp != nil {
		// remove finalizers
		chart.Finalizers = utils.RemoveString(chart.Finalizers, known.AppFinalizer)
		chart.Finalizers = utils.RemoveString(chart.Finalizers, known.FeedProtectionFinalizer)
		_, err := deployer.clusternetClient.AppsV1alpha1().HelmCharts(chart.Namespace).Update(context.TODO(), chart, metav1.UpdateOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			klog.WarningDepth(4,
				fmt.Sprintf("failed to remove finalizers from HelmChart %s: %v", klog.KObj(chart), err))
		}
		return err
	}

	_, err := repo.FindChartInRepoURL(chart.Spec.Repository, chart.Spec.Chart, chart.Spec.ChartVersion,
		"", "", "",
		getter.All(settings))
	if err != nil {
		// failed to find chart
		return deployer.helmChartController.UpdateChartStatus(chart, &appsapi.HelmChartStatus{
			Phase:  appsapi.HelmChartNotFound,
			Reason: err.Error(),
		})
	}
	return deployer.helmChartController.UpdateChartStatus(chart, &appsapi.HelmChartStatus{
		Phase: appsapi.HelmChartFound,
	})
}

func (deployer *Deployer) PopulateHelmRelease(desc *appsapi.Description) error {
	allExistingHelmReleases, err := deployer.hrLister.List(labels.SelectorFromSet(labels.Set{
		known.ConfigKindLabel:      desc.Kind,
		known.ConfigNameLabel:      desc.Name,
		known.ConfigNamespaceLabel: desc.Namespace,
		known.ConfigUIDLabel:       string(desc.UID),
	}))
	if err != nil {
		return err
	}
	// HelmReleases to be deleted
	hrsToBeDeleted := sets.String{}
	for _, hr := range allExistingHelmReleases {
		hrsToBeDeleted.Insert(klog.KObj(hr).String())
	}

	var allErrs []error
	for _, chartRef := range desc.Spec.Charts {
		chart, err := deployer.chartLister.HelmCharts(chartRef.Namespace).Get(chartRef.Name)
		if err != nil {
			return err
		}

		hr := &appsapi.HelmRelease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s", desc.Name, chartRef.Name),
				Namespace: desc.Namespace,
				Labels: map[string]string{
					known.ObjectCreatedByLabel: known.ClusternetHubName,
					known.ConfigKindLabel:      descriptionKind.Kind,
					known.ConfigNameLabel:      desc.Name,
					known.ConfigNamespaceLabel: desc.Namespace,
					known.ConfigUIDLabel:       string(desc.UID),
					known.ClusterIDLabel:       desc.Labels[known.ClusterIDLabel],
					known.ClusterNameLabel:     desc.Labels[known.ClusterNameLabel],
					// add subscription info
					known.ConfigSubscriptionNameLabel:      desc.Labels[known.ConfigSubscriptionNameLabel],
					known.ConfigSubscriptionNamespaceLabel: desc.Labels[known.ConfigSubscriptionNamespaceLabel],
					known.ConfigSubscriptionUIDLabel:       desc.Labels[known.ConfigSubscriptionUIDLabel],
				},
				Finalizers: []string{
					known.AppFinalizer,
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion:         descriptionKind.Version,
						Kind:               descriptionKind.Kind,
						Name:               desc.Name,
						UID:                desc.UID,
						Controller:         utilpointer.BoolPtr(true),
						BlockOwnerDeletion: utilpointer.BoolPtr(true),
					},
				},
			},
			Spec: appsapi.HelmReleaseSpec{
				TargetNamespace: chart.Spec.TargetNamespace,
				HelmOptions:     chart.Spec.HelmOptions,
			},
		}
		hrsToBeDeleted.Delete(klog.KObj(hr).String())

		err = deployer.syncHelmRelease(desc, hr)
		if err != nil {
			allErrs = append(allErrs, err)
			msg := fmt.Sprintf("Failed to sync HelmRelease %s: %v", klog.KObj(hr), err)
			klog.ErrorDepth(5, msg)
			deployer.recorder.Event(desc, corev1.EventTypeWarning, "HelmReleaseFailure", msg)
		}
	}

	for key := range hrsToBeDeleted {
		err := deployer.deleteHelmRelease(context.TODO(), key)
		if err != nil {
			allErrs = append(allErrs, err)
		}
	}

	return utilerrors.NewAggregate(allErrs)
}

func (deployer *Deployer) syncHelmRelease(desc *appsapi.Description, helmRelease *appsapi.HelmRelease) error {
	hr, err := deployer.hrLister.HelmReleases(helmRelease.Namespace).Get(helmRelease.Name)
	if err == nil {
		if hr.DeletionTimestamp != nil {
			return fmt.Errorf("HelmRelease %s is deleting, will resync later", klog.KObj(hr))
		}

		// update it
		if !reflect.DeepEqual(hr.Spec, helmRelease.Spec) {
			if hr.Labels == nil {
				hr.Labels = make(map[string]string)
			}
			for key, value := range helmRelease.Labels {
				hr.Labels[key] = value
			}

			hr.Spec = helmRelease.Spec
			if !utils.ContainsString(hr.Finalizers, known.AppFinalizer) {
				hr.Finalizers = append(hr.Finalizers, known.AppFinalizer)
			}

			_, err = deployer.clusternetClient.AppsV1alpha1().HelmReleases(hr.Namespace).Update(context.TODO(),
				hr, metav1.UpdateOptions{})
			if err == nil {
				msg := fmt.Sprintf("HelmReleases %s is updated successfully", klog.KObj(hr))
				klog.V(4).Info(msg)
				deployer.recorder.Event(desc, corev1.EventTypeNormal, "HelmReleaseUpdated", msg)
			}
			return err
		}
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	_, err = deployer.clusternetClient.AppsV1alpha1().HelmReleases(helmRelease.Namespace).Create(context.TODO(),
		helmRelease, metav1.CreateOptions{})
	if err == nil {
		msg := fmt.Sprintf("HelmReleases %s is created successfully", klog.KObj(helmRelease))
		klog.V(4).Info(msg)
		deployer.recorder.Event(desc, corev1.EventTypeNormal, "HelmReleasesCreated", msg)
	}
	return err
}

func (deployer *Deployer) deleteHelmRelease(ctx context.Context, namespacedKey string) error {
	// Convert the namespace/name string into a distinct namespace and name
	ns, name, err := cache.SplitMetaNamespaceKey(namespacedKey)
	if err != nil {
		return err
	}

	deletePropagationBackground := metav1.DeletePropagationBackground
	err = deployer.clusternetClient.AppsV1alpha1().HelmReleases(ns).Delete(ctx, name, metav1.DeleteOptions{
		PropagationPolicy: &deletePropagationBackground,
	})
	if err != nil && apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (deployer *Deployer) handleHelmRelease(hr *appsapi.HelmRelease) error {
	config, err := utils.GetChildClusterConfig(deployer.secretLister, deployer.clusterLister, hr.Namespace, hr.Labels[known.ClusterIDLabel])
	if err != nil {
		return err
	}

	deployCtx, err := newDeployContext(config)
	if err != nil {
		return err
	}
	cfg := new(action.Configuration)
	err = cfg.Init(deployCtx, hr.Spec.TargetNamespace, "secret", klog.V(5).Infof)
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

		hr.Finalizers = utils.RemoveString(hr.Finalizers, known.AppFinalizer)
		_, err = deployer.clusternetClient.AppsV1alpha1().HelmReleases(hr.Namespace).Update(context.TODO(), hr, metav1.UpdateOptions{})
		return err
	}

	// install or upgrade helm release
	chart, err := LocateHelmChart(hr.Spec.Repository, hr.Spec.Chart, hr.Spec.ChartVersion)
	if err != nil {
		deployer.recorder.Event(hr, corev1.EventTypeWarning, "ChartLocateFailure", err.Error())
		return err
	}

	overrideValues, err := deployer.getOverrides(hr)
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

	if err != nil {
		// repo update
		if strings.Contains(err.Error(), "helm repo update") {
			return UpdateRepo(hr.Spec.Repository)
		}

		if err := deployer.helmReleaseController.UpdateHelmReleaseStatus(hr, &appsapi.HelmReleaseStatus{
			Phase: release.StatusFailed,
			Notes: err.Error(),
		}); err != nil {
			return err
		}
		return err
	}

	status := &appsapi.HelmReleaseStatus{
		Version: rel.Version,
	}
	if rel.Info != nil {
		status.FirstDeployed = rel.Info.FirstDeployed.String()
		status.LastDeployed = rel.Info.LastDeployed.String()
		status.Description = rel.Info.Description
		status.Phase = rel.Info.Status
		status.Notes = rel.Info.Notes
	}

	return deployer.helmReleaseController.UpdateHelmReleaseStatus(hr, status)
}

func (deployer *Deployer) handleSecret(secret *corev1.Secret) error {
	if secret.DeletionTimestamp == nil {
		return nil
	}

	if secret.Name != known.ChildClusterSecretName {
		return nil
	}

	// check whether HelmReleases get cleaned up
	hrs, err := deployer.hrLister.HelmReleases(secret.Namespace).List(labels.SelectorFromSet(labels.Set{}))
	if err != nil {
		return err
	}

	if hrs != nil {
		return fmt.Errorf("waiting all HelmReleases in namespace %s get cleanedup", secret.Namespace)
	}

	secret.Finalizers = utils.RemoveString(secret.Finalizers, known.AppFinalizer)
	_, err = deployer.kubeClient.CoreV1().Secrets(secret.Namespace).Update(context.TODO(), secret, metav1.UpdateOptions{})
	if err != nil {
		klog.WarningDepth(4,
			fmt.Sprintf("failed to remove finalizer %s from Secrets %s: %v", known.AppFinalizer, klog.KObj(secret), err))
	}
	return err
}

func (deployer *Deployer) getOverrides(hr *appsapi.HelmRelease) (map[string]interface{}, error) {
	var overrideValues map[string]interface{}
	// get overrides
	controllerRef := metav1.GetControllerOf(hr)
	if controllerRef != nil {
		desc := deployer.resolveControllerRef(hr.Namespace, controllerRef)
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
			deployer.recorder.Event(desc, corev1.EventTypeWarning, "DescriptionNotRelated", msg)
			return overrideValues, nil
		}
		if len(desc.Spec.Raw) < index {
			msg := fmt.Sprintf("unequal lengths of Spec.Raw and Spec.Charts in Description %s", klog.KObj(desc))
			klog.ErrorDepth(5, msg)
			deployer.recorder.Event(desc, corev1.EventTypeWarning, "UnequalLengths", msg)
			return nil, errors.New(msg)
		}
		err := json.Unmarshal(desc.Spec.Raw[index], &overrideValues)
		return overrideValues, err
	}
	return overrideValues, nil
}

// resolveControllerRef returns the controller referenced by a ControllerRef,
// or nil if the ControllerRef could not be resolved to a matching controller
// of the correct Kind.
func (deployer *Deployer) resolveControllerRef(namespace string, controllerRef *metav1.OwnerReference) *appsapi.Description {
	// We can't look up by UID, so look up by Name and then verify UID.
	// Don't even try to look up by Name if it's the wrong Kind.
	if controllerRef.Kind != descriptionKind.Kind {
		return nil
	}
	desc, err := deployer.descLister.Descriptions(namespace).Get(controllerRef.Name)
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
