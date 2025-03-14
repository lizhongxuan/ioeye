package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lizhongxuan/ioeye/pkg/analyzer"
	"github.com/lizhongxuan/ioeye/pkg/api"
	"github.com/lizhongxuan/ioeye/pkg/ebpf"
	"github.com/lizhongxuan/ioeye/pkg/k8s"
	"github.com/lizhongxuan/ioeye/pkg/monitor"
)

func main() {
	// 命令行参数
	kubeconfig := flag.String("kubeconfig", "", "Path to kubeconfig file")
	namespace := flag.String("namespace", "", "Namespace to monitor (empty for all)")
	interval := flag.Int("interval", 10, "Metrics collection interval in seconds")
	apiAddr := flag.String("api-addr", ":8080", "Address to bind API server")
	flag.Parse()

	log.Println("Starting IOEye - eBPF driven storage performance optimizer")

	// 创建上下文，支持优雅退出
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 初始化Kubernetes客户端
	log.Println("Initializing Kubernetes client...")
	k8sClient, err := k8s.NewClient(*kubeconfig)
	if err != nil {
		log.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	// 初始化eBPF子系统
	log.Println("Initializing eBPF monitor...")
	bpfMonitor, err := ebpf.NewMonitor()
	if err != nil {
		log.Fatalf("Failed to initialize eBPF monitor: %v", err)
	}
	defer bpfMonitor.Close()

	// 启动eBPF监控
	log.Println("Starting eBPF monitor...")
	if err := bpfMonitor.Start(); err != nil {
		log.Fatalf("Failed to start eBPF monitor: %v", err)
	}

	// 初始化存储性能监控系统
	log.Println("Initializing storage monitor...")
	storageMonitor := monitor.NewStorageMonitor(
		bpfMonitor,
		k8sClient,
		monitor.WithNamespace(*namespace),
		monitor.WithInterval(*interval),
	)

	// 初始化存储性能分析器
	log.Println("Initializing storage analyzer...")
	storageAnalyzer := analyzer.NewStorageAnalyzer(
		analyzer.WithMaxHistoryPerPod(100),    // 保存100个历史数据点
		analyzer.WithAnomalyThreshold(2.0),    // 标准差阈值
	)

	// 启动API服务器
	log.Printf("Starting API server on %s...", *apiAddr)
	apiServer := api.NewAPIServer(storageMonitor, storageAnalyzer, *apiAddr)
	go func() {
		if err := apiServer.Start(ctx); err != nil {
			log.Fatalf("Failed to start API server: %v", err)
		}
	}()

	// 启动存储监控
	log.Println("Starting storage monitor...")
	if err := storageMonitor.Start(ctx); err != nil {
		log.Fatalf("Failed to start storage monitor: %v", err)
	}

	// 启动数据分析goroutine
	go func() {
		ticker := time.NewTicker(time.Duration(*interval) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// 获取所有Pod的最新指标
				allMetrics := storageMonitor.GetAllMetrics()
				
				// 更新存储分析器
				storageAnalyzer.AddMetrics(allMetrics)
				
				// 获取分析结果示例
				topSlowPods := storageAnalyzer.GetTopNSlowPods(5)
				if len(topSlowPods) > 0 {
					log.Printf("Top slow pod: %s (read latency: %d ns, write latency: %d ns)",
						topSlowPods[0].PodName, topSlowPods[0].ReadLatency, topSlowPods[0].WriteLatency)
				}
				
			case <-ctx.Done():
				return
			}
		}
	}()

	// 打印可用的API端点
	log.Println("Available API endpoints:")
	log.Println("  - GET /api/v1/metrics            - Get all pod metrics")
	log.Println("  - GET /api/v1/metrics/pod/{name} - Get specific pod metrics")
	log.Println("  - GET /api/v1/metrics/topslow    - Get top slow pods")
	log.Println("  - GET /api/v1/health             - Health check")

	// 等待信号退出
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down IOEye...")
	
	// 优雅关闭
	apiServer.Stop()
	storageMonitor.Stop()
} 