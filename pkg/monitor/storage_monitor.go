package monitor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lizhongxuan/ioeye/pkg/ebpf"
	"github.com/lizhongxuan/ioeye/pkg/k8s"
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
	metricsMutex  sync.RWMutex
	stopChan      chan struct{}
}

// PodStorageMetrics Pod存储性能指标
type PodStorageMetrics struct {
	PodName         string
	Namespace       string
	ReadLatency     uint64 // 纳秒
	WriteLatency    uint64 // 纳秒
	ReadIOPS        uint64
	WriteIOPS       uint64
	ReadThroughput  uint64 // 字节/秒
	WriteThroughput uint64 // 字节/秒
	QueueLatency    uint64 // 纳秒
	DiskLatency     uint64 // 纳秒
	NetworkLatency  uint64 // 纳秒
	Timestamp       time.Time
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
	sm.metricsMutex.RLock()
	defer sm.metricsMutex.RUnlock()
	
	metrics, ok := sm.metrics[podName]
	if !ok {
		return nil, fmt.Errorf("no metrics found for pod %s", podName)
	}
	
	// 返回副本而非原始对象
	metricsCopy := *metrics
	return &metricsCopy, nil
}

// GetAllMetrics 获取所有Pod的存储指标
func (sm *StorageMonitor) GetAllMetrics() map[string]*PodStorageMetrics {
	sm.metricsMutex.RLock()
	defer sm.metricsMutex.RUnlock()
	
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

	// 从eBPF获取基础I/O统计数据
	ioStatsData, err := sm.bpfMonitor.GetIOStatsData()
	if err != nil {
		return fmt.Errorf("failed to get I/O stats data: %v", err)
	}
	
	// 获取IOPS数据
	iopsData, err := sm.bpfMonitor.GetIOPS()
	if err != nil {
		return fmt.Errorf("failed to get IOPS data: %v", err)
	}
	
	// 获取吞吐量数据
	throughputData, err := sm.bpfMonitor.GetThroughput()
	if err != nil {
		return fmt.Errorf("failed to get throughput data: %v", err)
	}
	
	// 获取磁盘延迟数据
	diskLatencyData, err := sm.bpfMonitor.GetDiskLatencyData()
	if err != nil {
		return fmt.Errorf("failed to get disk latency data: %v", err)
	}

	// 获取队列延迟数据
	queueLatencyData, err := sm.bpfMonitor.GetQueueLatencyData()
	if err != nil {
		return fmt.Errorf("failed to get queue latency data: %v", err)
	}

	// 在更新指标前获取锁
	sm.metricsMutex.Lock()
	defer sm.metricsMutex.Unlock()

	// 生成指标
	now := time.Now()
	for _, podName := range pods {
		// 为每个Pod创建或更新指标对象
		metrics, ok := sm.metrics[podName]
		if !ok {
			metrics = &PodStorageMetrics{
				PodName:   podName,
				Namespace: sm.namespace,
			}
			sm.metrics[podName] = metrics
		}
		
		// 更新时间戳
		metrics.Timestamp = now
		
		// 填充基础I/O统计数据
		if ioStats, ok := ioStatsData[podName]; ok {
			metrics.ReadLatency = ioStats.ReadLatencyNs
			metrics.WriteLatency = ioStats.WriteLatencyNs
		}
		
		// 填充IOPS数据
		if iops, ok := iopsData[podName]; ok {
			metrics.ReadIOPS = iops["read_iops"]
			metrics.WriteIOPS = iops["write_iops"]
		}
		
		// 填充吞吐量数据
		if throughput, ok := throughputData[podName]; ok {
			metrics.ReadThroughput = throughput["read_throughput_bps"]
			metrics.WriteThroughput = throughput["write_throughput_bps"]
		}
		
		// 填充磁盘延迟数据
		if diskLatency, ok := diskLatencyData[podName]; ok {
			metrics.DiskLatency = diskLatency
		}
		
		// 填充队列延迟数据
		if queueLatency, ok := queueLatencyData[podName]; ok {
			metrics.QueueLatency = queueLatency
		}
	}

	return nil
}

// GetPodIOPS 获取特定Pod的IOPS指标
func (sm *StorageMonitor) GetPodIOPS(podName string) (readIOPS, writeIOPS uint64, err error) {
	metrics, err := sm.GetPodMetrics(podName)
	if err != nil {
		return 0, 0, err
	}
	
	return metrics.ReadIOPS, metrics.WriteIOPS, nil
}

// GetPodThroughput 获取特定Pod的吞吐量指标（字节/秒）
func (sm *StorageMonitor) GetPodThroughput(podName string) (readThroughput, writeThroughput uint64, err error) {
	metrics, err := sm.GetPodMetrics(podName)
	if err != nil {
		return 0, 0, err
	}
	
	return metrics.ReadThroughput, metrics.WriteThroughput, nil
}

// GetPodLatency 获取特定Pod的延迟指标（纳秒）
func (sm *StorageMonitor) GetPodLatency(podName string) (readLatency, writeLatency, queueLatency, diskLatency uint64, err error) {
	metrics, err := sm.GetPodMetrics(podName)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	
	return metrics.ReadLatency, metrics.WriteLatency, metrics.QueueLatency, metrics.DiskLatency, nil
}

// GetTopIOPSPods 获取IOPS最高的N个Pod
func (sm *StorageMonitor) GetTopIOPSPods(n int) []*PodStorageMetrics {
	sm.metricsMutex.RLock()
	defer sm.metricsMutex.RUnlock()
	
	// 创建一个Pod指标的切片
	pods := make([]*PodStorageMetrics, 0, len(sm.metrics))
	for _, metrics := range sm.metrics {
		podCopy := *metrics
		pods = append(pods, &podCopy)
	}
	
	// 按总IOPS（读+写）排序
	// 降序排列，最高的在前面
	for i := 0; i < len(pods)-1; i++ {
		for j := i + 1; j < len(pods); j++ {
			if (pods[i].ReadIOPS + pods[i].WriteIOPS) < (pods[j].ReadIOPS + pods[j].WriteIOPS) {
				pods[i], pods[j] = pods[j], pods[i]
			}
		}
	}
	
	// 返回前N个
	if n > len(pods) {
		n = len(pods)
	}
	
	return pods[:n]
}

// GetTopThroughputPods 获取吞吐量最高的N个Pod
func (sm *StorageMonitor) GetTopThroughputPods(n int) []*PodStorageMetrics {
	sm.metricsMutex.RLock()
	defer sm.metricsMutex.RUnlock()
	
	// 创建一个Pod指标的切片
	pods := make([]*PodStorageMetrics, 0, len(sm.metrics))
	for _, metrics := range sm.metrics {
		podCopy := *metrics
		pods = append(pods, &podCopy)
	}
	
	// 按总吞吐量（读+写）排序
	// 降序排列，最高的在前面
	for i := 0; i < len(pods)-1; i++ {
		for j := i + 1; j < len(pods); j++ {
			if (pods[i].ReadThroughput + pods[i].WriteThroughput) < (pods[j].ReadThroughput + pods[j].WriteThroughput) {
				pods[i], pods[j] = pods[j], pods[i]
			}
		}
	}
	
	// 返回前N个
	if n > len(pods) {
		n = len(pods)
	}
	
	return pods[:n]
}
