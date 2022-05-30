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

package deployer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/repo"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	utilpointer "k8s.io/utils/pointer"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/controllers/apps/aggregatestatus"
	"github.com/clusternet/clusternet/pkg/controllers/apps/base"
	"github.com/clusternet/clusternet/pkg/controllers/apps/feedinventory"
	"github.com/clusternet/clusternet/pkg/controllers/apps/helmchart"
	"github.com/clusternet/clusternet/pkg/controllers/apps/manifest"
	"github.com/clusternet/clusternet/pkg/controllers/apps/subscription"
	"github.com/clusternet/clusternet/pkg/features"
	clusternetclientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	clusternetinformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/hub/deployer/generic"
	"github.com/clusternet/clusternet/pkg/hub/deployer/helm"
	"github.com/clusternet/clusternet/pkg/hub/localizer"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

var (
	helmChartKind               = appsapi.SchemeGroupVersion.WithKind("HelmChart")
	subscriptionKind            = appsapi.SchemeGroupVersion.WithKind("Subscription")
	baseKind                    = appsapi.SchemeGroupVersion.WithKind("Base")
	deletePropagationBackground = metav1.DeletePropagationBackground
)

const (
	// DeploymentKind defined resource is a deployment
	DeploymentKind = "Deployment"
	// StatefulSetKind defined resource is a statefulset
	StatefulSetKind = "StatefulSet"
	// ServiceKind defined resource is a service
	ServiceKind = "Service"
	// JobKind defined resource is a job
	JobKind = "Job"
)

// Deployer defines configuration for the application deployer
type Deployer struct {
	chartLister applisters.HelmChartLister
	chartSynced cache.InformerSynced
	descLister  applisters.DescriptionLister
	descSynced  cache.InformerSynced
	baseLister  applisters.BaseLister
	baseSynced  cache.InformerSynced
	mfstLister  applisters.ManifestLister
	mfstSynced  cache.InformerSynced
	subLister   applisters.SubscriptionLister
	subSynced   cache.InformerSynced
	finvLister  applisters.FeedInventoryLister
	finvSynced  cache.InformerSynced
	locLister   applisters.LocalizationLister
	locSynced   cache.InformerSynced
	nsLister    corev1lister.NamespaceLister
	nsSynced    cache.InformerSynced

	clusternetClient        *clusternetclientset.Clientset
	kubeClient              *kubernetes.Clientset
	kubectlClusternetclient *kubernetes.Clientset

	subsController            *subscription.Controller
	mfstController            *manifest.Controller
	baseController            *base.Controller
	chartController           *helmchart.Controller
	finvController            *feedinventory.Controller
	aggregatestatusController *aggregatestatus.Controller

	helmDeployer    *helm.Deployer
	genericDeployer *generic.Deployer

	localizer *localizer.Localizer

	recorder record.EventRecorder

	// apiserver url of parent cluster
	apiserverURL string

	// namespace where Manifests are created
	reservedNamespace string
}

func NewDeployer(apiserverURL, systemNamespace, reservedNamespace string,
	kubeclient *kubernetes.Clientset, clusternetclient *clusternetclientset.Clientset,
	clusternetInformerFactory clusternetinformers.SharedInformerFactory, kubeInformerFactory kubeinformers.SharedInformerFactory,
	recorder record.EventRecorder, anonymousAuthSupported bool, kubectlClusternetclient *kubernetes.Clientset) (*Deployer, error) {
	feedInUseProtection := utilfeature.DefaultFeatureGate.Enabled(features.FeedInUseProtection)

	deployer := &Deployer{
		apiserverURL:            apiserverURL,
		reservedNamespace:       reservedNamespace,
		chartLister:             clusternetInformerFactory.Apps().V1alpha1().HelmCharts().Lister(),
		chartSynced:             clusternetInformerFactory.Apps().V1alpha1().HelmCharts().Informer().HasSynced,
		descLister:              clusternetInformerFactory.Apps().V1alpha1().Descriptions().Lister(),
		descSynced:              clusternetInformerFactory.Apps().V1alpha1().Descriptions().Informer().HasSynced,
		baseLister:              clusternetInformerFactory.Apps().V1alpha1().Bases().Lister(),
		baseSynced:              clusternetInformerFactory.Apps().V1alpha1().Bases().Informer().HasSynced,
		mfstLister:              clusternetInformerFactory.Apps().V1alpha1().Manifests().Lister(),
		mfstSynced:              clusternetInformerFactory.Apps().V1alpha1().Manifests().Informer().HasSynced,
		subLister:               clusternetInformerFactory.Apps().V1alpha1().Subscriptions().Lister(),
		subSynced:               clusternetInformerFactory.Apps().V1alpha1().Subscriptions().Informer().HasSynced,
		finvLister:              clusternetInformerFactory.Apps().V1alpha1().FeedInventories().Lister(),
		finvSynced:              clusternetInformerFactory.Apps().V1alpha1().FeedInventories().Informer().HasSynced,
		locLister:               clusternetInformerFactory.Apps().V1alpha1().Localizations().Lister(),
		locSynced:               clusternetInformerFactory.Apps().V1alpha1().Localizations().Informer().HasSynced,
		nsLister:                kubeInformerFactory.Core().V1().Namespaces().Lister(),
		nsSynced:                kubeInformerFactory.Core().V1().Namespaces().Informer().HasSynced,
		clusternetClient:        clusternetclient,
		kubeClient:              kubeclient,
		kubectlClusternetclient: kubectlClusternetclient,
		recorder:                recorder,
	}

	helmChartController, err := helmchart.NewController(clusternetclient,
		clusternetInformerFactory.Apps().V1alpha1().HelmCharts(),
		clusternetInformerFactory.Apps().V1alpha1().Bases(),
		feedInUseProtection,
		deployer.recorder, deployer.handleHelmChart)
	if err != nil {
		return nil, err
	}
	deployer.chartController = helmChartController

	helmDeployer, err := helm.NewDeployer(apiserverURL, systemNamespace,
		clusternetclient, kubeclient, clusternetInformerFactory,
		kubeInformerFactory, deployer.recorder, anonymousAuthSupported)
	if err != nil {
		return nil, err
	}
	deployer.helmDeployer = helmDeployer

	genericDeployer, err := generic.NewDeployer(apiserverURL, systemNamespace,
		clusternetclient, clusternetInformerFactory, kubeInformerFactory,
		deployer.recorder, anonymousAuthSupported)
	if err != nil {
		return nil, err
	}
	deployer.genericDeployer = genericDeployer

	subsController, err := subscription.NewController(clusternetclient,
		clusternetInformerFactory.Apps().V1alpha1().Subscriptions(),
		clusternetInformerFactory.Apps().V1alpha1().Bases(),
		deployer.recorder,
		deployer.handleSubscription)
	if err != nil {
		return nil, err
	}
	deployer.subsController = subsController

	mfstController, err := manifest.NewController(clusternetclient,
		clusternetInformerFactory.Apps().V1alpha1().Manifests(),
		clusternetInformerFactory.Apps().V1alpha1().Bases(),
		feedInUseProtection,
		deployer.recorder,
		deployer.handleManifest,
		reservedNamespace)
	if err != nil {
		return nil, err
	}
	deployer.mfstController = mfstController

	baseController, err := base.NewController(clusternetclient,
		clusternetInformerFactory.Apps().V1alpha1().Bases(),
		clusternetInformerFactory.Apps().V1alpha1().Descriptions(),
		deployer.recorder,
		deployer.handleBase)
	if err != nil {
		return nil, err
	}
	deployer.baseController = baseController

	l, err := localizer.NewLocalizer(clusternetclient, clusternetInformerFactory,
		deployer.handleHelmChart, deployer.handleManifest, deployer.recorder, reservedNamespace)
	if err != nil {
		return nil, err
	}
	deployer.localizer = l

	finv, err := feedinventory.NewController(clusternetclient,
		clusternetInformerFactory.Apps().V1alpha1().Subscriptions(),
		clusternetInformerFactory.Apps().V1alpha1().FeedInventories(),
		clusternetInformerFactory.Apps().V1alpha1().Manifests(),
		deployer.recorder,
		feedinventory.NewInTreeRegistry(),
		reservedNamespace)
	if err != nil {
		return nil, err
	}
	deployer.finvController = finv

	aggregatestatusController, err := aggregatestatus.NewController(clusternetclient,
		clusternetInformerFactory.Apps().V1alpha1().Subscriptions(),
		clusternetInformerFactory.Apps().V1alpha1().Descriptions(),
		deployer.recorder,
		deployer.handleAggregateStatus)
	if err != nil {
		return nil, err
	}
	deployer.aggregatestatusController = aggregatestatusController
	return deployer, nil
}

func (deployer *Deployer) Run(workers int, stopCh <-chan struct{}) {
	klog.Infof("starting Clusternet deployer ...")

	// Wait for the caches to be synced before starting workers
	if !cache.WaitForNamedCacheSync("clusternet-deployer",
		stopCh,
		deployer.chartSynced,
		deployer.descSynced,
		deployer.baseSynced,
		deployer.mfstSynced,
		deployer.subSynced,
		deployer.nsSynced,
		deployer.finvSynced,
		deployer.locSynced,
	) {
		return
	}

	go deployer.chartController.Run(workers, stopCh)
	go deployer.helmDeployer.Run(workers, stopCh)
	go deployer.genericDeployer.Run(workers, stopCh)
	go deployer.subsController.Run(workers, stopCh)
	go deployer.mfstController.Run(workers, stopCh)
	go deployer.baseController.Run(workers, stopCh)
	go deployer.localizer.Run(workers, stopCh)
	go deployer.aggregatestatusController.Run(workers, stopCh)

	// When using external FeedInventory controller, this feature gate should be closed
	if utilfeature.DefaultFeatureGate.Enabled(features.FeedInventory) {
		go deployer.finvController.Run(workers, stopCh)
	}

	<-stopCh
}

func (deployer *Deployer) handleSubscription(sub *appsapi.Subscription) error {
	klog.V(5).Infof("handle Subscription %s", klog.KObj(sub))
	if sub.DeletionTimestamp != nil {
		bases, err := deployer.baseLister.List(labels.SelectorFromSet(labels.Set{
			known.ConfigKindLabel:      subscriptionKind.Kind,
			known.ConfigNameLabel:      sub.Name,
			known.ConfigNamespaceLabel: sub.Namespace,
			known.ConfigUIDLabel:       string(sub.UID),
		}))
		if err != nil {
			return err
		}

		// delete all matching Base
		var allErrs []error
		for _, base := range bases {
			if base.DeletionTimestamp != nil {
				continue
			}
			if err := deployer.deleteBase(context.TODO(), klog.KObj(base).String()); err != nil {
				klog.ErrorDepth(5, err)
				allErrs = append(allErrs, err)
				continue
			}
		}
		if bases != nil || len(allErrs) > 0 {
			return fmt.Errorf("waiting for Bases belongs to Subscription %s getting deleted", klog.KObj(sub))
		}

		// remove label (subUID="Subscription") from referred Manifest/HelmChart
		if err := deployer.removeLabelsFromReferredFeeds(sub.UID, subscriptionKind.Kind); err != nil {
			return err
		}

		subCopy := sub.DeepCopy()
		subCopy.Finalizers = utils.RemoveString(subCopy.Finalizers, known.AppFinalizer)
		_, err = deployer.clusternetClient.AppsV1alpha1().Subscriptions(sub.Namespace).Update(context.TODO(), subCopy, metav1.UpdateOptions{})
		if err != nil {
			klog.WarningDepth(4,
				fmt.Sprintf("failed to remove finalizer %s from Subscription %s: %v", known.AppFinalizer, klog.KObj(sub), err))
		}
		return err
	}

	// populate Base and Localization (for dividing scheduling)
	err := deployer.populateBasesAndLocalizations(sub)
	if err != nil {
		return err
	}

	return nil
}

func (deployer *Deployer) handleAggregateStatus(sub *appsapi.Subscription) error {
	klog.Infof("handle AggregateStatus %s", klog.KObj(sub))
	if sub.DeletionTimestamp != nil {
		return nil
	}

	var errs []error
	aggregatedStatuses, err := deployer.DescriptionStatusChanged(sub)
	if err == nil {
		if aggregatedStatuses != nil {
			Status := sub.Status.DeepCopy()
			Status.AggregatedStatus = aggregatedStatuses

			err = deployer.UpdateSubscriptionStatus(sub, Status)
			if err != nil {
				klog.Errorf("Failed to aggregate baseStatus to Subscriptions(%s/%s). Error: %v.", sub.Namespace, sub.Name, err)
				deployer.recorder.Event(sub, corev1.EventTypeWarning, "AggregateStatusFailed", err.Error())
				errs = append(errs, err)
			} else {
				msg := fmt.Sprintf("Update Subscriptions(%s/%s) with AggregatedStatus successfully.", sub.Namespace, sub.Name)
				deployer.recorder.Event(sub, corev1.EventTypeNormal, "AggregateStatusSucceed", msg)
				err = deployer.AggregateFeedsStatus(sub)
				errs = append(errs, err)
			}
		} else {
			klog.V(5).Infof("AggregateStatus %s does not change, skip it.", klog.KObj(sub))
		}
	} else {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return utilerrors.NewAggregate(errs)
	}

	return nil
}

func (deployer *Deployer) AggregateFeedsStatus(sub *appsapi.Subscription) error {
	var errs []error
	for _, feed := range sub.Spec.Feeds {
		klog.Infof("sync resource status(%s/%s/%s)", feed.Kind, feed.Namespace, feed.Name)
		switch feed.Kind {
		case DeploymentKind:
			err := deployer.AggregateDeploymentStatus(feed, sub.Status.AggregatedStatus)
			errs = append(errs, err)
		case StatefulSetKind:
			err := deployer.AggregateStatefulSetStatus(feed, sub.Status.AggregatedStatus)
			errs = append(errs, err)
		case ServiceKind:
			err := deployer.AggregateServiceStatus(feed, sub.Status.AggregatedStatus)
			errs = append(errs, err)
		case JobKind:
			err := deployer.AggregateJobStatus(feed, sub.Status.AggregatedStatus, sub.Status.BindingClusters)
			errs = append(errs, err)
		default:
			klog.V(5).Infof("unsupport kind(%s) resource(%s/%s), skip.", feed.Kind, feed.Namespace, feed.Name)
		}
	}

	if len(errs) > 0 {
		return utilerrors.NewAggregate(errs)
	}

	return nil
}

func (deployer *Deployer) DescriptionStatusChanged(sub *appsapi.Subscription) ([]appsapi.AggregatedStatusItem, error) {
	allExistingDescs, listErr := deployer.descLister.List(labels.SelectorFromSet(labels.Set{
		known.ConfigSubscriptionNameLabel:      sub.Name,
		known.ConfigSubscriptionNamespaceLabel: sub.Namespace,
		known.ConfigSubscriptionUIDLabel:       string(sub.UID),
	}))
	if listErr != nil {
		return nil, listErr
	}

	aggregatedStatuses := make([]appsapi.AggregatedStatusItem, 0)
	for _, desc := range allExistingDescs {
		if desc.DeletionTimestamp != nil {
			continue
		}

		manifestStatuses := make([]appsapi.ManifestStatus, 0)

		manifestStatuses = append(manifestStatuses, desc.Status.ManifestStatuses...)
		aggregatedStatus := appsapi.AggregatedStatusItem{
			Cluster:          desc.Namespace,
			ManifestStatuses: manifestStatuses,
		}
		aggregatedStatuses = append(aggregatedStatuses, aggregatedStatus)
	}

	if reflect.DeepEqual(sub.Status.AggregatedStatus, aggregatedStatuses) {
		klog.V(4).Infof("New aggregatedStatuses are equal with old subscription(%s/%s) AggregatedStatus, no update required.",
			sub.Namespace, sub.Name)
		return nil, nil
	}

	return aggregatedStatuses, nil
}

func (deployer *Deployer) UpdateSubscriptionStatus(sub *appsapi.Subscription, status *appsapi.SubscriptionStatus) error {
	// NEVER modify objects from the store. It's a read-only, local cache.
	// You can use DeepCopy() to make a deep copy of original object and modify this copy
	// Or create a copy manually for better performance

	klog.V(5).Infof("try to update Subscription %q status", sub.Name)

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		sub.Status = *status
		_, err := deployer.clusternetClient.AppsV1alpha1().Subscriptions(sub.Namespace).UpdateStatus(context.TODO(), sub, metav1.UpdateOptions{})
		if err == nil {
			//TODO
			return nil
		}

		if updated, err := deployer.subLister.Subscriptions(sub.Namespace).Get(sub.Name); err == nil {
			// make a copy so we don't mutate the shared cache
			sub = updated.DeepCopy()
		} else {
			utilruntime.HandleError(fmt.Errorf("error getting updated Subscription %q from lister: %v", sub.Name, err))
		}
		return err
	})
}

// populateBasesAndLocalizations will populate a group of Base(s) from Subscription.
// Localization(s) will be populated as well for dividing scheduling.
func (deployer *Deployer) populateBasesAndLocalizations(sub *appsapi.Subscription) error {
	allExistingBases, listErr := deployer.baseLister.List(labels.SelectorFromSet(labels.Set{
		known.ConfigKindLabel:      subscriptionKind.Kind,
		known.ConfigNameLabel:      sub.Name,
		known.ConfigNamespaceLabel: sub.Namespace,
		known.ConfigUIDLabel:       string(sub.UID),
	}))
	if listErr != nil {
		return listErr
	}
	// Bases to be deleted
	basesToBeDeleted := sets.String{}
	for _, base := range allExistingBases {
		basesToBeDeleted.Insert(klog.KObj(base).String())
	}

	var allErrs []error
	for idx, namespacedName := range sub.Status.BindingClusters {
		// Convert the namespacedName/name string into a distinct namespacedName and name
		namespace, _, err := cache.SplitMetaNamespaceKey(namespacedName)
		if err != nil {
			allErrs = append(allErrs, fmt.Errorf("invalid resource key: %s", namespacedName))
			continue
		}

		ns, err2 := deployer.nsLister.Get(namespace)
		if err2 != nil {
			if apierrors.IsNotFound(err2) {
				continue
			}
			return fmt.Errorf("failed to populate Bases for Subscription %s: %v", klog.KObj(sub), err)
		}

		baseTemplate := &appsapi.Base{
			ObjectMeta: metav1.ObjectMeta{
				Name:      sub.Name,
				Namespace: namespace,
				Labels: map[string]string{
					known.ObjectCreatedByLabel: known.ClusternetHubName,
					known.ConfigKindLabel:      subscriptionKind.Kind,
					known.ConfigNameLabel:      sub.Name,
					known.ConfigNamespaceLabel: sub.Namespace,
					known.ConfigUIDLabel:       string(sub.UID),
					// add subscription info
					known.ConfigSubscriptionNameLabel:      sub.Name,
					known.ConfigSubscriptionNamespaceLabel: sub.Namespace,
					known.ConfigSubscriptionUIDLabel:       string(sub.UID),
				},
				Finalizers: []string{
					known.AppFinalizer,
				},
				// Base and Subscription are in different namespaces
			},
			Spec: appsapi.BaseSpec{
				Feeds: sub.Spec.Feeds,
			},
		}
		if ns.Labels != nil {
			baseTemplate.Labels[known.ClusterIDLabel] = ns.Labels[known.ClusterIDLabel]
			baseTemplate.Labels[known.ClusterNameLabel] = ns.Labels[known.ClusterNameLabel]
		}

		basesToBeDeleted.Delete(klog.KObj(baseTemplate).String())
		base, err := deployer.syncBase(sub, baseTemplate)
		if err != nil {
			allErrs = append(allErrs, err)
			msg := fmt.Sprintf("Failed to sync Base %s: %v", klog.KObj(base), err)
			klog.ErrorDepth(5, msg)
			deployer.recorder.Event(sub, corev1.EventTypeWarning, "FailedSyncingBase", msg)
			continue
		}

		// populate Localizations for dividing scheduling.
		err = deployer.populateLocalizations(sub, base, idx)
		if err != nil {
			allErrs = append(allErrs, err)
			klog.ErrorDepth(5, fmt.Sprintf("Failed to sync Localizations: %v", err))
		}
	}

	for key := range basesToBeDeleted {
		err := deployer.deleteBase(context.TODO(), key)
		if err != nil {
			allErrs = append(allErrs, err)
		}
	}

	return utilerrors.NewAggregate(allErrs)
}

func (deployer *Deployer) syncBase(sub *appsapi.Subscription, baseTemplate *appsapi.Base) (*appsapi.Base, error) {
	base, err := deployer.clusternetClient.AppsV1alpha1().Bases(baseTemplate.Namespace).Create(context.TODO(),
		baseTemplate, metav1.CreateOptions{})
	if err == nil {
		msg := fmt.Sprintf("Base %s is created successfully", klog.KObj(baseTemplate))
		klog.V(4).Info(msg)
		deployer.recorder.Event(sub, corev1.EventTypeNormal, "BaseCreated", msg)
		return base, nil
	}

	if !apierrors.IsAlreadyExists(err) {
		return nil, err
	}

	// update it
	base, err = deployer.baseLister.Bases(baseTemplate.Namespace).Get(baseTemplate.Name)
	if err != nil {
		return nil, err
	}

	if base.DeletionTimestamp != nil {
		return nil, fmt.Errorf("Base %s is deleting, will resync later", klog.KObj(base))
	}

	baseCopy := base.DeepCopy()
	if baseCopy.Labels == nil {
		baseCopy.Labels = make(map[string]string)
	}
	for key, value := range baseTemplate.Labels {
		baseCopy.Labels[key] = value
	}

	baseCopy.Spec = baseTemplate.Spec
	if !utils.ContainsString(baseCopy.Finalizers, known.AppFinalizer) {
		baseCopy.Finalizers = append(baseCopy.Finalizers, known.AppFinalizer)
	}

	base, err = deployer.clusternetClient.AppsV1alpha1().Bases(baseCopy.Namespace).Update(context.TODO(),
		baseCopy, metav1.UpdateOptions{})
	if err == nil {
		msg := fmt.Sprintf("Base %s is updated successfully", klog.KObj(baseCopy))
		klog.V(4).Info(msg)
		deployer.recorder.Event(sub, corev1.EventTypeNormal, "BaseUpdated", msg)
	}
	return base, err
}

func (deployer *Deployer) deleteBase(ctx context.Context, namespacedKey string) error {
	// Convert the namespace/name string into a distinct namespace and name
	ns, name, err := cache.SplitMetaNamespaceKey(namespacedKey)
	if err != nil {
		return err
	}

	base, err := deployer.baseLister.Bases(ns).Get(name)
	if err != nil {
		return err
	}
	// remove label (baseUID="Base") from referred Manifest/HelmChart
	if err := deployer.removeLabelsFromReferredFeeds(base.UID, baseKind.Kind); err != nil {
		return err
	}

	err = deployer.clusternetClient.AppsV1alpha1().Bases(ns).Delete(ctx, name, metav1.DeleteOptions{
		PropagationPolicy: &deletePropagationBackground,
	})
	if err != nil && apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (deployer *Deployer) populateLocalizations(sub *appsapi.Subscription, base *appsapi.Base, clusterIndex int) error {
	if len(base.UID) == 0 {
		return fmt.Errorf("waiting for UID set for Base %s", klog.KObj(base))
	}

	if sub.Spec.SchedulingStrategy != appsapi.DividingSchedulingStrategyType {
		return nil
	}

	finv, err := deployer.finvLister.FeedInventories(sub.Namespace).Get(sub.Name)
	if err != nil {
		klog.WarningDepth(5, fmt.Sprintf("failed to get FeedInventory %s: %v", klog.KObj(sub), err))
		return err
	}

	allExistingLocalizations, err := deployer.locLister.Localizations(base.Namespace).List(labels.SelectorFromSet(labels.Set{
		string(sub.UID): subscriptionKind.Kind,
	}))
	if err != nil {
		return err
	}
	// Localizations to be deleted
	locsToBeDeleted := sets.String{}
	for _, loc := range allExistingLocalizations {
		locsToBeDeleted.Insert(klog.KObj(loc).String())
	}

	var allErrs []error
	for _, feedOrder := range finv.Spec.Feeds {
		replicas, ok := sub.Status.Replicas[utils.GetFeedKey(feedOrder.Feed)]
		if !ok {
			continue
		}

		if len(replicas) == 0 {
			continue
		}

		if len(feedOrder.ReplicaJsonPath) == 0 {
			msg := fmt.Sprintf("no valid JSONPath is set for %s in FeedInventory %s",
				utils.FormatFeed(feedOrder.Feed), klog.KObj(finv))
			klog.ErrorDepth(5, msg)
			allErrs = append(allErrs, errors.New(msg))
			deployer.recorder.Event(finv, corev1.EventTypeWarning, "ReplicaJsonPathUnset", msg)
			continue
		}

		if len(replicas) < clusterIndex {
			msg := fmt.Sprintf("the length of status.Replicas for %s in Subscription %s is not matched with status.BindingClusters",
				utils.FormatFeed(feedOrder.Feed), klog.KObj(sub))
			klog.ErrorDepth(5, msg)
			allErrs = append(allErrs, errors.New(msg))
			deployer.recorder.Event(sub, corev1.EventTypeWarning, "BadSchedulingResult", msg)
			continue
		}

		suffixName := feedOrder.Feed.Name
		if len(feedOrder.Feed.Namespace) > 0 {
			suffixName = fmt.Sprintf("%s.%s", feedOrder.Feed.Namespace, feedOrder.Feed.Name)
		}
		loc := GenerateLocalizationTemplate(base, appsapi.ApplyNow)
		loc.Name = fmt.Sprintf("%s-%s-%s", base.Name, strings.ToLower(feedOrder.Feed.Kind), suffixName)
		loc.Labels[string(sub.UID)] = subscriptionKind.Kind
		loc.Spec.Feed = feedOrder.Feed
		loc.Spec.Overrides = []appsapi.OverrideConfig{
			{
				Name:  "dividing scheduling replicas",
				Value: fmt.Sprintf(`[{"path":%q,"value":%d,"op":"replace"}]`, feedOrder.ReplicaJsonPath, replicas[clusterIndex]),
				Type:  appsapi.JSONPatchType,
			},
		}

		err = deployer.syncLocalization(loc)
		if err != nil {
			allErrs = append(allErrs, err)
			msg := fmt.Sprintf("Failed to sync Localization %s: %v", klog.KObj(loc), err)
			klog.ErrorDepth(5, msg)
			deployer.recorder.Event(sub, corev1.EventTypeWarning, "FailedSyncingLocalization", msg)
		}
		locsToBeDeleted.Delete(klog.KObj(loc).String())
	}

	for key := range locsToBeDeleted {
		err := deployer.deleteLocalization(context.TODO(), key)
		if err != nil {
			allErrs = append(allErrs, err)
		}
	}

	return utilerrors.NewAggregate(allErrs)
}

func (deployer *Deployer) syncLocalization(loc *appsapi.Localization) error {
	_, err := deployer.clusternetClient.AppsV1alpha1().Localizations(loc.Namespace).Create(context.TODO(), loc, metav1.CreateOptions{})
	if err == nil {
		klog.V(4).Infof("Localization %s is created successfully", klog.KObj(loc))
		return nil
	}

	if !apierrors.IsAlreadyExists(err) {
		return err
	}

	// update it
	curLoc, err := deployer.locLister.Localizations(loc.Namespace).Get(loc.Name)
	if err != nil {
		return err
	}
	curLocCopy := curLoc.DeepCopy()

	if curLocCopy.Labels == nil {
		curLocCopy.Labels = make(map[string]string)
	}
	for key, value := range loc.Labels {
		curLocCopy.Labels[key] = value
	}

	if curLocCopy.Annotations == nil {
		curLocCopy.Annotations = make(map[string]string)
	}
	for key, value := range loc.Annotations {
		curLocCopy.Annotations[key] = value
	}

	curLocCopy.Spec = loc.Spec

	_, err = deployer.clusternetClient.AppsV1alpha1().Localizations(curLocCopy.Namespace).Update(context.TODO(), curLocCopy, metav1.UpdateOptions{})
	if err == nil {
		klog.V(4).Infof("Localization %s is updated successfully", klog.KObj(curLocCopy))
	}

	return err
}

func (deployer *Deployer) deleteLocalization(ctx context.Context, namespacedKey string) error {
	// Convert the namespace/name string into a distinct namespace and name
	ns, name, err := cache.SplitMetaNamespaceKey(namespacedKey)
	if err != nil {
		return err
	}

	err = deployer.clusternetClient.AppsV1alpha1().Localizations(ns).Delete(ctx, name, metav1.DeleteOptions{
		PropagationPolicy: &deletePropagationBackground,
	})
	if err != nil && apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (deployer *Deployer) handleBase(base *appsapi.Base) error {
	klog.V(5).Infof("handle Base %s", klog.KObj(base))
	if base.DeletionTimestamp != nil {
		descs, err := deployer.descLister.List(labels.SelectorFromSet(labels.Set{
			known.ConfigKindLabel:      baseKind.Kind,
			known.ConfigNameLabel:      base.Name,
			known.ConfigNamespaceLabel: base.Namespace,
			known.ConfigUIDLabel:       string(base.UID),
		}))
		if err != nil {
			return err
		}

		// delete all matching Descriptions
		var allErrs []error
		for _, desc := range descs {
			if desc.DeletionTimestamp != nil {
				continue
			}

			if err := deployer.deleteDescription(context.TODO(), klog.KObj(desc).String()); err != nil {
				klog.ErrorDepth(5, err)
				allErrs = append(allErrs, err)
				continue
			}
		}
		if descs != nil || len(allErrs) > 0 {
			return fmt.Errorf("waiting for Descriptions belongs to Base %s getting deleted", klog.KObj(base))
		}

		baseCopy := base.DeepCopy()
		baseCopy.Finalizers = utils.RemoveString(baseCopy.Finalizers, known.AppFinalizer)
		_, err = deployer.clusternetClient.AppsV1alpha1().Bases(baseCopy.Namespace).Update(context.TODO(), baseCopy, metav1.UpdateOptions{})
		if err != nil {
			klog.WarningDepth(4,
				fmt.Sprintf("failed to remove finalizer %s from Base %s: %v", known.AppFinalizer, klog.KObj(base), err))
		}
		return err
	}

	// add label (baseUID="Base") to referred Manifest/HelmChart
	if err := deployer.addLabelsToReferredFeeds(base); err != nil {
		return err
	}

	err := deployer.populateDescriptions(base)
	if err != nil {
		return err
	}

	return nil
}

func (deployer *Deployer) populateDescriptions(base *appsapi.Base) error {
	var allChartRefs []appsapi.ChartReference
	var allManifests []*appsapi.Manifest

	var err error
	var index int
	var chart *appsapi.HelmChart
	var manifests []*appsapi.Manifest
	for idx, feed := range base.Spec.Feeds {
		switch feed.Kind {
		case helmChartKind.Kind:
			chart, err = deployer.chartLister.HelmCharts(feed.Namespace).Get(feed.Name)
			if err != nil {
				break
			}
			if len(chart.Status.Phase) == 0 {
				msg := fmt.Sprintf("HelmChart %s is in verifying", klog.KObj(chart))
				klog.Warning(msg)
				deployer.recorder.Event(base, corev1.EventTypeWarning, "VerifyingHelmChart", msg)
				return fmt.Errorf(msg)
			}
			if chart.Status.Phase == appsapi.HelmChartNotFound {
				deployer.recorder.Event(base, corev1.EventTypeWarning, "HelmChartNotFound",
					fmt.Sprintf("helm chart %s is not found", klog.KObj(chart)))
				return nil
			}
			allChartRefs = append(allChartRefs, appsapi.ChartReference{
				Namespace: chart.Namespace,
				Name:      chart.Name,
			})

		default:
			manifests, err = utils.ListManifestsBySelector(deployer.reservedNamespace, deployer.mfstLister, feed)
			if err != nil {
				break
			}
			allManifests = append(allManifests, manifests...)
			if manifests == nil {
				err = apierrors.NewNotFound(schema.GroupResource{}, "")
			}
		}

		if err != nil {
			index = idx
			break
		}
	}

	if apierrors.IsNotFound(err) {
		msg := fmt.Sprintf("Base %s is using a nonexistent %s", klog.KObj(base), utils.FormatFeed(base.Spec.Feeds[index]))
		klog.Error(msg)
		deployer.recorder.Event(base, corev1.EventTypeWarning, fmt.Sprintf("Nonexistent%s", base.Spec.Feeds[index].Kind), msg)
		return errors.New(msg)
	}
	if err != nil {
		msg := fmt.Sprintf("failed to get matched objects %q for Base %s: %v", utils.FormatFeed(base.Spec.Feeds[index]), klog.KObj(base), err)
		klog.Error(msg)
		deployer.recorder.Event(base, corev1.EventTypeWarning, "FailedRetrievingObjects", msg)
		return err
	}

	allExistingDescriptions, err := deployer.descLister.List(labels.SelectorFromSet(labels.Set{
		known.ConfigKindLabel:      baseKind.Kind,
		known.ConfigNameLabel:      base.Name,
		known.ConfigNamespaceLabel: base.Namespace,
		known.ConfigUIDLabel:       string(base.UID),
	}))
	if err != nil {
		return err
	}
	// Descriptions to be deleted
	descsToBeDeleted := sets.String{}
	for _, desc := range allExistingDescriptions {
		if desc.DeletionTimestamp != nil {
			continue
		}
		descsToBeDeleted.Insert(klog.KObj(desc).String())
	}

	descTemplate := &appsapi.Description{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: base.Namespace,
			Labels: map[string]string{
				known.ObjectCreatedByLabel: known.ClusternetHubName,
				known.ConfigKindLabel:      baseKind.Kind,
				known.ConfigNameLabel:      base.Name,
				known.ConfigNamespaceLabel: base.Namespace,
				known.ConfigUIDLabel:       string(base.UID),
				known.ClusterIDLabel:       base.Labels[known.ClusterIDLabel],
				known.ClusterNameLabel:     base.Labels[known.ClusterNameLabel],
				// add subscription info
				known.ConfigSubscriptionNameLabel:      base.Labels[known.ConfigSubscriptionNameLabel],
				known.ConfigSubscriptionNamespaceLabel: base.Labels[known.ConfigSubscriptionNamespaceLabel],
				known.ConfigSubscriptionUIDLabel:       base.Labels[known.ConfigSubscriptionUIDLabel],
			},
			Finalizers: []string{
				known.AppFinalizer,
			},
		},
	}
	descTemplate.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(base, baseKind)})

	var allErrs []error
	if len(allChartRefs) > 0 {
		desc := descTemplate.DeepCopy()
		desc.Name = fmt.Sprintf("%s-helm", base.Name)
		desc.Spec.Deployer = appsapi.DescriptionHelmDeployer
		desc.Spec.Charts = allChartRefs
		err := deployer.syncDescriptions(base, desc)
		if err != nil {
			allErrs = append(allErrs, err)
			msg := fmt.Sprintf("Failed to sync Description %s: %v", klog.KObj(desc), err)
			klog.ErrorDepth(5, msg)
			deployer.recorder.Event(base, corev1.EventTypeWarning, "FailedSyncingDescription", msg)
		}
		descsToBeDeleted.Delete(klog.KObj(desc).String())
	}

	if len(allManifests) > 0 {
		var rawObjects [][]byte
		for _, manifest := range allManifests {
			rawObjects = append(rawObjects, manifest.Template.Raw)
		}
		desc := descTemplate.DeepCopy()
		desc.Name = fmt.Sprintf("%s-generic", base.Name)
		desc.Spec.Deployer = appsapi.DescriptionGenericDeployer
		desc.Spec.Raw = rawObjects
		err := deployer.syncDescriptions(base, desc)
		if err != nil {
			allErrs = append(allErrs, err)
			msg := fmt.Sprintf("Failed to sync Description %s: %v", klog.KObj(desc), err)
			klog.ErrorDepth(5, msg)
			deployer.recorder.Event(base, corev1.EventTypeWarning, "FailedSyncingDescription", msg)
		}
		descsToBeDeleted.Delete(klog.KObj(desc).String())
	}

	for key := range descsToBeDeleted {
		if err := deployer.deleteDescription(context.TODO(), key); err != nil {
			allErrs = append(allErrs, err)
			continue
		}
	}

	return utilerrors.NewAggregate(allErrs)
}

func (deployer *Deployer) syncDescriptions(base *appsapi.Base, desc *appsapi.Description) error {
	// apply overrides
	if err := deployer.localizer.ApplyOverridesToDescription(desc); err != nil {
		msg := fmt.Sprintf("Failed to apply overrides for Description %s: %v", klog.KObj(desc), err)
		klog.ErrorDepth(5, msg)
		deployer.recorder.Event(base, corev1.EventTypeWarning, "FailedApplyingOverrides", msg)
		return err
	}

	// delete Description with empty feeds
	if len(base.Spec.Feeds) == 0 {
		// in fact, this piece of codes will never be run. Just leave it here for the last protection.
		return deployer.deleteDescription(context.TODO(), klog.KObj(desc).String())
	}

	curDesc, err := deployer.descLister.Descriptions(desc.Namespace).Get(desc.Name)
	if err == nil {
		if curDesc.DeletionTimestamp != nil {
			return fmt.Errorf("description %s is deleting, will resync later", klog.KObj(curDesc))
		}

		// update it
		if !reflect.DeepEqual(curDesc.Spec, desc.Spec) {
			// prune feeds that are not subscribed any longer from description
			// for helm deployer, redundant HelmReleases will be deleted after re-calculating.
			// Here we only need to focus on generic deployer.
			pruneCtx, cancel := context.WithCancel(context.TODO())
			go wait.JitterUntilWithContext(pruneCtx, func(ctx context.Context) {
				if err := deployer.genericDeployer.PruneFeedsInDescription(ctx, curDesc.DeepCopy(), desc.DeepCopy()); err == nil {
					cancel()
					return
				}
			}, known.DefaultRetryPeriod, 0.3, true)

			curDescCopy := curDesc.DeepCopy()
			if curDescCopy.Labels == nil {
				curDescCopy.Labels = make(map[string]string)
			}
			for key, value := range desc.Labels {
				curDescCopy.Labels[key] = value
			}

			curDescCopy.Spec = desc.Spec
			if !utils.ContainsString(curDescCopy.Finalizers, known.AppFinalizer) {
				curDescCopy.Finalizers = append(curDescCopy.Finalizers, known.AppFinalizer)
			}

			_, err = deployer.clusternetClient.AppsV1alpha1().Descriptions(curDescCopy.Namespace).Update(context.TODO(),
				curDescCopy, metav1.UpdateOptions{})
			if err == nil {
				msg := fmt.Sprintf("Description %s is updated successfully", klog.KObj(desc))
				klog.V(4).Info(msg)
				deployer.recorder.Event(base, corev1.EventTypeNormal, "DescriptionUpdated", msg)
			}
			return err
		}
		return nil
	}

	_, err = deployer.clusternetClient.AppsV1alpha1().Descriptions(desc.Namespace).Create(context.TODO(),
		desc, metav1.CreateOptions{})
	if err == nil {
		msg := fmt.Sprintf("Description %s is created successfully", klog.KObj(desc))
		klog.V(4).Info(msg)
		deployer.recorder.Event(base, corev1.EventTypeNormal, "DescriptionCreated", msg)
	}
	return err
}

func (deployer *Deployer) deleteDescription(ctx context.Context, namespacedKey string) error {
	// Convert the namespace/name string into a distinct namespace and name
	ns, name, err := cache.SplitMetaNamespaceKey(namespacedKey)
	if err != nil {
		return err
	}

	err = deployer.clusternetClient.AppsV1alpha1().Descriptions(ns).Delete(ctx, name, metav1.DeleteOptions{
		PropagationPolicy: &deletePropagationBackground,
	})
	if err != nil && apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (deployer *Deployer) handleManifest(manifest *appsapi.Manifest) error {
	klog.V(5).Infof("handle Manifest %s", klog.KObj(manifest))
	if manifest.DeletionTimestamp != nil {
		if err := deployer.protectManifestFeed(manifest); err != nil {
			return err
		}

		// remove finalizers
		manifestCopy := manifest.DeepCopy()
		manifestCopy.Finalizers = utils.RemoveString(manifestCopy.Finalizers, known.AppFinalizer)
		manifestCopy.Finalizers = utils.RemoveString(manifestCopy.Finalizers, known.FeedProtectionFinalizer)
		_, err := deployer.clusternetClient.AppsV1alpha1().Manifests(manifest.Namespace).Update(context.TODO(), manifestCopy, metav1.UpdateOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			klog.WarningDepth(4,
				fmt.Sprintf("failed to remove finalizers from Manifest %s: %v", klog.KObj(manifest), err))
		}
		return err
	}

	// find all referred Base UIDs
	var baseUIDs []string
	for key, val := range manifest.Labels {
		if val == baseKind.Kind {
			baseUIDs = append(baseUIDs, key)
		}
	}

	return deployer.resyncBase(baseUIDs...)
}

func (deployer *Deployer) handleHelmChart(chart *appsapi.HelmChart) error {
	var err error
	klog.V(5).Infof("handle HelmChart %s", klog.KObj(chart))
	if chart.DeletionTimestamp != nil {
		if err = deployer.protectHelmChartFeed(chart); err != nil {
			return err
		}

		// remove finalizers
		chart.Finalizers = utils.RemoveString(chart.Finalizers, known.AppFinalizer)
		chart.Finalizers = utils.RemoveString(chart.Finalizers, known.FeedProtectionFinalizer)
		_, err = deployer.clusternetClient.AppsV1alpha1().HelmCharts(chart.Namespace).Update(context.TODO(), chart, metav1.UpdateOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			klog.WarningDepth(4,
				fmt.Sprintf("failed to remove finalizers from HelmChart %s: %v", klog.KObj(chart), err))
		}
		return err
	}

	var (
		username string
		password string

		chartPhase appsapi.HelmChartPhase
		reason     string
	)
	if chart.Spec.ChartPullSecret.Name != "" {
		username, password, err = utils.GetHelmRepoCredentials(deployer.kubeClient, chart.Spec.ChartPullSecret.Name, chart.Spec.ChartPullSecret.Namespace)
		if err != nil {
			return err
		}
	}
	chartPhase = appsapi.HelmChartFound
	if registry.IsOCI(chart.Spec.Repository) {
		var found bool
		found, err = utils.FindOCIChart(chart.Spec.Repository, chart.Spec.Chart, chart.Spec.ChartVersion)
		if !found {
			chartPhase = appsapi.HelmChartNotFound
			reason = fmt.Sprintf("not found a version matched %s for chart %s/%s", chart.Spec.ChartVersion, chart.Spec.Repository, chart.Spec.Chart)
		}
	} else {
		_, err = repo.FindChartInAuthRepoURL(chart.Spec.Repository, username, password, chart.Spec.Chart, chart.Spec.ChartVersion,
			"", "", "",
			getter.All(utils.Settings))
	}
	if err != nil {
		// failed to find chart
		chartPhase = appsapi.HelmChartNotFound
		reason = err.Error()
	}

	err = deployer.chartController.UpdateChartStatus(chart, &appsapi.HelmChartStatus{
		Phase:  chartPhase,
		Reason: reason,
	})
	if err != nil {
		return err
	}

	// find all referred Base UIDs
	var baseUIDs []string
	for key, val := range chart.Labels {
		if val == baseKind.Kind {
			baseUIDs = append(baseUIDs, key)
		}
	}

	return deployer.resyncBase(baseUIDs...)
}

func (deployer *Deployer) resyncBase(baseUIDs ...string) error {
	wg := sync.WaitGroup{}
	wg.Add(len(baseUIDs))
	errCh := make(chan error, len(baseUIDs))
	for _, baseuid := range baseUIDs {
		bases, err := deployer.baseLister.List(labels.SelectorFromSet(labels.Set{
			baseuid: baseKind.Kind,
		}))

		if err != nil {
			return err
		}

		go func() {
			defer wg.Done()
			if len(bases) == 0 {
				return
			}
			// here the length should always be 1
			if err := deployer.populateDescriptions(bases[0]); err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()

	// collect errors
	close(errCh)
	var allErrs []error
	for err := range errCh {
		allErrs = append(allErrs, err)
	}
	return utilerrors.NewAggregate(allErrs)
}

func (deployer *Deployer) addLabelsToReferredFeeds(b *appsapi.Base) error {
	var allHelmCharts []*appsapi.HelmChart
	var allManifests []*appsapi.Manifest
	var allErrs []error
	for _, feed := range b.Spec.Feeds {
		switch feed.Kind {
		case helmChartKind.Kind:
			chart, err := deployer.chartLister.HelmCharts(feed.Namespace).Get(feed.Name)
			if err == nil {
				allHelmCharts = append(allHelmCharts, chart)
			} else {
				allErrs = append(allErrs, err)
			}
		default:
			manifests, err := utils.ListManifestsBySelector(deployer.reservedNamespace, deployer.mfstLister, feed)
			if err == nil {
				allManifests = append(allManifests, manifests...)
			} else {
				allErrs = append(allErrs, err)
			}
		}
	}
	if len(allErrs) > 0 {
		return utilerrors.NewAggregate(allErrs)
	}

	labelsToPatch := map[string]*string{
		string(b.UID): utilpointer.StringPtr(baseKind.Kind),
	}
	if len(b.Labels[known.ConfigSubscriptionUIDLabel]) > 0 {
		labelsToPatch[b.Labels[known.ConfigSubscriptionUIDLabel]] = utilpointer.StringPtr(subscriptionKind.Kind)
	}

	wg := sync.WaitGroup{}
	wg.Add(len(allHelmCharts) + len(allManifests))
	errCh := make(chan error, len(allHelmCharts)+len(allManifests))
	for _, chart := range allHelmCharts {
		go func(chart *appsapi.HelmChart) {
			defer wg.Done()

			if chart.DeletionTimestamp != nil {
				return
			}
			if err := utils.PatchHelmChartLabelsAndAnnotations(deployer.clusternetClient, chart, labelsToPatch, nil); err != nil {
				errCh <- err
			}
		}(chart)
	}
	for _, manifest := range allManifests {
		go func(manifest *appsapi.Manifest) {
			defer wg.Done()

			if manifest.DeletionTimestamp != nil {
				return
			}
			if err := utils.PatchManifestLabelsAndAnnotations(deployer.clusternetClient, manifest, labelsToPatch, nil); err != nil {
				errCh <- err
			}
		}(manifest)
	}

	wg.Wait()

	// collect errors
	close(errCh)
	for err := range errCh {
		allErrs = append(allErrs, err)
	}
	return utilerrors.NewAggregate(allErrs)
}

func (deployer *Deployer) removeLabelsFromReferredFeeds(uid types.UID, kind string) error {
	allHelmCharts, err := deployer.chartLister.List(labels.SelectorFromSet(
		labels.Set{string(uid): kind}))
	if err != nil {
		return err
	}
	allManifests, err := deployer.mfstLister.List(labels.SelectorFromSet(
		labels.Set{string(uid): kind}))
	if err != nil {
		return err
	}
	if allHelmCharts == nil && allManifests == nil {
		return nil
	}

	labelsToPatch := map[string]*string{
		string(uid): nil,
	}

	wg := sync.WaitGroup{}
	wg.Add(len(allHelmCharts) + len(allManifests))
	errCh := make(chan error, len(allHelmCharts)+len(allManifests))
	for _, chart := range allHelmCharts {
		go func(chart *appsapi.HelmChart) {
			defer wg.Done()

			if chart.DeletionTimestamp != nil {
				return
			}
			if err := utils.PatchHelmChartLabelsAndAnnotations(deployer.clusternetClient, chart, labelsToPatch, nil); err != nil {
				errCh <- err
			}
		}(chart)
	}
	for _, manifest := range allManifests {
		go func(manifest *appsapi.Manifest) {
			defer wg.Done()

			if manifest.DeletionTimestamp != nil {
				return
			}
			if err := utils.PatchManifestLabelsAndAnnotations(deployer.clusternetClient, manifest, labelsToPatch, nil); err != nil {
				errCh <- err
			}
		}(manifest)
	}

	wg.Wait()

	// collect errors
	close(errCh)
	var allErrs []error
	for err := range errCh {
		allErrs = append(allErrs, err)
	}
	return utilerrors.NewAggregate(allErrs)
}

func (deployer *Deployer) protectManifestFeed(manifest *appsapi.Manifest) error {
	// find all Subscriptions that referring this manifest
	allRelatedSubscriptions, allSubInfos, err := findAllMatchingSubscriptions(deployer.subLister, manifest.Labels)
	if err != nil {
		return err
	}

	// block Manifest deletion until all Subscriptions that refer this Feed get deleted
	if utils.ContainsString(manifest.Finalizers, known.FeedProtectionFinalizer) && len(allRelatedSubscriptions) > 0 {
		msg := fmt.Sprintf("block deleting current %s until all Subscriptions (including %s) that refer this as a feed get deleted",
			manifest.Labels[known.ConfigKindLabel], strings.Join(allSubInfos, ", "))
		klog.WarningDepth(5, msg)

		annotationsToPatch := map[string]*string{}
		annotationsToPatch[known.FeedProtectionAnnotation] = utilpointer.StringPtr(msg)
		if err := utils.PatchManifestLabelsAndAnnotations(deployer.clusternetClient, manifest,
			nil, annotationsToPatch); err != nil {
			return err
		}

		return errors.New(msg)
	}

	// finalizer FeedProtectionFinalizer does not exist,
	// so we just remove this feed from all Subscriptions
	return removeFeedFromAllMatchingSubscriptions(deployer.clusternetClient, allRelatedSubscriptions, manifest.Labels)
}

func (deployer *Deployer) protectHelmChartFeed(chart *appsapi.HelmChart) error {
	// find all Subscriptions that referring this manifest
	allRelatedSubscriptions, allSubInfos, err := findAllMatchingSubscriptions(deployer.subLister, chart.Labels)
	if err != nil {
		return err
	}

	// block HelmChart deletion until all Subscriptions that refer this Feed get deleted
	if utils.ContainsString(chart.Finalizers, known.FeedProtectionFinalizer) && len(allRelatedSubscriptions) > 0 {
		msg := fmt.Sprintf("block deleting current HelmChart until all Subscriptions (including %s) that refer this as a feed get deleted",
			strings.Join(allSubInfos, ", "))
		klog.WarningDepth(5, msg)

		annotationsToPatch := map[string]*string{}
		annotationsToPatch[known.FeedProtectionAnnotation] = utilpointer.StringPtr(msg)
		if err := utils.PatchHelmChartLabelsAndAnnotations(deployer.clusternetClient, chart,
			nil, annotationsToPatch); err != nil {
			return err
		}

		return errors.New(msg)
	}

	// finalizer FeedProtectionFinalizer does not exist,
	// so we just remove this feed from all Subscriptions
	return removeFeedFromAllMatchingSubscriptions(deployer.clusternetClient, allRelatedSubscriptions, chart.Labels)
}

func (deployer *Deployer) AggregateDeploymentStatus(feed appsapi.Feed, status []appsapi.AggregatedStatusItem) error {
	if feed.APIVersion != "apps/v1" {
		return nil
	}

	obj, err := deployer.kubectlClusternetclient.AppsV1().Deployments(feed.Namespace).Get(context.TODO(), feed.Name, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("Failed to get deployment(%s/%s): %v", feed.Namespace, feed.Name, err)
		return err
	}

	oldStatus := &obj.Status
	newStatus := &appsv1.DeploymentStatus{}
	for _, item := range status {
		if item.Cluster == "" || len(item.ManifestStatuses) == 0 {
			continue
		}
		temp := &appsv1.DeploymentStatus{}
		for _, manifestStatus := range item.ManifestStatuses {
			if manifestStatus.Feed.Kind == feed.Kind &&
				manifestStatus.Feed.Namespace == feed.Namespace && manifestStatus.Feed.Name == feed.Name {
				if err := json.Unmarshal(manifestStatus.FeedStatus.Raw, temp); err != nil {
					klog.Errorf("Failed to unmarshal status")
					return err
				}
				klog.V(3).Infof("Grab deployment(%s/%s) status from cluster(%s), replicas: %d, ready: %d, updated: %d, available: %d, unavailable: %d",
					obj.Namespace, obj.Name, item.Cluster, temp.Replicas, temp.ReadyReplicas, temp.UpdatedReplicas, temp.AvailableReplicas, temp.UnavailableReplicas)
				newStatus.ObservedGeneration = obj.Generation
				newStatus.Replicas += temp.Replicas
				newStatus.ReadyReplicas += temp.ReadyReplicas
				newStatus.UpdatedReplicas += temp.UpdatedReplicas
				newStatus.AvailableReplicas += temp.AvailableReplicas
				newStatus.UnavailableReplicas += temp.UnavailableReplicas
				break
			}
		}
	}

	if oldStatus.ObservedGeneration == newStatus.ObservedGeneration &&
		oldStatus.Replicas == newStatus.Replicas &&
		oldStatus.ReadyReplicas == newStatus.ReadyReplicas &&
		oldStatus.UpdatedReplicas == newStatus.UpdatedReplicas &&
		oldStatus.AvailableReplicas == newStatus.AvailableReplicas &&
		oldStatus.UnavailableReplicas == newStatus.UnavailableReplicas {
		klog.V(3).Infof("ignore update deployment(%s/%s) status as up to date", obj.Namespace, obj.Name)
		return nil
	}

	oldStatus.ObservedGeneration = newStatus.ObservedGeneration
	oldStatus.Replicas = newStatus.Replicas
	oldStatus.ReadyReplicas = newStatus.ReadyReplicas
	oldStatus.UpdatedReplicas = newStatus.UpdatedReplicas
	oldStatus.AvailableReplicas = newStatus.AvailableReplicas
	oldStatus.UnavailableReplicas = newStatus.UnavailableReplicas

	if _, err = deployer.kubectlClusternetclient.AppsV1().Deployments(obj.Namespace).Update(context.TODO(), obj, metav1.UpdateOptions{}); err != nil {
		klog.Errorf("Failed to update deployment(%s/%s) status: %v", obj.Namespace, obj.Name, err)
		return err
	}

	return nil
}

// AggregateStatefulSetStatus summarize statefulset status and update to original objects.
func (deployer *Deployer) AggregateStatefulSetStatus(feed appsapi.Feed, status []appsapi.AggregatedStatusItem) error {
	if feed.APIVersion != "apps/v1" {
		return nil

	}

	obj, err := deployer.kubectlClusternetclient.AppsV1().StatefulSets(feed.Namespace).Get(context.TODO(), feed.Name, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("Failed to get statefulset(%s/%s): %v", feed.Namespace, feed.Name, err)
		return err
	}

	oldStatus := &obj.Status
	newStatus := &appsv1.StatefulSetStatus{}
	for _, item := range status {
		if item.Cluster == "" || len(item.ManifestStatuses) == 0 {
			continue
		}
		temp := &appsv1.StatefulSetStatus{}
		for _, manifestStatus := range item.ManifestStatuses {
			if manifestStatus.Feed.Kind == feed.Kind &&
				manifestStatus.Feed.Namespace == feed.Namespace && manifestStatus.Feed.Name == feed.Name {
				if err := json.Unmarshal(manifestStatus.FeedStatus.Raw, temp); err != nil {
					klog.Errorf("Failed to unmarshal status")
					return err
				}
				klog.V(3).Infof("Grab deployment(%s/%s) status from cluster(%s), replicas: %d, ready: %d, updated: %d",
					obj.Namespace, obj.Name, item.Cluster, temp.Replicas, temp.ReadyReplicas, temp.UpdatedReplicas)
				newStatus.ObservedGeneration = obj.Generation
				newStatus.Replicas += temp.Replicas
				newStatus.ReadyReplicas += temp.ReadyReplicas
				newStatus.CurrentReplicas += temp.CurrentReplicas
				newStatus.UpdatedReplicas += temp.UpdatedReplicas
				newStatus.AvailableReplicas += temp.AvailableReplicas
				break
			}
		}
	}

	if oldStatus.ObservedGeneration == newStatus.ObservedGeneration &&
		oldStatus.Replicas == newStatus.Replicas &&
		oldStatus.ReadyReplicas == newStatus.ReadyReplicas &&
		oldStatus.CurrentReplicas == newStatus.CurrentReplicas &&
		oldStatus.UpdatedReplicas == newStatus.UpdatedReplicas &&
		oldStatus.AvailableReplicas == newStatus.AvailableReplicas {
		klog.V(3).Infof("ignore update statefulset(%s/%s) status as up to date", obj.Namespace, obj.Name)
		return nil
	}

	oldStatus.ObservedGeneration = newStatus.ObservedGeneration
	oldStatus.Replicas = newStatus.Replicas
	oldStatus.ReadyReplicas = newStatus.ReadyReplicas
	oldStatus.CurrentReplicas = newStatus.CurrentReplicas
	oldStatus.UpdatedReplicas = newStatus.UpdatedReplicas
	oldStatus.AvailableReplicas = newStatus.AvailableReplicas

	if _, err = deployer.kubectlClusternetclient.AppsV1().StatefulSets(obj.Namespace).Update(context.TODO(), obj, metav1.UpdateOptions{}); err != nil {
		klog.Errorf("Failed to update statefulset(%s/%s) status: %v", obj.Namespace, obj.Name, err)
		return err
	}

	return nil
}

// AggregateServiceStatus summarize service status and update to original objects.
func (deployer *Deployer) AggregateServiceStatus(feed appsapi.Feed, status []appsapi.AggregatedStatusItem) error {
	if feed.APIVersion != "v1" {
		return nil
	}

	obj, err := deployer.kubectlClusternetclient.CoreV1().Services(feed.Namespace).Get(context.TODO(), feed.Name, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("Failed to get service(%s/%s): %v", feed.Namespace, feed.Name, err)
		return err
	}

	if obj.Spec.Type != corev1.ServiceTypeLoadBalancer {
		return nil
	}

	// If service type is of type LoadBalancer, collect the status.loadBalancer.ingress
	newStatus := &corev1.ServiceStatus{}
	for _, item := range status {
		if item.Cluster == "" || len(item.ManifestStatuses) == 0 {
			continue
		}
		temp := &corev1.ServiceStatus{}
		for _, manifestStatus := range item.ManifestStatuses {
			if manifestStatus.Feed.Kind == feed.Kind &&
				manifestStatus.Feed.Namespace == feed.Namespace && manifestStatus.Feed.Name == feed.Name {
				if err := json.Unmarshal(manifestStatus.FeedStatus.Raw, temp); err != nil {
					klog.Errorf("Failed to unmarshal status")
					return err
				}
				klog.V(3).Infof("Grab service(%s/%s) status from cluster(%s), loadBalancer status: %v",
					obj.Namespace, obj.Name, item.Cluster, temp.LoadBalancer)

				// Set cluster name as Hostname by default to indicate the status is collected from which member cluster.
				for i := range temp.LoadBalancer.Ingress {
					if temp.LoadBalancer.Ingress[i].Hostname == "" {
						temp.LoadBalancer.Ingress[i].Hostname = item.Cluster
					}
				}
				break
			}
		}
		newStatus.LoadBalancer.Ingress = append(newStatus.LoadBalancer.Ingress, temp.LoadBalancer.Ingress...)
	}

	if reflect.DeepEqual(obj.Status, *newStatus) {
		klog.V(3).Infof("ignore update service(%s/%s) status as up to date", obj.Namespace, obj.Name)
		return nil
	}

	obj.Status = *newStatus
	if _, err = deployer.kubectlClusternetclient.CoreV1().Services(obj.Namespace).Update(context.TODO(), obj, metav1.UpdateOptions{}); err != nil {
		klog.Errorf("Failed to update service(%s/%s) status: %v", obj.Namespace, obj.Name, err)
		return err
	}

	return nil
}

// AggregateJobStatus summarize job status and update to original objects.
func (deployer *Deployer) AggregateJobStatus(feed appsapi.Feed, status []appsapi.AggregatedStatusItem, cluster []string) error {
	if feed.APIVersion != "batch/v1" {
		return nil
	}

	obj, err := deployer.kubectlClusternetclient.BatchV1().Jobs(feed.Namespace).Get(context.TODO(), feed.Name, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("Failed to get job(%s/%s): %v", feed.Namespace, feed.Name, err)
		return err
	}

	newStatus, err := ParsingJobStatus(obj, status, cluster)
	if err != nil {
		return err
	}

	if reflect.DeepEqual(obj.Status, *newStatus) {
		klog.V(3).Infof("ignore update job(%s/%s) status as up to date", obj.Namespace, obj.Name)
		return nil
	}

	obj.Status = *newStatus
	if _, err = deployer.kubectlClusternetclient.BatchV1().Jobs(obj.Namespace).Update(context.TODO(), obj, metav1.UpdateOptions{}); err != nil {
		klog.Errorf("Failed to update job(%s/%s) status: %v", obj.Namespace, obj.Name, err)
		return err
	}
	return nil
}

// ParsingJobStatus generates new status of given 'AggregatedStatusItem'.
//nolint:gocyclo
func ParsingJobStatus(obj *batchv1.Job, status []appsapi.AggregatedStatusItem, clusters []string) (*batchv1.JobStatus, error) {
	var jobFailed []string
	var startTime, completionTime *metav1.Time
	successfulJobs, completionJobs := 0, 0
	newStatus := &batchv1.JobStatus{}
	for _, item := range status {
		if item.Cluster == "" || len(item.ManifestStatuses) == 0 {
			continue
		}
		temp := &batchv1.JobStatus{}
		for _, manifestStatus := range item.ManifestStatuses {
			if manifestStatus.Feed.Kind == obj.Kind &&
				manifestStatus.Feed.Namespace == obj.Namespace && manifestStatus.Feed.Name == obj.Name {
				if err := json.Unmarshal(manifestStatus.FeedStatus.Raw, temp); err != nil {
					klog.Errorf("Failed to unmarshal status of job(%s/%s): %v", obj.Namespace, obj.Name, err)
					return nil, err
				}
				klog.V(3).Infof("Grab job(%s/%s) status from cluster(%s), active: %d, succeeded %d, failed: %d",
					obj.Namespace, obj.Name, item.Cluster, temp.Active, temp.Succeeded, temp.Failed)

				newStatus.Active += temp.Active
				newStatus.Succeeded += temp.Succeeded
				newStatus.Failed += temp.Failed
				break
			}
		}

		isFinished, finishedStatus := getJobFinishedStatus(temp)
		if isFinished && finishedStatus == batchv1.JobComplete {
			successfulJobs++
		} else if isFinished && finishedStatus == batchv1.JobFailed {
			jobFailed = append(jobFailed, item.Cluster)
		}

		// StartTime
		if startTime == nil || temp.StartTime.Before(startTime) {
			startTime = temp.StartTime
		}
		// CompletionTime
		if temp.CompletionTime != nil {
			completionJobs++
			if completionTime == nil || completionTime.Before(temp.CompletionTime) {
				completionTime = temp.CompletionTime
			}
		}
	}

	if len(jobFailed) != 0 {
		newStatus.Conditions = append(newStatus.Conditions, batchv1.JobCondition{
			Type:               batchv1.JobFailed,
			Status:             corev1.ConditionTrue,
			LastProbeTime:      metav1.Now(),
			LastTransitionTime: metav1.Now(),
			Reason:             "JobFailed",
			Message:            fmt.Sprintf("Job executed failed in member clusters %s", strings.Join(jobFailed, ",")),
		})
	}

	if successfulJobs == len(clusters) {
		newStatus.Conditions = append(newStatus.Conditions, batchv1.JobCondition{
			Type:               batchv1.JobComplete,
			Status:             corev1.ConditionTrue,
			LastProbeTime:      metav1.Now(),
			LastTransitionTime: metav1.Now(),
			Reason:             "Completed",
			Message:            "Job completed",
		})
	}

	if startTime != nil {
		newStatus.StartTime = startTime.DeepCopy()
	}
	if completionTime != nil && completionJobs == len(clusters) {
		newStatus.CompletionTime = completionTime.DeepCopy()
	}

	return newStatus, nil
}

func getJobFinishedStatus(jobStatus *batchv1.JobStatus) (bool, batchv1.JobConditionType) {
	for _, c := range jobStatus.Conditions {
		if (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) && c.Status == corev1.ConditionTrue {
			return true, c.Type
		}
	}
	return false, ""
}
