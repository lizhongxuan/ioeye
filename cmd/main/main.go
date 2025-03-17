package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lizhongxuan/ioeye/pkg/analyzer"
	"github.com/lizhongxuan/ioeye/pkg/api"
	"github.com/lizhongxuan/ioeye/pkg/ebpf"
	"github.com/lizhongxuan/ioeye/pkg/k8s"
	"github.com/lizhongxuan/ioeye/pkg/monitor"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	// 命令行参数
	kubeconfig := flag.String("kubeconfig", "", "Path to kubeconfig file")
	namespace := flag.String("namespace", "", "Namespace to monitor (empty for all)")
	interval := flag.Int("interval", 10, "Metrics collection interval in seconds")
	apiAddr := flag.String("api-addr", ":8080", "Address to bind API server")
	flag.Parse()

	// 初始化zap日志，配置输出格式和代码行号
	// 创建自定义编码器配置
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "time"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder

	// 创建Core
	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		zapcore.AddSync(os.Stdout),
		zapcore.InfoLevel,
	)

	// 创建Logger，启用调用者信息（文件名和行号）
	logger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(0))
	defer logger.Sync() // 刷新缓冲区
	
	// 替换全局logger
	zap.ReplaceGlobals(logger)

	zap.L().Info("Starting IOEye - eBPF driven storage performance optimizer")

	// 创建上下文，支持优雅退出
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 初始化Kubernetes客户端
	zap.L().Info("Initializing Kubernetes client...")
	k8sClient, err := k8s.NewClient(*kubeconfig)
	if err != nil {
		zap.L().Error("Failed to create Kubernetes client", zap.Error(err))
		os.Exit(1)
	}

	// 初始化eBPF子系统
	zap.L().Info("Initializing eBPF monitor...")
	bpfMonitor, err := ebpf.NewMonitor()
	if err != nil {
		zap.L().Error("Failed to initialize eBPF monitor", zap.Error(err))
		os.Exit(1)
	}
	defer bpfMonitor.Close()

	// 启动eBPF监控
	zap.L().Info("Starting eBPF monitor...")
	if err := bpfMonitor.Start(); err != nil {
		zap.L().Error("Failed to start eBPF monitor", zap.Error(err))
		os.Exit(1)
	}

	// 初始化存储性能监控系统
	zap.L().Info("Initializing storage monitor...")
	storageMonitor := monitor.NewStorageMonitor(
		bpfMonitor,
		k8sClient,
		monitor.WithNamespace(*namespace),
		monitor.WithInterval(*interval),
	)

	// 初始化存储性能分析器
	zap.L().Info("Initializing storage analyzer...")
	storageAnalyzer := analyzer.NewStorageAnalyzer(
		analyzer.WithMaxHistoryPerPod(100),    // 保存100个历史数据点
		analyzer.WithAnomalyThreshold(2.0),    // 标准差阈值
	)

	// 启动API服务器
	zap.L().Info("Starting API server", zap.String("address", *apiAddr))
	apiServer := api.NewAPIServer(storageMonitor, storageAnalyzer, *apiAddr)
	go func() {
		if err := apiServer.Start(ctx); err != nil {
			zap.L().Error("Failed to start API server", zap.Error(err))
			os.Exit(1)
		}
	}()

	// 启动存储监控
	zap.L().Info("Starting storage monitor...")
	if err := storageMonitor.Start(ctx); err != nil {
		zap.L().Error("Failed to start storage monitor", zap.Error(err))
		os.Exit(1)
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
					zap.L().Info("Top slow pod detected",
						zap.String("pod", topSlowPods[0].PodName),
						zap.Uint64("read_latency_ns", topSlowPods[0].ReadLatency),
						zap.Uint64("write_latency_ns", topSlowPods[0].WriteLatency))
				}
				
			case <-ctx.Done():
				return
			}
		}
	}()

	// 打印可用的API端点
	zap.L().Info("Available API endpoints")
	zap.L().Info("- GET /api/v1/metrics            - Get all pod metrics")
	zap.L().Info("- GET /api/v1/metrics/pod/{name} - Get specific pod metrics")
	zap.L().Info("- GET /api/v1/metrics/topslow    - Get top slow pods")
	zap.L().Info("- GET /api/v1/health             - Health check")

	// 等待信号退出
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	zap.L().Info("Shutting down IOEye...")
	
	// 优雅关闭
	apiServer.Stop()
	storageMonitor.Stop()
} 