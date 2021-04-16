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

package registration

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
	"github.com/clusternet/clusternet/pkg/controllers/clusters/clusterregistrationrequest"
	clusternetClientSet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	informers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	"github.com/clusternet/clusternet/pkg/utils"
)

// Hub defines configuration for clusternet-hub
type Hub struct {
	HubContext context.Context

	crrController *clusterregistrationrequest.Controller

	clusternetInformerFactory informers.SharedInformerFactory

	kubeclientset       *kubernetes.Clientset
	clusternetclientset *clusternetClientSet.Clientset
}

// NewHub returns a new Hub.
func NewHub(ctx context.Context, kubeConfig string) (*Hub, error) {
	config, err := utils.GetKubeConfig(kubeConfig)
	if err != nil {
		return nil, err
	}

	// creating the clientset
	kubeclientset := kubernetes.NewForConfigOrDie(config)
	clusternetclientset := clusternetClientSet.NewForConfigOrDie(config)

	// creates the informer factory
	clusternetInformerFactory := informers.NewSharedInformerFactory(clusternetclientset, DefaultResync)

	hub := &Hub{
		HubContext:                ctx,
		clusternetInformerFactory: clusternetInformerFactory,
		kubeclientset:             kubeclientset,
		clusternetclientset:       clusternetclientset,
	}

	newCRRController, err := clusterregistrationrequest.NewController(ctx, kubeclientset, clusternetclientset,
		clusternetInformerFactory.Clusters().V1beta1().ClusterRegistrationRequests(),
		hub.handleClusterRegistrationRequests)
	if err != nil {
		return nil, err
	}
	hub.crrController = newCRRController

	// Start the informer factories to begin populating the informer caches
	clusternetInformerFactory.Start(ctx.Done())

	return hub, nil

}

func (hub *Hub) Run() error {
	klog.Info("starting Clusternet Hub ...")

	// initializing roles is really important
	// and nothing works if the roles don't get initialized
	hub.applyDefaultRBACRules()

	// Start method is non-blocking and runs all registered informers in a dedicated goroutine.
	hub.clusternetInformerFactory.Start(hub.HubContext.Done())

	// todo: gorountine
	hub.crrController.Run(DefaultThreadiness, hub.HubContext.Done())
	return nil
}

func (hub *Hub) applyDefaultRBACRules() {
	klog.Infof("applying default rbac rules")

	wg := sync.WaitGroup{}

	clusterroles := hub.defaultClusterRoles()
	wg.Add(len(clusterroles))
	for _, cr := range clusterroles {
		go func() {
			defer wg.Done()
			ensureClusterRole(hub.HubContext, cr, hub.kubeclientset)
		}()
	}

	wg.Wait()
}

func (hub *Hub) defaultClusterRoles() []rbacv1.ClusterRole {
	// default cluster roles for initializing

	// minimum role for child cluster registration
	clusterRoleForCRR := rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:        ClusterRegistrationRole,
			Annotations: map[string]string{AutoUpdateAnnotationKey: "true"},
			Labels: map[string]string{
				ClusterBootstrappingLabel: RBACDefaults,
				ClusterRegisteredByLabel:  ClusternetHubName,
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

	return []rbacv1.ClusterRole{
		clusterRoleForCRR,
	}
}

func (hub *Hub) defaultRoles(namespace string) []rbacv1.Role {
	// default roles for child cluster registration

	roleForManagedCluster := rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:        ManagedClusterRole,
			Namespace:   namespace,
			Annotations: map[string]string{AutoUpdateAnnotationKey: "true"},
			Labels: map[string]string{
				ClusterBootstrappingLabel: RBACDefaults,
				ClusterRegisteredByLabel:  ClusternetHubName,
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

func (hub *Hub) handleClusterRegistrationRequests(crr *clusterapi.ClusterRegistrationRequest) error {
	// If an error occurs during handling, we'll requeue the item so we can
	// attempt processing again later. This could have been caused by a
	// temporary network failure, or any other transient reason.

	// validate cluster id
	expectedClusterID := strings.TrimPrefix(crr.Name, fmt.Sprintf("%s-", CRRObjectNamePrefix))
	if expectedClusterID != string(crr.Spec.ClusterID) {
		err := fmt.Errorf("ClusterRegistrationRequest %q has got illegal update on spec.clusterID from %q to %q, will skip processing",
			crr.Name, expectedClusterID, crr.Spec.ClusterID)
		klog.Error(err)
		utilruntime.HandleError(hub.crrController.UpdateCRRStatus(crr, &clusterapi.ClusterRegistrationRequestStatus{
			Result:       clusterapi.RequestDenied,
			ErrorMessage: err.Error(),
		}))
		return nil
	}

	// 1. create dedicated namespace
	klog.V(5).Infof("create dedicated namespace for cluster %q (%q) if needed", crr.Spec.ClusterID, crr.Spec.ClusterName)
	ns, err := hub.createNamespaceForChildClusterIfNeeded(crr.Spec.ClusterID, crr.Spec.ClusterName)
	if err != nil {
		return err
	}

	// 2. create ManagedCluster object
	klog.V(5).Infof("create corresponding MangedCluster for cluster %q (%q) if needed", crr.Spec.ClusterID, crr.Spec.ClusterName)
	mc, err := hub.createManagedClusterIfNeeded(ns.Name, crr.Spec.ClusterName, crr.Spec.ClusterID, crr.Spec.ClusterType)
	if err != nil {
		return err
	}

	// 3. create ServiceAccount
	klog.V(5).Infof("create service account for cluster %q (%q) if needed", crr.Spec.ClusterID, crr.Spec.ClusterName)
	sa, err := hub.createServiceAccountIfNeeded(ns.Name, crr.Spec.ClusterName, crr.Spec.ClusterID)
	if err != nil {
		return err
	}

	// 4. binding default rbac rules
	klog.V(5).Infof("bind related clusterrols/roles for cluster %q (%q) if needed", crr.Spec.ClusterID, crr.Spec.ClusterName)
	err = hub.bindingDefaultClusterRolesIfNeeded(sa.Name, sa.Namespace)
	if err != nil {
		return err
	}
	err = hub.bindingRoleIfNeeded(sa.Name, sa.Namespace)
	if err != nil {
		return err
	}

	// 5. get credentials
	klog.V(5).Infof("get generated credentials for cluster %q (%q)", crr.Spec.ClusterID, crr.Spec.ClusterName)
	secret, err := getCredentialsForChildCluster(hub.HubContext, hub.kubeclientset, retry.DefaultBackoff, sa.Name, sa.Namespace)
	if err != nil {
		return err
	}

	// 6. update status
	err = hub.crrController.UpdateCRRStatus(crr, &clusterapi.ClusterRegistrationRequestStatus{
		Result:             clusterapi.RequestApproved,
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

func (hub *Hub) createNamespaceForChildClusterIfNeeded(clusterID types.UID, clusterName string) (*corev1.Namespace, error) {
	// checks for a existed dedicated namespace for child cluster
	// the clusterName here may vary, we use clusterID as the identifier
	namespaces, err := hub.kubeclientset.CoreV1().Namespaces().List(hub.HubContext, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{
			ClusterRegisteredByLabel: ClusternetAgentName,
			ClusterIDLabel:           string(clusterID),
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
			GenerateName: NamePrefixForChildCluster,
			Labels: map[string]string{
				ClusterRegisteredByLabel: ClusternetAgentName,
				ClusterIDLabel:           string(clusterID),
				ClusterNameLabel:         clusterName,
			},
		},
	}
	newNs, err = hub.kubeclientset.CoreV1().Namespaces().Create(hub.HubContext, newNs, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	klog.V(4).Infof("successfully create dedicated namespace %s for cluster %s", newNs.Name, clusterID)
	return newNs, nil
}

func (hub *Hub) createManagedClusterIfNeeded(namespace, clusterName string, clusterID types.UID, clusterType clusterapi.EdgeClusterType) (*clusterapi.ManagedCluster, error) {
	// checks for a existed ManagedCluster object
	// the clusterName here may vary, we use clusterID as the identifier
	mcs, err := hub.clusternetclientset.ClustersV1beta1().ManagedClusters(namespace).List(hub.HubContext, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{
			ClusterRegisteredByLabel: ClusternetAgentName,
			ClusterIDLabel:           string(clusterID),
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
				ClusterRegisteredByLabel: ClusternetAgentName,
				ClusterIDLabel:           string(clusterID),
				ClusterNameLabel:         clusterName,
			},
		},
		Spec: clusterapi.ManagedClusterSpec{
			ClusterID:   clusterID,
			ClusterType: clusterType,
		},
	}

	mc, err := hub.clusternetclientset.ClustersV1beta1().ManagedClusters(namespace).Create(hub.HubContext, managedCluster, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("failed to create ManagedCluster for cluster %q: %v", clusterID, err)
		return nil, err
	}

	klog.V(4).Infof("successfully create ManagedCluster %s/%s for cluster %s", mc.Namespace, mc.Name, clusterID)
	return mc, nil
}

func (hub *Hub) createServiceAccountIfNeeded(namespace, clusterName string, clusterID types.UID) (*corev1.ServiceAccount, error) {
	// checks for a existed dedicated service account created for child cluster to access parent cluster
	// the clusterName here may vary, we use clusterID as the identifier
	sas, err := hub.kubeclientset.CoreV1().ServiceAccounts(namespace).List(hub.HubContext, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{
			ClusterRegisteredByLabel: ClusternetAgentName,
			ClusterIDLabel:           string(clusterID),
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
			GenerateName: NamePrefixForChildCluster,
			Labels: map[string]string{
				ClusterRegisteredByLabel: ClusternetAgentName,
				ClusterIDLabel:           string(clusterID),
				ClusterNameLabel:         clusterName,
			},
		},
	}
	newSA, err = hub.kubeclientset.CoreV1().ServiceAccounts(namespace).Create(hub.HubContext, newSA, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	klog.V(4).Infof("successfully create dedicated service account %s for cluster %s", newSA.Name, clusterID)
	return newSA, nil
}

func (hub *Hub) bindingDefaultClusterRolesIfNeeded(serviceAccountName, serivceAccountNamespace string) error {
	allErrs := []error{}
	wg := sync.WaitGroup{}

	defaultClusterRoles := hub.defaultClusterRoles()
	wg.Add(len(defaultClusterRoles))
	for _, cr := range defaultClusterRoles {
		go func() {
			defer wg.Done()
			err := ensureClusterRoleBinding(hub.HubContext, rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:        cr.Name,
					Annotations: map[string]string{AutoUpdateAnnotationKey: "true"},
					Labels: map[string]string{
						ClusterBootstrappingLabel: RBACDefaults,
						ClusterRegisteredByLabel:  ClusternetHubName,
					},
				},
				Subjects: []rbacv1.Subject{
					{Kind: rbacv1.ServiceAccountKind, Name: serviceAccountName, Namespace: serivceAccountNamespace},
				},
				RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: cr.Name},
			}, hub.kubeclientset, retry.DefaultRetry)
			if err != nil {
				allErrs = append(allErrs, fmt.Errorf("failed to ensure binding for ClusterRole %q: %v", cr.Name, err))
			}
		}()
	}

	wg.Wait()
	return utilerrors.NewAggregate(allErrs)
}

func (hub *Hub) bindingRoleIfNeeded(serviceAccountName, namespace string) error {
	allErrs := []error{}
	wg := sync.WaitGroup{}

	// first we ensure default roles exist
	roles := hub.defaultRoles(namespace)
	wg.Add(len(roles))
	for _, r := range roles {
		go func() {
			defer wg.Done()
			err := ensureRole(hub.HubContext, r, hub.kubeclientset, retry.DefaultRetry)
			if err != nil {
				allErrs = append(allErrs, fmt.Errorf("failed to ensure Role %q: %v", r.Name, err))
			}
		}()
	}
	wg.Wait()

	if len(allErrs) != 0 {
		return utilerrors.NewAggregate(allErrs)
	}

	// then we bind these roles
	wg.Add(len(roles))
	for _, r := range roles {
		go func() {
			defer wg.Done()
			err := ensureRoleBinding(hub.HubContext, rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:        r.Name,
					Namespace:   r.Namespace,
					Annotations: map[string]string{AutoUpdateAnnotationKey: "true"},
					Labels: map[string]string{
						ClusterBootstrappingLabel: RBACDefaults,
						ClusterRegisteredByLabel:  ClusternetHubName,
					},
				},
				Subjects: []rbacv1.Subject{
					{Kind: rbacv1.ServiceAccountKind, Name: serviceAccountName, Namespace: namespace},
				},
				RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: r.Name},
			}, hub.kubeclientset, retry.DefaultRetry)
			if err != nil {
				allErrs = append(allErrs, fmt.Errorf("failed to ensure binding for Role %q: %v", r.Name, err))
			}
		}()
	}

	wg.Wait()
	return utilerrors.NewAggregate(allErrs)
}

// ensureClusterRole will make sure desired clusterrole exists and update the rules if available
func ensureClusterRole(ctx context.Context, clusterrole rbacv1.ClusterRole, client *kubernetes.Clientset) {
	klog.Infof("ensure ClusterRole %q...", clusterrole.Name)
	localCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// use JitterUntil to make sure this clusterrole gets initialized before we go next
	wait.JitterUntil(func() {
		_, err := client.RbacV1().ClusterRoles().Create(ctx, &clusterrole, metav1.CreateOptions{})
		if err == nil {
			// success on the creation
			cancel()
			return
		}
		if !errors.IsAlreadyExists(err) {
			utilruntime.HandleError(err)
			return
		}

		// try to auto update existing object
		cr, err := client.RbacV1().ClusterRoles().Get(ctx, clusterrole.Name, metav1.GetOptions{})
		if err != nil {
			utilruntime.HandleError(err)
			return
		}
		if autoUpdate, ok := cr.Annotations[AutoUpdateAnnotationKey]; ok && autoUpdate == "true" {
			_, err = client.RbacV1().ClusterRoles().Update(ctx, &clusterrole, metav1.UpdateOptions{})
			if err == nil {
				// success on the creation
				cancel()
				return
			}
			utilruntime.HandleError(err)
			return
		}
	}, DefaultAPICallRetryInterval, 0.4, true, localCtx.Done())

	klog.V(4).Infof("successfully ensure ClusterRole %q", clusterrole.Name)
}

func ensureClusterRoleBinding(ctx context.Context, clusterrolebinding rbacv1.ClusterRoleBinding, client *kubernetes.Clientset, backoff wait.Backoff) error {
	return wait.ExponentialBackoffWithContext(ctx, backoff, func() (done bool, err error) {
		_, err = client.RbacV1().ClusterRoleBindings().Create(ctx, &clusterrolebinding, metav1.CreateOptions{})
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
		if autoUpdate, ok := crb.Annotations[AutoUpdateAnnotationKey]; ok && autoUpdate == "true" {
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
	return wait.ExponentialBackoffWithContext(ctx, backoff, func() (done bool, err error) {
		_, err = client.RbacV1().Roles(role.Namespace).Create(ctx, &role, metav1.CreateOptions{})
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
		if autoUpdate, ok := r.Annotations[AutoUpdateAnnotationKey]; ok && autoUpdate == "true" {
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
		if autoUpdate, ok := rb.Annotations[AutoUpdateAnnotationKey]; ok && autoUpdate == "true" {
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
