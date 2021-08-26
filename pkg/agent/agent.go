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

package agent

import (
	"context"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2"

	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/controllers/proxies/sockets"
	"github.com/clusternet/clusternet/pkg/features"
	clusternetClientSet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	"github.com/clusternet/clusternet/pkg/known"
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

	// clientset for child cluster
	childKubeClientSet kubernetes.Interface

	// dedicated kubeconfig for accessing parent cluster, which is auto populated by the parent cluster
	// when cluster registration request gets approved
	parentDedicatedKubeConfig *rest.Config
	// secret that stores credentials from parent cluster
	secretFromParentCluster *corev1.Secret

	// report cluster status
	statusManager *Manager

	deployer *Deployer
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

	childKubeConfig, err := utils.LoadsKubeConfig(childKubeConfigFile, 1)
	if err != nil {
		return nil, err
	}
	// create clientset for child cluster
	childKubeClientSet := kubernetes.NewForConfigOrDie(childKubeConfig)

	agent := &Agent{
		AgentContext:       ctx,
		Identity:           identity,
		childKubeClientSet: childKubeClientSet,
		Options:            regOpts,
		statusManager:      NewStatusManager(ctx, childKubeConfig.Host, regOpts.ParentURL, childKubeClientSet, regOpts.ClusterStatusCollectFrequency, regOpts.ClusterStatusReportFrequency),
		deployer:           NewDeployer(regOpts.ClusterSyncMode, childKubeConfig.Host, childKubeClientSet),
	}
	return agent, nil
}

func (agent *Agent) Run() {
	klog.Info("starting agent controller ...")

	// start the leader election code loop
	leaderelection.RunOrDie(agent.AgentContext, *newLeaderElectionConfigWithDefaultValue(agent.Identity, agent.childKubeClientSet,
		leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				// we're notified when we start - this is where you would
				// usually put your code
				agent.registerSelfCluster(ctx)

				// setup websocket connection
				if utilfeature.DefaultFeatureGate.Enabled(features.SocketConnection) {
					klog.Infof("featuregate %s is enabled, preparing setting up socket connection...", features.SocketConnection)
					socketConn, err := sockets.NewController(agent.parentDedicatedKubeConfig, agent.Options.TunnelLogging)
					if err != nil {
						klog.Exitf("failed to setup websocket connection: %v", err)

					}
					go socketConn.Run(ctx, agent.ClusterID)
				}

				go agent.statusManager.Run(ctx, agent.parentDedicatedKubeConfig, agent.secretFromParentCluster)
				go agent.deployer.Run(ctx, agent.parentDedicatedKubeConfig, agent.secretFromParentCluster, agent.ClusterID)
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
func (agent *Agent) registerSelfCluster(ctx context.Context) {
	// complete your controller loop here
	klog.Info("start registering current cluster as a child cluster...")

	tryToUseSecret := true

	registerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	wait.JitterUntil(func() {
		// get cluster unique id
		if agent.ClusterID == nil {
			klog.Infof("retrieving cluster id")
			clusterID, err := agent.getClusterID(registerCtx, agent.childKubeClientSet)
			if err != nil {
				return
			}
			klog.Infof("current cluster id is %q", clusterID)
			agent.ClusterID = &clusterID
		}

		// get parent cluster kubeconfig
		if tryToUseSecret {
			secret, err := agent.childKubeClientSet.CoreV1().Secrets(ClusternetSystemNamespace).Get(registerCtx,
				ParentClusterSecretName, metav1.GetOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				klog.Errorf("failed to get secretFromParentCluster: %v", err)
				return
			}
			if err == nil {
				agent.secretFromParentCluster = secret
				klog.Infof("found existing secretFromParentCluster '%s/%s' that can be used to access parent cluster",
					ClusternetSystemNamespace, ParentClusterSecretName)

				if string(secret.Data[known.ClusterAPIServerURLKey]) != agent.Options.ParentURL {
					klog.Warningf("the parent url got changed from %q to %q", secret.Data[known.ClusterAPIServerURLKey], agent.Options.ParentURL)
					klog.Warningf("will try to re-register current cluster")
				} else {
					parentDedicatedKubeConfig, err := utils.GenerateKubeConfigFromToken(agent.Options.ParentURL,
						string(secret.Data[corev1.ServiceAccountTokenKey]), secret.Data[corev1.ServiceAccountRootCAKey], 2)
					if err == nil {
						agent.parentDedicatedKubeConfig = parentDedicatedKubeConfig
					}
				}
			}
		}

		// bootstrap cluster registration
		if err := agent.bootstrapClusterRegistrationIfNeeded(registerCtx); err != nil {
			klog.Error(err)
			klog.Warning("something went wrong when using existing parent cluster credentials, switch to use bootstrap token instead")
			tryToUseSecret = false
			agent.parentDedicatedKubeConfig = nil
			return
		}

		// Cancel the context on success
		cancel()
	}, DefaultRetryPeriod, 0.3, true, registerCtx.Done())
}

func (agent *Agent) getClusterID(ctx context.Context, childClientSet kubernetes.Interface) (types.UID, error) {
	lease, err := childClientSet.CoordinationV1().Leases(ClusternetSystemNamespace).Get(ctx, SelfClusterLeaseName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("unable to retrieve %s/%s Lease object: %v", ClusternetSystemNamespace, SelfClusterLeaseName, err)
		return "", err
	}
	return lease.UID, nil
}

func (agent *Agent) bootstrapClusterRegistrationIfNeeded(ctx context.Context) error {
	klog.Infof("try to bootstrap cluster registration if needed")

	clientConfig, err := agent.getBootstrapKubeConfigForParentCluster()
	if err != nil {
		return err
	}
	// create ClusterRegistrationRequest
	client := clusternetClientSet.NewForConfigOrDie(clientConfig)
	crr, err := client.ClustersV1beta1().ClusterRegistrationRequests().Create(ctx,
		newClusterRegistrationRequest(*agent.ClusterID, agent.Options.ClusterType,
			generateClusterName(agent.Options.ClusterName, agent.Options.ClusterNamePrefix),
			agent.Options.ClusterSyncMode),
		metav1.CreateOptions{})

	if err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create ClusterRegistrationRequest: %v", err)
		}
		klog.Infof("a ClusterRegistrationRequest has already been created for cluster %q", *agent.ClusterID)
		// todo: update spec?
	} else {
		klog.Infof("successfully create ClusterRegistrationRequest %q", klog.KObj(crr))
	}

	// wait until stopCh is closed or request is approved
	err = agent.waitingForApproval(ctx, client)

	return err
}

func (agent *Agent) getBootstrapKubeConfigForParentCluster() (*rest.Config, error) {
	if agent.parentDedicatedKubeConfig != nil {
		return agent.parentDedicatedKubeConfig, nil
	}

	// todo: move to option.Validate() ?
	if len(agent.Options.ParentURL) == 0 {
		klog.Exitf("please specify a parent cluster url by flag --%s", ClusterRegistrationURL)
	}
	if len(agent.Options.BootstrapToken) == 0 {
		klog.Exitf("please specify a token for parent cluster accessing by flag --%s", ClusterRegistrationToken)
	}

	// get bootstrap kubeconfig from token
	clientConfig, err := utils.GenerateKubeConfigFromToken(agent.Options.ParentURL, agent.Options.BootstrapToken, nil, 1)
	if err != nil {
		return nil, fmt.Errorf("error while creating kubeconfig: %v", err)
	}

	return clientConfig, nil
}

func (agent *Agent) waitingForApproval(ctx context.Context, client clusternetClientSet.Interface) error {
	var crr *clusterapi.ClusterRegistrationRequest
	var err error

	// wait until stopCh is closed or request is approved
	waitingCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	wait.JitterUntil(func() {
		crrName := generateClusterRegistrationRequestName(*agent.ClusterID)
		crr, err = client.ClustersV1beta1().ClusterRegistrationRequests().Get(waitingCtx, crrName, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("failed to get ClusterRegistrationRequest %s: %v", crrName, err)
			return
		}
		if clusterName, ok := crr.Labels[known.ClusterNameLabel]; ok {
			agent.Options.ClusterName = clusterName
			klog.V(5).Infof("found existing cluster name %q, reuse it", clusterName)
		}

		if crr.Status.Result != nil && *crr.Status.Result == clusterapi.RequestApproved {
			klog.Infof("the registration request for cluster %q gets approved", *agent.ClusterID)
			// cancel on success
			cancel()
			return
		}

		klog.V(4).Infof("the registration request for cluster %q (%q) is still waiting for approval...",
			*agent.ClusterID, agent.Options.ClusterName)
		return

	}, DefaultRetryPeriod, 0.4, true, waitingCtx.Done())

	parentDedicatedKubeConfig, err := utils.GenerateKubeConfigFromToken(agent.Options.ParentURL,
		string(crr.Status.DedicatedToken), crr.Status.CACertificate, 2)
	if err != nil {
		return err
	}
	agent.parentDedicatedKubeConfig = parentDedicatedKubeConfig

	// once the request gets approved
	// store auto-populated credentials to Secret "parent-cluster" in "clusternet-system" namespace
	agent.storeParentClusterCredentials(agent.AgentContext, crr)

	return nil
}

func (agent *Agent) storeParentClusterCredentials(ctx context.Context, crr *clusterapi.ClusterRegistrationRequest) {
	klog.V(4).Infof("store parent cluster credentials to secret for later use")
	secretCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: ParentClusterSecretName,
			Labels: map[string]string{
				known.ClusterBootstrappingLabel: known.CredentialsAuto,
				known.ClusterIDLabel:            string(*agent.ClusterID),
				known.ClusterNameLabel:          agent.Options.ClusterName,
			},
		},
		Data: map[string][]byte{
			corev1.ServiceAccountRootCAKey:    crr.Status.CACertificate,
			corev1.ServiceAccountTokenKey:     crr.Status.DedicatedToken,
			corev1.ServiceAccountNamespaceKey: []byte(crr.Status.DedicatedNamespace),
			known.ClusterAPIServerURLKey:      []byte(agent.Options.ParentURL),
		},
	}
	agent.secretFromParentCluster = secret
	wait.JitterUntil(func() {
		_, err := agent.childKubeClientSet.CoreV1().Secrets(ClusternetSystemNamespace).Create(secretCtx, secret, metav1.CreateOptions{})
		if err == nil {
			klog.V(5).Infof("successfully store parent cluster credentials")
			cancel()
			return
		} else {
			if apierrors.IsAlreadyExists(err) {
				klog.V(5).Infof("found existed parent cluster credentials, will try to update if needed")
				_, err = agent.childKubeClientSet.CoreV1().Secrets(ClusternetSystemNamespace).Update(secretCtx, secret, metav1.UpdateOptions{})
				if err == nil {
					cancel()
					return
				}
			}
			klog.ErrorDepth(5, fmt.Sprintf("failed to store parent cluster credentials: %v", err))
		}
	}, DefaultRetryPeriod, 0.4, true, secretCtx.Done())
}

func newLeaderElectionConfigWithDefaultValue(identity string, clientset kubernetes.Interface, callbacks leaderelection.LeaderCallbacks) *leaderelection.LeaderElectionConfig {
	return &leaderelection.LeaderElectionConfig{
		Lock: &resourcelock.LeaseLock{
			LeaseMeta: metav1.ObjectMeta{
				Name:      SelfClusterLeaseName,
				Namespace: ClusternetSystemNamespace,
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

func newClusterRegistrationRequest(clusterID types.UID, clusterType, clusterName, clusterSyncMode string) *clusterapi.ClusterRegistrationRequest {
	return &clusterapi.ClusterRegistrationRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: generateClusterRegistrationRequestName(clusterID),
			Labels: map[string]string{
				known.ClusterRegisteredByLabel: known.ClusternetAgentName,
				known.ClusterIDLabel:           string(clusterID),
				known.ClusterNameLabel:         clusterName,
			},
		},
		Spec: clusterapi.ClusterRegistrationRequestSpec{
			ClusterID:   clusterID,
			ClusterType: clusterapi.ClusterType(clusterType),
			ClusterName: clusterName,
			SyncMode:    clusterapi.ClusterSyncMode(clusterSyncMode),
		},
	}
}

func generateClusterRegistrationRequestName(clusterID types.UID) string {
	return fmt.Sprintf("%s%s", known.NamePrefixForClusternetObjects, string(clusterID))
}

func generateClusterName(clusterName, clusterNamePrefix string) string {
	if len(clusterName) == 0 {
		clusterName = fmt.Sprintf("%s-%s", clusterNamePrefix, utilrand.String(DefaultRandomUIDLength))
		klog.V(4).Infof("generate a random string %q as cluster name for later use", clusterName)
	}
	return clusterName
}
