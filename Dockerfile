FROM golang:1.21-alpine AS builder

# 安装依赖
RUN apk add --no-cache git gcc musl-dev llvm clang make

WORKDIR /app

# 首先复制go.mod和go.sum来缓存依赖
COPY go.mod ./
COPY go.sum ./

# 下载所有依赖
RUN go mod download

# 复制源代码
COPY . .

# 生成eBPF对象
RUN cd pkg/ebpf && go generate ./...

# 构建二进制文件
RUN CGO_ENABLED=1 GOOS=linux go build -a -ldflags '-linkmode external -extldflags "-static"' -o ioeye-agent ./cmd/main

# 使用Alpine作为最终镜像
FROM alpine:3.16

# 安装运行时依赖
RUN apk add --no-cache ca-certificates

WORKDIR /

# 从builder阶段复制二进制文件
COPY --from=builder /app/ioeye-agent /ioeye-agent
COPY --from=builder /app/bpf/io_tracer.c /bpf/io_tracer.c

# 设置entrypoint
ENTRYPOINT ["/ioeye-agent"] 