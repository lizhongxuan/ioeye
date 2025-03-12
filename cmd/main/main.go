package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/yourname/ioeye/pkg/ebpf"
	"github.com/yourname/ioeye/pkg/k8s"
	"github.com/yourname/ioeye/pkg/monitor"
)

func main() {
	// 命令行参数
	kubeconfig := flag.String("kubeconfig", "", "Path to kubeconfig file")
	namespace := flag.String("namespace", "", "Namespace to monitor (empty for all)")
	interval := flag.Int("interval", 10, "Metrics collection interval in seconds")
	flag.Parse()

	log.Println("Starting IOEye - eBPF driven storage performance optimizer")

	// 创建上下文，支持优雅退出
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 初始化Kubernetes客户端
	k8sClient, err := k8s.NewClient(*kubeconfig)
	if err != nil {
		log.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	// 初始化eBPF子系统
	bpfMonitor, err := ebpf.NewMonitor()
	if err != nil {
		log.Fatalf("Failed to initialize eBPF monitor: %v", err)
	}
	defer bpfMonitor.Close()

	// 初始化存储性能监控系统
	storageMonitor := monitor.NewStorageMonitor(
		bpfMonitor,
		k8sClient,
		monitor.WithNamespace(*namespace),
		monitor.WithInterval(*interval),
	)

	// 启动监控
	if err := storageMonitor.Start(ctx); err != nil {
		log.Fatalf("Failed to start storage monitor: %v", err)
	}

	// 等待信号退出
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down IOEye...")
} 