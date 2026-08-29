# Phase 0 实现说明

## 范围

Phase 0 包含仓库结构、共享模型、YAML 配置加载、结构化滚动日志、协议常量与编解码器、测试和 CI；不包含 Wintun、WireGuard、数据包路由、gVisor、数据库、Control 传输或 GUI。

## 协议决定

权威规格确定 Session 头字段和宽度，但没有指定字节序。RemLink 对全部多字节 Session 头字段使用网络字节序（大端）；黄金字节测试固定该行为，防止后续实现静默分歧。

权威 DOCX 和 `specs/spec.md` 实际枚举 14 个错误码。早期 Phase 0 任务文字写成“15 个”；实现遵循权威枚举并修正文档计数，不虚构第 15 个错误码。

## 固定依赖

- Go 1.26.7
- `gopkg.in/yaml.v3` v3.0.1
- `gopkg.in/natefinch/lumberjack.v2` v2.2.1
- `actions/checkout` v6.0.2（按 commit SHA 固定）
- `actions/setup-go` v7.0.0（按 commit SHA 固定）

使用 YAML v3 是因为实现时 v4 仍只有候选版本。
