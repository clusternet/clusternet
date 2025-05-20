/*
Copyright 2017 The Kubernetes Authors.

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

// This file was copied and modified from k8s.io/kubernetes/pkg/printers/internalversion/printer.go

package internalversion

import (
	"bytes"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apiserverinternalv1alpha1 "k8s.io/api/apiserverinternal/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	appsv1beta1 "k8s.io/api/apps/v1beta1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	autoscalingv2beta1 "k8s.io/api/autoscaling/v2beta1"
	batchv1 "k8s.io/api/batch/v1"
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	certificatesv1 "k8s.io/api/certificates/v1"
	certificatesv1beta1 "k8s.io/api/certificates/v1beta1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1beta1 "k8s.io/api/discovery/v1beta1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	flowcontrolv1 "k8s.io/api/flowcontrol/v1"
	flowcontrolv1beta3 "k8s.io/api/flowcontrol/v1beta3"
	networkingv1 "k8s.io/api/networking/v1"
	nodev1 "k8s.io/api/node/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	rbacv1beta1 "k8s.io/api/rbac/v1beta1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	storagev1 "k8s.io/api/storage/v1"
	storagev1beta1 "k8s.io/api/storage/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/duration"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/util/apihelpers"
	"k8s.io/client-go/util/certificate/csr"
	kubectlstoragehelper "k8s.io/kubectl/pkg/util/storage"

	"github.com/clusternet/clusternet/pkg/hub/registry/shadow/printers"
	"github.com/clusternet/clusternet/pkg/hub/registry/shadow/printers/util"
	pkgutils "github.com/clusternet/clusternet/pkg/utils"
)

const (
	loadBalancerWidth = 16

	// labelNodeRolePrefix is a label prefix for node roles
	// It's copied over to here until it's merged in core: https://github.com/kubernetes/kubernetes/pull/39112
	labelNodeRolePrefix = "node-role.kubernetes.io/"

	// nodeLabelRole specifies the role of a node
	nodeLabelRole = "kubernetes.io/role"
)

// AddHandlers adds print handlers for default Kubernetes types dealing with internal versions.
// TODO: handle errors from Handler
func AddHandlers(h printers.PrintHandler) {
	podColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Ready", Type: "string", Description: "The aggregate readiness state of this pod for accepting traffic."},
		{Name: "Status", Type: "string", Description: "The aggregate status of the containers in this pod."},
		{Name: "Restarts", Type: "string", Description: "The number of times the containers in this pod have been restarted and when the last container in this pod has restarted."},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
		{Name: "IP", Type: "string", Priority: 1, Description: corev1.PodStatus{}.SwaggerDoc()["podIP"]},
		{Name: "Node", Type: "string", Priority: 1, Description: corev1.PodSpec{}.SwaggerDoc()["nodeName"]},
		{Name: "Nominated Node", Type: "string", Priority: 1, Description: corev1.PodStatus{}.SwaggerDoc()["nominatedNodeName"]},
		{Name: "Readiness Gates", Type: "string", Priority: 1, Description: corev1.PodSpec{}.SwaggerDoc()["readinessGates"]},
	}
	h.TableHandler(podColumnDefinitions, printPodList)
	h.TableHandler(podColumnDefinitions, printPod)

	podTemplateColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Containers", Type: "string", Description: "Names of each container in the template."},
		{Name: "Images", Type: "string", Description: "Images referenced by each container in the template."},
		{Name: "Pod Labels", Type: "string", Description: "The labels for the pod template."},
	}
	h.TableHandler(podTemplateColumnDefinitions, printPodTemplate)
	h.TableHandler(podTemplateColumnDefinitions, printPodTemplateList)

	podDisruptionBudgetColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Min Available", Type: "string", Description: "The minimum number of pods that must be available."},
		{Name: "Max Unavailable", Type: "string", Description: "The maximum number of pods that may be unavailable."},
		{Name: "Allowed Disruptions", Type: "integer", Description: "Calculated number of pods that may be disrupted at this time."},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	}
	h.TableHandler(podDisruptionBudgetColumnDefinitions, printPodDisruptionBudget)
	h.TableHandler(podDisruptionBudgetColumnDefinitions, printPodDisruptionBudgetList)

	replicationControllerColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Desired", Type: "integer", Description: corev1.ReplicationControllerSpec{}.SwaggerDoc()["replicas"]},
		{Name: "Current", Type: "integer", Description: corev1.ReplicationControllerStatus{}.SwaggerDoc()["replicas"]},
		{Name: "Ready", Type: "integer", Description: corev1.ReplicationControllerStatus{}.SwaggerDoc()["readyReplicas"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
		{Name: "Containers", Type: "string", Priority: 1, Description: "Names of each container in the template."},
		{Name: "Images", Type: "string", Priority: 1, Description: "Images referenced by each container in the template."},
		{Name: "Selector", Type: "string", Priority: 1, Description: corev1.ReplicationControllerSpec{}.SwaggerDoc()["selector"]},
	}
	h.TableHandler(replicationControllerColumnDefinitions, printReplicationController)
	h.TableHandler(replicationControllerColumnDefinitions, printReplicationControllerList)

	replicaSetColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Desired", Type: "integer", Description: extensionsv1beta1.ReplicaSetSpec{}.SwaggerDoc()["replicas"]},
		{Name: "Current", Type: "integer", Description: extensionsv1beta1.ReplicaSetStatus{}.SwaggerDoc()["replicas"]},
		{Name: "Ready", Type: "integer", Description: extensionsv1beta1.ReplicaSetStatus{}.SwaggerDoc()["readyReplicas"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
		{Name: "Containers", Type: "string", Priority: 1, Description: "Names of each container in the template."},
		{Name: "Images", Type: "string", Priority: 1, Description: "Images referenced by each container in the template."},
		{Name: "Selector", Type: "string", Priority: 1, Description: extensionsv1beta1.ReplicaSetSpec{}.SwaggerDoc()["selector"]},
	}
	h.TableHandler(replicaSetColumnDefinitions, printReplicaSet)
	h.TableHandler(replicaSetColumnDefinitions, printReplicaSetList)

	daemonSetColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Desired", Type: "integer", Description: extensionsv1beta1.DaemonSetStatus{}.SwaggerDoc()["desiredNumberScheduled"]},
		{Name: "Current", Type: "integer", Description: extensionsv1beta1.DaemonSetStatus{}.SwaggerDoc()["currentNumberScheduled"]},
		{Name: "Ready", Type: "integer", Description: extensionsv1beta1.DaemonSetStatus{}.SwaggerDoc()["numberReady"]},
		{Name: "Up-to-date", Type: "integer", Description: extensionsv1beta1.DaemonSetStatus{}.SwaggerDoc()["updatedNumberScheduled"]},
		{Name: "Available", Type: "integer", Description: extensionsv1beta1.DaemonSetStatus{}.SwaggerDoc()["numberAvailable"]},
		{Name: "Node Selector", Type: "string", Description: corev1.PodSpec{}.SwaggerDoc()["nodeSelector"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
		{Name: "Containers", Type: "string", Priority: 1, Description: "Names of each container in the template."},
		{Name: "Images", Type: "string", Priority: 1, Description: "Images referenced by each container in the template."},
		{Name: "Selector", Type: "string", Priority: 1, Description: extensionsv1beta1.DaemonSetSpec{}.SwaggerDoc()["selector"]},
	}
	h.TableHandler(daemonSetColumnDefinitions, printDaemonSet)
	h.TableHandler(daemonSetColumnDefinitions, printDaemonSetList)

	jobColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Completions", Type: "string", Description: batchv1.JobStatus{}.SwaggerDoc()["succeeded"]},
		{Name: "Duration", Type: "string", Description: "Time required to complete the job."},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
		{Name: "Containers", Type: "string", Priority: 1, Description: "Names of each container in the template."},
		{Name: "Images", Type: "string", Priority: 1, Description: "Images referenced by each container in the template."},
		{Name: "Selector", Type: "string", Priority: 1, Description: batchv1.JobSpec{}.SwaggerDoc()["selector"]},
	}
	h.TableHandler(jobColumnDefinitions, printJob)
	h.TableHandler(jobColumnDefinitions, printJobList)

	cronJobColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Schedule", Type: "string", Description: batchv1beta1.CronJobSpec{}.SwaggerDoc()["schedule"]},
		{Name: "Suspend", Type: "boolean", Description: batchv1beta1.CronJobSpec{}.SwaggerDoc()["suspend"]},
		{Name: "Active", Type: "integer", Description: batchv1beta1.CronJobStatus{}.SwaggerDoc()["active"]},
		{Name: "Last Schedule", Type: "string", Description: batchv1beta1.CronJobStatus{}.SwaggerDoc()["lastScheduleTime"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
		{Name: "Containers", Type: "string", Priority: 1, Description: "Names of each container in the template."},
		{Name: "Images", Type: "string", Priority: 1, Description: "Images referenced by each container in the template."},
		{Name: "Selector", Type: "string", Priority: 1, Description: batchv1.JobSpec{}.SwaggerDoc()["selector"]},
	}
	h.TableHandler(cronJobColumnDefinitions, printCronJob)
	h.TableHandler(cronJobColumnDefinitions, printCronJobList)

	serviceColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Type", Type: "string", Description: corev1.ServiceSpec{}.SwaggerDoc()["type"]},
		{Name: "Cluster-IP", Type: "string", Description: corev1.ServiceSpec{}.SwaggerDoc()["clusterIP"]},
		{Name: "External-IP", Type: "string", Description: corev1.ServiceSpec{}.SwaggerDoc()["externalIPs"]},
		{Name: "Port(s)", Type: "string", Description: corev1.ServiceSpec{}.SwaggerDoc()["ports"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
		{Name: "Selector", Type: "string", Priority: 1, Description: corev1.ServiceSpec{}.SwaggerDoc()["selector"]},
	}

	h.TableHandler(serviceColumnDefinitions, printService)
	h.TableHandler(serviceColumnDefinitions, printServiceList)

	ingressColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Class", Type: "string", Description: "The name of the IngressClass resource that should be used for additional configuration"},
		{Name: "Hosts", Type: "string", Description: "Hosts that incoming requests are matched against before the ingress rule"},
		{Name: "Address", Type: "string", Description: "Address is a list containing ingress points for the load-balancer"},
		{Name: "Ports", Type: "string", Description: "Ports of TLS configurations that open"},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	}
	h.TableHandler(ingressColumnDefinitions, printIngress)
	h.TableHandler(ingressColumnDefinitions, printIngressList)

	ingressClassColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Controller", Type: "string", Description: "Controller that is responsible for handling this class"},
		{Name: "Parameters", Type: "string", Description: "A reference to a resource with additional parameters"},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	}
	h.TableHandler(ingressClassColumnDefinitions, printIngressClass)
	h.TableHandler(ingressClassColumnDefinitions, printIngressClassList)

	statefulSetColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Ready", Type: "string", Description: "Number of the pod with ready state"},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
		{Name: "Containers", Type: "string", Priority: 1, Description: "Names of each container in the template."},
		{Name: "Images", Type: "string", Priority: 1, Description: "Images referenced by each container in the template."},
	}
	h.TableHandler(statefulSetColumnDefinitions, printStatefulSet)
	h.TableHandler(statefulSetColumnDefinitions, printStatefulSetList)

	endpointColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Endpoints", Type: "string", Description: corev1.Endpoints{}.SwaggerDoc()["subsets"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	}
	h.TableHandler(endpointColumnDefinitions, printEndpoints)
	h.TableHandler(endpointColumnDefinitions, printEndpointsList)

	nodeColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Status", Type: "string", Description: "The status of the node"},
		{Name: "Roles", Type: "string", Description: "The roles of the node"},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
		{Name: "Version", Type: "string", Description: corev1.NodeSystemInfo{}.SwaggerDoc()["kubeletVersion"]},
		{Name: "Internal-IP", Type: "string", Priority: 1, Description: corev1.NodeStatus{}.SwaggerDoc()["addresses"]},
		{Name: "External-IP", Type: "string", Priority: 1, Description: corev1.NodeStatus{}.SwaggerDoc()["addresses"]},
		{Name: "OS-Image", Type: "string", Priority: 1, Description: corev1.NodeSystemInfo{}.SwaggerDoc()["osImage"]},
		{Name: "Kernel-Version", Type: "string", Priority: 1, Description: corev1.NodeSystemInfo{}.SwaggerDoc()["kernelVersion"]},
		{Name: "Container-Runtime", Type: "string", Priority: 1, Description: corev1.NodeSystemInfo{}.SwaggerDoc()["containerRuntimeVersion"]},
	}

	h.TableHandler(nodeColumnDefinitions, printNode)
	h.TableHandler(nodeColumnDefinitions, printNodeList)

	eventColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Last Seen", Type: "string", Description: corev1.Event{}.SwaggerDoc()["lastTimestamp"]},
		{Name: "Type", Type: "string", Description: corev1.Event{}.SwaggerDoc()["type"]},
		{Name: "Reason", Type: "string", Description: corev1.Event{}.SwaggerDoc()["reason"]},
		{Name: "Object", Type: "string", Description: corev1.Event{}.SwaggerDoc()["involvedObject"]},
		{Name: "Subobject", Type: "string", Priority: 1, Description: corev1.Event{}.InvolvedObject.SwaggerDoc()["fieldPath"]},
		{Name: "Source", Type: "string", Priority: 1, Description: corev1.Event{}.SwaggerDoc()["source"]},
		{Name: "Message", Type: "string", Description: corev1.Event{}.SwaggerDoc()["message"]},
		{Name: "First Seen", Type: "string", Priority: 1, Description: corev1.Event{}.SwaggerDoc()["firstTimestamp"]},
		{Name: "Count", Type: "string", Priority: 1, Description: corev1.Event{}.SwaggerDoc()["count"]},
		{Name: "Name", Type: "string", Priority: 1, Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
	}
	h.TableHandler(eventColumnDefinitions, printEvent)
	h.TableHandler(eventColumnDefinitions, printEventList)

	namespaceColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Status", Type: "string", Description: "The status of the namespace"},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	}
	h.TableHandler(namespaceColumnDefinitions, printNamespace)
	h.TableHandler(namespaceColumnDefinitions, printNamespaceList)

	secretColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Type", Type: "string", Description: corev1.Secret{}.SwaggerDoc()["type"]},
		{Name: "Data", Type: "string", Description: corev1.Secret{}.SwaggerDoc()["data"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	}
	h.TableHandler(secretColumnDefinitions, printSecret)
	h.TableHandler(secretColumnDefinitions, printSecretList)

	serviceAccountColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Secrets", Type: "string", Description: corev1.ServiceAccount{}.SwaggerDoc()["secrets"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	}
	h.TableHandler(serviceAccountColumnDefinitions, printServiceAccount)
	h.TableHandler(serviceAccountColumnDefinitions, printServiceAccountList)

	persistentVolumeColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Capacity", Type: "string", Description: corev1.PersistentVolumeSpec{}.SwaggerDoc()["capacity"]},
		{Name: "Access Modes", Type: "string", Description: corev1.PersistentVolumeSpec{}.SwaggerDoc()["accessModes"]},
		{Name: "Reclaim Policy", Type: "string", Description: corev1.PersistentVolumeSpec{}.SwaggerDoc()["persistentVolumeReclaimPolicy"]},
		{Name: "Status", Type: "string", Description: corev1.PersistentVolumeStatus{}.SwaggerDoc()["phase"]},
		{Name: "Claim", Type: "string", Description: corev1.PersistentVolumeSpec{}.SwaggerDoc()["claimRef"]},
		{Name: "StorageClass", Type: "string", Description: "StorageClass of the pv"},
		{Name: "Reason", Type: "string", Description: corev1.PersistentVolumeStatus{}.SwaggerDoc()["reason"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
		{Name: "VolumeMode", Type: "string", Priority: 1, Description: corev1.PersistentVolumeSpec{}.SwaggerDoc()["volumeMode"]},
	}
	h.TableHandler(persistentVolumeColumnDefinitions, printPersistentVolume)
	h.TableHandler(persistentVolumeColumnDefinitions, printPersistentVolumeList)

	persistentVolumeClaimColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Status", Type: "string", Description: corev1.PersistentVolumeClaimStatus{}.SwaggerDoc()["phase"]},
		{Name: "Volume", Type: "string", Description: corev1.PersistentVolumeClaimSpec{}.SwaggerDoc()["volumeName"]},
		{Name: "Capacity", Type: "string", Description: corev1.PersistentVolumeClaimStatus{}.SwaggerDoc()["capacity"]},
		{Name: "Access Modes", Type: "string", Description: corev1.PersistentVolumeClaimStatus{}.SwaggerDoc()["accessModes"]},
		{Name: "StorageClass", Type: "string", Description: "StorageClass of the pvc"},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
		{Name: "VolumeMode", Type: "string", Priority: 1, Description: corev1.PersistentVolumeClaimSpec{}.SwaggerDoc()["volumeMode"]},
	}
	h.TableHandler(persistentVolumeClaimColumnDefinitions, printPersistentVolumeClaim)
	h.TableHandler(persistentVolumeClaimColumnDefinitions, printPersistentVolumeClaimList)

	componentStatusColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Status", Type: "string", Description: "Status of the component conditions"},
		{Name: "Message", Type: "string", Description: "Message of the component conditions"},
		{Name: "Error", Type: "string", Description: "Error of the component conditions"},
	}
	h.TableHandler(componentStatusColumnDefinitions, printComponentStatus)
	h.TableHandler(componentStatusColumnDefinitions, printComponentStatusList)

	deploymentColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Ready", Type: "string", Description: "Number of the pod with ready state"},
		{Name: "Up-to-date", Type: "string", Description: extensionsv1beta1.DeploymentStatus{}.SwaggerDoc()["updatedReplicas"]},
		{Name: "Available", Type: "string", Description: extensionsv1beta1.DeploymentStatus{}.SwaggerDoc()["availableReplicas"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
		{Name: "Containers", Type: "string", Priority: 1, Description: "Names of each container in the template."},
		{Name: "Images", Type: "string", Priority: 1, Description: "Images referenced by each container in the template."},
		{Name: "Selector", Type: "string", Priority: 1, Description: extensionsv1beta1.DeploymentSpec{}.SwaggerDoc()["selector"]},
	}
	h.TableHandler(deploymentColumnDefinitions, printDeployment)
	h.TableHandler(deploymentColumnDefinitions, printDeploymentList)

	horizontalPodAutoscalerColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Reference", Type: "string", Description: autoscalingv2beta1.HorizontalPodAutoscalerSpec{}.SwaggerDoc()["scaleTargetRef"]},
		{Name: "Targets", Type: "string", Description: autoscalingv2beta1.HorizontalPodAutoscalerSpec{}.SwaggerDoc()["metrics"]},
		{Name: "MinPods", Type: "string", Description: autoscalingv2beta1.HorizontalPodAutoscalerSpec{}.SwaggerDoc()["minReplicas"]},
		{Name: "MaxPods", Type: "string", Description: autoscalingv2beta1.HorizontalPodAutoscalerSpec{}.SwaggerDoc()["maxReplicas"]},
		{Name: "Replicas", Type: "string", Description: autoscalingv2beta1.HorizontalPodAutoscalerStatus{}.SwaggerDoc()["currentReplicas"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	}
	h.TableHandler(horizontalPodAutoscalerColumnDefinitions, printHorizontalPodAutoscaler)
	h.TableHandler(horizontalPodAutoscalerColumnDefinitions, printHorizontalPodAutoscalerList)

	configMapColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Data", Type: "string", Description: corev1.ConfigMap{}.SwaggerDoc()["data"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	}
	h.TableHandler(configMapColumnDefinitions, printConfigMap)
	h.TableHandler(configMapColumnDefinitions, printConfigMapList)

	networkPolicyColumnDefinitioins := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Pod-Selector", Type: "string", Description: extensionsv1beta1.NetworkPolicySpec{}.SwaggerDoc()["podSelector"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	}
	h.TableHandler(networkPolicyColumnDefinitioins, printNetworkPolicy)
	h.TableHandler(networkPolicyColumnDefinitioins, printNetworkPolicyList)

	roleBindingsColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Role", Type: "string", Description: rbacv1beta1.RoleBinding{}.SwaggerDoc()["roleRef"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
		{Name: "Users", Type: "string", Priority: 1, Description: "Users in the roleBinding"},
		{Name: "Groups", Type: "string", Priority: 1, Description: "Groups in the roleBinding"},
		{Name: "ServiceAccounts", Type: "string", Priority: 1, Description: "ServiceAccounts in the roleBinding"},
	}
	h.TableHandler(roleBindingsColumnDefinitions, printRoleBinding)
	h.TableHandler(roleBindingsColumnDefinitions, printRoleBindingList)

	clusterRoleBindingsColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Role", Type: "string", Description: rbacv1beta1.ClusterRoleBinding{}.SwaggerDoc()["roleRef"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
		{Name: "Users", Type: "string", Priority: 1, Description: "Users in the clusterRoleBinding"},
		{Name: "Groups", Type: "string", Priority: 1, Description: "Groups in the clusterRoleBinding"},
		{Name: "ServiceAccounts", Type: "string", Priority: 1, Description: "ServiceAccounts in the clusterRoleBinding"},
	}
	h.TableHandler(clusterRoleBindingsColumnDefinitions, printClusterRoleBinding)
	h.TableHandler(clusterRoleBindingsColumnDefinitions, printClusterRoleBindingList)

	certificateSigningRequestColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
		{Name: "SignerName", Type: "string", Description: certificatesv1beta1.CertificateSigningRequestSpec{}.SwaggerDoc()["signerName"]},
		{Name: "Requestor", Type: "string", Description: certificatesv1beta1.CertificateSigningRequestSpec{}.SwaggerDoc()["request"]},
		{Name: "RequestedDuration", Type: "string", Description: certificatesv1beta1.CertificateSigningRequestSpec{}.SwaggerDoc()["expirationSeconds"]},
		{Name: "Condition", Type: "string", Description: certificatesv1beta1.CertificateSigningRequestStatus{}.SwaggerDoc()["conditions"]},
	}
	h.TableHandler(certificateSigningRequestColumnDefinitions, printCertificateSigningRequest)
	h.TableHandler(certificateSigningRequestColumnDefinitions, printCertificateSigningRequestList)

	leaseColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Holder", Type: "string", Description: coordinationv1.LeaseSpec{}.SwaggerDoc()["holderIdentity"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	}
	h.TableHandler(leaseColumnDefinitions, printLease)
	h.TableHandler(leaseColumnDefinitions, printLeaseList)

	storageClassColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Provisioner", Type: "string", Description: storagev1.StorageClass{}.SwaggerDoc()["provisioner"]},
		{Name: "ReclaimPolicy", Type: "string", Description: storagev1.StorageClass{}.SwaggerDoc()["reclaimPolicy"]},
		{Name: "VolumeBindingMode", Type: "string", Description: storagev1.StorageClass{}.SwaggerDoc()["volumeBindingMode"]},
		{Name: "AllowVolumeExpansion", Type: "string", Description: storagev1.StorageClass{}.SwaggerDoc()["allowVolumeExpansion"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	}

	h.TableHandler(storageClassColumnDefinitions, printStorageClass)
	h.TableHandler(storageClassColumnDefinitions, printStorageClassList)

	statusColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Status", Type: "string", Description: metav1.Status{}.SwaggerDoc()["status"]},
		{Name: "Reason", Type: "string", Description: metav1.Status{}.SwaggerDoc()["reason"]},
		{Name: "Message", Type: "string", Description: metav1.Status{}.SwaggerDoc()["Message"]},
	}

	h.TableHandler(statusColumnDefinitions, printStatus)

	controllerRevisionColumnDefinition := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Controller", Type: "string", Description: "Controller of the object"},
		{Name: "Revision", Type: "string", Description: appsv1beta1.ControllerRevision{}.SwaggerDoc()["revision"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	}
	h.TableHandler(controllerRevisionColumnDefinition, printControllerRevision)
	h.TableHandler(controllerRevisionColumnDefinition, printControllerRevisionList)

	resourceQuotaColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
		{Name: "Request", Type: "string", Description: "Request represents a minimum amount of cpu/memory that a container may consume."},
		{Name: "Limit", Type: "string", Description: "Limits control the maximum amount of cpu/memory that a container may use independent of contention on the node."},
	}
	h.TableHandler(resourceQuotaColumnDefinitions, printResourceQuota)
	h.TableHandler(resourceQuotaColumnDefinitions, printResourceQuotaList)

	priorityClassColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Value", Type: "integer", Description: schedulingv1.PriorityClass{}.SwaggerDoc()["value"]},
		{Name: "Global-Default", Type: "boolean", Description: schedulingv1.PriorityClass{}.SwaggerDoc()["globalDefault"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	}
	h.TableHandler(priorityClassColumnDefinitions, printPriorityClass)
	h.TableHandler(priorityClassColumnDefinitions, printPriorityClassList)

	runtimeClassColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Handler", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["handler"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	}
	h.TableHandler(runtimeClassColumnDefinitions, printRuntimeClass)
	h.TableHandler(runtimeClassColumnDefinitions, printRuntimeClassList)

	volumeAttachmentColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Attacher", Type: "string", Format: "name", Description: storagev1.VolumeAttachmentSpec{}.SwaggerDoc()["attacher"]},
		{Name: "PV", Type: "string", Description: storagev1.VolumeAttachmentSource{}.SwaggerDoc()["persistentVolumeName"]},
		{Name: "Node", Type: "string", Description: storagev1.VolumeAttachmentSpec{}.SwaggerDoc()["nodeName"]},
		{Name: "Attached", Type: "boolean", Description: storagev1.VolumeAttachmentStatus{}.SwaggerDoc()["attached"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	}
	h.TableHandler(volumeAttachmentColumnDefinitions, printVolumeAttachment)
	h.TableHandler(volumeAttachmentColumnDefinitions, printVolumeAttachmentList)

	endpointSliceColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "AddressType", Type: "string", Description: discoveryv1beta1.EndpointSlice{}.SwaggerDoc()["addressType"]},
		{Name: "Ports", Type: "string", Description: discoveryv1beta1.EndpointSlice{}.SwaggerDoc()["ports"]},
		{Name: "Endpoints", Type: "string", Description: discoveryv1beta1.EndpointSlice{}.SwaggerDoc()["endpoints"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	}
	h.TableHandler(endpointSliceColumnDefinitions, printEndpointSlice)
	h.TableHandler(endpointSliceColumnDefinitions, printEndpointSliceList)

	csiNodeColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Drivers", Type: "integer", Description: "Drivers indicates the number of CSI drivers registered on the node"},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	}
	h.TableHandler(csiNodeColumnDefinitions, printCSINode)
	h.TableHandler(csiNodeColumnDefinitions, printCSINodeList)

	csiDriverColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "AttachRequired", Type: "boolean", Description: storagev1.CSIDriverSpec{}.SwaggerDoc()["attachRequired"]},
		{Name: "PodInfoOnMount", Type: "boolean", Description: storagev1.CSIDriverSpec{}.SwaggerDoc()["podInfoOnMount"]},
		{Name: "StorageCapacity", Type: "boolean", Description: storagev1.CSIDriverSpec{}.SwaggerDoc()["storageCapacity"]},
	}
	csiDriverColumnDefinitions = append(csiDriverColumnDefinitions, []metav1.TableColumnDefinition{
		{Name: "TokenRequests", Type: "string", Description: storagev1.CSIDriverSpec{}.SwaggerDoc()["tokenRequests"]},
		{Name: "RequiresRepublish", Type: "boolean", Description: storagev1.CSIDriverSpec{}.SwaggerDoc()["requiresRepublish"]},
	}...)

	csiDriverColumnDefinitions = append(csiDriverColumnDefinitions, []metav1.TableColumnDefinition{
		{Name: "Modes", Type: "string", Description: storagev1.CSIDriverSpec{}.SwaggerDoc()["volumeLifecycleModes"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	}...)
	h.TableHandler(csiDriverColumnDefinitions, printCSIDriver)
	h.TableHandler(csiDriverColumnDefinitions, printCSIDriverList)

	csiStorageCapacityColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "StorageClassName", Type: "string", Description: storagev1beta1.CSIStorageCapacity{}.SwaggerDoc()["storageClassName"]},
		{Name: "Capacity", Type: "string", Description: storagev1beta1.CSIStorageCapacity{}.SwaggerDoc()["capacity"]},
	}
	h.TableHandler(csiStorageCapacityColumnDefinitions, printCSIStorageCapacity)
	h.TableHandler(csiStorageCapacityColumnDefinitions, printCSIStorageCapacityList)

	mutatingWebhookColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Webhooks", Type: "integer", Description: "Webhooks indicates the number of webhooks registered in this configuration"},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	}
	h.TableHandler(mutatingWebhookColumnDefinitions, printMutatingWebhook)
	h.TableHandler(mutatingWebhookColumnDefinitions, printMutatingWebhookList)

	validatingWebhookColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Webhooks", Type: "integer", Description: "Webhooks indicates the number of webhooks registered in this configuration"},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	}
	h.TableHandler(validatingWebhookColumnDefinitions, printValidatingWebhook)
	h.TableHandler(validatingWebhookColumnDefinitions, printValidatingWebhookList)

	flowSchemaColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "PriorityLevel", Type: "string", Description: flowcontrolv1beta3.PriorityLevelConfigurationReference{}.SwaggerDoc()["name"]},
		{Name: "MatchingPrecedence", Type: "string", Description: flowcontrolv1beta3.FlowSchemaSpec{}.SwaggerDoc()["matchingPrecedence"]},
		{Name: "DistinguisherMethod", Type: "string", Description: flowcontrolv1beta3.FlowSchemaSpec{}.SwaggerDoc()["distinguisherMethod"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
		{Name: "MissingPL", Type: "string", Description: "references a broken or non-existent PriorityLevelConfiguration"},
	}
	h.TableHandler(flowSchemaColumnDefinitions, printFlowSchema)
	h.TableHandler(flowSchemaColumnDefinitions, printFlowSchemaList)

	priorityLevelColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Type", Type: "string", Description: flowcontrolv1beta3.PriorityLevelConfigurationSpec{}.SwaggerDoc()["type"]},
		{Name: "AssuredConcurrencyShares", Type: "string", Description: flowcontrolv1beta3.LimitedPriorityLevelConfiguration{}.SwaggerDoc()["assuredConcurrencyShares"]},
		{Name: "Queues", Type: "string", Description: flowcontrolv1beta3.QueuingConfiguration{}.SwaggerDoc()["queues"]},
		{Name: "HandSize", Type: "string", Description: flowcontrolv1beta3.QueuingConfiguration{}.SwaggerDoc()["handSize"]},
		{Name: "QueueLengthLimit", Type: "string", Description: flowcontrolv1beta3.QueuingConfiguration{}.SwaggerDoc()["queueLengthLimit"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	}
	h.TableHandler(priorityLevelColumnDefinitions, printPriorityLevelConfiguration)
	h.TableHandler(priorityLevelColumnDefinitions, printPriorityLevelConfigurationList)

	storageVersionColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "CommonEncodingVersion", Type: "string", Description: apiserverinternalv1alpha1.StorageVersionStatus{}.SwaggerDoc()["commonEncodingVersion"]},
		{Name: "StorageVersions", Type: "string", Description: apiserverinternalv1alpha1.StorageVersionStatus{}.SwaggerDoc()["storageVersions"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	}
	h.TableHandler(storageVersionColumnDefinitions, printStorageVersion)
	h.TableHandler(storageVersionColumnDefinitions, printStorageVersionList)

	scaleColumnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["name"]},
		{Name: "Desired", Type: "integer", Description: autoscalingv1.ScaleSpec{}.SwaggerDoc()["replicas"]},
		{Name: "Available", Type: "integer", Description: autoscalingv1.ScaleStatus{}.SwaggerDoc()["replicas"]},
		{Name: "Age", Type: "string", Description: metav1.ObjectMeta{}.SwaggerDoc()["creationTimestamp"]},
	}
	h.TableHandler(scaleColumnDefinitions, printScale)
}

// Pass ports=nil for all ports.
func formatEndpoints(endpoints *corev1.Endpoints, ports sets.String) string {
	if len(endpoints.Subsets) == 0 {
		return "<none>"
	}
	list := []string{}
	max := 3
	more := false
	count := 0
	for i := range endpoints.Subsets {
		ss := &endpoints.Subsets[i]
		if len(ss.Ports) == 0 {
			// It's possible to have headless services with no ports.
			for i := range ss.Addresses {
				if len(list) == max {
					more = true
				}
				if !more {
					list = append(list, ss.Addresses[i].IP)
				}
				count++
			}
		} else {
			// "Normal" services with ports defined.
			for i := range ss.Ports {
				port := &ss.Ports[i]
				if ports == nil || ports.Has(port.Name) {
					for i := range ss.Addresses {
						if len(list) == max {
							more = true
						}
						addr := &ss.Addresses[i]
						if !more {
							hostPort := net.JoinHostPort(addr.IP, strconv.Itoa(int(port.Port)))
							list = append(list, hostPort)
						}
						count++
					}
				}
			}
		}
	}
	ret := strings.Join(list, ",")
	if more {
		return fmt.Sprintf("%s + %d more...", ret, count-max)
	}
	return ret
}

func formatDiscoveryPorts(ports []discoveryv1beta1.EndpointPort) string {
	list := []string{}
	max := 3
	more := false
	count := 0
	for _, port := range ports {
		if len(list) < max {
			portNum := "*"
			if port.Port != nil {
				portNum = strconv.Itoa(int(*port.Port))
			} else if port.Name != nil {
				portNum = *port.Name
			}
			list = append(list, portNum)
		} else if len(list) == max {
			more = true
		}
		count++
	}
	return listWithMoreString(list, more, count, max)
}

func formatDiscoveryEndpoints(endpoints []discoveryv1beta1.Endpoint) string {
	list := []string{}
	max := 3
	more := false
	count := 0
	for _, endpoint := range endpoints {
		for _, address := range endpoint.Addresses {
			if len(list) < max {
				list = append(list, address)
			} else if len(list) == max {
				more = true
			}
			count++
		}
	}
	return listWithMoreString(list, more, count, max)
}

func listWithMoreString(list []string, more bool, count, max int) string {
	ret := strings.Join(list, ",")
	if more {
		return fmt.Sprintf("%s + %d more...", ret, count-max)
	}
	if ret == "" {
		ret = "<unset>"
	}
	return ret
}

// translateMicroTimestampSince returns the elapsed time since timestamp in
// human-readable approximation.
func translateMicroTimestampSince(timestamp metav1.MicroTime) string {
	if timestamp.IsZero() {
		return "<unknown>"
	}

	return duration.HumanDuration(time.Since(timestamp.Time))
}

// translateTimestampSince returns the elapsed time since timestamp in
// human-readable approximation.
func translateTimestampSince(timestamp metav1.Time) string {
	if timestamp.IsZero() {
		return "<unknown>"
	}

	return duration.HumanDuration(time.Since(timestamp.Time))
}

var (
	podSuccessConditions = []metav1.TableRowCondition{{Type: metav1.RowCompleted, Status: metav1.ConditionTrue, Reason: string(corev1.PodSucceeded), Message: "The pod has completed successfully."}}
	podFailedConditions  = []metav1.TableRowCondition{{Type: metav1.RowCompleted, Status: metav1.ConditionTrue, Reason: string(corev1.PodFailed), Message: "The pod failed."}}
)

// func printPodList(podList *corev1.PodList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
func printPodList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	podList := &corev1.PodList{}
	// unstructObj
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), podList); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to  podList %v", err)
	}

	rows := make([]metav1.TableRow, 0, len(podList.Items))
	for i := range podList.Items {
		r, err := printPod(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

// func printPod(pod *corev1.Pod, options printers.GenerateOptions) ([]metav1.TableRow, error) {
func printPod(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	pod := &corev1.Pod{}
	// unstructObj
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), pod); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to pod %v", err)
	}
	restarts := 0
	totalContainers := len(pod.Spec.Containers)
	readyContainers := 0
	lastRestartDate := metav1.NewTime(time.Time{})

	reason := string(pod.Status.Phase)
	if pod.Status.Reason != "" {
		reason = pod.Status.Reason
	}

	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: pod},
	}

	switch pod.Status.Phase {
	case corev1.PodSucceeded:
		row.Conditions = podSuccessConditions
	case corev1.PodFailed:
		row.Conditions = podFailedConditions
	}

	initializing := false
	for i := range pod.Status.InitContainerStatuses {
		container := pod.Status.InitContainerStatuses[i]
		restarts += int(container.RestartCount)
		if container.LastTerminationState.Terminated != nil {
			terminatedDate := container.LastTerminationState.Terminated.FinishedAt
			if lastRestartDate.Before(&terminatedDate) {
				lastRestartDate = terminatedDate
			}
		}
		switch {
		case container.State.Terminated != nil && container.State.Terminated.ExitCode == 0:
			continue
		case container.State.Terminated != nil:
			// initialization is failed
			if len(container.State.Terminated.Reason) == 0 {
				if container.State.Terminated.Signal != 0 {
					reason = fmt.Sprintf("Init:Signal:%d", container.State.Terminated.Signal)
				} else {
					reason = fmt.Sprintf("Init:ExitCode:%d", container.State.Terminated.ExitCode)
				}
			} else {
				reason = "Init:" + container.State.Terminated.Reason
			}
			initializing = true
		case container.State.Waiting != nil && len(container.State.Waiting.Reason) > 0 && container.State.Waiting.Reason != "PodInitializing":
			reason = "Init:" + container.State.Waiting.Reason
			initializing = true
		default:
			reason = fmt.Sprintf("Init:%d/%d", i, len(pod.Spec.InitContainers))
			initializing = true
		}
		break
	}
	if !initializing {
		restarts = 0
		hasRunning := false
		for i := len(pod.Status.ContainerStatuses) - 1; i >= 0; i-- {
			container := pod.Status.ContainerStatuses[i]

			restarts += int(container.RestartCount)
			if container.LastTerminationState.Terminated != nil {
				terminatedDate := container.LastTerminationState.Terminated.FinishedAt
				if lastRestartDate.Before(&terminatedDate) {
					lastRestartDate = terminatedDate
				}
			}
			if container.State.Waiting != nil && container.State.Waiting.Reason != "" {
				reason = container.State.Waiting.Reason
			} else if container.State.Terminated != nil && container.State.Terminated.Reason != "" {
				reason = container.State.Terminated.Reason
			} else if container.State.Terminated != nil && container.State.Terminated.Reason == "" {
				if container.State.Terminated.Signal != 0 {
					reason = fmt.Sprintf("Signal:%d", container.State.Terminated.Signal)
				} else {
					reason = fmt.Sprintf("ExitCode:%d", container.State.Terminated.ExitCode)
				}
			} else if container.Ready && container.State.Running != nil {
				hasRunning = true
				readyContainers++
			}
		}

		// change pod status back to "Running" if there is at least one container still reporting as "Running" status
		if reason == "Completed" && hasRunning {
			if hasPodReadyCondition(pod.Status.Conditions) {
				reason = "Running"
			} else {
				reason = "NotReady"
			}
		}
	}

	if pod.DeletionTimestamp != nil && pod.Status.Reason == "NodeLost" {
		reason = "Unknown"
	} else if pod.DeletionTimestamp != nil {
		reason = "Terminating"
	}

	restartsStr := strconv.Itoa(restarts)
	if !lastRestartDate.IsZero() {
		restartsStr = fmt.Sprintf("%d (%s ago)", restarts, translateTimestampSince(lastRestartDate))
	}

	row.Cells = append(row.Cells, pod.Name, fmt.Sprintf("%d/%d", readyContainers, totalContainers), reason, restartsStr, translateTimestampSince(pod.CreationTimestamp))
	if options.Wide {
		nodeName := pod.Spec.NodeName
		nominatedNodeName := pod.Status.NominatedNodeName
		podIP := ""
		if len(pod.Status.PodIPs) > 0 {
			podIP = pod.Status.PodIPs[0].IP
		}

		if podIP == "" {
			podIP = "<none>"
		}
		if nodeName == "" {
			nodeName = "<none>"
		}
		if nominatedNodeName == "" {
			nominatedNodeName = "<none>"
		}

		readinessGates := "<none>"
		if len(pod.Spec.ReadinessGates) > 0 {
			trueConditions := 0
			for _, readinessGate := range pod.Spec.ReadinessGates {
				conditionType := readinessGate.ConditionType
				for _, condition := range pod.Status.Conditions {
					if condition.Type == conditionType {
						if condition.Status == corev1.ConditionTrue {
							trueConditions++
						}
						break
					}
				}
			}
			readinessGates = fmt.Sprintf("%d/%d", trueConditions, len(pod.Spec.ReadinessGates))
		}
		row.Cells = append(row.Cells, podIP, nodeName, nominatedNodeName, readinessGates)
	}

	return []metav1.TableRow{row}, nil
}

func hasPodReadyCondition(conditions []corev1.PodCondition) bool {
	for _, condition := range conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func printPodTemplate(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &corev1.PodTemplate{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to PodTemplate %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}
	names, images := layoutContainerCells(obj.Template.Spec.Containers)
	row.Cells = append(row.Cells, obj.Name, names, images, labels.FormatLabels(obj.Template.Labels))
	return []metav1.TableRow{row}, nil
}

func printPodTemplateList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &corev1.PodTemplateList{}
	// unstructObj
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to PodTemplateList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printPodTemplate(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printPodDisruptionBudget(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &policyv1.PodDisruptionBudget{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to PodDisruptionBudget %v", err)
	}

	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}

	var minAvailable string
	var maxUnavailable string
	if obj.Spec.MinAvailable != nil {
		minAvailable = obj.Spec.MinAvailable.String()
	} else {
		minAvailable = "N/A"
	}

	if obj.Spec.MaxUnavailable != nil {
		maxUnavailable = obj.Spec.MaxUnavailable.String()
	} else {
		maxUnavailable = "N/A"
	}

	row.Cells = append(row.Cells, obj.Name, minAvailable, maxUnavailable, int64(obj.Status.DisruptionsAllowed), translateTimestampSince(obj.CreationTimestamp))
	return []metav1.TableRow{row}, nil
}

// func printPodDisruptionBudgetList(list *policyv1.PodDisruptionBudgetList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
func printPodDisruptionBudgetList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &policyv1.PodDisruptionBudgetList{}
	// unstructObj
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to PodDisruptionBudgetList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printPodDisruptionBudget(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

// TODO(AdoHe): try to put wide output in a single method
// func printReplicationController(obj *corev1.ReplicationController, options printers.GenerateOptions) ([]metav1.TableRow, error) {
func printReplicationController(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &corev1.ReplicationController{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to ReplicationController %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}

	desiredReplicas := obj.Spec.Replicas
	currentReplicas := obj.Status.Replicas
	readyReplicas := obj.Status.ReadyReplicas

	row.Cells = append(row.Cells, obj.Name, int64(*desiredReplicas), int64(currentReplicas), int64(readyReplicas), translateTimestampSince(obj.CreationTimestamp))
	if options.Wide {
		names, images := layoutContainerCells(obj.Spec.Template.Spec.Containers)
		row.Cells = append(row.Cells, names, images, labels.FormatLabels(obj.Spec.Selector))
	}
	return []metav1.TableRow{row}, nil
}

func printReplicationControllerList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &corev1.ReplicationControllerList{}
	// unstructObj
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to ReplicationControllerList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printReplicationController(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

// func printReplicaSet(obj *appsv1.ReplicaSet, options printers.GenerateOptions) ([]metav1.TableRow, error) {
func printReplicaSet(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &appsv1.ReplicaSet{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to ReplicaSet %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}

	desiredReplicas := obj.Spec.Replicas
	currentReplicas := obj.Status.Replicas
	readyReplicas := obj.Status.ReadyReplicas

	row.Cells = append(row.Cells, obj.Name, int64(*desiredReplicas), int64(currentReplicas), int64(readyReplicas), translateTimestampSince(obj.CreationTimestamp))
	if options.Wide {
		names, images := layoutContainerCells(obj.Spec.Template.Spec.Containers)
		row.Cells = append(row.Cells, names, images, metav1.FormatLabelSelector(obj.Spec.Selector))
	}
	return []metav1.TableRow{row}, nil
}

// func printReplicaSetList(list *appsv1.ReplicaSetList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
func printReplicaSetList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &appsv1.ReplicaSetList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to ReplicaSetList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printReplicaSet(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

// func printJob(obj *batchv1.Job, options printers.GenerateOptions) ([]metav1.TableRow, error) {
func printJob(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &batchv1.Job{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to Job %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}

	var completions string
	if obj.Spec.Completions != nil {
		completions = fmt.Sprintf("%d/%d", obj.Status.Succeeded, *obj.Spec.Completions)
	} else {
		parallelism := int32(0)
		if obj.Spec.Parallelism != nil {
			parallelism = *obj.Spec.Parallelism
		}
		if parallelism > 1 {
			completions = fmt.Sprintf("%d/1 of %d", obj.Status.Succeeded, parallelism)
		} else {
			completions = fmt.Sprintf("%d/1", obj.Status.Succeeded)
		}
	}
	var jobDuration string
	switch {
	case obj.Status.StartTime == nil:
	case obj.Status.CompletionTime == nil:
		jobDuration = duration.HumanDuration(time.Since(obj.Status.StartTime.Time))
	default:
		jobDuration = duration.HumanDuration(obj.Status.CompletionTime.Sub(obj.Status.StartTime.Time))
	}

	row.Cells = append(row.Cells, obj.Name, completions, jobDuration, translateTimestampSince(obj.CreationTimestamp))
	if options.Wide {
		names, images := layoutContainerCells(obj.Spec.Template.Spec.Containers)
		row.Cells = append(row.Cells, names, images, metav1.FormatLabelSelector(obj.Spec.Selector))
	}
	return []metav1.TableRow{row}, nil
}

func printJobList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &batchv1.JobList{}
	// unstructObj
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to JobList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printJob(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printCronJob(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &batchv1.CronJob{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to CronJob %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}

	lastScheduleTime := "<none>"
	if obj.Status.LastScheduleTime != nil {
		lastScheduleTime = translateTimestampSince(*obj.Status.LastScheduleTime)
	}

	row.Cells = append(row.Cells, obj.Name, obj.Spec.Schedule, printBoolPtr(obj.Spec.Suspend), int64(len(obj.Status.Active)), lastScheduleTime, translateTimestampSince(obj.CreationTimestamp))
	if options.Wide {
		names, images := layoutContainerCells(obj.Spec.JobTemplate.Spec.Template.Spec.Containers)
		row.Cells = append(row.Cells, names, images, metav1.FormatLabelSelector(obj.Spec.JobTemplate.Spec.Selector))
	}
	return []metav1.TableRow{row}, nil
}

func printCronJobList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &batchv1.CronJobList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to CronJobList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printCronJob(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

// loadBalancerStatusStringer behaves mostly like a string interface and converts the given status to a string.
// `wide` indicates whether the returned value is meant for --o=wide output. If not, it's clipped to 16 bytes.
func loadBalancerStatusStringer(s corev1.LoadBalancerStatus, wide bool) string {
	ingress := s.Ingress
	result := sets.NewString()
	for i := range ingress {
		if ingress[i].IP != "" {
			result.Insert(ingress[i].IP)
		} else if ingress[i].Hostname != "" {
			result.Insert(ingress[i].Hostname)
		}
	}

	r := strings.Join(result.List(), ",")
	if !wide && len(r) > loadBalancerWidth {
		r = r[0:(loadBalancerWidth-3)] + "..."
	}
	return r
}

func getServiceExternalIP(svc *corev1.Service, wide bool) string {
	switch svc.Spec.Type {
	case corev1.ServiceTypeClusterIP:
		if len(svc.Spec.ExternalIPs) > 0 {
			return strings.Join(svc.Spec.ExternalIPs, ",")
		}
		return "<none>"
	case corev1.ServiceTypeNodePort:
		if len(svc.Spec.ExternalIPs) > 0 {
			return strings.Join(svc.Spec.ExternalIPs, ",")
		}
		return "<none>"
	case corev1.ServiceTypeLoadBalancer:
		lbIps := loadBalancerStatusStringer(svc.Status.LoadBalancer, wide)
		if len(svc.Spec.ExternalIPs) > 0 {
			results := []string{}
			if len(lbIps) > 0 {
				results = append(results, strings.Split(lbIps, ",")...)
			}
			results = append(results, svc.Spec.ExternalIPs...)
			return strings.Join(results, ",")
		}
		if len(lbIps) > 0 {
			return lbIps
		}
		return "<pending>"
	case corev1.ServiceTypeExternalName:
		return svc.Spec.ExternalName
	}
	return "<unknown>"
}

func makePortString(ports []corev1.ServicePort) string {
	pieces := make([]string, len(ports))
	for ix := range ports {
		port := &ports[ix]
		pieces[ix] = fmt.Sprintf("%d/%s", port.Port, port.Protocol)
		if port.NodePort > 0 {
			pieces[ix] = fmt.Sprintf("%d:%d/%s", port.Port, port.NodePort, port.Protocol)
		}
	}
	return strings.Join(pieces, ",")
}

func printService(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &corev1.Service{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to Service %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}
	svcType := obj.Spec.Type
	internalIP := "<none>"
	if len(obj.Spec.ClusterIPs) > 0 {
		internalIP = obj.Spec.ClusterIPs[0]
	}

	externalIP := getServiceExternalIP(obj, options.Wide)
	svcPorts := makePortString(obj.Spec.Ports)
	if len(svcPorts) == 0 {
		svcPorts = "<none>"
	}

	row.Cells = append(row.Cells, obj.Name, string(svcType), internalIP, externalIP, svcPorts, translateTimestampSince(obj.CreationTimestamp))
	if options.Wide {
		row.Cells = append(row.Cells, labels.FormatLabels(obj.Spec.Selector))
	}

	return []metav1.TableRow{row}, nil
}

func printServiceList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &corev1.ServiceList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to ServiceList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printService(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func formatHosts(rules []networkingv1.IngressRule) string {
	list := []string{}
	max := 3
	more := false
	for _, rule := range rules {
		if len(list) == max {
			more = true
		}
		if !more && len(rule.Host) != 0 {
			list = append(list, rule.Host)
		}
	}
	if len(list) == 0 {
		return "*"
	}
	ret := strings.Join(list, ",")
	if more {
		return fmt.Sprintf("%s + %d more...", ret, len(rules)-max)
	}
	return ret
}

func formatPorts(tls []networkingv1.IngressTLS) string {
	if len(tls) != 0 {
		return "80, 443"
	}
	return "80"
}

func printIngress(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &networkingv1.Ingress{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to Ingress %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}
	className := "<none>"
	if obj.Spec.IngressClassName != nil {
		className = *obj.Spec.IngressClassName
	}
	hosts := formatHosts(obj.Spec.Rules)
	address := ingressLoadBalancerStatusStringer(obj.Status.LoadBalancer, options.Wide)
	ports := formatPorts(obj.Spec.TLS)
	createTime := translateTimestampSince(obj.CreationTimestamp)
	row.Cells = append(row.Cells, obj.Name, className, hosts, address, ports, createTime)
	return []metav1.TableRow{row}, nil
}

// ingressLoadBalancerStatusStringer behaves mostly like a string interface and converts the given status to a string.
// `wide` indicates whether the returned value is meant for --o=wide output. If not, it's clipped to 16 bytes.
func ingressLoadBalancerStatusStringer(s networkingv1.IngressLoadBalancerStatus, wide bool) string {
	ingress := s.Ingress
	result := sets.NewString()
	for i := range ingress {
		if ingress[i].IP != "" {
			result.Insert(ingress[i].IP)
		} else if ingress[i].Hostname != "" {
			result.Insert(ingress[i].Hostname)
		}
	}

	r := strings.Join(result.List(), ",")
	if !wide && len(r) > loadBalancerWidth {
		r = r[0:(loadBalancerWidth-3)] + "..."
	}
	return r
}

func printIngressList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &networkingv1.IngressList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to IngressList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printIngress(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printIngressClass(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &networkingv1.IngressClass{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to IngressClass %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}
	parameters := "<none>"
	if obj.Spec.Parameters != nil {
		parameters = obj.Spec.Parameters.Kind
		if obj.Spec.Parameters.APIGroup != nil {
			parameters = parameters + "." + *obj.Spec.Parameters.APIGroup
		}
		parameters = parameters + "/" + obj.Spec.Parameters.Name
	}
	createTime := translateTimestampSince(obj.CreationTimestamp)
	row.Cells = append(row.Cells, obj.Name, obj.Spec.Controller, parameters, createTime)
	return []metav1.TableRow{row}, nil
}

func printIngressClassList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &networkingv1.IngressClassList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to IngressClassList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printIngressClass(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printStatefulSet(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &appsv1.StatefulSet{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to StatefulSet %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}
	desiredReplicas := obj.Spec.Replicas
	readyReplicas := obj.Status.ReadyReplicas
	createTime := translateTimestampSince(obj.CreationTimestamp)
	row.Cells = append(row.Cells, obj.Name, fmt.Sprintf("%d/%d", int64(readyReplicas), int64(*desiredReplicas)), createTime)
	if options.Wide {
		names, images := layoutContainerCells(obj.Spec.Template.Spec.Containers)
		row.Cells = append(row.Cells, names, images)
	}
	return []metav1.TableRow{row}, nil
}

func printStatefulSetList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &appsv1.StatefulSetList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to StatefulSetList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printStatefulSet(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printDaemonSet(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &appsv1.DaemonSet{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to DaemonSet %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}

	desiredScheduled := obj.Status.DesiredNumberScheduled
	currentScheduled := obj.Status.CurrentNumberScheduled
	numberReady := obj.Status.NumberReady
	numberUpdated := obj.Status.UpdatedNumberScheduled
	numberAvailable := obj.Status.NumberAvailable

	row.Cells = append(row.Cells, obj.Name, int64(desiredScheduled), int64(currentScheduled), int64(numberReady), int64(numberUpdated), int64(numberAvailable), labels.FormatLabels(obj.Spec.Template.Spec.NodeSelector), translateTimestampSince(obj.CreationTimestamp))
	if options.Wide {
		names, images := layoutContainerCells(obj.Spec.Template.Spec.Containers)
		row.Cells = append(row.Cells, names, images, metav1.FormatLabelSelector(obj.Spec.Selector))
	}
	return []metav1.TableRow{row}, nil
}

func printDaemonSetList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &appsv1.DaemonSetList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to DaemonSetList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printDaemonSet(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printEndpoints(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &corev1.Endpoints{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to Endpoints %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}
	row.Cells = append(row.Cells, obj.Name, formatEndpoints(obj, nil), translateTimestampSince(obj.CreationTimestamp))
	return []metav1.TableRow{row}, nil
}

func printEndpointsList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &corev1.EndpointsList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to EndpointsList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printEndpoints(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil

}

func printEndpointSlice(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &discoveryv1beta1.EndpointSlice{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to EndpointSlice %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}
	row.Cells = append(row.Cells, obj.Name, string(obj.AddressType), formatDiscoveryPorts(obj.Ports), formatDiscoveryEndpoints(obj.Endpoints), translateTimestampSince(obj.CreationTimestamp))
	return []metav1.TableRow{row}, nil
}

func printEndpointSliceList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &discoveryv1beta1.EndpointSliceList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to EndpointSliceList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printEndpointSlice(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printCSINode(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &storagev1.CSINode{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to CSINode %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}
	row.Cells = append(row.Cells, obj.Name, len(obj.Spec.Drivers), translateTimestampSince(obj.CreationTimestamp))
	return []metav1.TableRow{row}, nil
}

func printCSINodeList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &storagev1.CSINodeList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to CSINodeList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printCSINode(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printCSIDriver(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &storagev1.CSIDriver{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to CSIDriver %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}
	attachRequired := true
	if obj.Spec.AttachRequired != nil {
		attachRequired = *obj.Spec.AttachRequired
	}
	podInfoOnMount := false
	if obj.Spec.PodInfoOnMount != nil {
		podInfoOnMount = *obj.Spec.PodInfoOnMount
	}
	allModes := []string{}
	for _, mode := range obj.Spec.VolumeLifecycleModes {
		allModes = append(allModes, string(mode))
	}
	modes := strings.Join(allModes, ",")
	if len(modes) == 0 {
		modes = "<none>"
	}

	row.Cells = append(row.Cells, obj.Name, attachRequired, podInfoOnMount)
	storageCapacity := false
	if obj.Spec.StorageCapacity != nil {
		storageCapacity = *obj.Spec.StorageCapacity
	}
	row.Cells = append(row.Cells, storageCapacity)

	tokenRequests := "<unset>"
	if obj.Spec.TokenRequests != nil {
		audiences := []string{}
		for _, t := range obj.Spec.TokenRequests {
			audiences = append(audiences, t.Audience)
		}
		tokenRequests = strings.Join(audiences, ",")
	}
	requiresRepublish := false
	if obj.Spec.RequiresRepublish != nil {
		requiresRepublish = *obj.Spec.RequiresRepublish
	}
	row.Cells = append(row.Cells, tokenRequests, requiresRepublish)

	row.Cells = append(row.Cells, modes, translateTimestampSince(obj.CreationTimestamp))
	return []metav1.TableRow{row}, nil
}

func printCSIDriverList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &storagev1.CSIDriverList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to CSIDriverList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printCSIDriver(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printCSIStorageCapacity(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &storagev1beta1.CSIStorageCapacity{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to CSIStorageCapacity %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}

	capacity := "<unset>"
	if obj.Capacity != nil {
		capacity = obj.Capacity.String()
	}

	row.Cells = append(row.Cells, obj.Name, obj.StorageClassName, capacity)
	return []metav1.TableRow{row}, nil
}

func printCSIStorageCapacityList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &storagev1beta1.CSIStorageCapacityList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to CSIStorageCapacityList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printCSIStorageCapacity(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printMutatingWebhook(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &admissionregistrationv1.MutatingWebhookConfiguration{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to MutatingWebhookConfiguration %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}
	row.Cells = append(row.Cells, obj.Name, len(obj.Webhooks), translateTimestampSince(obj.CreationTimestamp))
	return []metav1.TableRow{row}, nil
}

func printMutatingWebhookList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &admissionregistrationv1.MutatingWebhookConfigurationList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to MutatingWebhookConfigurationList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printMutatingWebhook(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printValidatingWebhook(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to ValidatingWebhookConfiguration %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}
	row.Cells = append(row.Cells, obj.Name, len(obj.Webhooks), translateTimestampSince(obj.CreationTimestamp))
	return []metav1.TableRow{row}, nil
}

func printValidatingWebhookList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &admissionregistrationv1.ValidatingWebhookConfigurationList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to ValidatingWebhookConfigurationList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printValidatingWebhook(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printNamespace(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &corev1.Namespace{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to Namespace %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}
	row.Cells = append(row.Cells, obj.Name, string(obj.Status.Phase), translateTimestampSince(obj.CreationTimestamp))
	return []metav1.TableRow{row}, nil
}

func printNamespaceList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &corev1.NamespaceList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to NamespaceList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printNamespace(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printSecret(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &corev1.Secret{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to Secret %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}
	row.Cells = append(row.Cells, obj.Name, string(obj.Type), int64(len(obj.Data)), translateTimestampSince(obj.CreationTimestamp))
	return []metav1.TableRow{row}, nil
}

func printSecretList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &corev1.SecretList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to SecretList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printSecret(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printServiceAccount(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &corev1.ServiceAccount{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to ServiceAccount %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}
	row.Cells = append(row.Cells, obj.Name, int64(len(obj.Secrets)), translateTimestampSince(obj.CreationTimestamp))
	return []metav1.TableRow{row}, nil
}

func printServiceAccountList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &corev1.ServiceAccountList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to ServiceAccountList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printServiceAccount(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printNode(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &corev1.Node{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to Node %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}

	conditionMap := make(map[corev1.NodeConditionType]*corev1.NodeCondition)
	NodeAllConditions := []corev1.NodeConditionType{corev1.NodeReady}
	for i := range obj.Status.Conditions {
		cond := obj.Status.Conditions[i]
		conditionMap[cond.Type] = &cond
	}
	var status []string
	for _, validCondition := range NodeAllConditions {
		if condition, ok := conditionMap[validCondition]; ok {
			if condition.Status == corev1.ConditionTrue {
				status = append(status, string(condition.Type))
			} else {
				status = append(status, "Not"+string(condition.Type))
			}
		}
	}
	if len(status) == 0 {
		status = append(status, "Unknown")
	}
	if obj.Spec.Unschedulable {
		status = append(status, "SchedulingDisabled")
	}

	roles := strings.Join(findNodeRoles(obj), ",")
	if len(roles) == 0 {
		roles = "<none>"
	}

	row.Cells = append(row.Cells, obj.Name, strings.Join(status, ","), roles, translateTimestampSince(obj.CreationTimestamp), obj.Status.NodeInfo.KubeletVersion)
	if options.Wide {
		osImage, kernelVersion, crVersion := obj.Status.NodeInfo.OSImage, obj.Status.NodeInfo.KernelVersion, obj.Status.NodeInfo.ContainerRuntimeVersion
		if osImage == "" {
			osImage = "<unknown>"
		}
		if kernelVersion == "" {
			kernelVersion = "<unknown>"
		}
		if crVersion == "" {
			crVersion = "<unknown>"
		}
		row.Cells = append(row.Cells, getNodeInternalIP(obj), getNodeExternalIP(obj), osImage, kernelVersion, crVersion)
	}

	return []metav1.TableRow{row}, nil
}

// Returns first external ip of the node or "<none>" if none is found.
func getNodeExternalIP(node *corev1.Node) string {
	for _, address := range node.Status.Addresses {
		if address.Type == corev1.NodeExternalIP {
			return address.Address
		}
	}

	return "<none>"
}

// Returns the internal IP of the node or "<none>" if none is found.
func getNodeInternalIP(node *corev1.Node) string {
	for _, address := range node.Status.Addresses {
		if address.Type == corev1.NodeInternalIP {
			return address.Address
		}
	}

	return "<none>"
}

// findNodeRoles returns the roles of a given node.
// The roles are determined by looking for:
// * a node-role.kubernetes.io/<role>="" label
// * a kubernetes.io/role="<role>" label
func findNodeRoles(node *corev1.Node) []string {
	roles := sets.NewString()
	for k, v := range node.Labels {
		switch {
		case strings.HasPrefix(k, labelNodeRolePrefix):
			if role := strings.TrimPrefix(k, labelNodeRolePrefix); len(role) > 0 {
				roles.Insert(role)
			}

		case k == nodeLabelRole && v != "":
			roles.Insert(v)
		}
	}
	return roles.List()
}

func printNodeList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &corev1.NodeList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to NodeList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printNode(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printPersistentVolume(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &corev1.PersistentVolume{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to PersistentVolume %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}

	claimRefUID := ""
	if obj.Spec.ClaimRef != nil {
		claimRefUID += obj.Spec.ClaimRef.Namespace
		claimRefUID += "/"
		claimRefUID += obj.Spec.ClaimRef.Name
	}

	modesStr := kubectlstoragehelper.GetAccessModesAsString(obj.Spec.AccessModes)
	reclaimPolicyStr := string(obj.Spec.PersistentVolumeReclaimPolicy)

	aQty := obj.Spec.Capacity[corev1.ResourceStorage]
	aSize := aQty.String()

	phase := obj.Status.Phase
	if obj.ObjectMeta.DeletionTimestamp != nil {
		phase = "Terminating"
	}
	volumeMode := "<unset>"
	if obj.Spec.VolumeMode != nil {
		volumeMode = string(*obj.Spec.VolumeMode)
	}

	row.Cells = append(row.Cells, obj.Name, aSize, modesStr, reclaimPolicyStr,
		string(phase), claimRefUID, kubectlstoragehelper.GetPersistentVolumeClass(obj),
		obj.Status.Reason, translateTimestampSince(obj.CreationTimestamp), volumeMode)
	return []metav1.TableRow{row}, nil
}

func printPersistentVolumeList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &corev1.PersistentVolumeList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to PersistentVolumeList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printPersistentVolume(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printPersistentVolumeClaim(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &corev1.PersistentVolumeClaim{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to PersistentVolumeClaim %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}

	phase := obj.Status.Phase
	if obj.ObjectMeta.DeletionTimestamp != nil {
		phase = "Terminating"
	}

	storage := obj.Spec.Resources.Requests[corev1.ResourceStorage]
	capacity := ""
	accessModes := ""
	volumeMode := "<unset>"
	if obj.Spec.VolumeName != "" {
		accessModes = kubectlstoragehelper.GetAccessModesAsString(obj.Status.AccessModes)
		storage = obj.Status.Capacity[corev1.ResourceStorage]
		capacity = storage.String()
	}

	if obj.Spec.VolumeMode != nil {
		volumeMode = string(*obj.Spec.VolumeMode)
	}

	row.Cells = append(row.Cells, obj.Name, string(phase), obj.Spec.VolumeName, capacity, accessModes,
		kubectlstoragehelper.GetPersistentVolumeClaimClass(obj), translateTimestampSince(obj.CreationTimestamp), volumeMode)
	return []metav1.TableRow{row}, nil
}

func printPersistentVolumeClaimList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &corev1.PersistentVolumeClaimList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to PersistentVolumeClaimList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printPersistentVolumeClaim(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printEvent(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &corev1.Event{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to Event %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}

	firstTimestamp := translateTimestampSince(obj.FirstTimestamp)
	if obj.FirstTimestamp.IsZero() {
		firstTimestamp = translateMicroTimestampSince(obj.EventTime)
	}

	lastTimestamp := translateTimestampSince(obj.LastTimestamp)
	if obj.LastTimestamp.IsZero() {
		lastTimestamp = firstTimestamp
	}

	count := obj.Count
	if obj.Series != nil {
		lastTimestamp = translateMicroTimestampSince(obj.Series.LastObservedTime)
		count = obj.Series.Count
	} else if count == 0 {
		// Singleton events don't have a count set in the new API.
		count = 1
	}

	var target string
	if len(obj.InvolvedObject.Name) > 0 {
		target = fmt.Sprintf("%s/%s", strings.ToLower(obj.InvolvedObject.Kind), obj.InvolvedObject.Name)
	} else {
		target = strings.ToLower(obj.InvolvedObject.Kind)
	}
	if options.Wide {
		row.Cells = append(row.Cells,
			lastTimestamp,
			obj.Type,
			obj.Reason,
			target,
			obj.InvolvedObject.FieldPath,
			formatEventSource(obj.Source, obj.ReportingController, obj.ReportingInstance),
			strings.TrimSpace(obj.Message),
			firstTimestamp,
			int64(count),
			obj.Name,
		)
	} else {
		row.Cells = append(row.Cells,
			lastTimestamp,
			obj.Type,
			obj.Reason,
			target,
			strings.TrimSpace(obj.Message),
		)
	}

	return []metav1.TableRow{row}, nil
}

// Sorts and prints the EventList in a human-friendly format.
func printEventList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &corev1.EventList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to EventList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printEvent(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printRoleBinding(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &rbacv1.RoleBinding{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to EventList %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}

	roleRef := fmt.Sprintf("%s/%s", obj.RoleRef.Kind, obj.RoleRef.Name)
	row.Cells = append(row.Cells, obj.Name, roleRef, translateTimestampSince(obj.CreationTimestamp))
	if options.Wide {
		users, groups, sas, _ := SubjectsStrings(obj.Subjects)
		row.Cells = append(row.Cells, strings.Join(users, ", "), strings.Join(groups, ", "), strings.Join(sas, ", "))
	}
	return []metav1.TableRow{row}, nil
}

// Prints the RoleBinding in a human-friendly format.
func printRoleBindingList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &rbacv1.RoleBindingList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to RoleBindingList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printRoleBinding(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printClusterRoleBinding(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &rbacv1.ClusterRoleBinding{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to ClusterRoleBinding %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}

	roleRef := fmt.Sprintf("%s/%s", obj.RoleRef.Kind, obj.RoleRef.Name)
	row.Cells = append(row.Cells, obj.Name, roleRef, translateTimestampSince(obj.CreationTimestamp))
	if options.Wide {
		users, groups, sas, _ := SubjectsStrings(obj.Subjects)
		row.Cells = append(row.Cells, strings.Join(users, ", "), strings.Join(groups, ", "), strings.Join(sas, ", "))
	}
	return []metav1.TableRow{row}, nil
}

// Prints the ClusterRoleBinding in a human-friendly format.
func printClusterRoleBindingList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &rbacv1.ClusterRoleBindingList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to ClusterRoleBindingList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printClusterRoleBinding(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printCertificateSigningRequest(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &certificatesv1.CertificateSigningRequest{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to CertificateSigningRequest %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}
	status := extractCSRStatus(obj)
	signerName := "<none>"
	if obj.Spec.SignerName != "" {
		signerName = obj.Spec.SignerName
	}
	requestedDuration := "<none>"
	if obj.Spec.ExpirationSeconds != nil {
		requestedDuration = duration.HumanDuration(csr.ExpirationSecondsToDuration(*obj.Spec.ExpirationSeconds))
	}
	row.Cells = append(row.Cells, obj.Name, translateTimestampSince(obj.CreationTimestamp), signerName, obj.Spec.Username, requestedDuration, status)
	return []metav1.TableRow{row}, nil
}

func extractCSRStatus(csr *certificatesv1.CertificateSigningRequest) string {
	var approved, denied, failed bool
	for _, c := range csr.Status.Conditions {
		switch c.Type {
		case certificatesv1.CertificateApproved:
			approved = true
		case certificatesv1.CertificateDenied:
			denied = true
		case certificatesv1.CertificateFailed:
			failed = true
		}
	}
	var status string
	// must be in order of presidence
	if denied {
		status += "Denied"
	} else if approved {
		status += "Approved"
	} else {
		status += "Pending"
	}
	if failed {
		status += ",Failed"
	}
	if len(csr.Status.Certificate) > 0 {
		status += ",Issued"
	}
	return status
}

func printCertificateSigningRequestList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &certificatesv1.CertificateSigningRequestList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to CertificateSigningRequestList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printCertificateSigningRequest(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printComponentStatus(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &corev1.ComponentStatus{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to ComponentStatus %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}
	status := "Unknown"
	message := ""
	error := ""
	for _, condition := range obj.Conditions {
		if condition.Type == corev1.ComponentHealthy {
			if condition.Status == corev1.ConditionTrue {
				status = "Healthy"
			} else {
				status = "Unhealthy"
			}
			message = condition.Message
			error = condition.Error
			break
		}
	}
	row.Cells = append(row.Cells, obj.Name, status, message, error)
	return []metav1.TableRow{row}, nil
}

func printComponentStatusList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &corev1.ComponentStatusList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to ComponentStatusList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printComponentStatus(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printDeployment(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &appsv1.Deployment{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to Deployment %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}
	desiredReplicas := obj.Spec.Replicas
	updatedReplicas := obj.Status.UpdatedReplicas
	readyReplicas := obj.Status.ReadyReplicas
	availableReplicas := obj.Status.AvailableReplicas
	age := translateTimestampSince(obj.CreationTimestamp)
	containers := obj.Spec.Template.Spec.Containers
	selector, err := metav1.LabelSelectorAsSelector(obj.Spec.Selector)
	selectorString := ""
	if err != nil {
		selectorString = "<invalid>"
	} else {
		selectorString = selector.String()
	}
	row.Cells = append(row.Cells, obj.Name, fmt.Sprintf("%d/%d", int64(readyReplicas), int64(*desiredReplicas)), int64(updatedReplicas), int64(availableReplicas), age)
	if options.Wide {
		containers, images := layoutContainerCells(containers)
		row.Cells = append(row.Cells, containers, images, selectorString)
	}
	return []metav1.TableRow{row}, nil
}

func printDeploymentList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &appsv1.DeploymentList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to Deployment %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printDeployment(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func formatHPAMetrics(specs []autoscalingv2.MetricSpec, statuses []autoscalingv2.MetricStatus) string {
	if len(specs) == 0 {
		return "<none>"
	}
	list := []string{}
	max := 2
	more := false
	count := 0
	for i, spec := range specs {
		switch spec.Type {
		case autoscalingv2.ExternalMetricSourceType:
			if spec.External.Target.AverageValue != nil {
				current := "<unknown>"
				if len(statuses) > i && statuses[i].External != nil && statuses[i].External.Current.AverageValue != nil {
					current = statuses[i].External.Current.AverageValue.String()
				}
				list = append(list, fmt.Sprintf("%s/%s (avg)", current, spec.External.Target.AverageValue.String()))
			} else {
				current := "<unknown>"
				if len(statuses) > i && statuses[i].External != nil {
					current = statuses[i].External.Current.Value.String()
				}
				list = append(list, fmt.Sprintf("%s/%s", current, spec.External.Target.Value.String()))
			}
		case autoscalingv2.PodsMetricSourceType:
			current := "<unknown>"
			if len(statuses) > i && statuses[i].Pods != nil {
				current = statuses[i].Pods.Current.AverageValue.String()
			}
			list = append(list, fmt.Sprintf("%s/%s", current, spec.Pods.Target.AverageValue.String()))
		case autoscalingv2.ObjectMetricSourceType:
			if spec.Object.Target.AverageValue != nil {
				current := "<unknown>"
				if len(statuses) > i && statuses[i].Object != nil && statuses[i].Object.Current.AverageValue != nil {
					current = statuses[i].Object.Current.AverageValue.String()
				}
				list = append(list, fmt.Sprintf("%s/%s (avg)", current, spec.Object.Target.AverageValue.String()))
			} else {
				current := "<unknown>"
				if len(statuses) > i && statuses[i].Object != nil {
					current = statuses[i].Object.Current.Value.String()
				}
				list = append(list, fmt.Sprintf("%s/%s", current, spec.Object.Target.Value.String()))
			}
		case autoscalingv2.ResourceMetricSourceType:
			if spec.Resource.Target.AverageValue != nil {
				current := "<unknown>"
				if len(statuses) > i && statuses[i].Resource != nil {
					current = statuses[i].Resource.Current.AverageValue.String()
				}
				list = append(list, fmt.Sprintf("%s/%s", current, spec.Resource.Target.AverageValue.String()))
			} else {
				current := "<unknown>"
				if len(statuses) > i && statuses[i].Resource != nil && statuses[i].Resource.Current.AverageUtilization != nil {
					current = fmt.Sprintf("%d%%", *statuses[i].Resource.Current.AverageUtilization)
				}

				target := "<auto>"
				if spec.Resource.Target.AverageUtilization != nil {
					target = fmt.Sprintf("%d%%", *spec.Resource.Target.AverageUtilization)
				}
				list = append(list, fmt.Sprintf("%s/%s", current, target))
			}
		case autoscalingv2.ContainerResourceMetricSourceType:
			if spec.ContainerResource.Target.AverageValue != nil {
				current := "<unknown>"
				if len(statuses) > i && statuses[i].ContainerResource != nil {
					current = statuses[i].ContainerResource.Current.AverageValue.String()
				}
				list = append(list, fmt.Sprintf("%s/%s", current, spec.ContainerResource.Target.AverageValue.String()))
			} else {
				current := "<unknown>"
				if len(statuses) > i && statuses[i].ContainerResource != nil && statuses[i].ContainerResource.Current.AverageUtilization != nil {
					current = fmt.Sprintf("%d%%", *statuses[i].ContainerResource.Current.AverageUtilization)
				}

				target := "<auto>"
				if spec.ContainerResource.Target.AverageUtilization != nil {
					target = fmt.Sprintf("%d%%", *spec.ContainerResource.Target.AverageUtilization)
				}
				list = append(list, fmt.Sprintf("%s/%s", current, target))
			}
		default:
			list = append(list, "<unknown type>")
		}

		count++
	}

	if count > max {
		list = list[:max]
		more = true
	}

	ret := strings.Join(list, ", ")
	if more {
		return fmt.Sprintf("%s + %d more...", ret, count-max)
	}
	return ret
}

func printHorizontalPodAutoscaler(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &autoscalingv2.HorizontalPodAutoscaler{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to HorizontalPodAutoscaler %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}

	reference := fmt.Sprintf("%s/%s",
		obj.Spec.ScaleTargetRef.Kind,
		obj.Spec.ScaleTargetRef.Name)
	minPods := "<unset>"
	metrics := formatHPAMetrics(obj.Spec.Metrics, obj.Status.CurrentMetrics)
	if obj.Spec.MinReplicas != nil {
		minPods = fmt.Sprintf("%d", *obj.Spec.MinReplicas)
	}
	maxPods := obj.Spec.MaxReplicas
	currentReplicas := obj.Status.CurrentReplicas
	row.Cells = append(row.Cells, obj.Name, reference, metrics, minPods, int64(maxPods), int64(currentReplicas), translateTimestampSince(obj.CreationTimestamp))
	return []metav1.TableRow{row}, nil
}

func printHorizontalPodAutoscalerList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &autoscalingv2.HorizontalPodAutoscalerList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to HorizontalPodAutoscalerList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printHorizontalPodAutoscaler(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printConfigMap(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &corev1.ConfigMap{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to ConfigMap %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}
	row.Cells = append(row.Cells, obj.Name, int64(len(obj.Data)+len(obj.BinaryData)), translateTimestampSince(obj.CreationTimestamp))
	return []metav1.TableRow{row}, nil
}

func printConfigMapList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &corev1.ConfigMapList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to ConfigMapList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printConfigMap(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printNetworkPolicy(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &networkingv1.NetworkPolicy{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to NetworkPolicy %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}
	row.Cells = append(row.Cells, obj.Name, metav1.FormatLabelSelector(&obj.Spec.PodSelector), translateTimestampSince(obj.CreationTimestamp))
	return []metav1.TableRow{row}, nil
}

func printNetworkPolicyList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &networkingv1.NetworkPolicyList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to NetworkPolicyList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printNetworkPolicy(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printStorageClass(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &storagev1.StorageClass{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to StorageClass %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}

	name := obj.Name
	if util.IsDefaultAnnotation(obj.ObjectMeta) {
		name += " (default)"
	}
	provtype := obj.Provisioner
	reclaimPolicy := string(corev1.PersistentVolumeReclaimDelete)
	if obj.ReclaimPolicy != nil {
		reclaimPolicy = string(*obj.ReclaimPolicy)
	}

	volumeBindingMode := string(storagev1.VolumeBindingImmediate)
	if obj.VolumeBindingMode != nil {
		volumeBindingMode = string(*obj.VolumeBindingMode)
	}

	allowVolumeExpansion := false
	if obj.AllowVolumeExpansion != nil {
		allowVolumeExpansion = *obj.AllowVolumeExpansion
	}

	row.Cells = append(row.Cells, name, provtype, reclaimPolicy, volumeBindingMode, allowVolumeExpansion,
		translateTimestampSince(obj.CreationTimestamp))

	return []metav1.TableRow{row}, nil
}

func printStorageClassList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &storagev1.StorageClassList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to StorageClassList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printStorageClass(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printLease(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &coordinationv1.Lease{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to Lease %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}

	var holderIdentity string
	if obj.Spec.HolderIdentity != nil {
		holderIdentity = *obj.Spec.HolderIdentity
	}
	row.Cells = append(row.Cells, obj.Name, holderIdentity, translateTimestampSince(obj.CreationTimestamp))
	return []metav1.TableRow{row}, nil
}

func printLeaseList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &coordinationv1.LeaseList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to LeaseList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printLease(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printStatus(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &metav1.Status{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to Status %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}
	row.Cells = append(row.Cells, obj.Status, obj.Reason, obj.Message)

	return []metav1.TableRow{row}, nil
}

// Lay out all the containers on one line if use wide output.
func layoutContainerCells(containers []corev1.Container) (names string, images string) {
	var namesBuffer bytes.Buffer
	var imagesBuffer bytes.Buffer

	for i, container := range containers {
		namesBuffer.WriteString(container.Name)
		imagesBuffer.WriteString(container.Image)
		if i != len(containers)-1 {
			namesBuffer.WriteString(",")
			imagesBuffer.WriteString(",")
		}
	}
	return namesBuffer.String(), imagesBuffer.String()
}

// formatEventSource formats EventSource as a comma separated string excluding Host when empty.
// It uses reportingController when Source.Component is empty and reportingInstance when Source.Host is empty
func formatEventSource(es corev1.EventSource, reportingController, reportingInstance string) string {
	return formatEventSourceComponentInstance(
		firstNonEmpty(es.Component, reportingController),
		firstNonEmpty(es.Host, reportingInstance),
	)
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if len(s) > 0 {
			return s
		}
	}
	return ""
}

func formatEventSourceComponentInstance(component, instance string) string {
	if len(instance) == 0 {
		return component
	}
	return component + ", " + instance
}

func printControllerRevision(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &appsv1.ControllerRevision{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to ControllerRevision %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}

	controllerRef := metav1.GetControllerOf(obj)
	controllerName := "<none>"
	if controllerRef != nil {
		withKind := true
		gv, err := schema.ParseGroupVersion(controllerRef.APIVersion)
		if err != nil {
			return nil, err
		}
		gvk := gv.WithKind(controllerRef.Kind)
		controllerName = formatResourceName(gvk.GroupKind(), controllerRef.Name, withKind)
	}
	revision := obj.Revision
	age := translateTimestampSince(obj.CreationTimestamp)
	row.Cells = append(row.Cells, obj.Name, controllerName, revision, age)
	return []metav1.TableRow{row}, nil
}

func printControllerRevisionList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &appsv1.ControllerRevisionList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to ControllerRevisionList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printControllerRevision(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

// formatResourceName receives a resource kind, name, and boolean specifying
// whether or not to update the current name to "kind/name"
func formatResourceName(kind schema.GroupKind, name string, withKind bool) string {
	if !withKind || kind.Empty() {
		return name
	}

	return strings.ToLower(kind.String()) + "/" + name
}

func printResourceQuota(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	resourceQuota := &corev1.ResourceQuota{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), resourceQuota); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to ResourceQuota %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: resourceQuota},
	}

	resources := make([]corev1.ResourceName, 0, len(resourceQuota.Status.Hard))
	for resource := range resourceQuota.Status.Hard {
		resources = append(resources, resource)
	}
	sort.Sort(SortableResourceNames(resources))

	requestColumn := bytes.NewBuffer([]byte{})
	limitColumn := bytes.NewBuffer([]byte{})
	for i := range resources {
		w := requestColumn
		resource := resources[i]
		usedQuantity := resourceQuota.Status.Used[resource]
		hardQuantity := resourceQuota.Status.Hard[resource]

		// use limitColumn writer if a resource name prefixed with "limits" is found
		if pieces := strings.Split(resource.String(), "."); len(pieces) > 1 && pieces[0] == "limits" {
			w = limitColumn
		}

		fmt.Fprintf(w, "%s: %s/%s, ", resource, usedQuantity.String(), hardQuantity.String())
	}

	age := translateTimestampSince(resourceQuota.CreationTimestamp)
	row.Cells = append(row.Cells, resourceQuota.Name, age, strings.TrimSuffix(requestColumn.String(), ", "), strings.TrimSuffix(limitColumn.String(), ", "))
	return []metav1.TableRow{row}, nil
}

func printResourceQuotaList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &corev1.ResourceQuotaList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to ResourceQuotaList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printResourceQuota(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printPriorityClass(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &schedulingv1.PriorityClass{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to PriorityClass %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}

	name := obj.Name
	value := obj.Value
	globalDefault := obj.GlobalDefault
	row.Cells = append(row.Cells, name, int64(value), globalDefault, translateTimestampSince(obj.CreationTimestamp))

	return []metav1.TableRow{row}, nil
}

func printPriorityClassList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &schedulingv1.PriorityClassList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to PriorityClassList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printPriorityClass(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printRuntimeClass(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &nodev1.RuntimeClass{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to RuntimeClass %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}

	name := obj.Name
	handler := obj.Handler
	row.Cells = append(row.Cells, name, handler, translateTimestampSince(obj.CreationTimestamp))

	return []metav1.TableRow{row}, nil
}

func printRuntimeClassList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &nodev1.RuntimeClassList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to RuntimeClassList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printRuntimeClass(&unstructList.Items[i], options)

		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printVolumeAttachment(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &storagev1.VolumeAttachment{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to VolumeAttachment %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}

	name := obj.Name
	pvName := ""
	if obj.Spec.Source.PersistentVolumeName != nil {
		pvName = *obj.Spec.Source.PersistentVolumeName
	}
	row.Cells = append(row.Cells, name, obj.Spec.Attacher, pvName, obj.Spec.NodeName, obj.Status.Attached, translateTimestampSince(obj.CreationTimestamp))

	return []metav1.TableRow{row}, nil
}

func printVolumeAttachmentList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &storagev1.VolumeAttachmentList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to VolumeAttachmentList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printVolumeAttachment(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printFlowSchema(obj *flowcontrolv1.FlowSchema, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}

	name := obj.Name
	plName := obj.Spec.PriorityLevelConfiguration.Name
	distinguisherMethod := "<none>"
	if obj.Spec.DistinguisherMethod != nil {
		distinguisherMethod = string(obj.Spec.DistinguisherMethod.Type)
	}
	badPLRef := "?"
	for _, cond := range obj.Status.Conditions {
		if cond.Type == flowcontrolv1.FlowSchemaConditionDangling {
			badPLRef = string(cond.Status)
			break
		}
	}
	row.Cells = append(row.Cells, name, plName, int64(obj.Spec.MatchingPrecedence), distinguisherMethod, translateTimestampSince(obj.CreationTimestamp), badPLRef)

	return []metav1.TableRow{row}, nil
}

func printFlowSchemaList(list *flowcontrolv1.FlowSchemaList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	rows := make([]metav1.TableRow, 0, len(list.Items))
	fsSeq := make(apihelpers.FlowSchemaSequence, len(list.Items))
	for i := range list.Items {
		fsSeq[i] = &list.Items[i]
	}
	sort.Sort(fsSeq)
	for i := range fsSeq {
		r, err := printFlowSchema(fsSeq[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printStorageVersion(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &apiserverinternalv1alpha1.StorageVersion{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to StorageVersion %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}
	commonEncodingVersion := "<unset>"
	if obj.Status.CommonEncodingVersion != nil {
		commonEncodingVersion = *obj.Status.CommonEncodingVersion
	}
	row.Cells = append(row.Cells, obj.Name, commonEncodingVersion, formatStorageVersions(obj.Status.StorageVersions), translateTimestampSince(obj.CreationTimestamp))
	return []metav1.TableRow{row}, nil
}

func formatStorageVersions(storageVersions []apiserverinternalv1alpha1.ServerStorageVersion) string {
	list := []string{}
	max := 3
	more := false
	count := 0
	for _, sv := range storageVersions {
		if len(list) < max {
			list = append(list, fmt.Sprintf("%s=%s", sv.APIServerID, sv.EncodingVersion))
		} else if len(list) == max {
			more = true
		}
		count++
	}
	return listWithMoreString(list, more, count, max)
}

func printStorageVersionList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &apiserverinternalv1alpha1.StorageVersionList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to StorageVersionList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printStorageVersion(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printPriorityLevelConfiguration(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &flowcontrolv1beta3.PriorityLevelConfiguration{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to PriorityLevelConfiguration %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}
	name := obj.Name
	ncs := interface{}("<none>")
	queues := interface{}("<none>")
	handSize := interface{}("<none>")
	queueLengthLimit := interface{}("<none>")
	if obj.Spec.Limited != nil {
		ncs = obj.Spec.Limited.NominalConcurrencyShares
		if qc := obj.Spec.Limited.LimitResponse.Queuing; qc != nil {
			queues = qc.Queues
			handSize = qc.HandSize
			queueLengthLimit = qc.QueueLengthLimit
		}
	}
	row.Cells = append(row.Cells, name, string(obj.Spec.Type), ncs, queues, handSize, queueLengthLimit, translateTimestampSince(obj.CreationTimestamp))

	return []metav1.TableRow{row}, nil
}

func printPriorityLevelConfigurationList(unstructList *unstructured.UnstructuredList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	list := &flowcontrolv1beta3.PriorityLevelConfigurationList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructList.UnstructuredContent(), list); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to PriorityLevelConfigurationList %v", err)
	}
	rows := make([]metav1.TableRow, 0, len(list.Items))
	for i := range list.Items {
		r, err := printPriorityLevelConfiguration(&unstructList.Items[i], options)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func printScale(unstruct *unstructured.Unstructured, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	obj := &autoscalingv1.Scale{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstruct.UnstructuredContent(), obj); err != nil {
		return nil, fmt.Errorf("unable to convert unstructured object to Scale %v", err)
	}
	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: obj},
	}
	row.Cells = append(row.Cells, obj.Name, obj.Spec.Replicas, obj.Status.Replicas, translateTimestampSince(obj.CreationTimestamp))
	return []metav1.TableRow{row}, nil
}

func printBoolPtr(value *bool) string {
	if value != nil {
		return printBool(*value)
	}

	return "<unset>"
}

func printBool(value bool) string {
	if value {
		return "True"
	}

	return "False"
}

// SortableResourceNames - An array of sortable resource names
type SortableResourceNames []corev1.ResourceName

func (list SortableResourceNames) Len() int {
	return len(list)
}

func (list SortableResourceNames) Swap(i, j int) {
	list[i], list[j] = list[j], list[i]
}

func (list SortableResourceNames) Less(i, j int) bool {
	return list[i] < list[j]
}

// SubjectsStrings returns users, groups, serviceaccounts, unknown for display purposes.
func SubjectsStrings(subjects []rbacv1.Subject) ([]string, []string, []string, []string) {
	users := []string{}
	groups := []string{}
	sas := []string{}
	others := []string{}

	for _, subject := range subjects {
		switch subject.Kind {
		case rbacv1.ServiceAccountKind:
			sas = append(sas, fmt.Sprintf("%s/%s", subject.Namespace, subject.Name))

		case rbacv1.UserKind:
			users = append(users, subject.Name)

		case rbacv1.GroupKind:
			groups = append(groups, subject.Name)

		default:
			others = append(others, fmt.Sprintf("%s/%s/%s", subject.Kind, subject.Namespace, subject.Name))
		}
	}

	return users, groups, sas, others
}

func ObjectConvertToUnstructured(object runtime.Object) (*unstructured.Unstructured, error) {
	var raw []byte
	var err error
	if raw, err = pkgutils.Marshal(object); err != nil {
		return nil, err
	}
	unstructed := &unstructured.Unstructured{}
	err = unstructed.UnmarshalJSON(raw)
	if err != nil {
		return nil, err
	}
	return unstructed, nil
}
