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

package localizer

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/controllers/apps/globalization"
	"github.com/clusternet/clusternet/pkg/controllers/apps/localization"
	clusternetclientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	clusternetinformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

// Localizer defines configuration for the application localization
type Localizer struct {
	ctx context.Context

	clusternetClient *clusternetclientset.Clientset

	locLister      applisters.LocalizationLister
	locSynced      cache.InformerSynced
	globLister     applisters.GlobalizationLister
	globSynced     cache.InformerSynced
	chartLister    applisters.HelmChartLister
	chartSynced    cache.InformerSynced
	manifestLister applisters.ManifestLister
	manifestSynced cache.InformerSynced

	locController  *localization.Controller
	globController *globalization.Controller

	recorder record.EventRecorder
}

func NewLocalizer(ctx context.Context,
	clusternetClient *clusternetclientset.Clientset,
	clusternetInformerFactory clusternetinformers.SharedInformerFactory,
	recorder record.EventRecorder) (*Localizer, error) {

	localizer := &Localizer{
		ctx:              ctx,
		clusternetClient: clusternetClient,
		locLister:        clusternetInformerFactory.Apps().V1alpha1().Localizations().Lister(),
		locSynced:        clusternetInformerFactory.Apps().V1alpha1().Localizations().Informer().HasSynced,
		globLister:       clusternetInformerFactory.Apps().V1alpha1().Globalizations().Lister(),
		globSynced:       clusternetInformerFactory.Apps().V1alpha1().Globalizations().Informer().HasSynced,
		chartLister:      clusternetInformerFactory.Apps().V1alpha1().HelmCharts().Lister(),
		chartSynced:      clusternetInformerFactory.Apps().V1alpha1().HelmCharts().Informer().HasSynced,
		manifestLister:   clusternetInformerFactory.Apps().V1alpha1().Manifests().Lister(),
		manifestSynced:   clusternetInformerFactory.Apps().V1alpha1().Manifests().Informer().HasSynced,
		recorder:         recorder,
	}

	locController, err := localization.NewController(ctx, clusternetClient,
		clusternetInformerFactory.Apps().V1alpha1().Localizations(),
		clusternetInformerFactory.Apps().V1alpha1().HelmCharts(),
		clusternetInformerFactory.Apps().V1alpha1().Manifests(),
		recorder,
		localizer.handleLocalization)
	if err != nil {
		return nil, err
	}
	localizer.locController = locController

	globController, err := globalization.NewController(ctx,
		clusternetClient,
		clusternetInformerFactory.Apps().V1alpha1().Globalizations(),
		clusternetInformerFactory.Apps().V1alpha1().HelmCharts(),
		clusternetInformerFactory.Apps().V1alpha1().Manifests(),
		recorder,
		localizer.handleGlobalization)
	if err != nil {
		return nil, err
	}
	localizer.globController = globController

	return localizer, nil
}

func (l *Localizer) Run(workers int) {
	klog.Info("starting Clusternet localizer ...")

	// Wait for the caches to be synced before starting workers
	klog.V(5).Info("waiting for informer caches to sync")
	if !cache.WaitForCacheSync(l.ctx.Done(),
		l.locSynced,
		l.globSynced,
		l.chartSynced,
		l.manifestSynced,
	) {
		return
	}

	go l.locController.Run(workers, l.ctx.Done())
	go l.globController.Run(workers, l.ctx.Done())

	<-l.ctx.Done()
}

func (l *Localizer) handleLocalization(loc *appsapi.Localization) error {
	if loc.DeletionTimestamp != nil {
		// remove finalizer
		loc.Finalizers = utils.RemoveString(loc.Finalizers, known.AppFinalizer)
		_, err := l.clusternetClient.AppsV1alpha1().Localizations(loc.Namespace).Update(context.TODO(), loc, metav1.UpdateOptions{})
		if err != nil {
			klog.WarningDepth(4,
				fmt.Sprintf("failed to remove finalizer %s from Localization %s: %v", known.AppFinalizer, klog.KObj(loc), err))
		}
		return err
	}

	return nil
}

func (l *Localizer) handleGlobalization(glob *appsapi.Globalization) error {
	if glob.DeletionTimestamp != nil {
		// remove finalizer
		glob.Finalizers = utils.RemoveString(glob.Finalizers, known.AppFinalizer)
		_, err := l.clusternetClient.AppsV1alpha1().Globalizations().Update(context.TODO(), glob, metav1.UpdateOptions{})
		if err != nil {
			klog.WarningDepth(4,
				fmt.Sprintf("failed to remove finalizer %s from Globalization %s: %v", known.AppFinalizer, klog.KObj(glob), err))
		}
		return err
	}

	return nil
}

func (l *Localizer) ApplyOverridesToDescription(desc *appsapi.Description) error {
	// TODO

	return nil
}
