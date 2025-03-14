# IOEye 存储性能实时监控功能

本文档提供了有关如何使用IOEye实时监控Kubernetes集群中Pod存储性能的说明。

## 功能概述

IOEye使用eBPF技术实时监控Kubernetes Pod的存储性能指标，包括：

- **延迟指标**：读延迟、写延迟、队列延迟、磁盘延迟（纳秒）
- **IOPS指标**：读IOPS、写IOPS、总IOPS
- **吞吐量指标**：读吞吐量、写吞吐量、总吞吐量（字节/秒）

这些指标从Linux内核层面收集，提供了对存储I/O路径的深入可见性，有助于识别性能瓶颈。

## 部署方式

IOEye通过DaemonSet部署到集群中的每个节点上，并提供API接口来查询指标。可以使用以下命令部署：

```bash
kubectl apply -f deployments/ioeye-daemonset.yaml
kubectl apply -f deployments/ioeye-service.yaml
```

## API接口

IOEye提供了RESTful API来查询和监控存储性能指标：

### 1. 获取所有Pod的存储指标

```
GET /api/v1/metrics
```

示例响应：

```json
{
  "timestamp": "2023-05-15T10:21:30Z",
  "pod_metrics": {
    "nginx-pod-1": {
      "pod_name": "nginx-pod-1",
      "namespace": "default",
      "read_latency_ns": 1500000,
      "write_latency_ns": 2500000,
      "read_iops": 150,
      "write_iops": 50,
      "read_throughput_bps": 5242880,
      "write_throughput_bps": 1048576,
      "queue_latency_ns": 500000,
      "disk_latency_ns": 1200000,
      "timestamp": "2023-05-15T10:21:25Z"
    }
  },
  "top_slow_pods": [
    {
      "pod_name": "mongodb-0",
      "namespace": "db",
      "read_latency_ns": 3500000,
      "write_latency_ns": 4500000,
      "read_iops": 200,
      "write_iops": 100,
      "read_throughput_bps": 3145728,
      "write_throughput_bps": 1048576,
      "queue_latency_ns": 700000,
      "disk_latency_ns": 1500000,
      "timestamp": "2023-05-15T10:21:25Z"
    }
  ],
  "bottlenecks": {
    "nginx-pod-1": "none",
    "mongodb-0": "disk"
  },
  "anomalies": {
    "nginx-pod-1": false,
    "mongodb-0": true
  }
}
```

### 2. 获取特定Pod的存储指标

```
GET /api/v1/metrics/pod/{pod_name}
```

示例响应：

```json
{
  "timestamp": "2023-05-15T10:22:30Z",
  "pod_metrics": {
    "pod_name": "nginx-pod-1",
    "namespace": "default",
    "read_latency_ns": 1500000,
    "write_latency_ns": 2500000,
    "read_iops": 150,
    "write_iops": 50,
    "read_throughput_bps": 5242880,
    "write_throughput_bps": 1048576,
    "queue_latency_ns": 500000,
    "disk_latency_ns": 1200000,
    "timestamp": "2023-05-15T10:22:25Z"
  },
  "bottleneck": "none",
  "anomaly": false,
  "trend": {
    "direction": "stable",
    "change_percent": 2.5,
    "period": "5m"
  }
}
```

### 3. 获取延迟最高的Pod

```
GET /api/v1/metrics/topslow
```

示例响应：

```json
{
  "timestamp": "2023-05-15T10:23:30Z",
  "top_slow_pods": [
    {
      "pod_name": "mongodb-0",
      "namespace": "db",
      "read_latency_ns": 3500000,
      "write_latency_ns": 4500000,
      "read_iops": 200,
      "write_iops": 100,
      "read_throughput_bps": 3145728,
      "write_throughput_bps": 1048576,
      "queue_latency_ns": 700000,
      "disk_latency_ns": 1500000,
      "timestamp": "2023-05-15T10:23:25Z"
    }
  ]
}
```

## 监控集成

IOEye可以与Prometheus和Grafana集成，提供更丰富的可视化体验：

### Prometheus集成

部署ServiceMonitor（已包含在部署清单中）：

```bash
kubectl apply -f deployments/ioeye-service.yaml
```

这将创建一个ServiceMonitor，Prometheus会自动抓取IOEye的指标。

### Grafana仪表板

可以导入预构建的Grafana仪表板来可视化存储性能指标：

1. 在Grafana中，点击"+"按钮并选择"Import"
2. 输入仪表板ID：12345（示例ID，请以实际发布的仪表板ID为准）
3. 选择Prometheus数据源
4. 点击"Import"

## 使用示例

### 识别高延迟Pod

使用API或Grafana仪表板查看延迟最高的Pod：

```bash
curl http://<ioeye-api-ingress-host>/ioeye/api/v1/metrics/topslow
```

### 分析I/O瓶颈

对于高延迟的Pod，分析其瓶颈来源：

```bash
curl http://<ioeye-api-ingress-host>/ioeye/api/v1/metrics/pod/<pod-name>
```

观察返回的`bottleneck`字段，可能的值包括：
- `queue`: I/O队列是瓶颈
- `disk`: 磁盘设备是瓶颈
- `network`: 网络存储是瓶颈
- `unknown`: 无法确定瓶颈来源
- `none`: 没有明显瓶颈

### 检测性能异常

定期检查是否有Pod出现性能异常：

```bash
curl http://<ioeye-api-ingress-host>/ioeye/api/v1/metrics | jq '.anomalies'
```

## 故障排除

### API服务不可用

检查IOEye DaemonSet和API Service的状态：

```bash
kubectl get pods -n kube-system -l app=ioeye-agent
kubectl get service -n kube-system ioeye-api
kubectl get ingress -n kube-system ioeye-api
```

### 没有指标数据

检查eBPF程序是否正常工作：

```bash
kubectl logs -n kube-system -l app=ioeye-agent
```

## 参考资料

- [IOEye GitHub 仓库](https://github.com/lizhongxuan/ioeye)
- [eBPF 技术文档](https://ebpf.io/what-is-ebpf/)
- [Kubernetes 存储文档](https://kubernetes.io/docs/concepts/storage/) 