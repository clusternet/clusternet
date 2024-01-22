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
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/wait"
	apiserver "k8s.io/apiserver/pkg/server"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	controllermanagerapp "k8s.io/controller-manager/app"
	"k8s.io/controller-manager/pkg/clientbuilder"
	"k8s.io/klog/v2"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
	utilpointer "k8s.io/utils/pointer"
	mcsclientset "sigs.k8s.io/mcs-api/pkg/client/clientset/versioned"
	mcsInformers "sigs.k8s.io/mcs-api/pkg/client/informers/externalversions"

	"github.com/clusternet/clusternet/pkg/agent/deployer"
	"github.com/clusternet/clusternet/pkg/agent/options"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/controllers/mcs"
	"github.com/clusternet/clusternet/pkg/controllers/proxies/sockets"
	"github.com/clusternet/clusternet/pkg/features"
	clusternetclientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/predictor"
	"github.com/clusternet/clusternet/pkg/utils"
)

const (
	// default number of threads
	defaultThreadiness = 2

	legacyClusterIDAnnotation = "legacy-cluster-id"

	httpPrefix  = "http://"
	httpsPrefix = "https://"
)

// Agent defines configuration for clusternet-agent
type Agent struct {
	SecureServing *apiserver.SecureServingInfo

	// Identity is the unique string identifying a lease holder across
	// all participants in an election.
	Identity string

	// ClusterID denotes current child cluster id
	ClusterID *types.UID

	agentOptions *options.AgentOptions

	// clientset for child cluster
	childKubeClientSet kubernetes.Interface

	// clientset for child cluster election
	childElectionClientSet kubernetes.Interface

	// kube informer factory for child cluster
	kubeInformerFactory kubeinformers.SharedInformerFactory

	// dedicated kubeconfig for accessing parent cluster, which is auto populated by the parent cluster
	// when cluster registration request gets approved
	parentDedicatedKubeConfig *rest.Config
	// dedicated namespace in parent cluster for current child cluster
	DedicatedNamespace *string

	// report cluster status
	statusManager *Manager

	deployer *deployer.Deployer

	predictor *predictor.Server

	serviceExportEnabled    bool
	serviceExportController *mcs.ServiceExportController
}

// NewAgent returns a new Agent.
func NewAgent(opts *options.AgentOptions) (*Agent, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("unable to get hostname: %v", err)
	}

	// add a uniquifier so that two processes on the same host don't accidentally both become active
	identity := hostname + "_" + string(uuid.NewUUID())
	klog.V(4).Infof("current identity lock id %q", identity)

	childKubeConfig, err := utils.LoadsKubeConfig(&opts.ControllerOptions.ClientConnection)
	if err != nil {
		return nil, err
	}

	// create clientset for child cluster
	clientBuilder := clientbuilder.SimpleControllerClientBuilder{
		ClientConfig: childKubeConfig,
	}
	childKubeClientSet := kubernetes.NewForConfigOrDie(clientBuilder.ConfigOrDie("clusternet-agent-kube-client"))
	mcsClientSet := mcsclientset.NewForConfigOrDie(clientBuilder.ConfigOrDie("clusternet-agent-mcs-client"))
	var electionClientSet *kubernetes.Clientset
	if opts.ControllerOptions.LeaderElection.LeaderElect {
		electionClientSet = kubernetes.NewForConfigOrDie(clientBuilder.ConfigOrDie("clusternet-agent-election-client"))
	}

	ver, err := childKubeClientSet.Discovery().ServerVersion()
	if err != nil {
		return nil, err
	}

	// create metrics client for child cluster
	metricClient := metricsv.NewForConfigOrDie(childKubeConfig)

	// creates the informer factory
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(childKubeClientSet, known.DefaultResync)
	mcsInformerFactory := mcsInformers.NewSharedInformerFactory(mcsClientSet, known.DefaultResync)

	var p *predictor.Server
	if opts.ServeInternalPredictor {
		// predictorAddr is in the form "host:port"
		predictorAddr := strings.TrimLeft(opts.PredictorAddress, httpPrefix)
		predictorAddr = strings.TrimLeft(predictorAddr, httpsPrefix)
		p, err = predictor.NewServer(childKubeConfig, childKubeClientSet, kubeInformerFactory, predictorAddr)
		if err != nil {
			return nil, err
		}
	}

	// parse saTokenAutoGenerated by server version
	var saTokenAutoGenerated bool
	if saTokenAutoGenerated, err = utils.SATokenAutoGenerated(ver.String()); err != nil {
		return nil, err
	}

	// parse serviceExportEnabled (MultiClusterServiceEnabled) by server version
	var serviceExportEnabled bool
	if serviceExportEnabled, err = utils.MultiClusterServiceEnabled(ver.String()); err != nil {
		return nil, err
	}

	var serviceExportController *mcs.ServiceExportController
	if serviceExportEnabled {
		serviceExportController, err = mcs.NewServiceExportController(kubeInformerFactory.Discovery().V1().
			EndpointSlices(), mcsClientSet, mcsInformerFactory)
		if err != nil {
			return nil, err
		}
	}

	agent := &Agent{
		Identity:               identity,
		agentOptions:           opts,
		childKubeClientSet:     childKubeClientSet,
		childElectionClientSet: electionClientSet,
		kubeInformerFactory:    kubeInformerFactory,
		statusManager: NewStatusManager(
			childKubeConfig.Host,
			opts,
			childKubeClientSet,
			metricClient,
			kubeInformerFactory,
		),
		deployer: deployer.NewDeployer(
			opts.ClusterRegistrationOptions.ClusterSyncMode,
			childKubeConfig.Host,
			opts.ControllerOptions.LeaderElection.ResourceNamespace,
			saTokenAutoGenerated),
		predictor:               p,
		serviceExportEnabled:    serviceExportEnabled,
		serviceExportController: serviceExportController,
	}
	return agent, nil
}

func (agent *Agent) Run(ctx context.Context) error {
	err := agent.agentOptions.Config()
	if err != nil {
		return err
	}
	if err = agent.agentOptions.SecureServing.ApplyTo(&agent.SecureServing, nil); err != nil {
		return err
	}
	// Start up the metrics and healthz server.
	if agent.SecureServing != nil {
		handler := controllermanagerapp.BuildHandlerChain(
			utils.NewHealthzAndMetricsHandler("clusternet-agent", agent.agentOptions.DebuggingOptions),
			nil,
			nil,
		)
		if _, _, err = agent.SecureServing.Serve(handler, 0, ctx.Done()); err != nil {
			// fail early for secure handlers, removing the old error loop from above
			return fmt.Errorf("failed to start secure server: %v", err)
		}
	}

	klog.Info("starting agent controller ...")

	agent.kubeInformerFactory.Start(ctx.Done())

	// if leader election is disabled, so runCommand inline until done.
	if !agent.agentOptions.ControllerOptions.LeaderElection.LeaderElect {
		agent.run(ctx)
		klog.Warning("finished without leader elect")
		return nil
	}

	// leader election is enabled, runCommand via LeaderElector until done and exit.
	curIdentity, err := utils.GenerateIdentity()
	if err != nil {
		return err
	}
	le, err := leaderelection.NewLeaderElector(*utils.NewLeaderElectionConfigWithDefaultValue(
		curIdentity,
		agent.agentOptions.ControllerOptions.LeaderElection.ResourceName,
		agent.agentOptions.ControllerOptions.LeaderElection.ResourceNamespace,
		agent.agentOptions.ControllerOptions.LeaderElection.LeaseDuration.Duration,
		agent.agentOptions.ControllerOptions.LeaderElection.RenewDeadline.Duration,
		agent.agentOptions.ControllerOptions.LeaderElection.RetryPeriod.Duration,
		agent.childElectionClientSet,
		leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				agent.run(ctx)
			},
			OnStoppedLeading: func() {
				klog.Error("leader election got lost")
			},
			OnNewLeader: func(identity string) {
				// we're notified when new leader elected
				if identity == curIdentity {
					// I just got the lock
					return
				}
				klog.Infof("new leader elected: %s", identity)
			},
		},
	))
	if err != nil {
		return err
	}
	le.Run(ctx)
	return nil
}

func (agent *Agent) run(ctx context.Context) {
	agent.registerSelfCluster(ctx)

	// setup websocket connection
	if utilfeature.DefaultFeatureGate.Enabled(features.SocketConnection) {
		klog.Infof("featuregate %s is enabled, preparing setting up socket connection...", features.SocketConnection)
		socketConn, err := sockets.NewController(agent.parentDedicatedKubeConfig, agent.agentOptions.TunnelLogging)
		if err != nil {
			klog.Exitf("failed to setup websocket connection: %v", err)

		}
		go socketConn.Run(ctx, agent.ClusterID)
	}

	go wait.UntilWithContext(ctx, func(ctx context.Context) {
		agent.statusManager.Run(ctx, agent.parentDedicatedKubeConfig, agent.DedicatedNamespace, agent.ClusterID)
	}, time.Duration(0))

	go wait.UntilWithContext(ctx, func(ctx context.Context) {
		if err := agent.deployer.Run(
			ctx,
			agent.parentDedicatedKubeConfig,
			agent.childKubeClientSet,
			agent.DedicatedNamespace,
			agent.ClusterID,
			defaultThreadiness,
			agent.agentOptions.ClientConnection.QPS,
			agent.agentOptions.ClientConnection.Burst,
		); err != nil {
			klog.Error(err)
		}
	}, time.Duration(0))

	if agent.serviceExportEnabled {
		go wait.UntilWithContext(ctx, func(ctx context.Context) {
			if err := agent.serviceExportController.Run(ctx, agent.parentDedicatedKubeConfig, *agent.DedicatedNamespace); err != nil {
				klog.Error(err)
			}
		}, time.Duration(0))
	}

	if agent.predictor != nil {
		go wait.UntilWithContext(ctx, func(ctx context.Context) {
			agent.predictor.Run(ctx)
		}, time.Duration(0))
	}
	<-ctx.Done()
}

// registerSelfCluster begins registering. It starts registering and blocked until the context is done.
func (agent *Agent) registerSelfCluster(ctx context.Context) {
	// complete your controller loop here
	klog.Info("start registering current cluster as a child cluster...")

	tryToUseSecret := true

	registerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	wait.JitterUntilWithContext(registerCtx, func(ctx context.Context) {
		// get cluster unique id
		if agent.ClusterID == nil {
			klog.Infof("retrieving cluster id")
			clusterID, err := agent.getClusterID(ctx, agent.childKubeClientSet)
			if err != nil {
				return
			}
			klog.Infof("current cluster id is %q", clusterID)
			agent.ClusterID = &clusterID
		}

		// get parent cluster kubeconfig
		if tryToUseSecret {
			secret, err := agent.childKubeClientSet.CoreV1().
				Secrets(agent.agentOptions.ControllerOptions.LeaderElection.ResourceNamespace).
				Get(ctx, options.ParentClusterSecretName, metav1.GetOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				klog.Errorf("failed to get secretFromParentCluster: %v", err)
				return
			}
			if err == nil {
				klog.Infof("found existing secretFromParentCluster '%s/%s' that can be used to access parent cluster",
					agent.agentOptions.ControllerOptions.LeaderElection.ResourceNamespace, options.ParentClusterSecretName)

				if string(secret.Data[known.ClusterAPIServerURLKey]) != agent.agentOptions.ClusterRegistrationOptions.ParentURL {
					klog.Warningf("the parent url got changed from %q to %q",
						secret.Data[known.ClusterAPIServerURLKey],
						agent.agentOptions.ClusterRegistrationOptions.ParentURL)
					klog.Warningf("will try to re-register current cluster")
				} else {
					parentDedicatedKubeConfig, err2 := utils.GenerateKubeConfigFromToken(
						agent.agentOptions.ClusterRegistrationOptions.ParentURL,
						string(secret.Data[corev1.ServiceAccountTokenKey]),
						secret.Data[corev1.ServiceAccountRootCAKey],
					)
					if err2 == nil {
						agent.parentDedicatedKubeConfig = parentDedicatedKubeConfig
					}
				}
			}
		}

		// bootstrap cluster registration
		if err := agent.bootstrapClusterRegistrationIfNeeded(ctx); err != nil {
			klog.Error(err)
			klog.Warning("something went wrong when using existing parent cluster credentials, switch to use bootstrap token instead")
			tryToUseSecret = false
			agent.parentDedicatedKubeConfig = nil
			return
		}

		// Cancel the context on success
		cancel()
	}, known.DefaultRetryPeriod, 0.3, true)
}

func (agent *Agent) getClusterID(ctx context.Context, childClientSet kubernetes.Interface) (types.UID, error) {
	lease, err := childClientSet.CoordinationV1().
		Leases(agent.agentOptions.ControllerOptions.LeaderElection.ResourceNamespace).
		Get(ctx, agent.agentOptions.ControllerOptions.LeaderElection.ResourceName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("unable to retrieve %s/%s Lease object: %v",
			agent.agentOptions.ControllerOptions.LeaderElection.ResourceNamespace,
			agent.agentOptions.ControllerOptions.LeaderElection.ResourceName, err)
		return "", err
	}

	// for cluster registrations using clusternet-agent <= v0.14.0, please first upgrade to v0.15.0/v0.16.0, which
	// migrates the legacy cluster id.
	if legacyID, ok := lease.GetAnnotations()[legacyClusterIDAnnotation]; ok {
		// fallback to use legacy lease id
		return types.UID(legacyID), nil
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
	regOpts := agent.agentOptions.ClusterRegistrationOptions
	client := clusternetclientset.NewForConfigOrDie(clientConfig)
	crr, err := client.ClustersV1beta1().ClusterRegistrationRequests().Create(ctx,
		newClusterRegistrationRequest(*agent.ClusterID,
			regOpts.ClusterType,
			generateClusterName(regOpts.ClusterName, regOpts.ClusterNamePrefix),
			regOpts.ClusterNamespace,
			regOpts.ClusterSyncMode,
			regOpts.ClusterLabels),
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

	// get bootstrap kubeconfig from token
	clientConfig, err := utils.GenerateKubeConfigFromToken(
		agent.agentOptions.ClusterRegistrationOptions.ParentURL,
		agent.agentOptions.ClusterRegistrationOptions.BootstrapToken,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("error while creating kubeconfig: %v", err)
	}

	return clientConfig, nil
}

func (agent *Agent) waitingForApproval(ctx context.Context, client clusternetclientset.Interface) error {
	var crr *clusterapi.ClusterRegistrationRequest
	var err error

	// wait until stopCh is closed or request is approved
	waitingCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	wait.JitterUntilWithContext(waitingCtx, func(ctx context.Context) {
		crrName := generateClusterRegistrationRequestName(*agent.ClusterID)
		crr, err = client.ClustersV1beta1().ClusterRegistrationRequests().Get(ctx, crrName, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("failed to get ClusterRegistrationRequest %s: %v", crrName, err)
			return
		}
		if clusterName, ok := crr.Labels[known.ClusterNameLabel]; ok {
			agent.agentOptions.ClusterRegistrationOptions.ClusterName = clusterName
			klog.V(5).Infof("found existing cluster name %q, reuse it", clusterName)
		}

		if crr.Status.Result != nil && *crr.Status.Result == clusterapi.RequestApproved {
			klog.Infof("the registration request for cluster %q gets approved", *agent.ClusterID)
			// cancel on success
			cancel()
			return
		}

		klog.V(4).Infof("the registration request for cluster %q (%q) is still waiting for approval...",
			*agent.ClusterID, agent.agentOptions.ClusterRegistrationOptions.ClusterName)
	}, known.DefaultRetryPeriod, 0.4, true)

	parentDedicatedKubeConfig, err := utils.GenerateKubeConfigFromToken(
		agent.agentOptions.ClusterRegistrationOptions.ParentURL,
		string(crr.Status.DedicatedToken),
		crr.Status.CACertificate,
	)
	if err != nil {
		return err
	}
	agent.parentDedicatedKubeConfig = parentDedicatedKubeConfig
	agent.DedicatedNamespace = utilpointer.StringPtr(crr.Status.DedicatedNamespace)

	// once the request gets approved
	// store auto-populated credentials to Secret "parent-cluster" in "clusternet-system" namespace
	agent.storeParentClusterCredentials(ctx, crr)

	return nil
}

func (agent *Agent) storeParentClusterCredentials(ctx context.Context, crr *clusterapi.ClusterRegistrationRequest) {
	klog.V(4).Infof("store parent cluster credentials to secret for later use")
	secretCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: options.ParentClusterSecretName,
			Labels: map[string]string{
				known.ClusterBootstrappingLabel: known.CredentialsAuto,
				known.ClusterIDLabel:            string(*agent.ClusterID),
				known.ClusterNameLabel:          agent.agentOptions.ClusterRegistrationOptions.ClusterName,
			},
		},
		Data: map[string][]byte{
			corev1.ServiceAccountRootCAKey:    crr.Status.CACertificate,
			corev1.ServiceAccountTokenKey:     crr.Status.DedicatedToken,
			corev1.ServiceAccountNamespaceKey: []byte(crr.Status.DedicatedNamespace),
			known.ClusterAPIServerURLKey:      []byte(agent.agentOptions.ClusterRegistrationOptions.ParentURL),
		},
	}

	wait.JitterUntilWithContext(secretCtx, func(ctx context.Context) {
		_, err := agent.childKubeClientSet.CoreV1().
			Secrets(agent.agentOptions.ControllerOptions.LeaderElection.ResourceNamespace).
			Create(ctx, secret, metav1.CreateOptions{})
		if err == nil {
			klog.V(5).Infof("successfully store parent cluster credentials")
			cancel()
			return
		}

		if apierrors.IsAlreadyExists(err) {
			klog.V(5).Infof("found existed parent cluster credentials, will try to update if needed")
			_, err = agent.childKubeClientSet.CoreV1().
				Secrets(agent.agentOptions.ControllerOptions.LeaderElection.ResourceNamespace).
				Update(ctx, secret, metav1.UpdateOptions{})
			if err == nil {
				cancel()
				return
			}
		}
		klog.ErrorDepth(5, fmt.Sprintf("failed to store parent cluster credentials: %v", err))
	}, known.DefaultRetryPeriod, 0.4, true)
}

func newClusterRegistrationRequest(clusterID types.UID, clusterType, clusterName, clusterNamespace, clusterSyncMode, clusterLabels string) *clusterapi.ClusterRegistrationRequest {
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
			ClusterID:        clusterID,
			ClusterType:      clusterapi.ClusterType(clusterType),
			ClusterName:      clusterName,
			ClusterNamespace: clusterNamespace,
			SyncMode:         clusterapi.ClusterSyncMode(clusterSyncMode),
			ClusterLabels:    parseClusterLabels(clusterLabels),
		},
	}
}

func parseClusterLabels(clusterLabels string) map[string]string {
	if strings.TrimSpace(clusterLabels) == "" {
		return nil
	}
	clusterLabelsMap := make(map[string]string)
	clusterLabelsArray := strings.Split(clusterLabels, ",")
	for _, labelString := range clusterLabelsArray {
		labelArray := strings.Split(labelString, "=")
		if len(labelArray) != 2 {
			klog.Warningf("invalid cluster label %s", labelString)
			continue
		}
		clusterLabelsMap[labelArray[0]] = labelArray[1]
	}
	return clusterLabelsMap
}

func generateClusterRegistrationRequestName(clusterID types.UID) string {
	return fmt.Sprintf("%s%s", known.NamePrefixForClusternetObjects, string(clusterID))
}

func generateClusterName(clusterName, clusterNamePrefix string) string {
	if len(clusterName) == 0 {
		clusterName = fmt.Sprintf("%s-%s", clusterNamePrefix, utilrand.String(known.DefaultRandomIDLength))
		klog.V(4).Infof("generate a random string %q as cluster name for later use", clusterName)
	}
	return clusterName
}
