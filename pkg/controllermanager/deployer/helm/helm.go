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
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

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
	klog "k8s.io/klog/v2"
	utilpointer "k8s.io/utils/pointer"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/controllers/apps/description"
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

	// apiserver url of parent cluster
	apiserverURL string

	// systemNamespace specifies the default namespace to look up objects, like credentials
	// default to be "clusternet-system"
	systemNamespace string

	// If enabled, then the deployers in Clusternet will use anonymous when proxying requests to child clusters.
	// If not, serviceaccount "clusternet-hub-proxy" will be used instead.
	anonymousAuthSupported bool

	deployContextMap sync.Map
}

func NewDeployer(
	apiserverURL, systemNamespace string,
	clusternetClient *clusternetclientset.Clientset,
	kubeClient *kubernetes.Clientset,
	clusternetInformerFactory clusternetinformers.SharedInformerFactory,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	recorder record.EventRecorder,
	proxyingWithAnonymous bool,
) (*Deployer, error) {

	deployer := &Deployer{
		apiserverURL:           apiserverURL,
		systemNamespace:        systemNamespace,
		clusternetClient:       clusternetClient,
		kubeClient:             kubeClient,
		chartLister:            clusternetInformerFactory.Apps().V1alpha1().HelmCharts().Lister(),
		chartSynced:            clusternetInformerFactory.Apps().V1alpha1().HelmCharts().Informer().HasSynced,
		hrLister:               clusternetInformerFactory.Apps().V1alpha1().HelmReleases().Lister(),
		hrSynced:               clusternetInformerFactory.Apps().V1alpha1().HelmReleases().Informer().HasSynced,
		descLister:             clusternetInformerFactory.Apps().V1alpha1().Descriptions().Lister(),
		descSynced:             clusternetInformerFactory.Apps().V1alpha1().Descriptions().Informer().HasSynced,
		clusterLister:          clusternetInformerFactory.Clusters().V1beta1().ManagedClusters().Lister(),
		clusterSynced:          clusternetInformerFactory.Clusters().V1beta1().ManagedClusters().Informer().HasSynced,
		secretLister:           kubeInformerFactory.Core().V1().Secrets().Lister(),
		secretSynced:           kubeInformerFactory.Core().V1().Secrets().Informer().HasSynced,
		recorder:               recorder,
		anonymousAuthSupported: proxyingWithAnonymous,
	}

	hrController, err := helmrelease.NewController(clusternetClient,
		clusternetInformerFactory.Apps().V1alpha1().HelmReleases(),
		deployer.recorder,
		deployer.handleHelmRelease)
	if err != nil {
		return nil, err
	}
	deployer.helmReleaseController = hrController

	secretController, err := secret.NewController(kubeClient,
		kubeInformerFactory.Core().V1().Secrets(),
		deployer.recorder,
		deployer.handleSecret)
	if err != nil {
		return nil, err
	}
	deployer.secretController = secretController

	descController, err := description.NewController(clusternetClient,
		clusternetInformerFactory.Apps().V1alpha1().Descriptions(),
		clusternetInformerFactory.Apps().V1alpha1().HelmReleases(),
		clusternetInformerFactory.Apps().V1alpha1().HelmCharts(),
		deployer.recorder,
		deployer.handleDescription)
	if err != nil {
		return nil, err
	}
	deployer.descriptionController = descController

	return deployer, nil
}

func (deployer *Deployer) Run(workers int, ctx context.Context) {
	klog.Info("starting helm deployer...")
	defer klog.Info("shutting helm deployer")

	// Wait for the caches to be synced before starting workers
	if !cache.WaitForNamedCacheSync("helm-deployer",
		ctx.Done(),
		deployer.chartSynced,
		deployer.hrSynced,
		deployer.descSynced,
		deployer.clusterSynced,
		deployer.secretSynced,
	) {
		return
	}

	go deployer.helmReleaseController.Run(workers, ctx)
	go deployer.descriptionController.Run(workers, ctx)
	// 1 worker may get hang up, so we set minimum 2 workers here
	go deployer.secretController.Run(2, ctx)

	<-ctx.Done()
}

func (deployer *Deployer) handleDescription(descCopy *appsapi.Description) error {
	klog.V(5).Infof("handle Description %s", klog.KObj(descCopy))
	if descCopy.Spec.Deployer != appsapi.DescriptionHelmDeployer {
		return nil
	}

	if descCopy.DeletionTimestamp != nil {
		// make sure all controllees have been deleted
		hrs, err := deployer.hrLister.HelmReleases(descCopy.Namespace).List(labels.SelectorFromSet(labels.Set{
			known.ConfigKindLabel:      descriptionKind.Kind,
			known.ConfigNameLabel:      descCopy.Name,
			known.ConfigNamespaceLabel: descCopy.Namespace,
			known.ConfigUIDLabel:       string(descCopy.UID),
		}))
		if err != nil {
			return err
		}

		var allErrs []error
		for _, hr := range hrs {
			if hr.DeletionTimestamp != nil {
				continue
			}

			if err = deployer.deleteHelmRelease(context.TODO(), klog.KObj(hr).String()); err != nil {
				klog.ErrorDepth(5, err)
				allErrs = append(allErrs, err)
				continue
			}
		}

		if hrs != nil || len(allErrs) > 0 {
			return fmt.Errorf("waiting for HelmRelease belongs to Description %s getting deleted", klog.KObj(descCopy))
		}

		descCopy.Finalizers = utils.RemoveString(descCopy.Finalizers, known.AppFinalizer)
		_, err = deployer.clusternetClient.AppsV1alpha1().Descriptions(descCopy.Namespace).Update(context.TODO(), descCopy, metav1.UpdateOptions{})
		if err != nil {
			klog.WarningDepth(4,
				fmt.Sprintf("failed to remove finalizer %s from Description %s: %v", known.AppFinalizer, klog.KObj(descCopy), err))
		}
		return err
	}

	if err := deployer.populateHelmRelease(descCopy); err != nil {
		return err
	}

	return nil
}

func (deployer *Deployer) populateHelmRelease(desc *appsapi.Description) error {
	allExistingHelmReleases, err := deployer.hrLister.HelmReleases(desc.Namespace).List(labels.SelectorFromSet(labels.Set{
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

	if len(desc.Spec.Raw) != len(desc.Spec.Charts) || len(desc.Spec.ChartRaw) != len(desc.Spec.Charts) {
		msg := fmt.Sprintf("unequal lengths of (Spec.Raw|Spec.ChartRaw) and Spec.Charts in Description %s", klog.KObj(desc))
		klog.ErrorDepth(5, msg)
		deployer.recorder.Event(desc, corev1.EventTypeWarning, "UnequalLengths", msg)
		return errors.New(msg)
	}

	var allErrs []error
	for idx, chartRef := range desc.Spec.Charts {
		chart := &appsapi.HelmChart{}
		if err = utils.Unmarshal(desc.Spec.ChartRaw[idx], chart); err != nil {
			return err
		}
		defaultLabels := map[string]string{
			known.ObjectCreatedByLabel: known.ClusternetCtrlMgrName,
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
		}

		if chart.Labels == nil {
			chart.Labels = make(map[string]string)
		}
		// add default labels to chart
		for key, value := range defaultLabels {
			chart.Labels[key] = value
		}

		hr := &appsapi.HelmRelease{
			ObjectMeta: metav1.ObjectMeta{
				Name:        utils.GenerateHelmReleaseName(desc.Name, chartRef),
				Namespace:   desc.Namespace,
				Annotations: chart.Annotations,
				Labels:      chart.Labels,
				Finalizers: []string{
					known.AppFinalizer,
				},
			},
			Spec: appsapi.HelmReleaseSpec{
				ReleaseName:     utilpointer.String(chart.Name), // default to be the HelmChart name
				TargetNamespace: chart.Spec.TargetNamespace,
				HelmOptions:     chart.Spec.HelmOptions,
				Overrides:       []byte(strings.TrimSpace(string(desc.Spec.Raw[idx]))),
			},
		}
		hr.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(desc, descriptionKind)})
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
		err = deployer.deleteHelmRelease(context.TODO(), key)
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

		// backwards compatible
		// for HelmRelease that are already populated, spec.releaseName should be set to nil
		if hr.Spec.ReleaseName == nil {
			helmRelease.Spec.ReleaseName = nil
		}

		if reflect.DeepEqual(hr.Spec, helmRelease.Spec) &&
			reflect.DeepEqual(hr.Annotations, helmRelease.Annotations) &&
			reflect.DeepEqual(hr.Labels, helmRelease.Labels) {
			// seems to get overrides changed
			return deployer.handleHelmRelease(hr)
		}

		// update it
		hrCopy := hr.DeepCopy()
		if hrCopy.Annotations == nil {
			hrCopy.Annotations = make(map[string]string)
		}
		for key, value := range helmRelease.Annotations {
			hrCopy.Annotations[key] = value
		}
		if hrCopy.Labels == nil {
			hrCopy.Labels = make(map[string]string)
		}
		for key, value := range helmRelease.Labels {
			hrCopy.Labels[key] = value
		}

		hrCopy.Spec = helmRelease.Spec
		if !utils.ContainsString(hrCopy.Finalizers, known.AppFinalizer) {
			hrCopy.Finalizers = append(hrCopy.Finalizers, known.AppFinalizer)
		}

		_, err = deployer.clusternetClient.AppsV1alpha1().HelmReleases(hrCopy.Namespace).Update(context.TODO(),
			hrCopy, metav1.UpdateOptions{})
		if err == nil {
			msg := fmt.Sprintf("HelmReleases %s is updated successfully", klog.KObj(hrCopy))
			klog.V(4).Info(msg)
			deployer.recorder.Event(desc, corev1.EventTypeNormal, "HelmReleaseUpdated", msg)
		}
		return err
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
	klog.V(5).Infof("handle HelmRelease %s", klog.KObj(hr))

	if hr.DeletionTimestamp != nil {
		// if the cluster got lost
		if utils.IsClusterLost(hr.Labels[known.ClusterIDLabel], hr.Namespace, deployer.clusterLister) {
			hrCopy := hr.DeepCopy()
			hrCopy.Finalizers = utils.RemoveString(hrCopy.Finalizers, known.AppFinalizer)
			_, err := deployer.clusternetClient.AppsV1alpha1().HelmReleases(hrCopy.Namespace).Update(context.TODO(), hrCopy, metav1.UpdateOptions{})
			return err
		}
	}

	deployable, err := utils.DeployableByHub(deployer.clusterLister, hr.Labels[known.ClusterIDLabel], hr.Namespace)
	if err != nil {
		klog.ErrorDepth(4, err)
		deployer.recorder.Event(hr, corev1.EventTypeWarning, "ManagedClusterNotFound", err.Error())
		return err
	}
	if !deployable {
		klog.V(5).Infof("HelmRelease %s is not deployable by hub, skipping syncing", klog.KObj(hr))
		return nil
	}

	deployCtx, err := deployer.getDeployContext(hr)
	if err != nil {
		return err
	}
	return utils.ReconcileHelmRelease(context.TODO(), deployCtx, deployer.kubeClient, deployer.clusternetClient,
		deployer.hrLister, deployer.descLister, hr, deployer.recorder)
}

func (deployer *Deployer) handleSecret(secret *corev1.Secret) error {
	klog.V(5).Infof("handle Secret %s", klog.KObj(secret))
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

	secretCopy := secret.DeepCopy()
	secretCopy.Finalizers = utils.RemoveString(secretCopy.Finalizers, known.AppFinalizer)
	_, err = deployer.kubeClient.CoreV1().Secrets(secretCopy.Namespace).Update(context.TODO(), secretCopy, metav1.UpdateOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		klog.WarningDepth(4,
			fmt.Sprintf("failed to remove finalizer %s from Secrets %s: %v", known.AppFinalizer, klog.KObj(secretCopy), err))
	}
	return err
}

// getDeployContext will cache utils.DeployContext.
func (deployer *Deployer) getDeployContext(hr *appsapi.HelmRelease) (*utils.DeployContext, error) {
	deployCtx, ok := deployer.deployContextMap.Load(hr.Labels[known.ClusterIDLabel])
	if ok {
		return deployCtx.(*utils.DeployContext), nil
	}

	config, kubeQPS, kubeBurst, err := utils.GetChildClusterConfig(
		deployer.secretLister,
		deployer.clusterLister,
		hr.Namespace,
		hr.Labels[known.ClusterIDLabel],
		deployer.apiserverURL,
		deployer.systemNamespace,
		deployer.anonymousAuthSupported,
	)
	if err != nil {
		return nil, err
	}
	deployCtx, err = utils.NewDeployContext(config, kubeQPS, kubeBurst)
	if err != nil {
		return nil, err
	}
	deployer.deployContextMap.Store(hr.Labels[known.ClusterIDLabel], deployCtx)
	return deployCtx.(*utils.DeployContext), nil
}
