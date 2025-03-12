package k8s

import (
	"fmt"
	"os"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Client 封装Kubernetes客户端
type Client struct {
	clientset *kubernetes.Clientset
}

// NewClient 创建一个新的Kubernetes客户端
func NewClient(kubeconfigPath string) (*Client, error) {
	var config *rest.Config
	var err error

	if kubeconfigPath == "" {
		// 尝试集群内运行模式
		config, err = rest.InClusterConfig()
		if err != nil {
			// 如果在集群外运行，尝试使用默认的kubeconfig位置
			homeDir, _ := os.UserHomeDir()
			kubeconfigPath = filepath.Join(homeDir, ".kube", "config")
		}
	}

	if config == nil {
		// 使用提供的kubeconfig或默认路径
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to build kubeconfig: %v", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %v", err)
	}

	return &Client{
		clientset: clientset,
	}, nil
}

// ListPods 列出特定命名空间中的所有Pod
func (c *Client) ListPods(namespace string) ([]string, error) {
	var podNames []string
	
	// 如果namespace为空，则列出所有命名空间的Pod
	ns := namespace
	if ns == "" {
		ns = metav1.NamespaceAll
	}
	
	pods, err := c.clientset.CoreV1().Pods(ns).List(metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %v", err)
	}
	
	for _, pod := range pods.Items {
		podNames = append(podNames, pod.Name)
	}
	
	return podNames, nil
}

// GetPodVolumes 获取特定Pod的卷信息
func (c *Client) GetPodVolumes(namespace, podName string) ([]string, error) {
	var volumeNames []string
	
	pod, err := c.clientset.CoreV1().Pods(namespace).Get(podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod %s: %v", podName, err)
	}
	
	for _, volume := range pod.Spec.Volumes {
		volumeNames = append(volumeNames, volume.Name)
	}
	
	return volumeNames, nil
}

// GetCSIDrivers 返回集群中所有的CSI驱动
func (c *Client) GetCSIDrivers() ([]string, error) {
	var driverNames []string
	
	// 需要使用CSI API获取驱动列表
	// 此处为简化示例，仅返回常见的一些CSI驱动名称
	driverNames = []string{
		"csi.aws.ebs.com",
		"pd.csi.storage.gke.io",
		"disk.csi.azure.com",
		"cinder.csi.openstack.org",
	}
	
	return driverNames, nil
} 