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
	"sync"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	utilpointer "k8s.io/utils/pointer"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/controllers/apps/base"
	"github.com/clusternet/clusternet/pkg/controllers/apps/manifest"
	"github.com/clusternet/clusternet/pkg/controllers/apps/subscription"
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
	helmChartKind    = appsapi.SchemeGroupVersion.WithKind("HelmChart")
	subscriptionKind = appsapi.SchemeGroupVersion.WithKind("Subscription")
	baseKind         = appsapi.SchemeGroupVersion.WithKind("Base")
)

const (
	defaultScheduler = "default"
)

// Deployer defines configuration for the application deployer
type Deployer struct {
	ctx context.Context

	chartLister   applisters.HelmChartLister
	chartSynced   cache.InformerSynced
	descLister    applisters.DescriptionLister
	descSynced    cache.InformerSynced
	baseLister    applisters.BaseLister
	baseSynced    cache.InformerSynced
	mfstLister    applisters.ManifestLister
	mfstSynced    cache.InformerSynced
	clusterLister clusterlisters.ManagedClusterLister
	clusterSynced cache.InformerSynced

	kubeClient       *kubernetes.Clientset
	clusternetClient *clusternetclientset.Clientset

	subsController *subscription.Controller
	mfstController *manifest.Controller
	baseController *base.Controller

	helmDeployer    *helm.Deployer
	genericDeployer *generic.Deployer

	localizer *localizer.Localizer

	broadcaster record.EventBroadcaster
	recorder    record.EventRecorder
}

func NewDeployer(ctx context.Context, kubeclient *kubernetes.Clientset, clusternetclient *clusternetclientset.Clientset,
	clusternetInformerFactory clusternetinformers.SharedInformerFactory, kubeInformerFactory kubeinformers.SharedInformerFactory) (*Deployer, error) {

	deployer := &Deployer{
		ctx:              ctx,
		chartLister:      clusternetInformerFactory.Apps().V1alpha1().HelmCharts().Lister(),
		chartSynced:      clusternetInformerFactory.Apps().V1alpha1().HelmCharts().Informer().HasSynced,
		descLister:       clusternetInformerFactory.Apps().V1alpha1().Descriptions().Lister(),
		descSynced:       clusternetInformerFactory.Apps().V1alpha1().Descriptions().Informer().HasSynced,
		clusterLister:    clusternetInformerFactory.Clusters().V1beta1().ManagedClusters().Lister(),
		clusterSynced:    clusternetInformerFactory.Clusters().V1beta1().ManagedClusters().Informer().HasSynced,
		baseLister:       clusternetInformerFactory.Apps().V1alpha1().Bases().Lister(),
		baseSynced:       clusternetInformerFactory.Apps().V1alpha1().Bases().Informer().HasSynced,
		mfstLister:       clusternetInformerFactory.Apps().V1alpha1().Manifests().Lister(),
		mfstSynced:       clusternetInformerFactory.Apps().V1alpha1().Manifests().Informer().HasSynced,
		kubeClient:       kubeclient,
		clusternetClient: clusternetclient,
		broadcaster:      record.NewBroadcaster(),
	}

	//deployer.broadcaster.StartStructuredLogging(5)
	if deployer.kubeClient != nil {
		klog.Infof("sending events to api server")
		deployer.broadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: deployer.kubeClient.CoreV1().Events("")})
	} else {
		klog.Warningf("no api server defined - no events will be sent to API server.")
	}
	deployer.recorder = deployer.broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "clusternet-hub"})

	helmDeployer, err := helm.NewDeployer(ctx, clusternetclient, kubeclient, clusternetInformerFactory, kubeInformerFactory, deployer.recorder)
	if err != nil {
		return nil, err
	}
	deployer.helmDeployer = helmDeployer

	genericDeployer, err := generic.NewDeployer(ctx, clusternetclient, clusternetInformerFactory, kubeInformerFactory, deployer.recorder)
	if err != nil {
		return nil, err
	}
	deployer.genericDeployer = genericDeployer

	subsController, err := subscription.NewController(ctx,
		clusternetclient,
		clusternetInformerFactory.Apps().V1alpha1().Subscriptions(),
		clusternetInformerFactory.Apps().V1alpha1().Bases(),
		deployer.recorder,
		deployer.handleSubscription)
	if err != nil {
		return nil, err
	}
	deployer.subsController = subsController

	mfstController, err := manifest.NewController(ctx,
		clusternetclient,
		clusternetInformerFactory.Apps().V1alpha1().Manifests(),
		deployer.recorder,
		deployer.handleManifest)
	if err != nil {
		return nil, err
	}
	deployer.mfstController = mfstController

	baseController, err := base.NewController(ctx,
		clusternetclient,
		clusternetInformerFactory.Apps().V1alpha1().Bases(),
		clusternetInformerFactory.Apps().V1alpha1().Descriptions(),
		deployer.recorder,
		deployer.handleBase)
	if err != nil {
		return nil, err
	}
	deployer.baseController = baseController

	l, err := localizer.NewLocalizer(ctx, clusternetclient, clusternetInformerFactory, deployer.recorder)
	if err != nil {
		return nil, err
	}
	deployer.localizer = l

	return deployer, nil
}

func (deployer *Deployer) Run(workers int) {
	klog.Infof("starting Clusternet deployer ...")

	// Wait for the caches to be synced before starting workers
	klog.V(5).Info("waiting for informer caches to sync")
	if !cache.WaitForCacheSync(deployer.ctx.Done(),
		deployer.chartSynced,
		deployer.descSynced,
		deployer.baseSynced,
		deployer.mfstSynced,
		deployer.clusterSynced) {
		return
	}

	go deployer.helmDeployer.Run(workers)
	go deployer.genericDeployer.Run(workers)
	go deployer.subsController.Run(workers, deployer.ctx.Done())
	go deployer.mfstController.Run(workers, deployer.ctx.Done())
	go deployer.baseController.Run(workers, deployer.ctx.Done())
	go deployer.localizer.Run(workers)

	<-deployer.ctx.Done()
}

func (deployer *Deployer) handleSubscription(sub *appsapi.Subscription) error {
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

		sub.Finalizers = utils.RemoveString(sub.Finalizers, known.AppFinalizer)
		_, err = deployer.clusternetClient.AppsV1alpha1().Subscriptions(sub.Namespace).Update(context.TODO(), sub, metav1.UpdateOptions{})
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
			if curBase.Labels == nil {
				curBase.Labels = make(map[string]string)
			}
			for key, value := range base.Labels {
				curBase.Labels[key] = value
			}

			curBase.Spec = base.Spec
			if !utils.ContainsString(curBase.Finalizers, known.AppFinalizer) {
				curBase.Finalizers = append(curBase.Finalizers, known.AppFinalizer)
			}

			_, err = deployer.clusternetClient.AppsV1alpha1().Bases(curBase.Namespace).Update(context.TODO(),
				curBase, metav1.UpdateOptions{})
			if err == nil {
				msg := fmt.Sprintf("Base %s is updated successfully", klog.KObj(curBase))
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
	// remove label (baseUID="Base") from referred manifest
	if err := deployer.removeLabelsFromReferredManifests(base); err != nil {
		return err
	}

	deletePropagationBackground := metav1.DeletePropagationBackground
	err = deployer.clusternetClient.AppsV1alpha1().Bases(ns).Delete(ctx, name, metav1.DeleteOptions{
		PropagationPolicy: &deletePropagationBackground,
	})
	if err != nil && apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (deployer *Deployer) handleBase(base *appsapi.Base) error {
	// add label (baseUID="Base") to referred manifest
	if err := deployer.addLabelsToReferredManifests(base); err != nil {
		return err
	}

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

		base.Finalizers = utils.RemoveString(base.Finalizers, known.AppFinalizer)
		_, err = deployer.clusternetClient.AppsV1alpha1().Bases(base.Namespace).Update(context.TODO(), base, metav1.UpdateOptions{})
		if err != nil {
			klog.WarningDepth(4,
				fmt.Sprintf("failed to remove finalizer %s from Base %s: %v", known.AppFinalizer, klog.KObj(base), err))
		}
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
	var charts []*appsapi.HelmChart
	var manifests []*appsapi.Manifest
	for _, feed := range base.Spec.Feeds {
		if feed.Kind == helmChartKind.Kind && feed.APIVersion == helmChartKind.GroupVersion().String() {
			charts, err = utils.ListChartsBySelector(deployer.chartLister, feed)
			if err == nil {
				// verify HelmChart can be found
				for _, chart := range charts {
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
				}
			}
			if charts == nil {
				err = apierrors.NewNotFound(schema.GroupResource{}, "")
			}
		} else {
			manifests, err = utils.ListManifestsBySelector(deployer.mfstLister, feed)
			if err == nil {
				allManifests = append(allManifests, manifests...)
			}
			if manifests == nil {
				err = apierrors.NewNotFound(schema.GroupResource{}, "")
			}
		}

		if apierrors.IsNotFound(err) {
			msg := fmt.Sprintf("Base %s is using a nonexistent %s", klog.KObj(base), utils.FormatFeed(feed))
			klog.Error(msg)
			deployer.recorder.Event(base, corev1.EventTypeWarning, fmt.Sprintf("Nonexistent%s", feed.Kind), msg)
			return errors.New(msg)
		}
		if err != nil {
			msg := fmt.Sprintf("failed to get matched objects %q for Base %s: %v", utils.FormatFeed(feed), klog.KObj(base), err)
			klog.Error(msg)
			deployer.recorder.Event(base, corev1.EventTypeWarning, "FailedRetrievingObjects", msg)
			return err
		}
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

func (deployer *Deployer) syncDescriptions(base *appsapi.Base, description *appsapi.Description) error {
	// apply overrides
	if err := deployer.localizer.ApplyOverridesToDescription(description); err != nil {
		msg := fmt.Sprintf("Failed to apply overrides for Description %s: %v", klog.KObj(description), err)
		klog.ErrorDepth(5, msg)
		deployer.recorder.Event(base, corev1.EventTypeWarning, "FailedApplyingOverrides", msg)
		return err
	}

	desc, err := deployer.descLister.Descriptions(description.Namespace).Get(description.Name)
	if err == nil {
		if desc.DeletionTimestamp != nil {
			return fmt.Errorf("Description %s is deleting, will resync later", klog.KObj(desc))
		}

		// update it
		if !reflect.DeepEqual(desc.Spec, description.Spec) {
			if desc.Labels == nil {
				desc.Labels = make(map[string]string)
			}
			for key, value := range description.Labels {
				desc.Labels[key] = value
			}

			desc.Spec = description.Spec
			if !utils.ContainsString(desc.Finalizers, known.AppFinalizer) {
				desc.Finalizers = append(desc.Finalizers, known.AppFinalizer)
			}

			_, err = deployer.clusternetClient.AppsV1alpha1().Descriptions(desc.Namespace).Update(context.TODO(),
				desc, metav1.UpdateOptions{})
			if err == nil {
				msg := fmt.Sprintf("Description %s is updated successfully", klog.KObj(description))
				klog.V(4).Info(msg)
				deployer.recorder.Event(base, corev1.EventTypeNormal, "DescriptionUpdated", msg)
			}
			return err
		}
		return nil
	}

	_, err = deployer.clusternetClient.AppsV1alpha1().Descriptions(description.Namespace).Create(context.TODO(),
		description, metav1.CreateOptions{})
	if err == nil {
		msg := fmt.Sprintf("Description %s is created successfully", klog.KObj(description))
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

	deletePropagationBackground := metav1.DeletePropagationBackground
	err = deployer.clusternetClient.AppsV1alpha1().Descriptions(ns).Delete(ctx, name, metav1.DeleteOptions{
		PropagationPolicy: &deletePropagationBackground,
	})
	if err != nil && apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (deployer *Deployer) handleManifest(manifest *appsapi.Manifest) error {
	if manifest.DeletionTimestamp != nil {
		// TODO: other strategies

		// remove finalizer
		manifest.Finalizers = utils.RemoveString(manifest.Finalizers, known.AppFinalizer)
		_, err := deployer.clusternetClient.AppsV1alpha1().Manifests(manifest.Namespace).Update(context.TODO(), manifest, metav1.UpdateOptions{})
		if err != nil {
			klog.WarningDepth(4,
				fmt.Sprintf("failed to remove finalizer %s from Manifest %s: %v", known.AppFinalizer, klog.KObj(manifest), err))
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

func (deployer *Deployer) addLabelsToReferredManifests(b *appsapi.Base) error {
	var allManifests []*appsapi.Manifest
	var allErrs []error
	for _, feed := range b.Spec.Feeds {
		if feed.Kind != helmChartKind.Kind {
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
	if allManifests == nil || len(allManifests) == 0 {
		return nil
	}

	wg := sync.WaitGroup{}
	wg.Add(len(allManifests))
	errCh := make(chan error, len(allManifests))
	for _, manifest := range allManifests {
		go func(manifest *appsapi.Manifest) {
			defer wg.Done()

			if b.DeletionTimestamp != nil {
				return
			}

			curVal, ok := manifest.Labels[string(b.UID)]
			if ok && curVal == baseKind.Kind {
				return
			}
			labelsToPatch := map[string]*string{}
			labelsToPatch[string(b.UID)] = utilpointer.StringPtr(baseKind.Kind)
			if err := deployer.patchManifestLabels(manifest, labelsToPatch); err != nil {
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

func (deployer *Deployer) removeLabelsFromReferredManifests(b *appsapi.Base) error {
	manifests, err := deployer.mfstLister.List(labels.SelectorFromSet(
		labels.Set{string(b.UID): baseKind.Kind}))
	if err != nil {
		return err
	}
	if manifests == nil {
		return nil
	}

	wg := sync.WaitGroup{}
	wg.Add(len(manifests))
	errCh := make(chan error, len(manifests))
	for _, manifest := range manifests {
		go func(manifest *appsapi.Manifest) {
			defer wg.Done()

			if b.DeletionTimestamp != nil {
				return
			}

			_, ok := manifest.Labels[string(b.UID)]
			if !ok {
				return
			}
			labelsToPatch := map[string]*string{}
			labelsToPatch[string(b.UID)] = nil
			if err := deployer.patchManifestLabels(manifest, labelsToPatch); err != nil {
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

func (deployer *Deployer) patchManifestLabels(manifest *appsapi.Manifest, labels map[string]*string) error {
	labelOption := utils.LabelOption{Meta: utils.Meta{Labels: labels}}
	patchData, err := json.Marshal(labelOption)
	if err != nil {
		return err
	}

	_, err = deployer.clusternetClient.AppsV1alpha1().Manifests(manifest.Namespace).Patch(context.TODO(),
		manifest.Name,
		types.MergePatchType,
		patchData,
		metav1.PatchOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
