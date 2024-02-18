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
	"sort"
	"strings"
	"sync"

	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/registry"
	"helm.sh/helm/v3/pkg/repo"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/sets"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/wait"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	utilpointer "k8s.io/utils/pointer"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/controllermanager/deployer/generic"
	"github.com/clusternet/clusternet/pkg/controllermanager/deployer/helm"
	"github.com/clusternet/clusternet/pkg/controllermanager/deployer/localizer"
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
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

var (
	helmChartKind               = appsapi.SchemeGroupVersion.WithKind("HelmChart")
	subscriptionKind            = appsapi.SchemeGroupVersion.WithKind("Subscription")
	baseKind                    = appsapi.SchemeGroupVersion.WithKind("Base")
	deletePropagationBackground = metav1.DeletePropagationBackground
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

	clusternetClient *clusternetclientset.Clientset
	kubeClient       *kubernetes.Clientset

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
	recorder record.EventRecorder, anonymousAuthSupported bool) (*Deployer, error) {
	feedInUseProtection := utilfeature.DefaultFeatureGate.Enabled(features.FeedInUseProtection)

	deployer := &Deployer{
		apiserverURL:      apiserverURL,
		reservedNamespace: reservedNamespace,
		chartLister:       clusternetInformerFactory.Apps().V1alpha1().HelmCharts().Lister(),
		chartSynced:       clusternetInformerFactory.Apps().V1alpha1().HelmCharts().Informer().HasSynced,
		descLister:        clusternetInformerFactory.Apps().V1alpha1().Descriptions().Lister(),
		descSynced:        clusternetInformerFactory.Apps().V1alpha1().Descriptions().Informer().HasSynced,
		baseLister:        clusternetInformerFactory.Apps().V1alpha1().Bases().Lister(),
		baseSynced:        clusternetInformerFactory.Apps().V1alpha1().Bases().Informer().HasSynced,
		mfstLister:        clusternetInformerFactory.Apps().V1alpha1().Manifests().Lister(),
		mfstSynced:        clusternetInformerFactory.Apps().V1alpha1().Manifests().Informer().HasSynced,
		subLister:         clusternetInformerFactory.Apps().V1alpha1().Subscriptions().Lister(),
		subSynced:         clusternetInformerFactory.Apps().V1alpha1().Subscriptions().Informer().HasSynced,
		finvLister:        clusternetInformerFactory.Apps().V1alpha1().FeedInventories().Lister(),
		finvSynced:        clusternetInformerFactory.Apps().V1alpha1().FeedInventories().Informer().HasSynced,
		locLister:         clusternetInformerFactory.Apps().V1alpha1().Localizations().Lister(),
		locSynced:         clusternetInformerFactory.Apps().V1alpha1().Localizations().Informer().HasSynced,
		nsLister:          kubeInformerFactory.Core().V1().Namespaces().Lister(),
		nsSynced:          kubeInformerFactory.Core().V1().Namespaces().Informer().HasSynced,
		clusternetClient:  clusternetclient,
		kubeClient:        kubeclient,
		recorder:          recorder,
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

	// When using external FeedInventory controller, this feature gate should be closed
	if utilfeature.DefaultFeatureGate.Enabled(features.FeedInventory) {
		finv, err2 := feedinventory.NewController(clusternetclient,
			clusternetInformerFactory.Apps().V1alpha1().Subscriptions(),
			clusternetInformerFactory.Apps().V1alpha1().FeedInventories(),
			clusternetInformerFactory.Apps().V1alpha1().Manifests(),
			deployer.recorder,
			feedinventory.NewInTreeRegistry(),
			reservedNamespace, nil)
		if err2 != nil {
			return nil, err2
		}
		deployer.finvController = finv
	}
	aggregatestatusController, err := aggregatestatus.NewController(clusternetclient,
		clusternetInformerFactory.Apps().V1alpha1().Subscriptions(),
		clusternetInformerFactory.Apps().V1alpha1().Descriptions(),
		deployer.recorder)
	if err != nil {
		return nil, err
	}
	deployer.aggregatestatusController = aggregatestatusController
	return deployer, nil
}

func (deployer *Deployer) Run(workers int, ctx context.Context) {
	klog.Infof("starting Clusternet deployer ...")

	// Wait for the caches to be synced before starting workers
	if !cache.WaitForNamedCacheSync("clusternet-deployer",
		ctx.Done(),
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

	go deployer.chartController.Run(workers, ctx)
	go deployer.helmDeployer.Run(workers, ctx)
	go deployer.genericDeployer.Run(workers, ctx)
	go deployer.subsController.Run(workers, ctx)
	go deployer.mfstController.Run(workers, ctx)
	go deployer.baseController.Run(workers, ctx)
	go deployer.localizer.Run(workers, ctx)
	go deployer.aggregatestatusController.Run(workers, ctx)

	// When using external FeedInventory controller, this feature gate should be closed
	if utilfeature.DefaultFeatureGate.Enabled(features.FeedInventory) {
		go deployer.finvController.Run(workers, ctx)
	}

	<-ctx.Done()
}

func (deployer *Deployer) handleSubscription(subCopy *appsapi.Subscription) error {
	klog.V(5).Infof("handle Subscription %s", klog.KObj(subCopy))
	if subCopy.DeletionTimestamp != nil {
		bases, err := deployer.baseController.FindBaseBySubUID(string(subCopy.UID))
		if err != nil {
			return err
		}

		// delete all matching Base
		var allErrs []error
		for _, base := range bases {
			if base.DeletionTimestamp != nil {
				continue
			}
			if err = deployer.deleteBase(context.TODO(), klog.KObj(base).String()); err != nil {
				klog.ErrorDepth(5, err)
				allErrs = append(allErrs, err)
				continue
			}
		}
		if bases != nil || len(allErrs) > 0 {
			return fmt.Errorf("waiting for Bases belongs to Subscription %s getting deleted", klog.KObj(subCopy))
		}

		// remove label (subUID="Subscription") from referred Manifest/HelmChart
		if err = deployer.removeLabelsFromReferredFeeds(subCopy.UID, subscriptionKind.Kind); err != nil {
			return err
		}

		subCopy.Finalizers = utils.RemoveString(subCopy.Finalizers, known.AppFinalizer)
		_, err = deployer.clusternetClient.AppsV1alpha1().Subscriptions(subCopy.Namespace).Update(context.TODO(), subCopy, metav1.UpdateOptions{})
		if err != nil {
			klog.WarningDepth(4,
				fmt.Sprintf("failed to remove finalizer %s from Subscription %s: %v", known.AppFinalizer, klog.KObj(subCopy), err))
		}
		return err
	}

	// populate Base and Localization (for dividing scheduling)
	err := deployer.populateBasesAndLocalizations(subCopy)
	if err != nil {
		return err
	}

	return nil
}

// populateBasesAndLocalizations will populate a group of Base(s) from Subscription.
// Localization(s) will be populated as well for dividing scheduling.
func (deployer *Deployer) populateBasesAndLocalizations(sub *appsapi.Subscription) error {
	klog.V(5).Infof("Start populating bases and localizations for subscription %s/%s ", sub.Namespace, sub.Name)
	allExistingBases, err := deployer.baseController.FindBaseBySubUID(string(sub.UID))
	if err != nil {
		return err
	}
	// Bases to be deleted
	basesToBeDeleted := sets.String{}
	for _, base := range allExistingBases {
		basesToBeDeleted.Insert(klog.KObj(base).String())
	}

	var allErrs []error
	for idx, namespacedName := range sub.Status.BindingClusters {
		// Convert the namespacedName/name string into a distinct namespacedName and name
		namespace, _, err2 := cache.SplitMetaNamespaceKey(namespacedName)
		if err2 != nil {
			allErrs = append(allErrs, fmt.Errorf("invalid resource key: %s", namespacedName))
			continue
		}

		ns, err2 := deployer.nsLister.Get(namespace)
		if err2 != nil {
			if apierrors.IsNotFound(err2) {
				continue
			}
			return fmt.Errorf("failed to populate Bases for Subscription %s: %v", klog.KObj(sub), err2)
		}
		var basesInCurrentNamespace []*appsapi.Base
		for _, b := range allExistingBases {
			if b.Namespace == namespace {
				basesInCurrentNamespace = append(basesInCurrentNamespace, b)
			}
		}

		baseTemplate := &appsapi.Base{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s", sub.Name, utilrand.String(known.DefaultRandomIDLength)),
				Namespace: namespace,
				Labels: map[string]string{
					known.ObjectCreatedByLabel: known.ClusternetCtrlMgrName,
					known.ConfigKindLabel:      subscriptionKind.Kind,
					known.ConfigNamespaceLabel: sub.Namespace,
					known.ConfigUIDLabel:       string(sub.UID),
					// add subscription info
					known.ConfigSubscriptionNamespaceLabel: sub.Namespace,
					known.ConfigSubscriptionUIDLabel:       string(sub.UID),
				},
				Finalizers: []string{
					known.AppFinalizer,
				},
			},
			Spec: appsapi.BaseSpec{
				Feeds: sub.Spec.Feeds,
			},
		}
		if ns.Labels != nil {
			baseTemplate.Labels[known.ClusterIDLabel] = ns.Labels[known.ClusterIDLabel]
			baseTemplate.Labels[known.ClusterNameLabel] = ns.Labels[known.ClusterNameLabel]
		}

		// sync base object
		if len(basesInCurrentNamespace) > 0 {
			// backward compatible with legacy base name format
			// reuse existing base object if found, otherwise create base with random uid as postfix
			sort.SliceStable(basesInCurrentNamespace, func(i, j int) bool {
				return basesInCurrentNamespace[i].CreationTimestamp.Second() > basesInCurrentNamespace[j].
					CreationTimestamp.Second()
			})
			if len(basesInCurrentNamespace) > 1 {
				klog.Warningf("found more than one base objects belong to Subscription %s", klog.KObj(sub))
				deployer.recorder.Eventf(sub, corev1.EventTypeWarning, "MultipleBases",
					"found more than one base objects belongs to Subscription %s", klog.KObj(sub))

				// TODO: could remove below logic if we really want to prune redundant base objects
				for _, namespacedBase := range basesInCurrentNamespace[1:] {
					basesToBeDeleted.Delete(klog.KObj(namespacedBase).String())
				}
			}
			// choose the newest one from existing base objects
			baseTemplate.Name = basesInCurrentNamespace[0].Name
			baseTemplate.Spec.Feeds = sub.Spec.Feeds
			basesToBeDeleted.Delete(klog.KObj(baseTemplate).String())
			klog.Infof("reuse latest existed Base %s for Subscription %s", klog.KObj(baseTemplate), klog.KObj(sub))
		}

		base, err2 := deployer.syncBase(sub, baseTemplate)
		if err2 != nil {
			allErrs = append(allErrs, err2)
			msg := fmt.Sprintf("Failed to sync Base %s: %v", klog.KObj(baseTemplate), err2)
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
		err = deployer.deleteBase(context.TODO(), key)
		if err != nil {
			allErrs = append(allErrs, err)
		}
	}

	return utilerrors.NewAggregate(allErrs)
}

func (deployer *Deployer) syncBase(sub *appsapi.Subscription, baseTemplate *appsapi.Base) (*appsapi.Base, error) {
	// append sub name to labels when name length is less than 63 - (5+1)
	if len(sub.Name) <= utilvalidation.LabelValueMaxLength-known.DefaultRandomIDLength-1 {
		baseTemplate.Labels[known.ConfigNameLabel] = sub.Name
		baseTemplate.Labels[known.ConfigSubscriptionNameLabel] = sub.Name
	} else {
		delete(baseTemplate.Labels, known.ConfigNameLabel)
		delete(baseTemplate.Labels, known.ConfigSubscriptionNameLabel)
	}
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

	base, err = deployer.clusternetClient.AppsV1alpha1().
		Bases(baseCopy.Namespace).
		Update(context.TODO(), baseCopy, metav1.UpdateOptions{})
	if err == nil {
		msg := fmt.Sprintf("Base %s is updated successfully", klog.KObj(baseCopy))
		klog.V(4).Info(msg)
		deployer.recorder.Event(sub, corev1.EventTypeNormal, "BaseUpdated", msg)
		return base, nil
	}
	return nil, err
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
	if err = deployer.removeLabelsFromReferredFeeds(base.UID, baseKind.Kind); err != nil {
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
	klog.V(5).Infof("Start populate localization for Subscription %s with Base %s", klog.KObj(sub), klog.KObj(base))
	if len(base.UID) == 0 {
		return fmt.Errorf("waiting for UID set for Base %s", klog.KObj(base))
	}

	allExistingLocalizations, err := deployer.locLister.Localizations(base.Namespace).List(labels.SelectorFromSet(labels.Set{
		string(sub.UID):            subscriptionKind.Kind,
		known.ObjectCreatedByLabel: known.ClusternetCtrlMgrName,
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
	if sub.Spec.SchedulingStrategy == appsapi.DividingSchedulingStrategyType {
		finv, err2 := deployer.finvLister.FeedInventories(sub.Namespace).Get(sub.Name)
		if err2 != nil {
			klog.WarningDepth(5, fmt.Sprintf("failed to get FeedInventory %s: %v", klog.KObj(sub), err2))
			return err2
		}

		for _, feedOrder := range finv.Spec.Feeds {
			if feedOrder.DesiredReplicas == nil {
				continue
			}

			replicas, ok := sub.Status.Replicas[utils.GetFeedKey(feedOrder.Feed)]
			if !ok || len(replicas) == 0 {
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

			if len(replicas) <= clusterIndex {
				msg := fmt.Sprintf("the length of status.Replicas for %s in Subscription %s is not matched with status.BindingClusters",
					utils.FormatFeed(feedOrder.Feed), klog.KObj(sub))
				klog.ErrorDepth(5, msg)
				allErrs = append(allErrs, errors.New(msg))
				deployer.recorder.Event(sub, corev1.EventTypeWarning, "BadSchedulingResult", msg)
				continue
			}

			loc := GenerateLocalizationTemplate(base, appsapi.ApplyNow)
			loc.Labels[string(sub.UID)] = subscriptionKind.Kind
			loc.Spec.Feed = feedOrder.Feed
			loc.Spec.Overrides = []appsapi.OverrideConfig{
				{
					Name:  "dividing scheduling replicas",
					Value: fmt.Sprintf(`[{"path":%q,"value":%d,"op":"replace"}]`, feedOrder.ReplicaJsonPath, replicas[clusterIndex]),
					Type:  appsapi.JSONPatchType,
				},
			}

			var localsForCurrentFeed []*appsapi.Localization
			for _, local := range allExistingLocalizations {
				if local.Spec.Feed == feedOrder.Feed {
					localsForCurrentFeed = append(localsForCurrentFeed, local)
				}
			}
			if len(localsForCurrentFeed) > 0 {
				// backward compatible with legacy localization name format
				// reuse existing localization objects if found, otherwise create localization with random uid as postfix
				sort.SliceStable(localsForCurrentFeed, func(i, j int) bool {
					return localsForCurrentFeed[i].CreationTimestamp.Second() > localsForCurrentFeed[j].
						CreationTimestamp.Second()
				})
				if len(localsForCurrentFeed) > 1 {
					klog.Warningf("found more than one localization objects for Feed %q in Base %s",
						utils.FormatFeed(feedOrder.Feed), klog.KObj(base))
					deployer.recorder.Eventf(sub, corev1.EventTypeWarning, "MultipleLocalizations",
						"found more than one auto-populated localization objects belong to Base %s", klog.KObj(base))

					// TODO: could remove below logic if we really want to prune redundant localization objects
					for _, namespacedBase := range localsForCurrentFeed[1:] {
						locsToBeDeleted.Delete(klog.KObj(namespacedBase).String())
					}
				}
				// choose the newest one from existing localization objects
				loc.Name = localsForCurrentFeed[0].Name
				locsToBeDeleted.Delete(klog.KObj(loc).String())
				klog.Infof("reuse latest existed Localization %s for Feed %q in Base %s",
					klog.KObj(loc), utils.FormatFeed(feedOrder.Feed), klog.KObj(base))
			}

			err = deployer.syncLocalization(loc)
			if err != nil {
				allErrs = append(allErrs, err)
				msg := fmt.Sprintf("Failed to sync Localization %s: %v", klog.KObj(loc), err)
				klog.ErrorDepth(5, msg)
				deployer.recorder.Event(sub, corev1.EventTypeWarning, "FailedSyncingLocalization", msg)
			}
		}
	}

	for key := range locsToBeDeleted {
		err = deployer.deleteLocalization(context.TODO(), key)
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

func (deployer *Deployer) handleBase(baseCopy *appsapi.Base) error {
	klog.V(5).Infof("handle Base %s", klog.KObj(baseCopy))
	if baseCopy.DeletionTimestamp != nil {
		descs, err := deployer.descLister.Descriptions(baseCopy.Namespace).List(labels.SelectorFromSet(labels.Set{
			known.ConfigKindLabel:      baseKind.Kind,
			known.ConfigNameLabel:      baseCopy.Name,
			known.ConfigNamespaceLabel: baseCopy.Namespace,
			known.ConfigUIDLabel:       string(baseCopy.UID),
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

			if err = deployer.deleteDescription(context.TODO(), klog.KObj(desc).String()); err != nil {
				klog.ErrorDepth(5, err)
				allErrs = append(allErrs, err)
				continue
			}
		}
		if descs != nil || len(allErrs) > 0 {
			return fmt.Errorf("waiting for Descriptions belongs to Base %s getting deleted", klog.KObj(baseCopy))
		}

		baseCopy.Finalizers = utils.RemoveString(baseCopy.Finalizers, known.AppFinalizer)
		_, err = deployer.clusternetClient.AppsV1alpha1().Bases(baseCopy.Namespace).Update(context.TODO(), baseCopy, metav1.UpdateOptions{})
		if err != nil {
			klog.WarningDepth(4,
				fmt.Sprintf("failed to remove finalizer %s from Base %s: %v", known.AppFinalizer, klog.KObj(baseCopy), err))
		}
		return err
	}

	// add label (baseUID="Base") to referred Manifest/HelmChart
	if err := deployer.addLabelsToReferredFeeds(baseCopy); err != nil {
		return err
	}

	err := deployer.populateDescriptions(baseCopy)
	if err != nil {
		return err
	}

	return nil
}

func (deployer *Deployer) populateDescriptions(base *appsapi.Base) error {
	var allChartRefs []appsapi.ChartReference
	var allCharts []*appsapi.HelmChart
	var allManifests []*appsapi.Manifest

	var err error
	var index int
	var manifests []*appsapi.Manifest
	var chart *appsapi.HelmChart
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
			allCharts = append(allCharts, chart)
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

	allExistingDescriptions, err := deployer.descLister.Descriptions(base.Namespace).List(labels.SelectorFromSet(labels.Set{
		known.ConfigKindLabel:      baseKind.Kind,
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
				known.ObjectCreatedByLabel: known.ClusternetCtrlMgrName,
				known.ConfigKindLabel:      baseKind.Kind,
				known.ConfigNamespaceLabel: base.Namespace,
				known.ConfigUIDLabel:       string(base.UID),
				known.ClusterIDLabel:       base.Labels[known.ClusterIDLabel],
				known.ClusterNameLabel:     base.Labels[known.ClusterNameLabel],
				// add subscription info
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
		for _, chart2 := range allCharts {
			chartByte, err2 := utils.Marshal(chart2)
			if err2 != nil {
				allErrs = append(allErrs, err2)
				continue
			}
			desc.Spec.ChartRaw = append(desc.Spec.ChartRaw, chartByte)
		}
		err = deployer.syncDescriptions(base, desc)
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
		err = deployer.syncDescriptions(base, desc)
		if err != nil {
			allErrs = append(allErrs, err)
			msg := fmt.Sprintf("Failed to sync Description %s: %v", klog.KObj(desc), err)
			klog.ErrorDepth(5, msg)
			deployer.recorder.Event(base, corev1.EventTypeWarning, "FailedSyncingDescription", msg)
		}
		descsToBeDeleted.Delete(klog.KObj(desc).String())
	}

	for key := range descsToBeDeleted {
		if err = deployer.deleteDescription(context.TODO(), key); err != nil {
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
				if err = deployer.genericDeployer.PruneFeedsInDescription(ctx, curDesc.DeepCopy(), desc.DeepCopy()); err != nil {
					klog.Warningf("Prune feed in description %s failed: %v", klog.KObj(curDesc), err)
					return
				}
				cancel()
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

func (deployer *Deployer) handleManifest(manifestCopy *appsapi.Manifest) error {
	klog.V(5).Infof("handle Manifest %s", klog.KObj(manifestCopy))
	if manifestCopy.DeletionTimestamp != nil {
		if err := deployer.protectManifestFeed(manifestCopy); err != nil {
			return err
		}

		// remove finalizers
		manifestCopy.Finalizers = utils.RemoveString(manifestCopy.Finalizers, known.AppFinalizer)
		manifestCopy.Finalizers = utils.RemoveString(manifestCopy.Finalizers, known.FeedProtectionFinalizer)
		_, err := deployer.clusternetClient.AppsV1alpha1().Manifests(manifestCopy.Namespace).Update(context.TODO(), manifestCopy, metav1.UpdateOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			klog.WarningDepth(4,
				fmt.Sprintf("failed to remove finalizers from Manifest %s: %v", klog.KObj(manifestCopy), err))
		}
		return err
	}

	// find all referred Base UIDs
	var baseUIDs []string
	for key, val := range manifestCopy.Labels {
		if val == baseKind.Kind {
			baseUIDs = append(baseUIDs, key)
		}
	}

	return deployer.resyncBase(baseUIDs...)
}

func (deployer *Deployer) handleHelmChart(chartCopy *appsapi.HelmChart) error {
	var err error
	klog.V(5).Infof("handle HelmChart %s", klog.KObj(chartCopy))
	if chartCopy.DeletionTimestamp != nil {
		if err = deployer.protectHelmChartFeed(chartCopy); err != nil {
			return err
		}

		// remove finalizers
		chartCopy.Finalizers = utils.RemoveString(chartCopy.Finalizers, known.AppFinalizer)
		chartCopy.Finalizers = utils.RemoveString(chartCopy.Finalizers, known.FeedProtectionFinalizer)
		_, err = deployer.clusternetClient.AppsV1alpha1().HelmCharts(chartCopy.Namespace).Update(context.TODO(), chartCopy, metav1.UpdateOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			klog.WarningDepth(4,
				fmt.Sprintf("failed to remove finalizers from HelmChart %s: %v", klog.KObj(chartCopy), err))
		}
		return err
	}

	var (
		username string
		password string

		chartPhase appsapi.HelmChartPhase
		reason     string
	)
	if chartCopy.Spec.ChartPullSecret.Name != "" {
		username, password, err = utils.GetHelmRepoCredentials(
			deployer.kubeClient,
			chartCopy.Spec.ChartPullSecret.Name,
			chartCopy.Spec.ChartPullSecret.Namespace,
		)
		if err != nil {
			return err
		}
	}
	chartPhase = appsapi.HelmChartFound
	if registry.IsOCI(chartCopy.Spec.Repository) {
		var found bool
		found, err = utils.FindOCIChart(chartCopy.Spec.Repository, chartCopy.Spec.Chart, chartCopy.Spec.ChartVersion)
		if !found {
			chartPhase = appsapi.HelmChartNotFound
			reason = fmt.Sprintf("not found a version matched %s for chart %s/%s",
				chartCopy.Spec.ChartVersion, chartCopy.Spec.Repository, chartCopy.Spec.Chart)
		}
	} else {
		_, err = repo.FindChartInAuthRepoURL(
			chartCopy.Spec.Repository,
			username,
			password,
			chartCopy.Spec.Chart,
			chartCopy.Spec.ChartVersion,
			"", "", "",
			getter.All(utils.Settings),
		)
	}
	if err != nil {
		// failed to find chart
		chartPhase = appsapi.HelmChartNotFound
		reason = err.Error()
	}

	err = deployer.chartController.UpdateChartStatus(chartCopy, &appsapi.HelmChartStatus{
		Phase:  chartPhase,
		Reason: reason,
	})
	if err != nil {
		return err
	}

	// find all referred Base UIDs
	var baseUIDs []string
	for key, val := range chartCopy.Labels {
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
		base, err := deployer.baseController.FindBaseByUID(baseuid)
		if err != nil {
			wg.Done()
			klog.Warning(fmt.Sprintf("failed to resyncBase %s: %v", baseuid, err))
			continue
		}

		go func() {
			defer wg.Done()
			if err = deployer.populateDescriptions(base); err != nil {
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
			if err = utils.PatchHelmChartLabelsAndAnnotations(deployer.clusternetClient, chart, labelsToPatch, nil); err != nil {
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
			if err = utils.PatchManifestLabelsAndAnnotations(deployer.clusternetClient, manifest, labelsToPatch, nil); err != nil {
				errCh <- err
			}
		}(manifest)
	}

	wg.Wait()

	// collect errors
	close(errCh)
	var allErrs []error
	for err = range errCh {
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
		if err = utils.PatchManifestLabelsAndAnnotations(deployer.clusternetClient, manifest,
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
		if err = utils.PatchHelmChartLabelsAndAnnotations(deployer.clusternetClient, chart,
			nil, annotationsToPatch); err != nil {
			return err
		}

		return errors.New(msg)
	}

	// finalizer FeedProtectionFinalizer does not exist,
	// so we just remove this feed from all Subscriptions
	return removeFeedFromAllMatchingSubscriptions(deployer.clusternetClient, allRelatedSubscriptions, chart.Labels)
}
