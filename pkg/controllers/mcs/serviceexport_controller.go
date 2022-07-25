package mcs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dixudx/yacht"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"
	mcsclientset "sigs.k8s.io/mcs-api/pkg/client/clientset/versioned"
	mcsInformers "sigs.k8s.io/mcs-api/pkg/client/informers/externalversions/apis/v1alpha1"

	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

func init() {
	utilruntime.Must(v1alpha1.AddToScheme(scheme.Scheme))
}

type reconciler struct {
	//local msc client
	mcsClientset    *mcsclientset.Clientset
	localk8sClient  kubernetes.Interface
	parentk8sClient kubernetes.Interface
	// child cluster dedicated namespace
	dedicatedNamespace string
}

func NewController(parentDedicatedKubeConfig *rest.Config, localClient kubernetes.Interface, mcsClientset *mcsclientset.Clientset,
	seInformers mcsInformers.ServiceExportInformer, namespace string, workers int) *yacht.Controller {
	parentClient := kubernetes.NewForConfigOrDie(parentDedicatedKubeConfig)

	r := &reconciler{
		mcsClientset:       mcsClientset,
		parentk8sClient:    parentClient,
		localk8sClient:     localClient,
		dedicatedNamespace: namespace,
	}

	c := yacht.NewController("serviceexport").WithWorkers(workers).WithCacheSynced(seInformers.Informer().HasSynced).WithHandlerFunc(r.Handle)
	seInformers.Informer().AddEventHandler(utils.HandleAllWith(c.Enqueue))

	return c
}

func (r *reconciler) Handle(obj interface{}) (requeueAfter *time.Duration, err error) {
	ctx := context.Background()
	key := obj.(string)
	namespace, seName, err := cache.SplitMetaNamespaceKey(key)
	utilruntime.Must(err)

	se, err := r.mcsClientset.MulticlusterV1alpha1().ServiceExports(namespace).Get(ctx, seName, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			utilruntime.HandleError(fmt.Errorf("service export '%s' in work queue no longer exists", key))
			return nil, nil
		}
		return nil, err
	}

	seTerminating := se.DeletionTimestamp != nil

	if !utils.ContainsString(se.Finalizers, known.SeFinalizer) && !seTerminating {
		se.Finalizers = append(se.Finalizers, known.SeFinalizer)
		se, err = r.mcsClientset.MulticlusterV1alpha1().ServiceExports(namespace).Update(context.TODO(),
			se, metav1.UpdateOptions{})
		if err != nil {
			d := time.Second
			return &d, err
		}
	}

	// recycle corresponding endpoint slice in parent cluster.
	if seTerminating {
		if err = r.parentk8sClient.DiscoveryV1().EndpointSlices(r.dedicatedNamespace).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(labels.Set{discoveryv1.LabelServiceName: utils.DerivedName(se.Name)}).String(),
		}); err != nil {
			// try next time, make sure we clear endpoint slice
			d := time.Second
			return &d, err
		}
		se.Finalizers = utils.RemoveString(se.Finalizers, known.SeFinalizer)
		se, err = r.mcsClientset.MulticlusterV1alpha1().ServiceExports(namespace).Update(context.TODO(),
			se, metav1.UpdateOptions{})
		if err != nil {
			d := time.Second
			return &d, err
		}
		klog.Infof("service export %s has been recycled successfully", se.Name)
		return nil, nil
	}

	var endpointSliceList *discoveryv1.EndpointSliceList
	if endpointSliceList, err = r.localk8sClient.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{discoveryv1.LabelServiceName: se.Name}).String(),
	}); err != nil {
		if errors.IsNotFound(err) {
			// service may not exist now, try next time.
			d := time.Second
			return &d, err
		}
		return nil, err
	}

	wg := sync.WaitGroup{}
	var allErrs []error
	errCh := make(chan error, len(endpointSliceList.Items))
	for index := range endpointSliceList.Items {
		wg.Add(1)
		slice := endpointSliceList.Items[index].DeepCopy()
		newSlice := r.constructEndpointSlice(slice, se)
		go func(slice *discoveryv1.EndpointSlice) {
			defer wg.Done()
			if err = ApplyEndPointSliceWithRetry(r.parentk8sClient, slice); err != nil {
				errCh <- err
				klog.Infof("slice %s sync err: %s", slice.Name, err)
			}
		}(newSlice)
	}
	wg.Wait()
	// collect errors
	close(errCh)
	for err := range errCh {
		allErrs = append(allErrs, err)
	}
	if len(allErrs) > 0 {
		reason := utilerrors.NewAggregate(allErrs).Error()
		msg := fmt.Sprintf("failed to sync endpoint slices of service export %s: %s", klog.KObj(se), reason)
		klog.ErrorDepth(5, msg)
		d := time.Second
		return &d, err
	}
	klog.Infof("service export %s has been synced successfully", se.Name)
	return nil, nil
}

// constructEndpointSlice construct a new endpoint slice from local slice.
func (r *reconciler) constructEndpointSlice(slice *discoveryv1.EndpointSlice, se *v1alpha1.ServiceExport) *discoveryv1.EndpointSlice {
	// mutate slice fields before upload to parent cluster.
	newSlice := &discoveryv1.EndpointSlice{}
	newSlice.AddressType = slice.AddressType
	newSlice.Endpoints = slice.Endpoints
	newSlice.Ports = slice.Ports
	newSlice.Labels = make(map[string]string)

	newSlice.GetLabels()[known.LabelServiceName] = se.Name
	newSlice.GetLabels()[discoveryv1.LabelServiceName] = utils.DerivedName(se.Name)

	if subNamespace, exist := se.GetLabels()[known.ConfigSubscriptionNamespaceLabel]; exist {
		newSlice.GetLabels()[known.ConfigSubscriptionNamespaceLabel] = subNamespace
	}

	newSlice.Namespace = r.dedicatedNamespace
	newSlice.Name = slice.Name
	return newSlice
}

// ApplyEndPointSliceWithRetry create or update existed slices.
func ApplyEndPointSliceWithRetry(client kubernetes.Interface, slice *discoveryv1.EndpointSlice) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		var lastError error
		_, lastError = client.DiscoveryV1().EndpointSlices(slice.GetNamespace()).Create(context.TODO(), slice, metav1.CreateOptions{})
		if lastError == nil {
			return nil
		}
		if !errors.IsAlreadyExists(lastError) {
			return lastError
		}

		curObj, err := client.DiscoveryV1().EndpointSlices(slice.GetNamespace()).Get(context.TODO(), slice.GetName(), metav1.GetOptions{})
		if err != nil {
			return err
		}
		lastError = nil

		if utils.ResourceNeedResync(curObj, slice, false) {
			// try to update slice
			curObj.Ports = slice.Ports
			curObj.Endpoints = slice.Endpoints
			curObj.AddressType = slice.AddressType
			_, lastError = client.DiscoveryV1().EndpointSlices(slice.GetNamespace()).Update(context.TODO(), curObj, metav1.UpdateOptions{})
			if lastError == nil {
				return nil
			}
		}
		return lastError
	})
}
