# Phase 0 验证记录

- 日期：2026-08-25
- 主机：Windows amd64
- Go 工具链：1.26.7
- 范围：仓库和基础模型

## 验证证据

| 检查 | 结果 |
|---|---|
| `gofmt` 清洁检查 | PASS |
| `go mod verify` | PASS（全部模块已验证） |
| `go test -count=1 ./...` | PASS |
| `go vet ./...` | PASS |
| Windows/amd64 `go build ./...` | PASS |
| Linux/amd64 CGO=0 交叉构建 | PASS |
| 运行三端 Phase 0 入口 | PASS |

协议测试覆盖 20 字节 Session 头黄金字节、帧往返、畸形头拒绝、13 种 Control 消息和权威规格实际枚举的 14 个错误码。

配置测试覆盖默认值、IPv4 网络、Client URL、未知/敏感 YAML 字段拒绝和多文档 YAML 拒绝。日志测试覆盖 11 个模块、结构化字段、滚动文件、幂等关闭、默认禁用包日志，以及显式启用采样后的逐键限速。

## 非阻塞环境说明

本机 Windows Go 环境 `CGO_ENABLED=0` 且没有 C 编译器，因此额外的 `go test -race ./...` 无法执行。Phase 0 验收不要求 race detector；日志采样器仍由互斥锁保护并有单元测试，CI 在 Windows 与 Linux 执行要求的构建、测试和 vet。
