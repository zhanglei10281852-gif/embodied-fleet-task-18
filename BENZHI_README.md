# BENZHI_README

## 项目说明

- 项目：zhanglei10281852-gif/embodied-fleet-task-18
- 项目用途：Embodied Fleet is a production-style Go backend for coordinating inspection and transport robots in warehouses and industrial parks. It manages mission intake, shared-corridor reservations, charging capacity, field assignments, execution leases, maintenance handovers, retries, escalation, and durable audit history.
- Go 工具链：`golang:1.26`
- 前端工具链：无

## 标准构建、运行和测试命令

进入容器后执行：

```bash
# 编译
cd '/app' && GOTOOLCHAIN=local go build ./...

# 启动
cd '/app' && GOTOOLCHAIN=local go run ./cmd/executor
cd '/app' && GOTOOLCHAIN=local go run ./cmd/migrate
cd '/app' && GOTOOLCHAIN=local go run ./cmd/scheduler

# 测试
cd '/app' && GOTOOLCHAIN=local go test ./...
```

## Docker 构建和进入容器

```bash
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh benzhi-task-196-amd64 linux/amd64
./build_benzhi_docker.sh benzhi-task-196-arm64 linux/arm64
docker run -it benzhi-task-196-amd64:latest
docker run -it --platform linux/arm64 benzhi-task-196-arm64:latest
```

## 题目验证命令

1. 预期退出码 1：`go test ./internal/engine -run ^TestEngine_ActivateWindow$ -count=1`

## Bug 复现

Bug 现象、触发步骤和完整错误信息见 `BUG_REPRO.md`。
