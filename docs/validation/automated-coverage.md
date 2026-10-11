# 自动化验证

先构建前端，再运行 Go 检查。前端产物通过 `go:embed` 嵌入程序，未构建时 Go 编译可能失败。

```powershell
npm ci --prefix frontend
npm run typecheck --prefix frontend
npm run build --prefix frontend
go mod verify
go test -count=1 ./...
go vet ./...
./scripts/maintenance/Test-RepositoryHygiene.ps1
./scripts/validation/Test-FrontendProduction.ps1
./scripts/validation/Test-Architecture.ps1
./scripts/validation/Test-AcceptanceTools.ps1
```

## 检查范围

| 位置 / 工具 | 范围 |
| --- | --- |
| `internal/protocol` | Session framing、Control 消息和错误码 |
| `internal/config`、`internal/database`、`internal/ipam`、`internal/bootstrap` | 配置、迁移、节点登记、Token 与地址分配 |
| `internal/control`、`internal/session`、`internal/admin` | 心跳、会话状态、超时、统计和网络变更 |
| `internal/clientsettings`、`internal/clientstate`、`internal/desktopclient` | 应用内配置、本机加密、旧数据导入、首次启动不连接、保存后连接与串行重连 |
| `internal/platform/windows/tray` | 原生托盘创建、激活与清理 |
| `scripts/test_export_nas_image.py` | Docker 镜像格式、旧应用替换、持久数据隔离、Linux 执行权限，拒绝被篡改的发布文件、运行层和错误平台 |
| `internal/overlay/clientwg`、`internal/subnet` | MuxTun、PacketMux、UDP 封装、双向校验和队列 |
| `internal/subnetgateway` | gVisor TCP/UDP/ICMP、flow 管理和会话隔离 |
| Windows 平台、身份及命令入口测试 | 路由、DPAPI、Wintun 运行库和客户端编排；平台专属测试需要对应系统 |
| `Test-FrontendProduction.ps1` | 生产前端适配器和开发 fixture 泄漏检查 |
| `Test-Architecture.ps1` | 架构约束、Docker 权限与端口、打包入口和依赖版本策略 |
| `Test-RepositoryHygiene.ps1` | Git 候选文件、生成物、配置占位值和文档链接 |
| `Test-AcceptanceTools.ps1` | 验收证据缺失、前置条件及篡改拒绝 |
| `Test-ReleasePackage.ps1` | 发布包角色隔离、必需文件、元数据和全部文件摘要 |

## 发布验证

在 Windows PowerShell 7 中执行：

```powershell
./scripts/build-release.ps1 -Version 1.1.0
```

脚本执行三端前端检查、Go test/vet、镜像打包回归测试、架构与验收工具检查，再构建两个 Windows Wails 客户端和 Linux 服务端，生成两个 ZIP 与一个 Server Docker 镜像 `.tar.gz`。客户端重新解压校验；Server 构建目录校验完整文件清单，镜像打包器读回归档，校验配置摘要、全部镜像层摘要、应用文件与可执行权限。需要 Python 3.11+ 和已有 RemLink Server 运行镜像，参见 [镜像打包前置条件](../../deploy/docker/README.ugreen.md)。

Go 检查默认针对当前操作系统；交叉编译不等于运行另一平台的测试。`go test -race` 需要目标平台支持及可用 C 编译器，应在具备条件的环境单独执行。

## 实机验收

Linux 内核 WireGuard、管理员 Windows 路由/Wintun、目标服务、重复现场网段和故障恢复，按 [T01–T18 手册](T01-T18-runbook.md) 在真实拓扑中验证。使用 `New-AcceptanceRun.ps1` 初始化结果，`Set-AcceptanceResult.ps1` 记录证据，`Test-AcceptanceRun.ps1` 校验证据路径和摘要。

这份文档描述测试范围与执行方法。通过状态以当次命令输出和验收运行记录为准。
