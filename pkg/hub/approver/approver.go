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

package approver

import (
	"context"
	"fmt"
	"strings"
	"sync"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeInformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1Lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	"github.com/clusternet/clusternet/pkg/apis/clusters"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/apis/proxies"
	"github.com/clusternet/clusternet/pkg/controllers/clusters/clusterregistrationrequest"
	clusternetClientSet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	clusternetInformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	clusterListers "github.com/clusternet/clusternet/pkg/generated/listers/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

// CRRApprover defines configuration for ClusterRegistrationRequests approver
type CRRApprover struct {
	ctx context.Context

	crrController *clusterregistrationrequest.Controller

	crrLister  clusterListers.ClusterRegistrationRequestLister
	mclsLister clusterListers.ManagedClusterLister

	nsLister corev1Lister.NamespaceLister
	saLister corev1Lister.ServiceAccountLister

	kubeclient       *kubernetes.Clientset
	clusternetclient *clusternetClientSet.Clientset

	socketConnection bool
}

// NewCRRApprover returns a new CRRApprover for ClusterRegistrationRequest.
func NewCRRApprover(ctx context.Context, kubeclient *kubernetes.Clientset, clusternetclient *clusternetClientSet.Clientset,
	clusternetInformerFactory clusternetInformers.SharedInformerFactory, kubeInformerFactory kubeInformers.SharedInformerFactory,
	socketConnection bool) (*CRRApprover, error) {
	crrApprover := &CRRApprover{
		ctx:              ctx,
		kubeclient:       kubeclient,
		clusternetclient: clusternetclient,
		crrLister:        clusternetInformerFactory.Clusters().V1beta1().ClusterRegistrationRequests().Lister(),
		mclsLister:       clusternetInformerFactory.Clusters().V1beta1().ManagedClusters().Lister(),
		nsLister:         kubeInformerFactory.Core().V1().Namespaces().Lister(),
		saLister:         kubeInformerFactory.Core().V1().ServiceAccounts().Lister(),
		socketConnection: socketConnection,
	}

	newCRRController, err := clusterregistrationrequest.NewController(ctx,
		kubeclient, clusternetclient,
		clusternetInformerFactory.Clusters().V1beta1().ClusterRegistrationRequests(),
		crrApprover.handleClusterRegistrationRequests)
	if err != nil {
		return nil, err
	}
	crrApprover.crrController = newCRRController

	return crrApprover, nil
}

func (crrApprover *CRRApprover) Run(threadiness int) {
	klog.Info("starting Clusternet CRRApprover ...")

	// initializing roles is really important
	// and nothing works if the roles don't get initialized
	crrApprover.applyDefaultRBACRules()

	// todo: gorountine
	crrApprover.crrController.Run(threadiness, crrApprover.ctx.Done())
	return
}

func (crrApprover *CRRApprover) applyDefaultRBACRules() {
	klog.Infof("applying default rbac rules")
	clusterroles := crrApprover.bootstrappingClusterRoles()
	wg := sync.WaitGroup{}
	wg.Add(len(clusterroles))
	for _, clusterrole := range clusterroles {
		go func(cr rbacv1.ClusterRole) {
			defer wg.Done()

			// make sure this clusterrole gets initialized before we go next
			for {
				err := utils.EnsureClusterRole(crrApprover.ctx, cr, crrApprover.kubeclient, retry.DefaultBackoff)
				if err == nil {
					break
				}
				klog.ErrorDepth(2, err)
			}
		}(clusterrole)
	}

	wg.Wait()
}

func (crrApprover *CRRApprover) bootstrappingClusterRoles() []rbacv1.ClusterRole {
	// default cluster roles for initializing

	return []rbacv1.ClusterRole{}
}

func (crrApprover *CRRApprover) defaultRoles(namespace string) []rbacv1.Role {
	// default roles for child cluster registration

	roleForManagedCluster := rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:        ManagedClusterRole,
			Namespace:   namespace,
			Annotations: map[string]string{known.AutoUpdateAnnotation: "true"},
			Labels: map[string]string{
				known.ClusterBootstrappingLabel: known.RBACDefaults,
				known.ObjectCreatedByLabel:      known.ClusternetHubName,
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"*"},
				Resources: []string{"*"},
				Verbs:     []string{"*"},
			},
		},
	}

	return []rbacv1.Role{
		roleForManagedCluster,
	}
}

func (crrApprover *CRRApprover) defaultClusterRoles(clusterID types.UID) []rbacv1.ClusterRole {
	clusterRoles := rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:        SocketsClusterRoleNamePrefix + string(clusterID),
			Annotations: map[string]string{known.AutoUpdateAnnotation: "true"},
			Labels: map[string]string{
				known.ClusterBootstrappingLabel: known.RBACDefaults,
				known.ObjectCreatedByLabel:      known.ClusternetHubName,
				known.ClusterIDLabel:            string(clusterID),
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{clusters.GroupName},
				Resources: []string{"clusterregistrationrequests"},
				Verbs: []string{
					"create", // create cluster registration requests
					"get",    // and get the created object, we don't allow to "list" operation due to security concerns
				},
			},
		},
	}

	if crrApprover.socketConnection {
		clusterRoles.Rules = append(clusterRoles.Rules, rbacv1.PolicyRule{
			APIGroups:     []string{proxies.GroupName},
			Resources:     []string{"sockets"},
			ResourceNames: []string{string(clusterID)},
			Verbs:         []string{"*"},
		})
	}

	return []rbacv1.ClusterRole{
		clusterRoles,
	}
}

func (crrApprover *CRRApprover) handleClusterRegistrationRequests(crr *clusterapi.ClusterRegistrationRequest) error {
	// If an error occurs during handling, we'll requeue the item so we can
	// attempt processing again later. This could have been caused by a
	// temporary network failure, or any other transient reason.

	result := new(clusterapi.ApprovedResult)

	// validate cluster id
	expectedClusterID := strings.TrimPrefix(crr.Name, known.NamePrefixForClusternetObjects)
	if expectedClusterID != string(crr.Spec.ClusterID) {
		err := fmt.Errorf("ClusterRegistrationRequest %q has got illegal update on spec.clusterID from %q to %q, will skip processing",
			crr.Name, expectedClusterID, crr.Spec.ClusterID)
		klog.Error(err)

		*result = clusterapi.RequestDenied
		utilruntime.HandleError(crrApprover.crrController.UpdateCRRStatus(crr, &clusterapi.ClusterRegistrationRequestStatus{
			Result:       result,
			ErrorMessage: err.Error(),
		}))
		return nil
	}

	if crr.Status.Result != nil {
		klog.V(4).Infof("ClusterRegistrationRequest %q has already been processed with Result %q. Skip it.", *crr.Status.Result)
		return nil
	}

	// 1. create dedicated namespace
	klog.V(5).Infof("create dedicated namespace for cluster %q (%q) if needed", crr.Spec.ClusterID, crr.Spec.ClusterName)
	ns, err := crrApprover.createNamespaceForChildClusterIfNeeded(crr.Spec.ClusterID, crr.Spec.ClusterName)
	if err != nil {
		return err
	}

	// 2. create ManagedCluster object
	klog.V(5).Infof("create corresponding MangedCluster for cluster %q (%q) if needed", crr.Spec.ClusterID, crr.Spec.ClusterName)
	mc, err := crrApprover.createManagedClusterIfNeeded(ns.Name, crr.Spec.ClusterName, crr.Spec.ClusterID, crr.Spec.ClusterType, crr.Spec.SyncMode)
	if err != nil {
		return err
	}

	// 3. create ServiceAccount
	klog.V(5).Infof("create service account for cluster %q (%q) if needed", crr.Spec.ClusterID, crr.Spec.ClusterName)
	sa, err := crrApprover.createServiceAccountIfNeeded(ns.Name, crr.Spec.ClusterName, crr.Spec.ClusterID)
	if err != nil {
		return err
	}

	// 4. binding default rbac rules
	klog.V(5).Infof("bind related clusterroles/roles for cluster %q (%q) if needed", crr.Spec.ClusterID, crr.Spec.ClusterName)
	err = crrApprover.bindingClusterRolesIfNeeded(sa.Name, sa.Namespace, crr.Spec.ClusterID)
	if err != nil {
		return err
	}
	err = crrApprover.bindingRoleIfNeeded(sa.Name, sa.Namespace)
	if err != nil {
		return err
	}

	// 5. get credentials
	klog.V(5).Infof("get generated credentials for cluster %q (%q)", crr.Spec.ClusterID, crr.Spec.ClusterName)
	secret, err := getCredentialsForChildCluster(crrApprover.ctx, crrApprover.kubeclient, retry.DefaultBackoff, sa.Name, sa.Namespace)
	if err != nil {
		return err
	}

	// 6. update status
	*result = clusterapi.RequestApproved
	err = crrApprover.crrController.UpdateCRRStatus(crr, &clusterapi.ClusterRegistrationRequestStatus{
		Result:             result,
		ErrorMessage:       "",
		DedicatedNamespace: ns.Name,
		ManagedClusterName: mc.Name,
		DedicatedToken:     secret.Data[corev1.ServiceAccountTokenKey],
		CACertificate:      secret.Data[corev1.ServiceAccountRootCAKey],
	})
	if err != nil {
		return err
	}

	return nil
}

func (crrApprover *CRRApprover) createNamespaceForChildClusterIfNeeded(clusterID types.UID, clusterName string) (*corev1.Namespace, error) {
	// checks for an existed dedicated namespace for child cluster
	// the clusterName here may vary, we use clusterID as the identifier
	namespaces, err := crrApprover.nsLister.List(labels.SelectorFromSet(labels.Set{
		known.ObjectCreatedByLabel: known.ClusternetAgentName,
		known.ClusterIDLabel:       string(clusterID),
	}))
	if err != nil {
		return nil, err
	}
	if namespaces != nil {
		if len(namespaces) > 1 {
			klog.Warningf("found multiple namespaces dedicated for cluster %s !!!", clusterID)
		}
		return namespaces[0], nil
	}

	klog.V(4).Infof("no dedicated namespace for cluster %s found, will create a new one", clusterID)
	newNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: known.NamePrefixForClusternetObjects,
			Labels: map[string]string{
				known.ObjectCreatedByLabel: known.ClusternetAgentName,
				known.ClusterIDLabel:       string(clusterID),
				known.ClusterNameLabel:     clusterName,
			},
		},
	}
	newNs, err = crrApprover.kubeclient.CoreV1().Namespaces().Create(crrApprover.ctx, newNs, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	klog.V(4).Infof("successfully create dedicated namespace %s for cluster %s", newNs.Name, clusterID)
	return newNs, nil
}

func (crrApprover *CRRApprover) createManagedClusterIfNeeded(namespace, clusterName string, clusterID types.UID,
	clusterType clusterapi.ClusterType, clusterSyncMode clusterapi.ClusterSyncMode) (*clusterapi.ManagedCluster, error) {
	// checks for an existed ManagedCluster object
	// the clusterName here may vary, we use clusterID as the identifier
	mcs, err := crrApprover.mclsLister.List(labels.SelectorFromSet(labels.Set{
		known.ObjectCreatedByLabel: known.ClusternetAgentName,
		known.ClusterIDLabel:       string(clusterID),
	}))
	if err != nil {
		return nil, err
	}
	if mcs != nil {
		if len(mcs) > 1 {
			klog.Warningf("found multiple ManagedCluster objects dedicated for cluster %s !!!", clusterID)
		}
		return mcs[0], nil
	}

	managedCluster := &clusterapi.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName,
			Labels: map[string]string{
				known.ObjectCreatedByLabel: known.ClusternetAgentName,
				known.ClusterIDLabel:       string(clusterID),
				known.ClusterNameLabel:     clusterName,
			},
		},
		Spec: clusterapi.ManagedClusterSpec{
			ClusterID:   clusterID,
			ClusterType: clusterType,
			SyncMode:    clusterSyncMode,
		},
	}

	mc, err := crrApprover.clusternetclient.ClustersV1beta1().ManagedClusters(namespace).Create(crrApprover.ctx, managedCluster, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("failed to create ManagedCluster for cluster %q: %v", clusterID, err)
		return nil, err
	}

	klog.V(4).Infof("successfully create ManagedCluster %s/%s for cluster %s", mc.Namespace, mc.Name, clusterID)
	return mc, nil
}

func (crrApprover *CRRApprover) createServiceAccountIfNeeded(namespace, clusterName string, clusterID types.UID) (*corev1.ServiceAccount, error) {
	// checks for an existed dedicated service account created for child cluster to access parent cluster
	// the clusterName here may vary, we use clusterID as the identifier
	sas, err := crrApprover.saLister.List(labels.SelectorFromSet(labels.Set{
		known.ObjectCreatedByLabel: known.ClusternetAgentName,
		known.ClusterIDLabel:       string(clusterID),
	}))
	if err != nil {
		return nil, err
	}
	if sas != nil {
		if len(sas) > 1 {
			klog.Warningf("found multiple service accounts dedicated for cluster %s !!!", clusterID)
		}
		return sas[0], nil
	}

	// no need to use backoff since we use generateName to create new ServiceAccount
	klog.V(4).Infof("no dedicated service account for cluster %s found, will create a new one", clusterID)
	newSA := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: known.NamePrefixForClusternetObjects,
			Labels: map[string]string{
				known.ObjectCreatedByLabel: known.ClusternetAgentName,
				known.ClusterIDLabel:       string(clusterID),
				known.ClusterNameLabel:     clusterName,
			},
		},
	}
	newSA, err = crrApprover.kubeclient.CoreV1().ServiceAccounts(namespace).Create(crrApprover.ctx, newSA, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	klog.V(4).Infof("successfully create dedicated service account %s for cluster %s", newSA.Name, clusterID)
	return newSA, nil
}

func (crrApprover *CRRApprover) bindingClusterRolesIfNeeded(serviceAccountName, serivceAccountNamespace string, clusterID types.UID) error {
	var allErrs []error
	wg := sync.WaitGroup{}

	// create sockets clusterrole first
	clusterRoles := crrApprover.defaultClusterRoles(clusterID)
	wg.Add(len(clusterRoles))
	for _, clusterrole := range clusterRoles {
		go func(cr rbacv1.ClusterRole) {
			defer wg.Done()
			err := utils.EnsureClusterRole(crrApprover.ctx, cr, crrApprover.kubeclient, retry.DefaultRetry)
			if err != nil {
				allErrs = append(allErrs, err)
			}
		}(clusterrole)
	}
	wg.Wait()
	if len(allErrs) != 0 {
		return utilerrors.NewAggregate(allErrs)
	}

	// then we bind all the clusterroles
	wg.Add(len(clusterRoles))
	for _, clusterrole := range clusterRoles {
		go func(cr rbacv1.ClusterRole) {
			defer wg.Done()
			err := utils.EnsureClusterRoleBinding(crrApprover.ctx, rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:        cr.Name,
					Annotations: map[string]string{known.AutoUpdateAnnotation: "true"},
					Labels: map[string]string{
						known.ClusterBootstrappingLabel: known.RBACDefaults,
						known.ObjectCreatedByLabel:      known.ClusternetHubName,
						known.ClusterIDLabel:            string(clusterID),
					},
				},
				Subjects: []rbacv1.Subject{
					{Kind: rbacv1.ServiceAccountKind, Name: serviceAccountName, Namespace: serivceAccountNamespace},
				},
				RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: cr.Name},
			}, crrApprover.kubeclient, retry.DefaultRetry)
			if err != nil {
				allErrs = append(allErrs, err)
			}
		}(clusterrole)
	}

	wg.Wait()
	return utilerrors.NewAggregate(allErrs)
}

func (crrApprover *CRRApprover) bindingRoleIfNeeded(serviceAccountName, namespace string) error {
	var allErrs []error
	wg := sync.WaitGroup{}

	// first we ensure default roles exist
	roles := crrApprover.defaultRoles(namespace)
	wg.Add(len(roles))
	for _, role := range roles {
		go func(r rbacv1.Role) {
			defer wg.Done()
			err := utils.EnsureRole(crrApprover.ctx, r, crrApprover.kubeclient, retry.DefaultRetry)
			if err != nil {
				allErrs = append(allErrs, err)
			}
		}(role)
	}
	wg.Wait()

	if len(allErrs) != 0 {
		return utilerrors.NewAggregate(allErrs)
	}

	// then we bind these roles
	wg.Add(len(roles))
	for _, role := range roles {
		go func(r rbacv1.Role) {
			defer wg.Done()
			err := utils.EnsureRoleBinding(crrApprover.ctx, rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:        r.Name,
					Namespace:   r.Namespace,
					Annotations: map[string]string{known.AutoUpdateAnnotation: "true"},
					Labels: map[string]string{
						known.ClusterBootstrappingLabel: known.RBACDefaults,
						known.ObjectCreatedByLabel:      known.ClusternetHubName,
					},
				},
				Subjects: []rbacv1.Subject{
					{Kind: rbacv1.ServiceAccountKind, Name: serviceAccountName, Namespace: namespace},
				},
				RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: r.Name},
			}, crrApprover.kubeclient, retry.DefaultRetry)
			if err != nil {
				allErrs = append(allErrs, err)
			}
		}(role)
	}

	wg.Wait()
	return utilerrors.NewAggregate(allErrs)
}

func getCredentialsForChildCluster(ctx context.Context, client *kubernetes.Clientset, backoff wait.Backoff, saName, saNamespace string) (*corev1.Secret, error) {
	var secret *corev1.Secret
	var sa *corev1.ServiceAccount
	var lastError error
	err := wait.ExponentialBackoffWithContext(ctx, backoff, func() (done bool, err error) {
		// first we get the auto-created secret name from serviceaccount
		sa, lastError = client.CoreV1().ServiceAccounts(saNamespace).Get(ctx, saName, metav1.GetOptions{})
		if lastError != nil {
			return false, nil
		}
		if len(sa.Secrets) == 0 {
			lastError = fmt.Errorf("waiting for secret got populated in ServiceAccount %s/%s", saNamespace, saName)
			return false, nil
		}

		secretName := sa.Secrets[0].Name
		secret, lastError = client.CoreV1().Secrets(saNamespace).Get(ctx, secretName, metav1.GetOptions{})
		if lastError != nil {
			return false, nil
		}
		return true, nil
	})

	if err == nil {
		return secret, nil
	}
	return nil, lastError
}
