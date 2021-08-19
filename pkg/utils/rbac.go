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

package utils

import (
	"context"
	"fmt"

	"k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"github.com/clusternet/clusternet/pkg/known"
)

// EnsureClusterRole will make sure desired clusterrole exists and update it if available
func EnsureClusterRole(ctx context.Context, clusterrole v1.ClusterRole, client *kubernetes.Clientset, backoff wait.Backoff) error {
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
		if autoUpdate, ok := cr.Annotations[known.AutoUpdateAnnotation]; ok && autoUpdate == "true" {
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

// EnsureClusterRoleBinding will make sure desired clusterrolebinding exists and update it if available
func EnsureClusterRoleBinding(ctx context.Context, clusterrolebinding v1.ClusterRoleBinding, client *kubernetes.Clientset, backoff wait.Backoff) error {
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
		if autoUpdate, ok := crb.Annotations[known.AutoUpdateAnnotation]; ok && autoUpdate == "true" {
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

// EnsureRole will make sure desired role exists and update it if available
func EnsureRole(ctx context.Context, role v1.Role, client *kubernetes.Clientset, backoff wait.Backoff) error {
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
		if autoUpdate, ok := r.Annotations[known.AutoUpdateAnnotation]; ok && autoUpdate == "true" {
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

// EnsureRoleBinding will make sure desired rolebinding exists and update it if available
func EnsureRoleBinding(ctx context.Context, rolebinding v1.RoleBinding, client *kubernetes.Clientset, backoff wait.Backoff) error {
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
		if autoUpdate, ok := rb.Annotations[known.AutoUpdateAnnotation]; ok && autoUpdate == "true" {
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
