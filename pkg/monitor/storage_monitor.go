package monitor

import (
	"context"
	"fmt"
	"time"

	"github.com/yourname/ioeye/pkg/ebpf"
	"github.com/yourname/ioeye/pkg/k8s"
)

// StorageMonitorOption 配置存储监控器的选项
type StorageMonitorOption func(*StorageMonitor)

// StorageMonitor 存储性能监控器
type StorageMonitor struct {
	bpfMonitor    *ebpf.Monitor
	k8sClient     *k8s.Client
	namespace     string
	interval      int
	metrics       map[string]*PodStorageMetrics
	stopChan      chan struct{}
}

// PodStorageMetrics Pod存储性能指标
type PodStorageMetrics struct {
	PodName       string
	Namespace     string
	ReadLatency   uint64 // 纳秒
	WriteLatency  uint64 // 纳秒
	ReadIOPS      uint64
	WriteIOPS     uint64
	ReadThroughput uint64 // 字节/秒
	WriteThroughput uint64 // 字节/秒
	QueueLatency  uint64 // 纳秒
	DiskLatency   uint64 // 纳秒
	NetworkLatency uint64 // 纳秒
	Timestamp     time.Time
}

// WithNamespace 设置要监控的命名空间
func WithNamespace(namespace string) StorageMonitorOption {
	return func(sm *StorageMonitor) {
		sm.namespace = namespace
	}
}

// WithInterval 设置监控间隔（秒）
func WithInterval(interval int) StorageMonitorOption {
	return func(sm *StorageMonitor) {
		sm.interval = interval
	}
}

// NewStorageMonitor 创建新的存储性能监控器
func NewStorageMonitor(bpfMonitor *ebpf.Monitor, k8sClient *k8s.Client, opts ...StorageMonitorOption) *StorageMonitor {
	sm := &StorageMonitor{
		bpfMonitor: bpfMonitor,
		k8sClient:  k8sClient,
		interval:   10, // 默认10秒
		metrics:    make(map[string]*PodStorageMetrics),
		stopChan:   make(chan struct{}),
	}

	// 应用选项
	for _, opt := range opts {
		opt(sm)
	}

	return sm
}

// Start 启动存储性能监控
func (sm *StorageMonitor) Start(ctx context.Context) error {
	// 创建一个新的context，接收外部取消信号
	monitorCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// 启动监控goroutine
	go func() {
		ticker := time.NewTicker(time.Duration(sm.interval) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := sm.collectMetrics(); err != nil {
					fmt.Printf("Error collecting metrics: %v\n", err)
				}
			case <-monitorCtx.Done():
				return
			case <-sm.stopChan:
				return
			}
		}
	}()

	return nil
}

// Stop 停止监控
func (sm *StorageMonitor) Stop() {
	close(sm.stopChan)
}

// GetPodMetrics 获取特定Pod的存储指标
func (sm *StorageMonitor) GetPodMetrics(podName string) (*PodStorageMetrics, error) {
	metrics, ok := sm.metrics[podName]
	if !ok {
		return nil, fmt.Errorf("no metrics found for pod %s", podName)
	}
	return metrics, nil
}

// GetAllMetrics 获取所有Pod的存储指标
func (sm *StorageMonitor) GetAllMetrics() map[string]*PodStorageMetrics {
	// 返回metrics的拷贝
	result := make(map[string]*PodStorageMetrics, len(sm.metrics))
	for k, v := range sm.metrics {
		metricsCopy := *v
		result[k] = &metricsCopy
	}
	return result
}

// 内部方法

// collectMetrics 收集所有存储性能指标
func (sm *StorageMonitor) collectMetrics() error {
	// 从K8s获取Pod列表
	pods, err := sm.k8sClient.ListPods(sm.namespace)
	if err != nil {
		return fmt.Errorf("failed to list pods: %v", err)
	}

	// 从eBPF获取I/O延迟数据
	ioLatencyData, err := sm.bpfMonitor.GetIOLatencyData()
	if err != nil {
		return fmt.Errorf("failed to get I/O latency data: %v", err)
	}

	// 从eBPF获取队列延迟数据
	queueLatencyData, err := sm.bpfMonitor.GetQueueLatencyData()
	if err != nil {
		return fmt.Errorf("failed to get queue latency data: %v", err)
	}

	// 生成指标
	now := time.Now()
	for _, podName := range pods {
		// 在实际实现中，我们会将eBPF数据与Pod信息关联起来
		// 这里简化处理，假设我们能直接获取到对应关系
		
		metrics := &PodStorageMetrics{
			PodName:   podName,
			Namespace: sm.namespace,
			Timestamp: now,
		}
		
		// 填充I/O延迟数据
		if podData, ok := ioLatencyData[podName]; ok {
			metrics.ReadLatency = podData["read_latency_ns"]
			metrics.WriteLatency = podData["write_latency_ns"]
		}
		
		// 填充队列延迟数据（这里简化处理）
		// 在实际实现中，我们需要知道每个Pod对应的设备
		for _, queueLatency := range queueLatencyData {
			metrics.QueueLatency = queueLatency
			break
		}
		
		// 更新指标
		sm.metrics[podName] = metrics
	}
	
	return nil
} 