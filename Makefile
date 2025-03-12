.PHONY: all build clean generate docker-build docker-push deploy

# 变量
BINARY_NAME=ioeye-agent
DOCKER_REPO=lizhongxuan/ioeye
DOCKER_TAG=latest

# 默认目标
all: generate build

# 生成eBPF代码
generate:
	@echo "生成eBPF代码..."
	cd pkg/ebpf && go generate ./...

# 构建程序
build:
	@echo "构建 $(BINARY_NAME)..."
	CGO_ENABLED=1 go build -o bin/$(BINARY_NAME) ./cmd/main

# 运行测试
test:
	@echo "运行测试..."
	go test -v ./...

# 清理
clean:
	@echo "清理..."
	rm -rf bin/
	rm -f pkg/ebpf/bpf_*.go

# 构建Docker镜像
docker-build:
	@echo "构建Docker镜像..."
	docker build -t $(DOCKER_REPO):$(DOCKER_TAG) .

# 推送Docker镜像
docker-push:
	@echo "推送Docker镜像..."
	docker push $(DOCKER_REPO):$(DOCKER_TAG)

# 部署到Kubernetes
deploy:
	@echo "部署到Kubernetes..."
	kubectl apply -f deployments/ioeye-daemonset.yaml

# 下载依赖
deps:
	@echo "下载依赖..."
	go mod tidy 