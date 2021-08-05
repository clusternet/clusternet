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
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
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
	"github.com/clusternet/clusternet/pkg/controllers/apps/description"
	"github.com/clusternet/clusternet/pkg/controllers/apps/manifest"
	"github.com/clusternet/clusternet/pkg/controllers/apps/subscription"
	clusternetclientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	clusternetinformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	clusterlisters "github.com/clusternet/clusternet/pkg/generated/listers/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/hub/deployer/helm"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

var (
	helmChartKind    = appsapi.SchemeGroupVersion.WithKind("HelmChart")
	subscriptionKind = appsapi.SchemeGroupVersion.WithKind("Subscription")
	baseKind         = appsapi.SchemeGroupVersion.WithKind("Base")
	descriptionKind  = appsapi.SchemeGroupVersion.WithKind("Description")
)

const (
	defaultScheduler = "default"
)

// Deployer defines configuration for the application deployer
type Deployer struct {
	ctx context.Context

	chartLister   applisters.HelmChartLister
	descLister    applisters.DescriptionLister
	subsLister    applisters.SubscriptionLister
	hrLister      applisters.HelmReleaseLister
	baseLister    applisters.BaseLister
	mfstLister    applisters.ManifestLister
	clusterLister clusterlisters.ManagedClusterLister

	kubeclient       *kubernetes.Clientset
	clusternetclient *clusternetclientset.Clientset

	subsController *subscription.Controller
	descController *description.Controller
	mfstController *manifest.Controller
	baseController *base.Controller

	helmDeployer *helm.HelmDeployer

	broadcaster record.EventBroadcaster
	recorder    record.EventRecorder
}

func NewDeployer(ctx context.Context, kubeclient *kubernetes.Clientset, clusternetclient *clusternetclientset.Clientset,
	clusternetInformerFactory clusternetinformers.SharedInformerFactory, kubeInformerFactory kubeinformers.SharedInformerFactory) (*Deployer, error) {

	deployer := &Deployer{
		ctx:              ctx,
		chartLister:      clusternetInformerFactory.Apps().V1alpha1().HelmCharts().Lister(),
		descLister:       clusternetInformerFactory.Apps().V1alpha1().Descriptions().Lister(),
		subsLister:       clusternetInformerFactory.Apps().V1alpha1().Subscriptions().Lister(),
		hrLister:         clusternetInformerFactory.Apps().V1alpha1().HelmReleases().Lister(),
		clusterLister:    clusternetInformerFactory.Clusters().V1beta1().ManagedClusters().Lister(),
		baseLister:       clusternetInformerFactory.Apps().V1alpha1().Bases().Lister(),
		mfstLister:       clusternetInformerFactory.Apps().V1alpha1().Manifests().Lister(),
		kubeclient:       kubeclient,
		clusternetclient: clusternetclient,
		broadcaster:      record.NewBroadcaster(),
	}

	//deployer.broadcaster.StartStructuredLogging(5)
	if deployer.kubeclient != nil {
		klog.Infof("sending events to api server")
		deployer.broadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: deployer.kubeclient.CoreV1().Events("")})
	} else {
		klog.Warningf("no api server defined - no events will be sent to API server.")
	}
	deployer.recorder = deployer.broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "clusternet-hub"})

	helmDeployer, err := helm.NewHelmDeployer(ctx, clusternetclient, kubeclient, clusternetInformerFactory, kubeInformerFactory, deployer.recorder)
	if err != nil {
		return nil, err
	}
	deployer.helmDeployer = helmDeployer

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

	descController, err := description.NewController(ctx,
		clusternetclient,
		clusternetInformerFactory.Apps().V1alpha1().Descriptions(),
		clusternetInformerFactory.Apps().V1alpha1().HelmReleases(),
		deployer.recorder,
		deployer.handleDescription)
	if err != nil {
		return nil, err
	}
	deployer.descController = descController

	mfstController, err := manifest.NewController(ctx,
		clusternetclient,
		clusternetInformerFactory.Apps().V1alpha1().Manifests(),
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

	return deployer, nil
}

func (deployer *Deployer) Run(workers int) {
	klog.Infof("starting Clusternet deployer ...")

	go deployer.helmDeployer.Run(workers)
	go deployer.subsController.Run(workers, deployer.ctx.Done())
	go deployer.descController.Run(workers, deployer.ctx.Done())
	go deployer.mfstController.Run(workers, deployer.ctx.Done())
	go deployer.baseController.Run(workers, deployer.ctx.Done())

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
		deletePropagationBackground := metav1.DeletePropagationBackground
		for _, base := range bases {
			if base.DeletionTimestamp != nil {
				continue
			}
			err = deployer.clusternetclient.AppsV1alpha1().Bases(base.Namespace).Delete(context.TODO(), base.Name, metav1.DeleteOptions{
				PropagationPolicy: &deletePropagationBackground,
			})
			if err != nil {
				return err
			}
		}
		if bases != nil {
			return fmt.Errorf("waiting for Bases belongs to Subscription %s getting deleted", klog.KObj(sub))
		}

		sub.Finalizers = utils.RemoveString(sub.Finalizers, known.AppFinalizer)
		_, err = deployer.clusternetclient.AppsV1alpha1().Subscriptions(sub.Namespace).Update(context.TODO(), sub, metav1.UpdateOptions{})
		if err != nil {
			klog.WarningDepth(4,
				fmt.Sprintf("failed to remove finalizer %s from Subscription %s: %v", known.AppFinalizer, klog.KObj(sub), err))
		}
		return err
	}

	if sub.Spec.SchedulerName != defaultScheduler {
		klog.V(4).Infof("Subscription %s is using customized scheduler %q ", klog.KObj(sub), sub.Spec.SchedulerName)
		return nil
	}

	err := deployer.populateBases(sub)
	if err != nil {
		return err
	}

	return nil
}

func (deployer *Deployer) getChartsBySelector(base *appsapi.Base, feed appsapi.Feed) ([]*appsapi.HelmChart, error) {
	namespace := base.Labels[known.ConfigSubscriptionNamespaceLabel]
	if len(feed.Namespace) > 0 {
		namespace = feed.Namespace
	}
	selector, err := getLabelsSelectorFromFeed(feed, namespace)
	if err != nil {
		return nil, err
	}
	return deployer.chartLister.HelmCharts(namespace).List(selector)
}

func (deployer *Deployer) getManifestsBySelector(base *appsapi.Base, feed appsapi.Feed) ([]*appsapi.Manifest, error) {
	namespace := base.Labels[known.ConfigSubscriptionNamespaceLabel]
	if len(feed.Namespace) > 0 {
		namespace = feed.Namespace
	}
	selector, err := getLabelsSelectorFromFeed(feed, namespace)
	if err != nil {
		return nil, err
	}
	return deployer.mfstLister.Manifests(appsapi.ReservedNamespace).List(selector)
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
		basesToBeDeleted.Insert(fmt.Sprintf("%s/%s", base.Namespace, base.Name))
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

		basesToBeDeleted.Delete(fmt.Sprintf("%s/%s", base.Namespace, base.Name))

		err := deployer.syncBase(sub, base)
		if err != nil {
			allErrs = append(allErrs, err)
			msg := fmt.Sprintf("Failed to sync Base %s: %v", klog.KObj(base), err)
			klog.ErrorDepth(5, msg)
			deployer.recorder.Event(sub, corev1.EventTypeWarning, "FailedSyncingBase", msg)
		}
	}

	deletePropagationBackground := metav1.DeletePropagationBackground
	for key := range basesToBeDeleted {
		// Convert the namespace/name string into a distinct namespace and name
		ns, name, err := cache.SplitMetaNamespaceKey(key)
		if err != nil {
			allErrs = append(allErrs, err)
		} else {
			err = deployer.clusternetclient.AppsV1alpha1().Bases(ns).Delete(context.TODO(), name, metav1.DeleteOptions{
				PropagationPolicy: &deletePropagationBackground,
			})
			if err != nil {
				allErrs = append(allErrs, err)
			}
		}
	}

	return utilerrors.NewAggregate(allErrs)
}

func (deployer *Deployer) syncBase(sub *appsapi.Subscription, base *appsapi.Base) error {
	curBase, err := deployer.baseLister.Bases(base.Namespace).Get(base.Name)
	if err == nil {
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

			_, err = deployer.clusternetclient.AppsV1alpha1().Bases(curBase.Namespace).Update(context.TODO(),
				curBase, metav1.UpdateOptions{})
			if err == nil {
				msg := fmt.Sprintf("Base %s is updated successfully", klog.KObj(curBase))
				klog.V(4).Info(msg)
				deployer.recorder.Event(sub, corev1.EventTypeNormal, "BaseUpdated", msg)
			}
			return err
		}
		return nil
	}

	_, err = deployer.clusternetclient.AppsV1alpha1().Bases(curBase.Namespace).Create(context.TODO(),
		base, metav1.CreateOptions{})
	if err == nil {
		msg := fmt.Sprintf("Base %s is created successfully", klog.KObj(base))
		klog.V(4).Info(msg)
		deployer.recorder.Event(sub, corev1.EventTypeNormal, "BaseCreated", msg)
	}
	return err
}

func (deployer *Deployer) handleBase(base *appsapi.Base) error {
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
		deletePropagationBackground := metav1.DeletePropagationBackground
		for _, desc := range descs {
			if desc.DeletionTimestamp != nil {
				continue
			}
			err = deployer.clusternetclient.AppsV1alpha1().Descriptions(base.Namespace).Delete(context.TODO(), desc.Name, metav1.DeleteOptions{
				PropagationPolicy: &deletePropagationBackground,
			})
			if err != nil {
				return err
			}
		}
		if descs != nil {
			return fmt.Errorf("waiting for Descriptions belongs to Base %s getting deleted", klog.KObj(base))
		}

		base.Finalizers = utils.RemoveString(base.Finalizers, known.AppFinalizer)
		_, err = deployer.clusternetclient.AppsV1alpha1().Bases(base.Namespace).Update(context.TODO(), base, metav1.UpdateOptions{})
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
			charts, err = deployer.getChartsBySelector(base, feed)
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
		} else {
			manifests, err = deployer.getManifestsBySelector(base, feed)
			if err == nil {
				allManifests = append(allManifests, manifests...)
			}
		}

		if errors.IsNotFound(err) {
			msg := fmt.Sprintf("Base %s is using a nonexistent %s", klog.KObj(base), formatFeed(feed))
			klog.Error(msg)
			deployer.recorder.Event(base, corev1.EventTypeWarning, fmt.Sprintf("Nonexistent%s", feed.Kind), msg)
			return nil
		}
		if err != nil {
			msg := fmt.Sprintf("failed to get matched objects %q for Base %s: %v", formatFeed(feed), klog.KObj(base), err)
			klog.Error(msg)
			deployer.recorder.Event(base, corev1.EventTypeWarning, "FailedRetrievingObjects", msg)
			return err
		}
	}

	if len(allChartRefs) == 0 && len(allManifests) == 0 {
		deployer.recorder.Event(base, corev1.EventTypeWarning, "NoResourcesMatched", "No resources get matched")
		return nil
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

	// TODO: apply overrides

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
	}

	if len(allManifests) > 0 {
		var rawObjects []runtime.RawExtension
		for _, manifest := range allManifests {
			rawObjects = append(rawObjects, manifest.Template)
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
	}

	return utilerrors.NewAggregate(allErrs)
}

func (deployer *Deployer) syncDescriptions(base *appsapi.Base, description *appsapi.Description) error {
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

			_, err = deployer.clusternetclient.AppsV1alpha1().Descriptions(desc.Namespace).Update(context.TODO(),
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

	_, err = deployer.clusternetclient.AppsV1alpha1().Descriptions(description.Namespace).Create(context.TODO(),
		description, metav1.CreateOptions{})
	if err == nil {
		msg := fmt.Sprintf("Description %s is created successfully", klog.KObj(description))
		klog.V(4).Info(msg)
		deployer.recorder.Event(base, corev1.EventTypeNormal, "DescriptionCreated", msg)
	}
	return err
}

func (deployer *Deployer) handleDescription(desc *appsapi.Description) error {
	if desc.DeletionTimestamp != nil {
		// make sure all controllees have been deleted
		hrs, err := deployer.hrLister.HelmReleases(desc.Namespace).List(labels.SelectorFromSet(labels.Set{
			known.ConfigKindLabel:      descriptionKind.Kind,
			known.ConfigNameLabel:      desc.Name,
			known.ConfigNamespaceLabel: desc.Namespace,
		}))
		if err != nil {
			return err
		}

		deletePropagationBackground := metav1.DeletePropagationBackground
		for _, hr := range hrs {
			if metav1.IsControlledBy(hr, desc) {
				if hr.DeletionTimestamp == nil {
					return deployer.clusternetclient.AppsV1alpha1().HelmReleases(hr.Namespace).Delete(context.TODO(), hr.Name, metav1.DeleteOptions{
						PropagationPolicy: &deletePropagationBackground,
					})
				}
				return fmt.Errorf("waiting for HelmRelease %s getting deleted", klog.KObj(hr))
			}
		}

		if hrs != nil {
			return fmt.Errorf("waiting for HelmRelease belongs to Description %s getting deleted", klog.KObj(desc))
		}

		desc.Finalizers = utils.RemoveString(desc.Finalizers, known.AppFinalizer)
		_, err = deployer.clusternetclient.AppsV1alpha1().Descriptions(desc.Namespace).Update(context.TODO(), desc, metav1.UpdateOptions{})
		if err != nil {
			klog.WarningDepth(4,
				fmt.Sprintf("failed to remove finalizer %s from Description %s: %v", known.AppFinalizer, klog.KObj(desc), err))
		}
		return err
	}

	if desc.Spec.Deployer == appsapi.DescriptionHelmDeployer {
		err := deployer.helmDeployer.PopulateHelmRelease(desc)
		if err != nil {
			return err
		}
	}

	// TODO: @dixudx: generic deployer

	return nil
}

func (deployer *Deployer) handleManifest(manifest *appsapi.Manifest) error {
	// TODO: @dixudx

	return nil
}
