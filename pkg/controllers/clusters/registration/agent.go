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
	"os"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2"

	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	clusternetClientSet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	"github.com/clusternet/clusternet/pkg/utils"
)

// Agent defines configuration for clusternet-agent
type Agent struct {
	AgentContext context.Context

	// Identity is the unique string identifying a lease holder across
	// all participants in an election.
	Identity string

	// ClusterID denotes current child cluster id
	ClusterID *types.UID

	// Options for cluster registration
	Options *ClusterRegistrationOptions

	// kubeconfig for child cluster
	childKubeConfig *rest.Config

	// parent cluster kubeconfig, only for bootstrapping usage, i.e., limited access to parent cluster
	parentBootstrapKubeConfig *rest.Config
}

// NewAgent returns a new Agent.
func NewAgent(ctx context.Context, childKubeConfigFile string, regOpts *ClusterRegistrationOptions) (*Agent, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("unable to get hostname: %v", err)
	}

	// add a uniquifier so that two processes on the same host don't accidentally both become active
	identity := hostname + "_" + string(uuid.NewUUID())
	klog.V(4).Infof("current identity lock id %q", identity)

	childKubeConfig, err := utils.GetKubeConfig(childKubeConfigFile)
	if err != nil {
		return nil, err
	}

	agent := &Agent{
		AgentContext:    ctx,
		Identity:        identity,
		childKubeConfig: childKubeConfig,
		Options:         regOpts,
	}
	return agent, nil
}

func (agent *Agent) Run() {
	klog.Info("starting agent controller ...")

	// create clientset for child cluster
	clientSet := kubernetes.NewForConfigOrDie(agent.childKubeConfig)

	// start the leader election code loop
	leaderelection.RunOrDie(agent.AgentContext, *newLeaderElectionConfigWithDefaultValue(agent.Identity, clientSet,
		leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				// we're notified when we start - this is where you would
				// usually put your code
				agent.registerSelfCluster(ctx, clientSet)
			},
			OnStoppedLeading: func() {
				klog.Error("leader election got lost")
			},
			OnNewLeader: func(identity string) {
				// we're notified when new leader elected
				if identity == agent.Identity {
					// I just got the lock
					return
				}
				klog.Infof("new leader elected: %s", identity)
			},
		},
	))
}

// registerSelfCluster begins registering. It starts registering and blocked until the context is done.
func (agent *Agent) registerSelfCluster(ctx context.Context, childClientSet kubernetes.Interface) {
	// complete your controller loop here
	klog.Info("start registering current cluster as a child cluster...")

	registerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	wait.JitterUntil(func() {
		// get cluster unique id
		if agent.ClusterID == nil {
			klog.Infof("retrieving cluster id")
			clusterID, err := agent.getClusterID(registerCtx, childClientSet)
			if err != nil {
				return
			}
			klog.Infof("current cluster id is %q", clusterID)
			agent.ClusterID = &clusterID
		}

		// get parent cluster kubeconfig
		_, err := childClientSet.CoreV1().Secrets(SelfClusterLeaseNamespace).Get(registerCtx,
			ParentClusterSecretName, metav1.GetOptions{})
		if err != nil {
			if !apierrors.IsNotFound(err) {
				klog.Errorf("failed to get secret: %v", err)
				return
			}
		}
		// todo: secret exists?

		// bootstrap cluster registration
		if err := bootstrapClusterRegistration(registerCtx, agent.Options, *agent.ClusterID); err != nil {
			klog.Error(err)
			return
		}

		// Cancel the context on success
		cancel()
	}, DefaultRetryPeriod, 0.3, true, registerCtx.Done())
}

func (agent *Agent) getClusterID(ctx context.Context, childClientSet kubernetes.Interface) (types.UID, error) {
	lease, err := childClientSet.CoordinationV1().Leases(SelfClusterLeaseNamespace).Get(ctx, SelfClusterLeaseName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("unable to retrieve %s/%s Lease object: %v", SelfClusterLeaseNamespace, SelfClusterLeaseName, err)
		return "", err
	}
	return lease.UID, nil
}

func bootstrapClusterRegistration(ctx context.Context, regOpts *ClusterRegistrationOptions, clusterID types.UID) error {
	klog.Infof("try to bootstrap cluster registration")

	// todo: move to option.Validate() ?
	if len(regOpts.ParentURL) == 0 {
		klog.Exitf("please specify a parent cluster url by flag --%s", ClusterRegistrationURL)
	}
	if len(regOpts.BootstrapToken) == 0 {
		klog.Exitf("please specify a token for parent cluster accessing by flag --%s", ClusterRegistrationToken)
	}

	// get bootstrap kubeconfig
	bootstrapKubeConfig := utils.CreateKubeConfigWithToken(regOpts.ParentURL, regOpts.BootstrapToken, regOpts.UnsafeParentCA)
	clientConfig, err := clientcmd.NewDefaultClientConfig(*bootstrapKubeConfig, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return fmt.Errorf("error while creating kubeconfig: %v", err)
	}

	// create ClusterRegistrationRequest
	client := clusternetClientSet.NewForConfigOrDie(clientConfig)
	crr, err := client.ClustersV1beta1().ClusterRegistrationRequests().Create(ctx,
		newClusterRegistrationRequest(clusterID, regOpts.ClusterType, generateClusterName(regOpts.ClusterName, regOpts.ClusterNamePrefix)),
		metav1.CreateOptions{})

	if err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create ClusterRegistrationRequest: %v", err)
		}
		klog.Infof("a ClusterRegistrationRequest has already been created for cluster %q", clusterID)
		// todo: update spec?
	} else {
		klog.Infof("successfully create ClusterRegistrationRequest %q", klog.KObj(crr))
	}

	crrName := generateClusterRegistrationRequestName(clusterID)
	waitingCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// wait until stopCh is closed or request is approved
	wait.JitterUntil(func() {
		crr, err = client.ClustersV1beta1().ClusterRegistrationRequests().Get(ctx, crrName, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("failed to get ClusterRegistrationRequest %s: %v", crrName, err)
			return
		}
		if clusterName, ok := crr.Labels[ClusterNameLabel]; ok {
			regOpts.ClusterName = clusterName
			klog.Infof("found existing cluster name %q, reuse it", clusterName)
		}

		if crr.Status.Result == clusterapi.RequestApproved {
			klog.Infof("the registration request for cluster %s gets approved", clusterID)
			cancel()
			return
		}

		klog.V(4).Infof("the registration request for cluster %q (%q) is still waiting for approval...", clusterID, regOpts.ClusterName)
		return

	}, DefaultRetryPeriod, 0.4, true, waitingCtx.Done())

	return nil
}

func newLeaderElectionConfigWithDefaultValue(identity string, clientset kubernetes.Interface, callbacks leaderelection.LeaderCallbacks) *leaderelection.LeaderElectionConfig {
	return &leaderelection.LeaderElectionConfig{
		Lock: &resourcelock.LeaseLock{
			LeaseMeta: metav1.ObjectMeta{
				Name:      SelfClusterLeaseName,
				Namespace: SelfClusterLeaseNamespace,
			},
			Client: clientset.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{
				Identity: identity,
			},
		},
		// IMPORTANT: you MUST ensure that any code you have that
		// is protected by the lease must terminate **before**
		// you call cancel. Otherwise, you could have a background
		// loop still running and another process could
		// get elected before your background loop finished, violating
		// the stated goal of the lease.
		ReleaseOnCancel: true,
		LeaseDuration:   DefaultLeaseDuration,
		RenewDeadline:   DefaultRenewDeadline,
		RetryPeriod:     DefaultRetryPeriod,
		Callbacks:       callbacks,
	}
}

func newClusterRegistrationRequest(clusterID types.UID, clusterType, clusterName string) *clusterapi.ClusterRegistrationRequest {
	return &clusterapi.ClusterRegistrationRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: generateClusterRegistrationRequestName(clusterID),
			Labels: map[string]string{
				ClusterRegisteredByLabel: ClusternetAgentName,
				ClusterIDLabel:           string(clusterID),
				ClusterNameLabel:         clusterName,
			},
		},
		Spec: clusterapi.ClusterRegistrationRequestSpec{
			ClusterID:   clusterID,
			ClusterType: clusterapi.EdgeClusterType(clusterType),
			ClusterName: clusterName,
		},
	}
}

func generateClusterRegistrationRequestName(clusterID types.UID) string {
	return fmt.Sprintf("%s-%s", CRRObjectNamePrefix, string(clusterID))
}

func generateClusterName(clusterName, clusterNamePrefix string) string {
	if len(clusterName) == 0 {
		clusterName = fmt.Sprintf("%s-%s", clusterNamePrefix, utilrand.String(DefaultRandomUIDLength))
		klog.V(4).Infof("generate a random string %q as cluster name for later use", clusterName)
	}
	return clusterName
}
