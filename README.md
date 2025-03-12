# IOEye

IOEye是一个基于eBPF和Golang的云原生存储性能监控与优化系统，专注于解决容器化环境中存储性能抖动问题，提升应用响应速度。

## 核心功能

1. **Pod存储性能监控**：实时监控Kubernetes集群中各个Pod的存储性能指标，包括IOPS、吞吐量和延迟。
2. **CSI I/O路径分析**：深入监控Kubernetes CSI存储的I/O路径，分析延迟来源（队列、磁盘、网络）。
3. **性能瓶颈识别**：通过eBPF技术跟踪内核中存储I/O路径，精确定位性能瓶颈。
4. **可视化与告警**：提供直观的性能指标可视化界面和异常情况告警机制。

## 技术架构

- **eBPF程序**：内核态收集存储I/O路径数据
- **Agent组件**：用户态收集和分析数据
- **Kubernetes集成**：通过CRD和Operator模式实现与K8s的无缝集成
- **可视化与API接口**：提供RESTful API和Web界面



## 开发环境要求

- Go 1.21+
- LLVM/Clang 11+
- Linux内核 5.10+
- Kubernetes 1.22+
