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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	"github.com/clusternet/clusternet/pkg/apis/clusters"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/apis/proxies"
	"github.com/clusternet/clusternet/pkg/controllers/clusters/clusterregistrationrequest"
	clusternetClientSet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	clusterInformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/known"
)

// CRRApprover defines configuration for ClusterRegistrationRequests approver
type CRRApprover struct {
	ctx              context.Context
	crrController    *clusterregistrationrequest.Controller
	kubeclient       *kubernetes.Clientset
	clusternetclient *clusternetClientSet.Clientset
	socketConnection bool
}

// NewCRRApprover returns a new CRRApprover for ClusterRegistrationRequest.
func NewCRRApprover(ctx context.Context, kubeclient *kubernetes.Clientset, clusternetclient *clusternetClientSet.Clientset, crrsInformer clusterInformers.ClusterRegistrationRequestInformer, socketConnection bool) (*CRRApprover, error) {
	crrApprover := &CRRApprover{
		ctx:              ctx,
		kubeclient:       kubeclient,
		clusternetclient: clusternetclient,
		socketConnection: socketConnection,
	}

	newCRRController, err := clusterregistrationrequest.NewController(ctx, kubeclient, clusternetclient, crrsInformer, crrApprover.handleClusterRegistrationRequests)
	if err != nil {
		return nil, err
	}
	crrApprover.crrController = newCRRController

	return crrApprover, nil
}

func (crrApprover *CRRApprover) Run(threadiness int) error {
	klog.Info("starting Clusternet CRRApprover ...")

	// initializing roles is really important
	// and nothing works if the roles don't get initialized
	crrApprover.applyDefaultRBACRules()

	// todo: gorountine
	crrApprover.crrController.Run(threadiness, crrApprover.ctx.Done())
	return nil
}

func (crrApprover *CRRApprover) applyDefaultRBACRules() {
	klog.Infof("applying default rbac rules")

	wg := sync.WaitGroup{}

	clusterroles := crrApprover.bootstrappingClusterRoles()
	wg.Add(len(clusterroles))
	for _, clusterrole := range clusterroles {
		go func(cr rbacv1.ClusterRole) {
			defer wg.Done()

			klog.V(5).Infof("ensure ClusterRole %q...", cr.Name)
			// make sure this clusterrole gets initialized before we go next
			for {
				err := ensureClusterRole(crrApprover.ctx, cr, crrApprover.kubeclient, retry.DefaultBackoff)
				if err == nil {
					break
				}
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
			Annotations: map[string]string{known.AutoUpdateAnnotationKey: "true"},
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
			Annotations: map[string]string{known.AutoUpdateAnnotationKey: "true"},
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
	// checks for a existed dedicated namespace for child cluster
	// the clusterName here may vary, we use clusterID as the identifier
	namespaces, err := crrApprover.kubeclient.CoreV1().Namespaces().List(crrApprover.ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{
			known.ObjectCreatedByLabel: known.ClusternetAgentName,
			known.ClusterIDLabel:       string(clusterID),
		}).String(),
	})
	if err != nil {
		return nil, err
	}

	if len(namespaces.Items) > 0 {
		if len(namespaces.Items) > 1 {
			klog.Warningf("found multiple namespaces dedicated for cluster %s !!!", clusterID)
		}
		return &namespaces.Items[0], nil
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
	// checks for a existed ManagedCluster object
	// the clusterName here may vary, we use clusterID as the identifier
	mcs, err := crrApprover.clusternetclient.ClustersV1beta1().ManagedClusters(namespace).List(crrApprover.ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{
			known.ObjectCreatedByLabel: known.ClusternetAgentName,
			known.ClusterIDLabel:       string(clusterID),
		}).String(),
	})
	if err != nil {
		return nil, err
	}

	if len(mcs.Items) > 0 {
		if len(mcs.Items) > 1 {
			klog.Warningf("found multiple ManagedCluster objects dedicated for cluster %s !!!", clusterID)
		}
		return &mcs.Items[0], nil
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
	// checks for a existed dedicated service account created for child cluster to access parent cluster
	// the clusterName here may vary, we use clusterID as the identifier
	sas, err := crrApprover.kubeclient.CoreV1().ServiceAccounts(namespace).List(crrApprover.ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{
			known.ObjectCreatedByLabel: known.ClusternetAgentName,
			known.ClusterIDLabel:       string(clusterID),
		}).String(),
	})
	if err != nil {
		return nil, err
	}

	if len(sas.Items) > 0 {
		if len(sas.Items) > 1 {
			klog.Warningf("found multiple service accounts dedicated for cluster %s !!!", clusterID)
		}
		return &sas.Items[0], nil
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
	allErrs := []error{}
	wg := sync.WaitGroup{}

	// create sockets clusterrole first
	clusterRoles := crrApprover.defaultClusterRoles(clusterID)
	wg.Add(len(clusterRoles))
	for _, clusterrole := range clusterRoles {
		go func(cr rbacv1.ClusterRole) {
			defer wg.Done()
			err := ensureClusterRole(crrApprover.ctx, cr, crrApprover.kubeclient, retry.DefaultRetry)
			if err != nil {
				allErrs = append(allErrs, fmt.Errorf("failed to ensure ClusterRole %q: %v", cr.Name, err))
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
			err := ensureClusterRoleBinding(crrApprover.ctx, rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:        cr.Name,
					Annotations: map[string]string{known.AutoUpdateAnnotationKey: "true"},
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
				allErrs = append(allErrs, fmt.Errorf("failed to ensure binding for ClusterRole %q: %v", cr.Name, err))
			}
		}(clusterrole)
	}

	wg.Wait()
	return utilerrors.NewAggregate(allErrs)
}

func (crrApprover *CRRApprover) bindingRoleIfNeeded(serviceAccountName, namespace string) error {
	allErrs := []error{}
	wg := sync.WaitGroup{}

	// first we ensure default roles exist
	roles := crrApprover.defaultRoles(namespace)
	wg.Add(len(roles))
	for _, role := range roles {
		go func(r rbacv1.Role) {
			defer wg.Done()
			err := ensureRole(crrApprover.ctx, r, crrApprover.kubeclient, retry.DefaultRetry)
			if err != nil {
				allErrs = append(allErrs, fmt.Errorf("failed to ensure Role %q: %v", r.Name, err))
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
			err := ensureRoleBinding(crrApprover.ctx, rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:        r.Name,
					Namespace:   r.Namespace,
					Annotations: map[string]string{known.AutoUpdateAnnotationKey: "true"},
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
				allErrs = append(allErrs, fmt.Errorf("failed to ensure binding for Role %q: %v", r.Name, err))
			}
		}(role)
	}

	wg.Wait()
	return utilerrors.NewAggregate(allErrs)
}

// ensureClusterRole will make sure desired clusterrole exists and update the rules if available
func ensureClusterRole(ctx context.Context, clusterrole rbacv1.ClusterRole, client *kubernetes.Clientset, backoff wait.Backoff) error {
	klog.V(5).Infof("ensure ClusterRole %s...", clusterrole.Name)
	return wait.ExponentialBackoffWithContext(ctx, backoff, func() (bool, error) {
		_, err := client.RbacV1().ClusterRoles().Create(ctx, &clusterrole, metav1.CreateOptions{})
		if err == nil {
			// success on the creating
			return true, nil
		}
		if !errors.IsAlreadyExists(err) {
			klog.Errorf("failed to create ClusterRole %s: %v, will retry", clusterrole.Name, err)
			return false, nil
		}

		// try to auto update existing object
		cr, err := client.RbacV1().ClusterRoles().Get(ctx, clusterrole.Name, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("failed to get ClusterRole %s: %v, will retry", clusterrole.Name, err)
			return false, nil
		}
		if autoUpdate, ok := cr.Annotations[known.AutoUpdateAnnotationKey]; ok && autoUpdate == "true" {
			_, err = client.RbacV1().ClusterRoles().Update(ctx, &clusterrole, metav1.UpdateOptions{})
			if err == nil {
				// success on the updating
				return true, nil
			}
			klog.Errorf("failed to update ClusterRole %s: %v, will retry", clusterrole.Name, err)
			return false, nil
		}
		return true, nil
	})
}

func ensureClusterRoleBinding(ctx context.Context, clusterrolebinding rbacv1.ClusterRoleBinding, client *kubernetes.Clientset, backoff wait.Backoff) error {
	klog.V(5).Infof("ensure ClusterRoleBinding %s...", clusterrolebinding.Name)
	return wait.ExponentialBackoffWithContext(ctx, backoff, func() (bool, error) {
		_, err := client.RbacV1().ClusterRoleBindings().Create(ctx, &clusterrolebinding, metav1.CreateOptions{})
		if err == nil {
			// success on the creating
			return true, nil
		}
		if !errors.IsAlreadyExists(err) {
			klog.Errorf("failed to create ClusterRoleBinding %s: %v, will retry", clusterrolebinding.Name, err)
			return false, nil
		}

		// try to auto update existing object
		crb, err := client.RbacV1().ClusterRoleBindings().Get(ctx, clusterrolebinding.Name, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("failed to get ClusterRoleBinding %s: %v, will retry", clusterrolebinding.Name, err)
			return false, nil
		}
		if autoUpdate, ok := crb.Annotations[known.AutoUpdateAnnotationKey]; ok && autoUpdate == "true" {
			_, err = client.RbacV1().ClusterRoleBindings().Update(ctx, &clusterrolebinding, metav1.UpdateOptions{})
			if err == nil {
				// success on the updating
				return true, nil
			}
			klog.Errorf("failed to update ClusterRoleBinding %s: %v, will retry", clusterrolebinding.Name, err)
			return false, nil
		}
		return true, nil
	})
}

func ensureRole(ctx context.Context, role rbacv1.Role, client *kubernetes.Clientset, backoff wait.Backoff) error {
	klog.V(5).Infof("ensure Role %s/%s...", role.Namespace, role.Name)
	return wait.ExponentialBackoffWithContext(ctx, backoff, func() (bool, error) {
		_, err := client.RbacV1().Roles(role.Namespace).Create(ctx, &role, metav1.CreateOptions{})
		if err == nil {
			// success on the creating
			return true, nil
		}
		if !errors.IsAlreadyExists(err) {
			klog.ErrorDepth(4, fmt.Errorf("failed to create Role %s/%s: %v, will retry", role.Namespace, role.Name, err))
			return false, nil
		}

		// try to auto update existing object
		r, err := client.RbacV1().Roles(role.Namespace).Get(ctx, role.Name, metav1.GetOptions{})
		if err != nil {
			klog.ErrorDepth(4, fmt.Errorf("failed to get Role %s/%s: %v, will retry", role.Namespace, role.Name, err))
			return false, nil
		}
		if autoUpdate, ok := r.Annotations[known.AutoUpdateAnnotationKey]; ok && autoUpdate == "true" {
			_, err = client.RbacV1().Roles(role.Namespace).Update(ctx, &role, metav1.UpdateOptions{})
			if err == nil {
				// success on the updating
				return true, nil
			}
			klog.ErrorDepth(4, fmt.Sprintf("failed to update Role %s/%s: %v, will retry", role.Namespace, role.Name, err))
			return false, nil
		}
		return true, nil
	})
}

func ensureRoleBinding(ctx context.Context, rolebinding rbacv1.RoleBinding, client *kubernetes.Clientset, backoff wait.Backoff) error {
	klog.V(5).Infof("ensure RoleBinding %s/%s...", rolebinding.Namespace, rolebinding.Name)
	return wait.ExponentialBackoffWithContext(ctx, backoff, func() (done bool, err error) {
		_, err = client.RbacV1().RoleBindings(rolebinding.Namespace).Create(ctx, &rolebinding, metav1.CreateOptions{})
		if err == nil {
			// success on the creating
			return true, nil
		}
		if !errors.IsAlreadyExists(err) {
			klog.Errorf("failed to create RoleBinding %s/%s: %v, will retry", rolebinding.Namespace, rolebinding.Name, err)
			return false, nil
		}

		// try to auto update existing object
		rb, err := client.RbacV1().RoleBindings(rolebinding.Namespace).Get(ctx, rolebinding.Name, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("failed to get RoleBinding %s/%s: %v, will retry", rolebinding.Namespace, rolebinding.Name, err)
			return false, nil
		}
		if autoUpdate, ok := rb.Annotations[known.AutoUpdateAnnotationKey]; ok && autoUpdate == "true" {
			_, err = client.RbacV1().RoleBindings(rolebinding.Namespace).Update(ctx, &rolebinding, metav1.UpdateOptions{})
			if err == nil {
				// success on the updating
				return true, nil
			}
			klog.Errorf("failed to update RoleBinding %s/%s: %v, will retry", rolebinding.Namespace, rolebinding.Name, err)
			return false, nil
		}
		return true, nil
	})
}

func getCredentialsForChildCluster(ctx context.Context, client *kubernetes.Clientset, backoff wait.Backoff, saName, saNamespace string) (*corev1.Secret, error) {
	var secret *corev1.Secret
	var err error

	err = wait.ExponentialBackoffWithContext(ctx, backoff, func() (done bool, err error) {
		// first we get the auto-created secret name from serviceaccount
		sa, err := client.CoreV1().ServiceAccounts(saNamespace).Get(ctx, saName, metav1.GetOptions{})
		if err != nil {
			klog.ErrorDepth(4, fmt.Sprintf("failed to get ServiceAccount %s/%s: %v, will retry", saNamespace, saName, err))
			return false, nil
		}
		if len(sa.Secrets) == 0 {
			klog.WarningDepth(4, "waiting for secret get populated in ServiceAccount '%s/%s'...", saNamespace, saName)
			return false, nil
		}

		secretName := sa.Secrets[0].Name
		secret, err = client.CoreV1().Secrets(saNamespace).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("failed to get Secret %s/%s: %v, will retry", saNamespace, saName, err)
			return false, nil
		}
		return true, nil
	})
	return secret, err
}
