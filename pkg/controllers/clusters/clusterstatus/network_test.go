package clusterstatus

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

func TestFindPodCommandParameterFromArgs(t *testing.T) {
	podArgs := BuildPod("kube-apiserver", "component", "kube-apiserver")
	podArgs.Spec.Containers = []corev1.Container{
		{
			Args: []string{"--service-cluster-ip-range=1.2.3.4"},
		},
	}

	kubeClient := fake.NewSimpleClientset()
	kubeFactory := informers.NewSharedInformerFactory(kubeClient, 0)
	podInformer := kubeFactory.Core().V1().Pods()

	err := podInformer.Informer().GetIndexer().Add(podArgs)
	if err != nil {
		t.Errorf("Add pod err: %v", err)
	}

	podArgsLister := podInformer.Lister()

	tests := []struct {
		description string
		podLister   corelisters.PodLister
		expectedIP  string
	}{
		{
			description: "service-cluster-ip-range is in args",
			podLister:   podArgsLister,
			expectedIP:  "1.2.3.4",
		},
	}

	for _, test := range tests {
		clusterIPRange := findPodCommandParameter(test.podLister, "kube-apiserver", "--service-cluster-ip-range")
		if clusterIPRange != test.expectedIP {
			t.Errorf("get clusteriprange is %v", clusterIPRange)
		}
	}

}

func TestFindPodCommandParameterFromArgsWithK8sLabel(t *testing.T) {
	podArgs := BuildPod("kube-apiserver", "app.kubernetes.io/component", "kube-apiserver")
	podArgs.Spec.Containers = []corev1.Container{
		{
			Args: []string{"--service-cluster-ip-range=1.2.3.4"},
		},
	}

	kubeClient := fake.NewSimpleClientset()
	kubeFactory := informers.NewSharedInformerFactory(kubeClient, 0)
	podInformer := kubeFactory.Core().V1().Pods()

	err := podInformer.Informer().GetIndexer().Add(podArgs)
	if err != nil {
		t.Errorf("Add pod err: %v", err)
	}

	podArgsLister := podInformer.Lister()

	tests := []struct {
		description string
		podLister   corelisters.PodLister
		expectedIP  string
	}{
		{
			description: "service-cluster-ip-range is in args",
			podLister:   podArgsLister,
			expectedIP:  "1.2.3.4",
		},
	}

	for _, test := range tests {
		clusterIPRange := findPodCommandParameter(test.podLister, "kube-apiserver", "--service-cluster-ip-range")
		if clusterIPRange != test.expectedIP {
			t.Errorf("get clusteriprange is %v", clusterIPRange)
		}
	}

}

func TestFindPodCommandParameterFromCommand(t *testing.T) {
	podCommand := BuildPod("kube-apiserver", "component", "kube-apiserver")
	podCommand.Spec.Containers = []corev1.Container{
		{
			Command: []string{"--service-cluster-ip-range=1.2.3.4"},
		},
	}

	store := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{"namespace": cache.MetaNamespaceIndexFunc})
	lister := corelisters.NewPodLister(store)
	if err := store.Add(podCommand); err != nil {
		t.Errorf("add pod err %v", err)
	}

	tests := []struct {
		description string
		podLister   corelisters.PodLister
		expectedIP  string
	}{
		{
			description: "service-cluster-ip-range is in command",
			podLister:   lister,
			expectedIP:  "1.2.3.4",
		},
	}

	for _, test := range tests {
		clusterIPRange := findPodCommandParameter(test.podLister, "kube-apiserver", "--service-cluster-ip-range")
		if clusterIPRange != test.expectedIP {
			t.Errorf("get clusteriprange is %v", clusterIPRange)
		}
	}
}

func TestFindPodCommandParameterFromCommandWithK8sLabel(t *testing.T) {
	podCommand := BuildPod("kube-apiserver", "app.kubernetes.io/component", "kube-apiserver")
	podCommand.Spec.Containers = []corev1.Container{
		{
			Command: []string{"--service-cluster-ip-range=1.2.3.4"},
		},
	}

	store := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{"namespace": cache.MetaNamespaceIndexFunc})
	lister := corelisters.NewPodLister(store)
	if err := store.Add(podCommand); err != nil {
		t.Errorf("add pod err %v", err)
	}

	tests := []struct {
		description string
		podLister   corelisters.PodLister
		expectedIP  string
	}{
		{
			description: "service-cluster-ip-range is in command",
			podLister:   lister,
			expectedIP:  "1.2.3.4",
		},
	}

	for _, test := range tests {
		clusterIPRange := findPodCommandParameter(test.podLister, "kube-apiserver", "--service-cluster-ip-range")
		if clusterIPRange != test.expectedIP {
			t.Errorf("get clusteriprange is %v", clusterIPRange)
		}
	}
}

func BuildPod(name, labelKey, lableValue string) *corev1.Pod {
	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "kube-system",
			UID:       uuid.NewUUID(),
			Name:      name,
			Labels:    map[string]string{labelKey: lableValue},
		},
	}
	return pod
}
