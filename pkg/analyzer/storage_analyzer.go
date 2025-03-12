package analyzer

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/lizhongxuan/ioeye/pkg/monitor"
)

// LatencyThreshold 定义I/O延迟阈值（纳秒）
const (
	ReadLatencyThreshold  = 10 * 1000 * 1000 // 10ms
	WriteLatencyThreshold = 20 * 1000 * 1000 // 20ms
	QueueLatencyThreshold = 5 * 1000 * 1000  // 5ms
)

// BottleneckType 表示瓶颈类型
type BottleneckType string

const (
	BottleneckTypeNone    BottleneckType = "none"
	BottleneckTypeQueue   BottleneckType = "queue"
	BottleneckTypeDisk    BottleneckType = "disk"
	BottleneckTypeNetwork BottleneckType = "network"
	BottleneckTypeUnknown BottleneckType = "unknown"
)

// StorageAnalyzer 存储性能分析器
type StorageAnalyzer struct {
	mu               sync.RWMutex
	metricsHistory   map[string][]*monitor.PodStorageMetrics
	maxHistoryPerPod int
	podBottlenecks   map[string]BottleneckType
	anomalyDetected  map[string]bool
	anomalyThreshold float64 // 异常检测阈值
}

// NewStorageAnalyzer 创建新的存储性能分析器
func NewStorageAnalyzer(options ...func(*StorageAnalyzer)) *StorageAnalyzer {
	sa := &StorageAnalyzer{
		metricsHistory:   make(map[string][]*monitor.PodStorageMetrics),
		maxHistoryPerPod: 100, // 默认每个Pod保存100个历史数据点
		podBottlenecks:   make(map[string]BottleneckType),
		anomalyDetected:  make(map[string]bool),
		anomalyThreshold: 2.0, // 默认标准差阈值
	}

	// 应用选项
	for _, option := range options {
		option(sa)
	}

	return sa
}

// WithMaxHistoryPerPod 设置每个Pod的最大历史记录数
func WithMaxHistoryPerPod(max int) func(*StorageAnalyzer) {
	return func(sa *StorageAnalyzer) {
		if max > 0 {
			sa.maxHistoryPerPod = max
		}
	}
}

// WithAnomalyThreshold 设置异常检测阈值
func WithAnomalyThreshold(threshold float64) func(*StorageAnalyzer) {
	return func(sa *StorageAnalyzer) {
		if threshold > 0 {
			sa.anomalyThreshold = threshold
		}
	}
}

// AddMetrics 添加新的指标数据
func (sa *StorageAnalyzer) AddMetrics(metrics map[string]*monitor.PodStorageMetrics) {
	sa.mu.Lock()
	defer sa.mu.Unlock()

	// 添加新数据
	for podName, podMetrics := range metrics {
		// 深拷贝指标
		metricsCopy := *podMetrics

		// 添加到历史记录
		sa.metricsHistory[podName] = append(sa.metricsHistory[podName], &metricsCopy)

		// 如果超出历史记录限制，则删除最旧的记录
		if len(sa.metricsHistory[podName]) > sa.maxHistoryPerPod {
			sa.metricsHistory[podName] = sa.metricsHistory[podName][1:]
		}

		// 分析瓶颈
		sa.podBottlenecks[podName] = sa.analyzeBottleneck(podMetrics)

		// 检测异常
		sa.anomalyDetected[podName] = sa.detectAnomaly(podName)
	}
}

// GetTopNSlowPods 获取延迟最高的N个Pod
func (sa *StorageAnalyzer) GetTopNSlowPods(n int) []*monitor.PodStorageMetrics {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	type podLatency struct {
		podName string
		latency uint64 // 总延迟（读+写）
		metrics *monitor.PodStorageMetrics
	}

	var latencies []podLatency

	// 获取每个Pod的最新指标
	for podName, history := range sa.metricsHistory {
		if len(history) == 0 {
			continue
		}

		latestMetrics := history[len(history)-1]
		totalLatency := latestMetrics.ReadLatency + latestMetrics.WriteLatency

		latencies = append(latencies, podLatency{
			podName: podName,
			latency: totalLatency,
			metrics: latestMetrics,
		})
	}

	// 按延迟排序
	sort.Slice(latencies, func(i, j int) bool {
		return latencies[i].latency > latencies[j].latency
	})

	// 获取前N个
	result := make([]*monitor.PodStorageMetrics, 0, n)
	for i := 0; i < n && i < len(latencies); i++ {
		result = append(result, latencies[i].metrics)
	}

	return result
}

// GetBottleneckType 获取Pod的瓶颈类型
func (sa *StorageAnalyzer) GetBottleneckType(podName string) BottleneckType {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	bottleneck, exists := sa.podBottlenecks[podName]
	if !exists {
		return BottleneckTypeUnknown
	}

	return bottleneck
}

// HasAnomalyDetected 检查Pod是否检测到异常
func (sa *StorageAnalyzer) HasAnomalyDetected(podName string) bool {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	anomaly, exists := sa.anomalyDetected[podName]
	if !exists {
		return false
	}

	return anomaly
}

// GetLatencyTrend 获取Pod的延迟趋势
func (sa *StorageAnalyzer) GetLatencyTrend(podName string, duration time.Duration) (trend string, change float64, err error) {
	sa.mu.RLock()
	defer sa.mu.RUnlock()

	history, exists := sa.metricsHistory[podName]
	if !exists || len(history) < 2 {
		return "unknown", 0, fmt.Errorf("insufficient data for pod %s", podName)
	}

	// 找到时间范围内的数据点
	now := time.Now()
	startTime := now.Add(-duration)

	var oldestInRange, latest *monitor.PodStorageMetrics
	latest = history[len(history)-1]

	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Timestamp.Before(startTime) {
			oldestInRange = history[i]
			break
		}
	}

	if oldestInRange == nil {
		oldestInRange = history[0]
	}

	// 计算总延迟变化
	oldTotalLatency := oldestInRange.ReadLatency + oldestInRange.WriteLatency
	newTotalLatency := latest.ReadLatency + latest.WriteLatency

	// 没有初始延迟的情况
	if oldTotalLatency == 0 {
		if newTotalLatency > 0 {
			return "increased", 100, nil
		}
		return "stable", 0, nil
	}

	// 计算变化百分比
	changePercent := (float64(newTotalLatency) - float64(oldTotalLatency)) / float64(oldTotalLatency) * 100

	// 确定趋势
	if changePercent > 10 {
		return "increased", changePercent, nil
	} else if changePercent < -10 {
		return "decreased", changePercent, nil
	}
	return "stable", changePercent, nil
}

// 内部方法

// analyzeBottleneck 分析存储瓶颈
func (sa *StorageAnalyzer) analyzeBottleneck(metrics *monitor.PodStorageMetrics) BottleneckType {
	// 首先检查是否有明显瓶颈
	if metrics.QueueLatency > QueueLatencyThreshold &&
		metrics.QueueLatency > metrics.DiskLatency &&
		metrics.QueueLatency > metrics.NetworkLatency {
		return BottleneckTypeQueue
	}

	if metrics.DiskLatency > metrics.QueueLatency &&
		metrics.DiskLatency > metrics.NetworkLatency {
		return BottleneckTypeDisk
	}

	if metrics.NetworkLatency > metrics.QueueLatency &&
		metrics.NetworkLatency > metrics.DiskLatency {
		return BottleneckTypeNetwork
	}

	// 如果没有明显瓶颈但存在高延迟
	if metrics.ReadLatency > ReadLatencyThreshold ||
		metrics.WriteLatency > WriteLatencyThreshold {
		return BottleneckTypeUnknown
	}

	return BottleneckTypeNone
}

// detectAnomaly 检测Pod存储性能异常
func (sa *StorageAnalyzer) detectAnomaly(podName string) bool {
	history, exists := sa.metricsHistory[podName]
	if !exists || len(history) < 10 { // 需要足够的历史数据
		return false
	}

	// 计算读写延迟的平均值和标准差
	var sumRead, sumWrite uint64
	for _, metrics := range history {
		sumRead += metrics.ReadLatency
		sumWrite += metrics.WriteLatency
	}

	avgRead := float64(sumRead) / float64(len(history))
	avgWrite := float64(sumWrite) / float64(len(history))

	var sumSqDiffRead, sumSqDiffWrite float64
	for _, metrics := range history {
		diffRead := float64(metrics.ReadLatency) - avgRead
		diffWrite := float64(metrics.WriteLatency) - avgWrite
		sumSqDiffRead += diffRead * diffRead
		sumSqDiffWrite += diffWrite * diffWrite
	}

	stdDevRead := sumSqDiffRead / float64(len(history))
	stdDevWrite := sumSqDiffWrite / float64(len(history))

	// 获取最新指标
	latest := history[len(history)-1]

	// 检查是否超过标准差阈值
	readZScore := (float64(latest.ReadLatency) - avgRead) / stdDevRead
	writeZScore := (float64(latest.WriteLatency) - avgWrite) / stdDevWrite

	// 如果任一延迟超过阈值
	if readZScore > sa.anomalyThreshold || writeZScore > sa.anomalyThreshold {
		return true
	}

	return false
}
