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
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/repo"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	utilpointer "k8s.io/utils/pointer"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/controllers/apps/base"
	"github.com/clusternet/clusternet/pkg/controllers/apps/helmchart"
	"github.com/clusternet/clusternet/pkg/controllers/apps/manifest"
	"github.com/clusternet/clusternet/pkg/controllers/apps/subscription"
	"github.com/clusternet/clusternet/pkg/features"
	clusternetclientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	clusternetinformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	clusterlisters "github.com/clusternet/clusternet/pkg/generated/listers/clusters/v1beta1"
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
	defaultScheduler = "default"
)

// Deployer defines configuration for the application deployer
type Deployer struct {
	chartLister   applisters.HelmChartLister
	chartSynced   cache.InformerSynced
	descLister    applisters.DescriptionLister
	descSynced    cache.InformerSynced
	baseLister    applisters.BaseLister
	baseSynced    cache.InformerSynced
	mfstLister    applisters.ManifestLister
	mfstSynced    cache.InformerSynced
	subLister     applisters.SubscriptionLister
	subSynced     cache.InformerSynced
	clusterLister clusterlisters.ManagedClusterLister
	clusterSynced cache.InformerSynced

	clusternetClient *clusternetclientset.Clientset

	subsController  *subscription.Controller
	mfstController  *manifest.Controller
	baseController  *base.Controller
	chartController *helmchart.Controller

	helmDeployer    *helm.Deployer
	genericDeployer *generic.Deployer

	localizer *localizer.Localizer

	recorder record.EventRecorder

	// apiserver url of parent cluster
	apiserverURL string
}

func NewDeployer(apiserverURL string, kubeclient *kubernetes.Clientset, clusternetclient *clusternetclientset.Clientset,
	clusternetInformerFactory clusternetinformers.SharedInformerFactory, kubeInformerFactory kubeinformers.SharedInformerFactory,
	recorder record.EventRecorder) (*Deployer, error) {
	feedInUseProtection := utilfeature.DefaultFeatureGate.Enabled(features.FeedInUseProtection)

	deployer := &Deployer{
		apiserverURL:     apiserverURL,
		chartLister:      clusternetInformerFactory.Apps().V1alpha1().HelmCharts().Lister(),
		chartSynced:      clusternetInformerFactory.Apps().V1alpha1().HelmCharts().Informer().HasSynced,
		descLister:       clusternetInformerFactory.Apps().V1alpha1().Descriptions().Lister(),
		descSynced:       clusternetInformerFactory.Apps().V1alpha1().Descriptions().Informer().HasSynced,
		baseLister:       clusternetInformerFactory.Apps().V1alpha1().Bases().Lister(),
		baseSynced:       clusternetInformerFactory.Apps().V1alpha1().Bases().Informer().HasSynced,
		mfstLister:       clusternetInformerFactory.Apps().V1alpha1().Manifests().Lister(),
		mfstSynced:       clusternetInformerFactory.Apps().V1alpha1().Manifests().Informer().HasSynced,
		subLister:        clusternetInformerFactory.Apps().V1alpha1().Subscriptions().Lister(),
		subSynced:        clusternetInformerFactory.Apps().V1alpha1().Subscriptions().Informer().HasSynced,
		clusterLister:    clusternetInformerFactory.Clusters().V1beta1().ManagedClusters().Lister(),
		clusterSynced:    clusternetInformerFactory.Clusters().V1beta1().ManagedClusters().Informer().HasSynced,
		clusternetClient: clusternetclient,
		recorder:         recorder,
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

	helmDeployer, err := helm.NewDeployer(apiserverURL, clusternetclient, kubeclient, clusternetInformerFactory,
		kubeInformerFactory, deployer.recorder)
	if err != nil {
		return nil, err
	}
	deployer.helmDeployer = helmDeployer

	genericDeployer, err := generic.NewDeployer(apiserverURL, clusternetclient, clusternetInformerFactory,
		kubeInformerFactory, deployer.recorder)
	if err != nil {
		return nil, err
	}
	deployer.genericDeployer = genericDeployer

	subsController, err := subscription.NewController(clusternetclient,
		clusternetInformerFactory.Apps().V1alpha1().Subscriptions(),
		clusternetInformerFactory.Apps().V1alpha1().Bases(),
		clusternetInformerFactory.Clusters().V1beta1().ManagedClusters(),
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
		deployer.handleManifest)
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
		deployer.handleHelmChart, deployer.handleManifest, deployer.recorder)
	if err != nil {
		return nil, err
	}
	deployer.localizer = l

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
		deployer.clusterSynced,
		deployer.subSynced,
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

	if sub.Spec.SchedulerName != defaultScheduler {
		klog.V(4).Infof("Subscription %s is using customized scheduler %q ", klog.KObj(sub), sub.Spec.SchedulerName)
		deployer.recorder.Event(sub, corev1.EventTypeNormal, "SkipScheduling",
			fmt.Sprintf("customized scheduler %s is specified", sub.Spec.SchedulerName))
		return nil
	}

	err := deployer.populateBases(sub)
	if err != nil {
		return err
	}

	return nil
}

func (deployer *Deployer) populateBases(sub *appsapi.Subscription) error {
	var mcls []*clusterapi.ManagedCluster
	for _, subscriber := range sub.Spec.Subscribers {
		selector, err := metav1.LabelSelectorAsSelector(subscriber.ClusterAffinity)
		if err != nil {
			return err
		}
		clusters, err := deployer.clusterLister.ManagedClusters("").List(selector)
		if err != nil {
			return err
		}

		if clusters == nil {
			deployer.recorder.Event(sub, corev1.EventTypeWarning, "NoClusters", "No clusters get matched")
			return nil
		}

		mcls = append(mcls, clusters...)
	}

	allExistingBases, err := deployer.baseLister.List(labels.SelectorFromSet(labels.Set{
		known.ConfigKindLabel:      subscriptionKind.Kind,
		known.ConfigNameLabel:      sub.Name,
		known.ConfigNamespaceLabel: sub.Namespace,
		known.ConfigUIDLabel:       string(sub.UID),
	}))
	if err != nil {
		return err
	}
	// Bases to be deleted
	basesToBeDeleted := sets.String{}
	for _, base := range allExistingBases {
		basesToBeDeleted.Insert(klog.KObj(base).String())
	}

	var allErrs []error
	for _, cluster := range mcls {
		base := &appsapi.Base{
			ObjectMeta: metav1.ObjectMeta{
				Name:      sub.Name,
				Namespace: cluster.Namespace,
				Labels: map[string]string{
					known.ObjectCreatedByLabel: known.ClusternetHubName,
					known.ConfigKindLabel:      subscriptionKind.Kind,
					known.ConfigNameLabel:      sub.Name,
					known.ConfigNamespaceLabel: sub.Namespace,
					known.ConfigUIDLabel:       string(sub.UID),
					known.ClusterIDLabel:       cluster.Labels[known.ClusterIDLabel],
					known.ClusterNameLabel:     cluster.Labels[known.ClusterNameLabel],
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

		err := deployer.syncBase(sub, base)
		if err != nil {
			allErrs = append(allErrs, err)
			msg := fmt.Sprintf("Failed to sync Base %s: %v", klog.KObj(base), err)
			klog.ErrorDepth(5, msg)
			deployer.recorder.Event(sub, corev1.EventTypeWarning, "FailedSyncingBase", msg)
		}
		basesToBeDeleted.Delete(klog.KObj(base).String())
	}

	for key := range basesToBeDeleted {
		err := deployer.deleteBase(context.TODO(), key)
		if err != nil {
			allErrs = append(allErrs, err)
		}
	}

	return utilerrors.NewAggregate(allErrs)
}

func (deployer *Deployer) syncBase(sub *appsapi.Subscription, base *appsapi.Base) error {
	if curBase, err := deployer.baseLister.Bases(base.Namespace).Get(base.Name); err == nil {
		if curBase.DeletionTimestamp != nil {
			return fmt.Errorf("Base %s is deleting, will resync later", klog.KObj(curBase))
		}

		// update it
		if !reflect.DeepEqual(curBase.Spec, base.Spec) {
			curBaseCopy := curBase.DeepCopy()
			if curBaseCopy.Labels == nil {
				curBaseCopy.Labels = make(map[string]string)
			}
			for key, value := range base.Labels {
				curBaseCopy.Labels[key] = value
			}

			curBaseCopy.Spec = base.Spec
			if !utils.ContainsString(curBaseCopy.Finalizers, known.AppFinalizer) {
				curBaseCopy.Finalizers = append(curBaseCopy.Finalizers, known.AppFinalizer)
			}

			_, err = deployer.clusternetClient.AppsV1alpha1().Bases(curBaseCopy.Namespace).Update(context.TODO(),
				curBaseCopy, metav1.UpdateOptions{})
			if err == nil {
				msg := fmt.Sprintf("Base %s is updated successfully", klog.KObj(curBaseCopy))
				klog.V(4).Info(msg)
				deployer.recorder.Event(sub, corev1.EventTypeNormal, "BaseUpdated", msg)
			}
			return err
		}
		return nil
	} else {
		if !apierrors.IsNotFound(err) {
			return err
		}
	}

	_, err := deployer.clusternetClient.AppsV1alpha1().Bases(base.Namespace).Create(context.TODO(),
		base, metav1.CreateOptions{})
	if err == nil {
		msg := fmt.Sprintf("Base %s is created successfully", klog.KObj(base))
		klog.V(4).Info(msg)
		deployer.recorder.Event(sub, corev1.EventTypeNormal, "BaseCreated", msg)
	}
	return err
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
			if chart.Status.Phase != appsapi.HelmChartFound {
				deployer.recorder.Event(base, corev1.EventTypeWarning, "HelmChartNotFound",
					fmt.Sprintf("helm chart %s is not found", klog.KObj(chart)))
				return nil
			} else {
				allChartRefs = append(allChartRefs, appsapi.ChartReference{
					Namespace: chart.Namespace,
					Name:      chart.Name,
				})
			}
		default:
			manifests, err = utils.ListManifestsBySelector(deployer.mfstLister, feed)
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
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         baseKind.Version,
					Kind:               baseKind.Kind,
					Name:               base.Name,
					UID:                base.UID,
					Controller:         utilpointer.BoolPtr(true),
					BlockOwnerDeletion: utilpointer.BoolPtr(true),
				},
			},
		},
	}

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
	klog.V(5).Infof("handle HelmChart %s", klog.KObj(chart))
	if chart.DeletionTimestamp != nil {
		if err := deployer.protectHelmChartFeed(chart); err != nil {
			return err
		}

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
		getter.All(utils.Settings))
	if err != nil {
		// failed to find chart
		return deployer.chartController.UpdateChartStatus(chart, &appsapi.HelmChartStatus{
			Phase:  appsapi.HelmChartNotFound,
			Reason: err.Error(),
		})
	}
	err = deployer.chartController.UpdateChartStatus(chart, &appsapi.HelmChartStatus{
		Phase: appsapi.HelmChartFound,
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
			manifests, err := utils.ListManifestsBySelector(deployer.mfstLister, feed)
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
