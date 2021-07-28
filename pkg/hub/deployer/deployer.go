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
	"k8s.io/apimachinery/pkg/selection"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	kubeInformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/controllers/apps/description"
	"github.com/clusternet/clusternet/pkg/controllers/apps/manifest"
	"github.com/clusternet/clusternet/pkg/controllers/apps/subscription"
	clusternetClientSet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	clusternetInformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	appListers "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	clusterListers "github.com/clusternet/clusternet/pkg/generated/listers/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/hub/deployer/helm"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

var (
	helmChartKind    = appsapi.SchemeGroupVersion.WithKind("HelmChart")
	subscriptionKind = appsapi.SchemeGroupVersion.WithKind("Subscription")
	descriptionKind  = appsapi.SchemeGroupVersion.WithKind("Description")
)

// Deployer defines configuration for the application deployer
type Deployer struct {
	ctx context.Context

	chartLister    appListers.HelmChartLister
	descLister     appListers.DescriptionLister
	subsLister     appListers.SubscriptionLister
	hrLister       appListers.HelmReleaseLister
	baseLister     appListers.BaseLister
	manifestLister appListers.ManifestLister
	clusterLister  clusterListers.ManagedClusterLister

	kubeclient       *kubernetes.Clientset
	clusternetclient *clusternetClientSet.Clientset

	subsController     *subscription.Controller
	descController     *description.Controller
	manifestController *manifest.Controller

	helmDeployer *helm.HelmDeployer

	broadcaster record.EventBroadcaster
	recorder    record.EventRecorder
}

func NewDeployer(ctx context.Context, kubeclient *kubernetes.Clientset, clusternetclient *clusternetClientSet.Clientset,
	clusternetInformerFactory clusternetInformers.SharedInformerFactory, kubeInformerFactory kubeInformers.SharedInformerFactory) (*Deployer, error) {

	deployer := &Deployer{
		ctx:              ctx,
		chartLister:      clusternetInformerFactory.Apps().V1alpha1().HelmCharts().Lister(),
		descLister:       clusternetInformerFactory.Apps().V1alpha1().Descriptions().Lister(),
		subsLister:       clusternetInformerFactory.Apps().V1alpha1().Subscriptions().Lister(),
		hrLister:         clusternetInformerFactory.Apps().V1alpha1().HelmReleases().Lister(),
		clusterLister:    clusternetInformerFactory.Clusters().V1beta1().ManagedClusters().Lister(),
		baseLister:       clusternetInformerFactory.Apps().V1alpha1().Bases().Lister(),
		manifestLister:   clusternetInformerFactory.Apps().V1alpha1().Manifests().Lister(),
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
		clusternetInformerFactory.Apps().V1alpha1().Descriptions(),
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
		deployer.handleDescription)
	if err != nil {
		return nil, err
	}
	deployer.descController = descController

	manifestController, err := manifest.NewController(ctx,
		clusternetclient,
		clusternetInformerFactory.Apps().V1alpha1().Manifests(),
		deployer.handleManifest)
	if err != nil {
		return nil, err
	}
	deployer.manifestController = manifestController

	return deployer, nil
}

func (deployer *Deployer) Run(workers int) {
	klog.Infof("starting Clusternet deployer ...")

	go deployer.helmDeployer.Run(workers)
	go deployer.subsController.Run(workers, deployer.ctx.Done())
	go deployer.descController.Run(workers, deployer.ctx.Done())
	go deployer.manifestController.Run(workers, deployer.ctx.Done())

	<-deployer.ctx.Done()
}

func (deployer *Deployer) handleSubscription(subs *appsapi.Subscription) error {
	if subs.DeletionTimestamp != nil {
		descs, err := deployer.descLister.List(labels.SelectorFromSet(labels.Set{
			known.ConfigKindLabel:      subscriptionKind.Kind,
			known.ConfigNameLabel:      subs.Name,
			known.ConfigNamespaceLabel: subs.Namespace,
		}))
		if err != nil {
			return err
		}
		// delete all matching Description
		deletePropagationBackground := metav1.DeletePropagationBackground
		for _, desc := range descs {
			if desc.DeletionTimestamp != nil {
				continue
			}
			err = deployer.clusternetclient.AppsV1alpha1().Descriptions(desc.Namespace).Delete(context.TODO(), desc.Name, metav1.DeleteOptions{
				PropagationPolicy: &deletePropagationBackground,
			})
			if err != nil {
				return err
			}
		}
		if descs != nil {
			return fmt.Errorf("waiting for Descriptions belongs to Subscription %s getting deleted", klog.KObj(subs))
		}

		bases, err := deployer.baseLister.List(labels.SelectorFromSet(labels.Set{
			known.ConfigKindLabel:      subscriptionKind.Kind,
			known.ConfigNameLabel:      subs.Name,
			known.ConfigNamespaceLabel: subs.Namespace,
		}))
		if err != nil {
			return err
		}

		// delete all matching Base
		for _, base := range bases {
			if base.DeletionTimestamp != nil {
				continue
			}
			err = deployer.clusternetclient.AppsV1alpha1().Descriptions(base.Namespace).Delete(context.TODO(), base.Name, metav1.DeleteOptions{
				PropagationPolicy: &deletePropagationBackground,
			})
			if err != nil {
				return err
			}
		}
		if descs != nil {
			return fmt.Errorf("waiting for Bases belongs to Subscription %s getting deleted", klog.KObj(subs))
		}

		subs.Finalizers = utils.RemoveString(subs.Finalizers, known.AppFinalizer)
		_, err = deployer.clusternetclient.AppsV1alpha1().Subscriptions(subs.Namespace).Update(context.TODO(), subs, metav1.UpdateOptions{})
		if err != nil {
			klog.WarningDepth(4,
				fmt.Sprintf("failed to remove finalizer %s from Subscription %s: %v", known.AppFinalizer, klog.KObj(subs), err))
		}
		return err
	}

	var charts []*appsapi.HelmChart
	var manifests []*appsapi.Manifest
	for _, feed := range subs.Spec.Feeds {
		if feed.Kind == helmChartKind.Kind && feed.APIVersion == helmChartKind.Version {
			chartList, err := deployer.getChartsBySelector(subs, feed)
			if errors.IsNotFound(err) {
				msg := fmt.Sprintf("Subscription %s is using a nonexistent HelmChart, feed %s/%s %s/%s %s", klog.KObj(subs), feed.APIVersion, feed.Kind, feed.Namespace, feed.Name, feed.FeedSelector.String())
				klog.Error(msg)
				deployer.recorder.Event(subs, corev1.EventTypeWarning, "NonexistentHelmChart", msg)
				return nil
			}
			if err != nil {
				msg := fmt.Sprintf("failed to get charts matching %q for Subscription %s: %v", feed, klog.KObj(subs), err)
				klog.Error(msg)
				deployer.recorder.Event(subs, corev1.EventTypeWarning, "FailedRetrievingHelmCharts", msg)
				return err
			}
			charts = append(charts, chartList...)
		} else {
			manifestList, err := deployer.getManifestsBySelector(subs, feed)
			if errors.IsNotFound(err) {
				msg := fmt.Sprintf("Subscription %s is using a nonexistent Objects %s", klog.KObj(subs), FormatFeed(feed))
				klog.Error(msg)
				deployer.recorder.Event(subs, corev1.EventTypeWarning, "NonexistentObject", msg)
				return nil
			}
			if err != nil {
				msg := fmt.Sprintf("failed to get Objects matching %q for Subscription %s: %v", feed, klog.KObj(subs), err)
				klog.Error(msg)
				deployer.recorder.Event(subs, corev1.EventTypeWarning, "FailedRetrievingObject", msg)
				return err
			}
			manifests = append(manifests, manifestList...)
		}
	}

	if len(charts) == 0 && len(manifests) == 0 {
		deployer.recorder.Event(subs, corev1.EventTypeWarning, "NoFeedsMatched", "No feeds get matched")
		return nil
	}

	var allErrs []error
	if len(charts) > 0 {
		// verify HelmChart can be found
		var chartRefs []appsapi.ChartReference
		for _, chart := range charts {
			if len(chart.Status.Phase) == 0 {
				msg := fmt.Sprintf("HelmChart %s is in verifying", klog.KObj(chart))
				klog.Warning(msg)
				deployer.recorder.Event(subs, corev1.EventTypeWarning, "VerifyingHelmChart", msg)
				return fmt.Errorf(msg)
			}

			if chart.Status.Phase != appsapi.HelmChartFound {
				deployer.recorder.Event(subs, corev1.EventTypeWarning, "HelmChartNotFound",
					fmt.Sprintf("helm chart %s is not found", klog.KObj(chart)))
				return nil
			}

			chartRefs = append(chartRefs, appsapi.ChartReference{
				Name:      chart.Name,
				Namespace: chart.Namespace,
			})
		}

		deployer.recorder.Event(subs, corev1.EventTypeNormal, "HelmChartsMatched", "helm charts get matched")
		allErrs = append(allErrs, deployer.populateDescriptionsForHelm(subs, chartRefs)...)
	}

	if len(manifests) > 0 {
		deployer.recorder.Event(subs, corev1.EventTypeNormal, "ManifestsMatched", "manifests get matched")
		allErrs = append(allErrs, deployer.populateBases(subs, manifests)...)
	}

	return utilerrors.NewAggregate(allErrs)
}

func (deployer *Deployer) handleManifest(manifest *appsapi.Manifest) error {
	if manifest.DeletionTimestamp != nil {
		manifest.Finalizers = utils.RemoveString(manifest.Finalizers, known.AppFinalizer)
		_, err := deployer.clusternetclient.AppsV1alpha1().Manifests(manifest.Namespace).Update(context.TODO(), manifest, metav1.UpdateOptions{})
		if err != nil {
			klog.WarningDepth(4,
				fmt.Sprintf("failed to remove finalizer %s from Manifest %s: %v", known.AppFinalizer, klog.KObj(manifest), err))
		}
		return err
	}

	subscriptions, err := deployer.subsLister.List(labels.NewSelector())
	if err != nil {
		return err
	}

	var matched *appsapi.Subscription
	for _, subs := range subscriptions {
		for _, feed := range subs.Spec.Feeds {
			if IsFeedMatches(feed, manifest) {
				matched = subs
			}
		}
	}

	var allErrs []error

	if matched != nil {
		deployer.recorder.Event(matched, corev1.EventTypeNormal, "ManifestsMatched", "manifests get matched")
		allErrs = deployer.populateBases(matched, []*appsapi.Manifest{manifest})
	}

	return utilerrors.NewAggregate(allErrs)
}

func (deployer *Deployer) getChartsBySelector(subs *appsapi.Subscription, feed appsapi.Feed) ([]*appsapi.HelmChart, error) {
	namespace := subs.Namespace
	if len(feed.Namespace) != 0 {
		namespace = feed.Namespace
	}
	if len(feed.Name) > 0 {
		chart, err := deployer.chartLister.HelmCharts(namespace).Get(feed.Name)
		if err != nil {
			return nil, err
		}
		return []*appsapi.HelmChart{chart}, nil
	}

	if feed.FeedSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(feed.FeedSelector)
		if err != nil {
			return nil, err
		}
		chartList, err := deployer.chartLister.HelmCharts(namespace).List(selector)
		if err != nil {
			return nil, err
		}
		return chartList, nil
	}

	return []*appsapi.HelmChart{}, nil
}

func (deployer *Deployer) getManifestsBySelector(subs *appsapi.Subscription, feed appsapi.Feed) ([]*appsapi.Manifest, error) {
	namespace := subs.Namespace
	if len(feed.Namespace) != 0 {
		namespace = feed.Namespace
	}
	if len(feed.Name) > 0 {
		manifest, err := deployer.manifestLister.Manifests(namespace).Get(feed.Name)
		if err != nil {
			return nil, err
		}
		return []*appsapi.Manifest{manifest}, nil
	}

	selector := labels.NewSelector()
	if len(feed.Kind) > 0 {
		requirement, err := labels.NewRequirement(known.ConfigKindLabel, selection.Equals, []string{feed.Kind})
		if err != nil {
			return nil, err
		}

		selector = selector.Add(*requirement)
	}

	if len(feed.APIVersion) > 0 {
		requirement, err := labels.NewRequirement(known.ConfigVersionLabel, selection.Equals, []string{feed.APIVersion})
		if err != nil {
			return nil, err
		}

		selector = selector.Add(*requirement)
	}

	if feed.FeedSelector != nil {
		feedSelector, err := metav1.LabelSelectorAsSelector(feed.FeedSelector)
		if err != nil {
			return nil, err
		}

		reqs, _ := feedSelector.Requirements()
		for _, r := range reqs {
			selector.Add(r)
		}
	}

	if selector.Empty() {
		return []*appsapi.Manifest{}, nil
	}

	manifestList, err := deployer.manifestLister.Manifests(namespace).List(selector)
	if err != nil {
		return nil, err
	}
	return manifestList, nil
}

func (deployer *Deployer) populateDescriptionsForHelm(subs *appsapi.Subscription, chartRefs []appsapi.ChartReference) (errs []error) {
	var mcls []*clusterapi.ManagedCluster
	for _, subscriber := range subs.Spec.Subscribers {
		selector, err := metav1.LabelSelectorAsSelector(subscriber.ClusterAffinity)
		if err != nil {
			errs = append(errs, err)
			return
		}
		clusters, err := deployer.clusterLister.ManagedClusters("").List(selector)
		if err != nil {
			errs = append(errs, err)
			return
		}

		if clusters == nil {
			deployer.recorder.Event(subs, corev1.EventTypeWarning, "NoClusters", "No clusters get matched")
			errs = append(errs, err)
			return
		}

		mcls = append(mcls, clusters...)
	}

	for _, cluster := range mcls {
		if !cluster.Status.AppPusher {
			msg := fmt.Sprintf("skip deploying Subscription %s to cluster %s for disabling AppPusher",
				klog.KObj(subs), cluster.Spec.ClusterID)
			klog.V(4).Info(msg)
			deployer.recorder.Event(subs, corev1.EventTypeNormal, "SkipDeploying", msg)
			continue
		}
		if cluster.Spec.SyncMode == clusterapi.Pull {
			msg := fmt.Sprintf("skip deploying Subscription %s to cluster %s with sync mode setting to Pull",
				klog.KObj(subs), cluster.Spec.ClusterID)
			klog.V(4).Info(msg)
			deployer.recorder.Event(subs, corev1.EventTypeNormal, "SkipDeploying", msg)
			continue
		}

		description := &appsapi.Description{
			ObjectMeta: metav1.ObjectMeta{
				Name:      subs.Name,
				Namespace: cluster.Namespace,
				Labels: map[string]string{
					known.ObjectCreatedByLabel: known.ClusternetHubName,
					known.ClusterIDLabel:       cluster.Labels[known.ClusterIDLabel],
					known.ClusterNameLabel:     cluster.Labels[known.ClusterNameLabel],
					known.ConfigKindLabel:      subscriptionKind.Kind,
					known.ConfigNameLabel:      subs.Name,
					known.ConfigNamespaceLabel: subs.Namespace,
					known.ConfigUIDLabel:       string(subs.UID),
					// add subscription info
					known.ConfigSubscriptionNameLabel:      subs.Name,
					known.ConfigSubscriptionNamespaceLabel: subs.Namespace,
					known.ConfigSubscriptionUIDLabel:       string(subs.UID),
				},
				Finalizers: []string{
					known.AppFinalizer,
				},
			},
			Spec: appsapi.DescriptionSpec{
				Deployer: appsapi.DescriptionHelmDeployer,
				Charts:   chartRefs,
			},
		}

		err := deployer.syncDescriptions(subs, description)
		if err != nil {
			errs = append(errs, err)
			msg := fmt.Sprintf("Failed to sync Description %s: %v", klog.KObj(description), err)
			klog.ErrorDepth(5, msg)
			deployer.recorder.Event(subs, corev1.EventTypeWarning, "DescriptionFailure", msg)
		}
	}

	return
}

func (deployer *Deployer) syncDescriptions(subs *appsapi.Subscription, description *appsapi.Description) error {
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
				deployer.recorder.Event(subs, corev1.EventTypeNormal, "DescriptionUpdated", msg)
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
		deployer.recorder.Event(subs, corev1.EventTypeNormal, "DescriptionCreated", msg)
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

	return nil
}

func (deployer *Deployer) populateBases(subs *appsapi.Subscription, manifests []*appsapi.Manifest) (errs []error) {
	toCreate, toSync, toDelete, err := deployer.calcClustersToSyncBases(subs)
	if err != nil {
		return []error{err}
	}

	msg := fmt.Sprintf("For Subscription %s, begin to create %d Bases, sync %d Bases, delete %d Bases  ", klog.KObj(subs), len(toCreate), len(toSync), len(toDelete))
	klog.V(4).Info(msg)
	deployer.recorder.Event(subs, corev1.EventTypeNormal, "BaseProgressing", msg)

	var feeds []appsapi.Feed
	for _, manifest := range manifests {
		feed := appsapi.Feed{
			Name:       manifest.Name,
			Namespace:  manifest.Namespace,
			APIVersion: manifest.Labels[known.ConfigVersionLabel],
			Kind:       manifest.Labels[known.ConfigKindLabel],
		}

		feeds = append(feeds, feed)
	}

	for _, cluster := range toCreate {
		base := deployer.newBase(subs, cluster, feeds)

		_, err = deployer.clusternetclient.AppsV1alpha1().Bases(base.Namespace).Create(context.TODO(),
			base, metav1.CreateOptions{})
		if err == nil {
			msg := fmt.Sprintf("Base %s is created successfully", klog.KObj(base))
			klog.V(4).Info(msg)
			deployer.recorder.Event(subs, corev1.EventTypeNormal, "BaseCreated", msg)
		} else {
			errs = append(errs, err)
			msg := fmt.Sprintf("Failed to create Base %s: %v", klog.KObj(base), err)
			klog.ErrorDepth(5, msg)
			deployer.recorder.Event(subs, corev1.EventTypeWarning, "BaseFailure", msg)
		}
	}

	for _, cluster := range toSync {
		base := deployer.newBase(subs, cluster, feeds)

		err := deployer.syncBases(subs, base)
		if err == nil {
			msg := fmt.Sprintf("Base %s is synced successfully", klog.KObj(base))
			klog.V(4).Info(msg)
			deployer.recorder.Event(subs, corev1.EventTypeNormal, "BaseSynced", msg)
		} else {
			errs = append(errs, err)
			msg := fmt.Sprintf("Failed to sync Base %s: %v", klog.KObj(base), err)
			klog.ErrorDepth(5, msg)
			deployer.recorder.Event(subs, corev1.EventTypeWarning, "BaseFailure", msg)
		}
	}

	for _, base := range toDelete {
		err = deployer.clusternetclient.AppsV1alpha1().Bases(base.Namespace).Delete(context.TODO(),
			base.Name, metav1.DeleteOptions{})
		if err == nil {
			msg := fmt.Sprintf("Base %s is deleting", klog.KObj(base))
			klog.V(4).Info(msg)
			deployer.recorder.Event(subs, corev1.EventTypeNormal, "BaseDeleting", msg)
		} else {
			errs = append(errs, err)
			msg := fmt.Sprintf("Failed to delete Base %s: %v", klog.KObj(base), err)
			klog.ErrorDepth(5, msg)
			deployer.recorder.Event(subs, corev1.EventTypeWarning, "BaseFailure", msg)
		}
	}

	return
}

func (deployer *Deployer) calcClustersToSyncBases(subs *appsapi.Subscription) (toCreate, toSync []*clusterapi.ManagedCluster, toDelete []*appsapi.Base, returnErr error) {
	var expectClusters []*clusterapi.ManagedCluster
	for _, subscriber := range subs.Spec.Subscribers {
		selector, err := metav1.LabelSelectorAsSelector(subscriber.ClusterAffinity)
		if err != nil {
			returnErr = err
			return
		}
		clusters, err := deployer.clusterLister.ManagedClusters("").List(selector)
		if err != nil {
			returnErr = err
			return
		}

		for _, cluster := range clusters {
			if !cluster.Status.AppPusher {
				continue
			}

			if cluster.Spec.SyncMode == clusterapi.Pull {
				continue
			}
			expectClusters = append(expectClusters, cluster)
		}
	}

	deployedBases, err := deployer.baseLister.List(labels.SelectorFromSet(labels.Set{
		known.ConfigKindLabel:      subscriptionKind.Kind,
		known.ConfigNameLabel:      subs.Name,
		known.ConfigNamespaceLabel: subs.Namespace,
	}))
	if err != nil {
		return
	}

	for _, cluster := range expectClusters {
		deployed := false
		for _, base := range deployedBases {
			if base.Namespace == cluster.Namespace {
				deployed = true
				break
			}
		}

		if deployed {
			toSync = append(toSync, cluster)
		} else {
			toCreate = append(toCreate, cluster)
		}
	}

	for _, base := range deployedBases {
		shouldDeploy := false
		for _, cluster := range expectClusters {
			if base.Namespace == cluster.Namespace {
				shouldDeploy = true
				break
			}
		}

		if !shouldDeploy {
			toDelete = append(toDelete, base)
		}
	}

	return
}

func (deployer *Deployer) newBase(subs *appsapi.Subscription, cluster *clusterapi.ManagedCluster, feeds []appsapi.Feed) *appsapi.Base {
	base := &appsapi.Base{
		ObjectMeta: metav1.ObjectMeta{
			Name:      subs.Name,
			Namespace: cluster.Namespace,
			Labels: map[string]string{
				known.ObjectCreatedByLabel: known.ClusternetHubName,
				known.ClusterIDLabel:       cluster.Labels[known.ClusterIDLabel],
				known.ClusterNameLabel:     cluster.Labels[known.ClusterNameLabel],
				known.ConfigKindLabel:      subscriptionKind.Kind,
				known.ConfigNameLabel:      subs.Name,
				known.ConfigNamespaceLabel: subs.Namespace,
				known.ConfigUIDLabel:       string(subs.UID),
				// add subscription info
				known.ConfigSubscriptionNameLabel:      subs.Name,
				known.ConfigSubscriptionNamespaceLabel: subs.Namespace,
				known.ConfigSubscriptionUIDLabel:       string(subs.UID),
			},
			Finalizers: []string{
				known.AppFinalizer,
			},
		},
		Spec: appsapi.BaseSpec{
			Feeds: feeds,
		},
	}

	return base
}

func (deployer *Deployer) syncBases(subs *appsapi.Subscription, base *appsapi.Base) error {
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
				msg := fmt.Sprintf("Description %s is updated successfully", klog.KObj(curBase))
				klog.V(4).Info(msg)
				deployer.recorder.Event(subs, corev1.EventTypeNormal, "BaseUpdated", msg)
			}
			return err
		}
		return nil
	}

	return err
}

func IsFeedMatches(f appsapi.Feed, manifest *appsapi.Manifest) bool {
	if len(f.Name) > 0 {
		if f.Namespace == manifest.Namespace &&
			f.Name == manifest.Name &&
			f.Kind == manifest.Labels[known.ConfigKindLabel] &&
			f.APIVersion == manifest.Labels[known.ConfigVersionLabel] {
			return true
		} else {
			return false
		}
	} else {
		selector, err := metav1.LabelSelectorAsSelector(f.FeedSelector)
		if err != nil {
			return false
		}
		return selector.Matches(labels.Set(manifest.Labels))
	}
}
