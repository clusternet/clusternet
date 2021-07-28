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

	chartLister   appListers.HelmChartLister
	descLister    appListers.DescriptionLister
	hrLister      appListers.HelmReleaseLister
	clusterLister clusterListers.ManagedClusterLister

	kubeclient       *kubernetes.Clientset
	clusternetclient *clusternetClientSet.Clientset

	subsController *subscription.Controller
	descController *description.Controller

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
		hrLister:         clusternetInformerFactory.Apps().V1alpha1().HelmReleases().Lister(),
		clusterLister:    clusternetInformerFactory.Clusters().V1beta1().ManagedClusters().Lister(),
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

	return deployer, nil
}

func (deployer *Deployer) Run(workers int) {
	klog.Infof("starting Clusternet deployer ...")

	go deployer.helmDeployer.Run(workers)
	go deployer.subsController.Run(workers, deployer.ctx.Done())
	go deployer.descController.Run(workers, deployer.ctx.Done())

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

		subs.Finalizers = utils.RemoveString(subs.Finalizers, known.AppFinalizer)
		_, err = deployer.clusternetclient.AppsV1alpha1().Subscriptions(subs.Namespace).Update(context.TODO(), subs, metav1.UpdateOptions{})
		if err != nil {
			klog.WarningDepth(4,
				fmt.Sprintf("failed to remove finalizer %s from Subscription %s: %v", known.AppFinalizer, klog.KObj(subs), err))
		}
		return err
	}

	var charts []*appsapi.HelmChart
	for _, feed := range subs.Spec.Feeds {
		if feed.Kind != helmChartKind.Kind && feed.APIVersion != helmChartKind.Version {
			msg := fmt.Sprintf("Subscription only support %s for now", helmChartKind.String())
			klog.Error(msg)
			deployer.recorder.Event(subs, corev1.EventTypeWarning, "OnlySupportHelmChart", msg)
			return nil
		}

		chartList, err := deployer.getChartsBySelector(subs, feed)
		if errors.IsNotFound(err) {
			msg := fmt.Sprintf("Subscription %s is using a nonexistent HelmChart %s/%s", klog.KObj(subs), subs.Namespace, feed.Name)
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
	}

	if len(charts) == 0 {
		deployer.recorder.Event(subs, corev1.EventTypeWarning, "NoHelmCharts", "No helm charts get matched")
		return nil
	}

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

	return deployer.populateDescriptionsForHelm(subs, chartRefs)
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
		chartList, err := deployer.chartLister.HelmCharts(subs.Namespace).List(selector)
		if err != nil {
			return nil, err
		}
		return chartList, nil
	}

	return []*appsapi.HelmChart{}, nil
}

func (deployer *Deployer) populateDescriptionsForHelm(subs *appsapi.Subscription, chartRefs []appsapi.ChartReference) error {
	var mcls []*clusterapi.ManagedCluster
	for _, subscriber := range subs.Spec.Subscribers {
		selector, err := metav1.LabelSelectorAsSelector(subscriber.ClusterAffinity)
		if err != nil {
			return err
		}
		clusters, err := deployer.clusterLister.ManagedClusters("").List(selector)
		if err != nil {
			return err
		}

		if clusters == nil {
			deployer.recorder.Event(subs, corev1.EventTypeWarning, "NoClusters", "No clusters get matched")
			return nil
		}

		mcls = append(mcls, clusters...)
	}

	var allErrs []error
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
			allErrs = append(allErrs, err)
			msg := fmt.Sprintf("Failed to sync Description %s: %v", klog.KObj(description), err)
			klog.ErrorDepth(5, msg)
			deployer.recorder.Event(subs, corev1.EventTypeWarning, "DescriptionFailure", msg)
		}
	}

	return utilerrors.NewAggregate(allErrs)
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
