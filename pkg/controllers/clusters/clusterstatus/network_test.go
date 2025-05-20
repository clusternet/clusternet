package clusterstatus

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/uuid"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	corelisters "k8s.io/client-go/listers/core/v1"
)

func TestFindServiceIPRange(t *testing.T) {
	tests := []struct {
		description string
		buildPod    func(string, string) (*corev1.Pod, labels.Selector)
	}{
		{
			description: "service ip range in kube-apiserver",
			buildPod:    buildAPIServerPod,
		},
		{
			description: "service ip range in kube-controller-manager",
			buildPod:    buildControllerManagerWithK8sLabel,
		},
	}

	const expected = "1.0.0.0/8"

	for _, test := range tests {
		pod, _ := test.buildPod(expected, "0.0.0.0/0")

		podLister, err := buildPodLister(pod)
		if err != nil {
			t.Errorf("new pod lister error: %v", err)
			continue
		}

		result, _ := findServiceIPRange(podLister)
		if result != expected {
			t.Errorf("test for %s: expected %s, got %s", test.description, expected, result)
		}
	}
}

func TestFindPodIPRange(t *testing.T) {
	tests := []struct {
		description string
		buildPod    func(string, string) (*corev1.Pod, labels.Selector)
	}{
		{
			description: "pod ip range in kube-controller-manager",
			buildPod:    buildControllerManagerWithK8sLabel,
		},
		{
			description: "pod ip range in kube-proxy",
			buildPod:    buildProxyPod,
		},
	}

	const expected = "2.0.0.0/8"

	for _, test := range tests {
		pod, _ := test.buildPod("0.0.0.0/0", expected)

		podLister, err := buildPodLister(pod)
		if err != nil {
			t.Errorf("new pod lister error: %v", err)
			continue
		}

		result, _ := findPodIPRange(podLister)
		if result != expected {
			t.Errorf("test for %s: expected %s, got %s", test.description, expected, result)
		}
	}
}

func TestFindPodCommandParameter(t *testing.T) {
	tests := []struct {
		description string
		buildPod    func(string, string) (*corev1.Pod, labels.Selector)
	}{
		{
			description: "parameter in command",
			buildPod:    buildAPIServerPod,
		},
		{
			description: "parameter in args",
			buildPod:    buildAPIServerPodWithArgs,
		},
	}

	const parameter = "--service-cluster-ip-range"
	const expected = "1.0.0.0/8"

	for _, test := range tests {
		pod, labelSelector := test.buildPod(expected, "0.0.0.0/0")

		podLister, err := buildPodLister(pod)
		if err != nil {
			t.Errorf("test for %s error: %v", test.description, err)
			continue
		}

		result, err := findPodCommandParameter(podLister, labelSelector, parameter)
		if err != nil {
			t.Errorf("test for %s error: %v", test.description, err)
		} else if result != expected {
			t.Errorf("test for %s: expected %s, got %s", test.description, expected, result)
		}
	}
}

func buildPodLister(pods ...*corev1.Pod) (corelisters.PodLister, error) {
	kubeClientset := fake.NewSimpleClientset()
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClientset, 0)
	podInformer := kubeInformerFactory.Core().V1().Pods()

	for _, pod := range pods {
		err := podInformer.Informer().GetIndexer().Add(pod)
		if err != nil {
			return nil, err
		}
	}
	return podInformer.Lister(), nil
}

func buildAPIServerPod(serviceIPRange, _ string) (*corev1.Pod, labels.Selector) { // no --cluster-cidr parameter
	labelKey, labelValue := "component", "kube-apiserver"
	return buildPod("kube-system", labelValue+"-xxx", labelKey, labelValue,
		[]string{"kube-apiserver", "--service-cluster-ip-range=" + serviceIPRange},
		[]string{})
}

func buildAPIServerPodWithArgs(serviceIPRange, _ string) (*corev1.Pod, labels.Selector) { // no --cluster-cidr parameter
	labelKey, labelValue := "component", "kube-apiserver"
	return buildPod("kube-system", labelValue+"-xxx", labelKey, labelValue,
		[]string{"kube-apiserver"},
		[]string{"--service-cluster-ip-range=" + serviceIPRange})
}

func buildControllerManagerWithK8sLabel(serviceIPRange, podIPRange string) (*corev1.Pod, labels.Selector) {
	labelKey, labelValue := "app.kubernetes.io/component", "kube-controller-manager"
	return buildPod("kube-system", "kube-controller-manager-xxx", labelKey, labelValue,
		[]string{"kube-controller-manager", "--service-cluster-ip-range=" + serviceIPRange, "--cluster-cidr=" + podIPRange},
		[]string{})
}

func buildProxyPod(serviceIPRange, podIPRange string) (*corev1.Pod, labels.Selector) {
	return buildPod("kube-system", "kube-proxy-xxx", "k8s-app", "kube-proxy",
		[]string{"kube-controller-manager", "--service-cluster-ip-range=" + serviceIPRange, "--cluster-cidr=" + podIPRange},
		[]string{})
}

//nolint:unparam
func buildPod(namespace, name, labelKey, labelValue string, cmd, args []string) (*corev1.Pod, labels.Selector) {
	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			UID:       uuid.NewUUID(),
			Name:      name,
			Labels:    map[string]string{labelKey: labelValue},
		},
	}
	pod.Spec.Containers = []corev1.Container{
		{
			Command: cmd,
			Args:    args,
		},
	}
	return pod, labels.SelectorFromSet(pod.Labels)
}
