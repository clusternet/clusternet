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

	v1 "k8s.io/api/rbac/v1"
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
	var lastError error
	err := wait.ExponentialBackoffWithContext(ctx, backoff, func(ctx context.Context) (bool, error) {
		_, lastError = client.RbacV1().ClusterRoles().Create(ctx, &clusterrole, metav1.CreateOptions{})
		if lastError == nil {
			// success on the creating
			return true, nil
		}
		if !errors.IsAlreadyExists(lastError) {
			return false, nil
		}

		// try to auto update existing object
		cr, err := client.RbacV1().ClusterRoles().Get(ctx, clusterrole.Name, metav1.GetOptions{})
		if err != nil {
			lastError = err
			return false, nil
		}
		if autoUpdate, ok := cr.Annotations[known.AutoUpdateAnnotation]; ok && autoUpdate == "true" {
			_, lastError = client.RbacV1().ClusterRoles().Update(ctx, &clusterrole, metav1.UpdateOptions{})
			if lastError != nil {
				return false, nil
			}
		}
		// success on the updating
		return true, nil
	})
	if err == nil {
		return nil
	}
	return fmt.Errorf("failed to ensure ClusterRole %s: %v", clusterrole.Name, lastError)
}

// EnsureClusterRoleBinding will make sure desired clusterrolebinding exists and update it if available
func EnsureClusterRoleBinding(ctx context.Context, clusterrolebinding v1.ClusterRoleBinding, client *kubernetes.Clientset, backoff wait.Backoff) error {
	klog.V(5).Infof("ensure ClusterRoleBinding %s...", clusterrolebinding.Name)
	var lastError error
	err := wait.ExponentialBackoffWithContext(ctx, backoff, func(ctx context.Context) (bool, error) {
		_, lastError = client.RbacV1().ClusterRoleBindings().Create(ctx, &clusterrolebinding, metav1.CreateOptions{})
		if lastError == nil {
			// success on the creating
			return true, nil
		}
		if !errors.IsAlreadyExists(lastError) {
			return false, nil
		}

		// try to auto update existing object
		crb, err := client.RbacV1().ClusterRoleBindings().Get(ctx, clusterrolebinding.Name, metav1.GetOptions{})
		if err != nil {
			lastError = err
			return false, nil
		}
		if autoUpdate, ok := crb.Annotations[known.AutoUpdateAnnotation]; ok && autoUpdate == "true" {
			_, lastError = client.RbacV1().ClusterRoleBindings().Update(ctx, &clusterrolebinding, metav1.UpdateOptions{})
			if lastError != nil {
				return false, nil
			}
		}
		// success on the updating
		return true, nil
	})
	if err == nil {
		return nil
	}
	return fmt.Errorf("failed to ensure ClusterRoleBinding %s: %v", clusterrolebinding.Name, lastError)
}

// EnsureRole will make sure desired role exists and update it if available
func EnsureRole(ctx context.Context, role v1.Role, client *kubernetes.Clientset, backoff wait.Backoff) error {
	klog.V(5).Infof("ensure Role %s...", klog.KObj(&role).String())
	var lastError error
	err := wait.ExponentialBackoffWithContext(ctx, backoff, func(ctx context.Context) (bool, error) {
		_, lastError = client.RbacV1().Roles(role.Namespace).Create(ctx, &role, metav1.CreateOptions{})
		if lastError == nil {
			// success on the creating
			return true, nil
		}
		if !errors.IsAlreadyExists(lastError) {
			return false, nil
		}

		// try to auto update existing object
		r, err := client.RbacV1().Roles(role.Namespace).Get(ctx, role.Name, metav1.GetOptions{})
		if err != nil {
			lastError = err
			return false, nil
		}
		if autoUpdate, ok := r.Annotations[known.AutoUpdateAnnotation]; ok && autoUpdate == "true" {
			_, lastError = client.RbacV1().Roles(role.Namespace).Update(ctx, &role, metav1.UpdateOptions{})
			if lastError != nil {
				return false, nil
			}
		}
		// success on the updating
		return true, nil
	})
	if err == nil {
		return nil
	}
	return fmt.Errorf("failed to ensure Role %s: %v", klog.KObj(&role).String(), lastError)
}

// EnsureRoleBinding will make sure desired rolebinding exists and update it if available
func EnsureRoleBinding(ctx context.Context, rolebinding v1.RoleBinding, client *kubernetes.Clientset, backoff wait.Backoff) error {
	klog.V(5).Infof("ensure RoleBinding %s...", klog.KObj(&rolebinding).String())
	var lastError error
	err := wait.ExponentialBackoffWithContext(ctx, backoff, func(ctx context.Context) (done bool, err error) {
		_, lastError = client.RbacV1().RoleBindings(rolebinding.Namespace).Create(ctx, &rolebinding, metav1.CreateOptions{})
		if lastError == nil {
			// success on the creating
			return true, nil
		}
		if !errors.IsAlreadyExists(lastError) {
			return false, nil
		}

		// try to auto update existing object
		rb, err := client.RbacV1().RoleBindings(rolebinding.Namespace).Get(ctx, rolebinding.Name, metav1.GetOptions{})
		if err != nil {
			lastError = err
			return false, nil
		}
		if autoUpdate, ok := rb.Annotations[known.AutoUpdateAnnotation]; ok && autoUpdate == "true" {
			_, lastError = client.RbacV1().RoleBindings(rolebinding.Namespace).Update(ctx, &rolebinding, metav1.UpdateOptions{})
			if lastError != nil {
				return false, nil
			}
		}
		// success on the updating
		return true, nil
	})
	if err == nil {
		return nil
	}
	return fmt.Errorf("failed to ensure RoleBinding %s: %v", klog.KObj(&rolebinding).String(), lastError)
}
