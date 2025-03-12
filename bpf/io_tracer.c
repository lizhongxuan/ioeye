#include <linux/bpf.h>
#include <linux/ptrace.h>
#include <linux/version.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>
#include <linux/blkdev.h>
#include <linux/fs.h>

// 定义数据结构
struct io_event_t {
    u64 ts;          // 时间戳
    u32 pid;         // 进程ID
    u32 tid;         // 线程ID
    u64 io_start;    // I/O开始时间
    u64 io_end;      // I/O结束时间
    u64 bytes;       // I/O字节数
    char comm[16];   // 进程名
    char disk[32];   // 磁盘设备名
    u8 operation;    // 操作类型 (0=read, 1=write)
    u8 io_type;      // I/O类型 (0=sync, 1=async)
};

// 定义延迟信息结构
struct latency_info_t {
    u64 total_read_ns;
    u64 total_write_ns;
    u64 count_read;
    u64 count_write;
};

// 定义eBPF映射

// 用于存储进行中的I/O请求
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 10240);
    __type(key, struct request *);
    __type(value, struct io_event_t);
} requests SEC(".maps");

// 按进程统计的I/O延迟
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 1024);
    __type(key, u32);
    __type(value, struct latency_info_t);
} latency_by_pid SEC(".maps");

// 用于事件输出的环形缓冲区
struct {
    __uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
    __uint(key_size, sizeof(int));
    __uint(value_size, sizeof(int));
} events SEC(".maps");

// 辅助函数
static __always_inline void update_latency_stats(u32 pid, u64 duration, u8 operation) {
    struct latency_info_t *latency, zero = {};
    
    latency = bpf_map_lookup_elem(&latency_by_pid, &pid);
    if (!latency) {
        bpf_map_update_elem(&latency_by_pid, &pid, &zero, BPF_ANY);
        latency = bpf_map_lookup_elem(&latency_by_pid, &pid);
        if (!latency)
            return;
    }
    
    if (operation == 0) { // read
        latency->total_read_ns += duration;
        latency->count_read += 1;
    } else if (operation == 1) { // write
        latency->total_write_ns += duration;
        latency->count_write += 1;
    }
}

// 跟踪块I/O请求开始
SEC("tracepoint/block/block_rq_issue")
int trace_block_rq_issue(struct trace_event_raw_block_rq_issue *ctx) {
    struct io_event_t io_event = {};
    struct request *req = (struct request *)ctx->rq;
    
    io_event.ts = bpf_ktime_get_ns();
    io_event.io_start = io_event.ts;
    io_event.pid = bpf_get_current_pid_tgid() >> 32;
    io_event.tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
    
    // 获取进程名称
    bpf_get_current_comm(&io_event.comm, sizeof(io_event.comm));
    
    // 确定操作类型
    unsigned int cmd_flags = BPF_CORE_READ(req, cmd_flags);
    if (cmd_flags & REQ_OP_WRITE)
        io_event.operation = 1; // write
    else
        io_event.operation = 0; // read
    
    // 存储请求信息供后续处理
    bpf_map_update_elem(&requests, &req, &io_event, BPF_ANY);
    
    return 0;
}

// 跟踪块I/O请求完成
SEC("tracepoint/block/block_rq_complete")
int trace_block_rq_complete(struct trace_event_raw_block_rq_complete *ctx) {
    struct request *req = (struct request *)ctx->rq;
    struct io_event_t *io_eventp, io_event = {};
    
    // 查找对应的开始事件
    io_eventp = bpf_map_lookup_elem(&requests, &req);
    if (!io_eventp)
        return 0;
    
    // 复制事件数据
    __builtin_memcpy(&io_event, io_eventp, sizeof(io_event));
    
    // 更新完成时间和延迟
    io_event.io_end = bpf_ktime_get_ns();
    u64 duration = io_event.io_end - io_event.io_start;
    
    // 更新统计信息
    update_latency_stats(io_event.pid, duration, io_event.operation);
    
    // 将事件发送到用户空间
    bpf_perf_event_output(ctx, &events, BPF_F_CURRENT_CPU, &io_event, sizeof(io_event));
    
    // 删除请求记录
    bpf_map_delete_elem(&requests, &req);
    
    return 0;
}

// 跟踪VFS读取操作
SEC("kprobe/vfs_read")
int trace_vfs_read_entry(struct pt_regs *ctx) {
    struct io_event_t io_event = {};
    
    io_event.ts = bpf_ktime_get_ns();
    io_event.io_start = io_event.ts;
    io_event.pid = bpf_get_current_pid_tgid() >> 32;
    io_event.tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
    io_event.operation = 0; // read
    
    // 获取进程名称
    bpf_get_current_comm(&io_event.comm, sizeof(io_event.comm));
    
    // 存储当前文件操作信息(简化版，实际需要存储文件描述符等更多信息)
    u64 id = bpf_get_current_pid_tgid();
    bpf_map_update_elem(&requests, &id, &io_event, BPF_ANY);
    
    return 0;
}

// 跟踪VFS读取操作完成
SEC("kretprobe/vfs_read")
int trace_vfs_read_exit(struct pt_regs *ctx) {
    u64 id = bpf_get_current_pid_tgid();
    struct io_event_t *io_eventp;
    
    io_eventp = bpf_map_lookup_elem(&requests, &id);
    if (!io_eventp)
        return 0;
    
    // 获取返回值（读取的字节数）
    io_eventp->bytes = PT_REGS_RC(ctx);
    io_eventp->io_end = bpf_ktime_get_ns();
    
    // 计算延迟
    u64 duration = io_eventp->io_end - io_eventp->io_start;
    update_latency_stats(io_eventp->pid, duration, io_eventp->operation);
    
    // 删除请求记录
    bpf_map_delete_elem(&requests, &id);
    
    return 0;
}

// 跟踪VFS写入操作
SEC("kprobe/vfs_write")
int trace_vfs_write_entry(struct pt_regs *ctx) {
    struct io_event_t io_event = {};
    
    io_event.ts = bpf_ktime_get_ns();
    io_event.io_start = io_event.ts;
    io_event.pid = bpf_get_current_pid_tgid() >> 32;
    io_event.tid = bpf_get_current_pid_tgid() & 0xFFFFFFFF;
    io_event.operation = 1; // write
    
    // 获取进程名称
    bpf_get_current_comm(&io_event.comm, sizeof(io_event.comm));
    
    // 存储当前文件操作信息
    u64 id = bpf_get_current_pid_tgid();
    bpf_map_update_elem(&requests, &id, &io_event, BPF_ANY);
    
    return 0;
}

// 跟踪VFS写入操作完成
SEC("kretprobe/vfs_write")
int trace_vfs_write_exit(struct pt_regs *ctx) {
    u64 id = bpf_get_current_pid_tgid();
    struct io_event_t *io_eventp;
    
    io_eventp = bpf_map_lookup_elem(&requests, &id);
    if (!io_eventp)
        return 0;
    
    // 获取返回值（写入的字节数）
    io_eventp->bytes = PT_REGS_RC(ctx);
    io_eventp->io_end = bpf_ktime_get_ns();
    
    // 计算延迟
    u64 duration = io_eventp->io_end - io_eventp->io_start;
    update_latency_stats(io_eventp->pid, duration, io_eventp->operation);
    
    // 删除请求记录
    bpf_map_delete_elem(&requests, &id);
    
    return 0;
}

char LICENSE[] SEC("license") = "GPL"; 