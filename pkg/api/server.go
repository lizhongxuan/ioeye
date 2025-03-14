package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/lizhongxuan/ioeye/pkg/analyzer"
	"github.com/lizhongxuan/ioeye/pkg/monitor"
)

// Server 代表API服务器
type Server struct {
	httpServer    *http.Server
	storageMonitor *monitor.StorageMonitor
	storageAnalyzer *analyzer.StorageAnalyzer
	address       string
}

// PodMetricsResponse 是Pod指标的API响应格式
type PodMetricsResponse struct {
	Timestamp    time.Time                        `json:"timestamp"`
	PodMetrics   map[string]*PodMetrics           `json:"pod_metrics"`
	TopSlowPods  []*PodMetrics                    `json:"top_slow_pods,omitempty"`
	Bottlenecks  map[string]string                `json:"bottlenecks,omitempty"`
	Anomalies    map[string]bool                  `json:"anomalies,omitempty"`
}

// PodMetrics 包含单个Pod的存储性能指标
type PodMetrics struct {
	PodName         string    `json:"pod_name"`
	Namespace       string    `json:"namespace"`
	ReadLatency     uint64    `json:"read_latency_ns"`
	WriteLatency    uint64    `json:"write_latency_ns"`
	ReadIOPS        uint64    `json:"read_iops"`
	WriteIOPS       uint64    `json:"write_iops"`
	ReadThroughput  uint64    `json:"read_throughput_bps"`
	WriteThroughput uint64    `json:"write_throughput_bps"`
	QueueLatency    uint64    `json:"queue_latency_ns,omitempty"`
	DiskLatency     uint64    `json:"disk_latency_ns,omitempty"`
	NetworkLatency  uint64    `json:"network_latency_ns,omitempty"`
	Timestamp       time.Time `json:"timestamp"`
}

// NewAPIServer 创建一个新的API服务器
func NewAPIServer(storageMonitor *monitor.StorageMonitor, storageAnalyzer *analyzer.StorageAnalyzer, address string) *Server {
	if address == "" {
		address = ":8080" // 默认监听所有接口的8080端口
	}
	
	return &Server{
		storageMonitor: storageMonitor,
		storageAnalyzer: storageAnalyzer,
		address:       address,
	}
}

// Start 启动API服务器
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	
	// 注册API路由
	mux.HandleFunc("/api/v1/metrics", s.handleGetAllMetrics)
	mux.HandleFunc("/api/v1/metrics/pod/", s.handleGetPodMetrics)
	mux.HandleFunc("/api/v1/metrics/topslow", s.handleGetTopSlowPods)
	mux.HandleFunc("/api/v1/health", s.handleHealth)
	
	s.httpServer = &http.Server{
		Addr:    s.address,
		Handler: mux,
	}
	
	// 在后台启动HTTP服务器
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("HTTP server error: %v\n", err)
		}
	}()
	
	fmt.Printf("API server started on %s\n", s.address)
	
	// 等待上下文取消信号
	<-ctx.Done()
	
	// 优雅关闭HTTP服务器
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	return s.httpServer.Shutdown(shutdownCtx)
}

// Stop 停止API服务器
func (s *Server) Stop() error {
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// handleGetAllMetrics 处理获取所有Pod指标的请求
func (s *Server) handleGetAllMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	// 从存储监控器获取所有Pod的指标
	allPodMetrics := s.storageMonitor.GetAllMetrics()
	
	// 转换为API响应格式
	podMetricsMap := make(map[string]*PodMetrics)
	bottlenecks := make(map[string]string)
	anomalies := make(map[string]bool)
	
	for podName, metrics := range allPodMetrics {
		podMetricsMap[podName] = convertToPodMetrics(metrics)
		
		// 获取瓶颈类型
		if s.storageAnalyzer != nil {
			bottleneckType := s.storageAnalyzer.GetBottleneckType(podName)
			bottlenecks[podName] = string(bottleneckType)
			
			// 获取异常检测结果
			anomalies[podName] = s.storageAnalyzer.HasAnomalyDetected(podName)
		}
	}
	
	// 获取延迟最高的5个Pod
	var topSlowPods []*PodMetrics
	if s.storageAnalyzer != nil {
		slowPods := s.storageAnalyzer.GetTopNSlowPods(5)
		for _, pod := range slowPods {
			topSlowPods = append(topSlowPods, convertToPodMetrics(pod))
		}
	}
	
	response := PodMetricsResponse{
		Timestamp:   time.Now(),
		PodMetrics:  podMetricsMap,
		TopSlowPods: topSlowPods,
		Bottlenecks: bottlenecks,
		Anomalies:   anomalies,
	}
	
	// 返回JSON响应
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// handleGetPodMetrics 处理获取单个Pod指标的请求
func (s *Server) handleGetPodMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	// 从URL路径中提取Pod名称
	podName := r.URL.Path[len("/api/v1/metrics/pod/"):]
	if podName == "" {
		http.Error(w, "Pod name is required", http.StatusBadRequest)
		return
	}
	
	// 获取指定Pod的指标
	metrics, err := s.storageMonitor.GetPodMetrics(podName)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get metrics for pod %s: %v", podName, err), http.StatusNotFound)
		return
	}
	
	// 转换为API响应格式
	podMetrics := convertToPodMetrics(metrics)
	
	// 添加瓶颈和异常信息
	bottleneck := ""
	var anomaly bool
	
	if s.storageAnalyzer != nil {
		bottleneck = string(s.storageAnalyzer.GetBottleneckType(podName))
		anomaly = s.storageAnalyzer.HasAnomalyDetected(podName)
	}
	
	// 构建响应
	response := map[string]interface{}{
		"timestamp":  time.Now(),
		"pod_metrics": podMetrics,
		"bottleneck": bottleneck,
		"anomaly":    anomaly,
	}
	
	// 如果存储分析器可用，添加趋势信息
	if s.storageAnalyzer != nil {
		trend, change, err := s.storageAnalyzer.GetLatencyTrend(podName, 5*time.Minute)
		if err == nil {
			response["trend"] = map[string]interface{}{
				"direction":      trend,
				"change_percent": change,
				"period":         "5m",
			}
		}
	}
	
	// 返回JSON响应
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// handleGetTopSlowPods 处理获取延迟最高的Pod请求
func (s *Server) handleGetTopSlowPods(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	// 默认返回前5个延迟最高的Pod
	limit := 5
	
	var slowPods []*PodMetrics
	
	if s.storageAnalyzer != nil {
		// 获取延迟最高的Pod
		topSlowPodsMetrics := s.storageAnalyzer.GetTopNSlowPods(limit)
		
		// 转换为API响应格式
		for _, pod := range topSlowPodsMetrics {
			slowPods = append(slowPods, convertToPodMetrics(pod))
		}
	}
	
	// 构建响应
	response := map[string]interface{}{
		"timestamp": time.Now(),
		"top_slow_pods": slowPods,
	}
	
	// 返回JSON响应
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// handleHealth 处理健康检查请求
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	response := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now(),
	}
	
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// 辅助函数，将内部指标结构转换为API响应结构
func convertToPodMetrics(metrics *monitor.PodStorageMetrics) *PodMetrics {
	return &PodMetrics{
		PodName:         metrics.PodName,
		Namespace:       metrics.Namespace,
		ReadLatency:     metrics.ReadLatency,
		WriteLatency:    metrics.WriteLatency,
		ReadIOPS:        metrics.ReadIOPS,
		WriteIOPS:       metrics.WriteIOPS,
		ReadThroughput:  metrics.ReadThroughput,
		WriteThroughput: metrics.WriteThroughput,
		QueueLatency:    metrics.QueueLatency,
		DiskLatency:     metrics.DiskLatency,
		NetworkLatency:  metrics.NetworkLatency,
		Timestamp:       metrics.Timestamp,
	}
} 