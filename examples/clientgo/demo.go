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

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"github.com/clusternet/clusternet/pkg/wrappers/clientgo"
)

func main() {
	config, err := rest.InClusterConfig()
	if err != nil {
		klog.Error(err)
		os.Exit(1)
	}

	// This is the ONLY place you need to wrap for Clusternet
	config.Wrap(func(rt http.RoundTripper) http.RoundTripper {
		return clientgo.NewClusternetTransport(config.Host, rt)
	})

	// now we could create and visit all the resources
	client := kubernetes.NewForConfigOrDie(config)
	_, err = client.CoreV1().Namespaces().Create(context.TODO(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "demo",
		},
	}, metav1.CreateOptions{})
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			fmt.Fprintf(os.Stderr, "error create Namespace: %v", err)
			os.Exit(1)
		}
	}
	nss, err := client.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error listing Namespaces: %v", err)
		os.Exit(1)
	}

	for _, ns := range nss.Items {
		fmt.Printf("Namespace: %s\n", ns.Name)
	}

	sas, err := client.CoreV1().ServiceAccounts("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error listing ServiceAccounts: %v", err)
		os.Exit(1)
	}

	for _, sa := range sas.Items {
		fmt.Printf("ServiceAccount: %s\n", sa.Name)
	}

	deploys, err := client.AppsV1().Deployments("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error listing Deployments: %v", err)
		os.Exit(1)
	}

	for _, deploy := range deploys.Items {
		fmt.Printf("deploy: %s\n", deploy.Name)
	}
}
