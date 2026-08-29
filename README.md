# RemLink

RemLink v1.0 是面向工业网络的 IPv4 三层远程接入系统。Linux Server 使用内核 WireGuard 作为中心节点；每个 Windows 节点只复用一个内嵌 wireguard-go/Wintun 适配器；Engineer 使用 PacketMux；Site 使用 gVisor 用户态网关访问现场子网。

源码仓库根目录的 `RemLink_v1.0_技术设计与AI开发规格书_Netstack版.docx` 是权威需求文档，`specs/` 是分阶段实现与验收拆解。若 Markdown 与 DOCX 不一致，以 DOCX 为准。

## 当前状态

Phase 0–10 的生产代码、前端、自动化测试、发布打包和 Docker 基线均已实现。自动化覆盖 Session 状态机、Bootstrap/IPAM/数据库、Control 重连与重新 Bootstrap、数据包验证、进程内 gVisor TCP/UDP/ICMP 往返、网络迁移以及重复现场 CIDR 隔离。

Gate A–D 和物理 T01–T18 保持 `NOT_RUN`：当前工作区没有所需的 Linux 内核 WireGuard 与两台/四台管理员权限 Windows 实机拓扑。不能用单元测试或浏览器测试冒充物理验收结果。

## 仓库结构

| 路径 | 内容 |
|---|---|
| `cmd/` | Engineer、Site、Server 三端程序入口 |
| `internal/` | 控制面、WireGuard、Session、netstack、持久化和平台实现 |
| `frontend/` | Engineer Wails/Vue 界面与 Server Vue 管理界面 |
| `config/` | 三端安全示例配置；真实本机配置不会提交 |
| `deploy/` | Server Docker Compose、Dockerfile 与部署基线 |
| `scripts/` | 发布构建、验收、架构与仓库检查工具 |
| `specs/` | 分阶段开发任务和验收清单 |
| `docs/` | 中文部署、实现状态、验证手册和设计资料 |
| `third_party/` | 随项目分发的第三方许可文件 |

根目录 DOCX 是权威需求文档，源码实现和阶段验收入口分别位于 `cmd/`、`internal/` 与 `specs/`，不要把 `dist/` 发布包当成源码提交。

## 三端部署与使用

从零部署 Server、Engineer、Site，完成注册、连接、运维、备份、升级和故障排查，请直接阅读：

- [三端部署与使用指南](docs/deployment-and-usage.md)
- [T01–T18 验收执行手册](docs/validation/T01-T18-runbook.md)
- [需求与证据矩阵](docs/validation/requirements-evidence.md)
- [当前实现状态](docs/implementation-status.md)

发布包中的 `docker/compose.release.yaml` 可直接使用预编译 Linux Server 构建最小运行镜像；源码仓库开发构建使用 `deploy/docker/compose.yaml`。

## 构建与验证

在仓库根目录执行：

~~~powershell
./scripts/maintenance/Test-RepositoryHygiene.ps1
npm ci --prefix frontend
npm run typecheck --prefix frontend
npm run build --prefix frontend
go test -count=1 ./...
go vet ./...
./scripts/validation/Test-Architecture.ps1
./scripts/validation/Test-AcceptanceTools.ps1
./scripts/build-release.ps1
~~~

发布脚本在 `dist/` 生成 Engineer、Site、Server 三个相互独立的目录和 ZIP。Windows 两端的默认 YAML 位于各自 EXE 旁，运行生成的 DPAPI 身份、日志和 `wintun.dll` 也只写入各自包目录；Server 包单独包含 Linux 二进制、Docker 文件、部署指南、验收工具与校验和。

## Git 提交边界

- 应提交源码、测试、Markdown/DOCX 规格、示例 YAML、示例 `.env`、CI、第三方许可和内嵌的官方 Wintun DLL。
- 不提交 `build/`、`dist/`、`node_modules/`、前端编译目录、验收证据、本机数据库、日志或运行时身份。
- 不提交真实 `.env`、Engineer/Site 实际 YAML、`identity.json`、`site-profiles.json`、Node Token、Join Token、Admin Token、WireGuard 私钥或 TLS 私钥。
- ZIP/EXE 应通过 GitHub Release 或其他发布渠道分发，不应直接进入源码历史。

首次提交和每次推送前运行仓库检查；它会同时检查 Git 候选文件、敏感配置、超大文件和 Markdown 相对链接：

~~~powershell
./scripts/maintenance/Test-RepositoryHygiene.ps1
git status --short
git diff --check
~~~

## 安全边界

- Windows YAML 保存 Server URL、节点名称、首次注册 Join Token 和 Site netstack 上限；Join Token 是明文便捷配置，Node Token 与 WireGuard 私钥仍由机器级 DPAPI 保护，不得写入 YAML。
- Site YAML 不保存现场 CIDR；远程网段由 Session 动态下发。
- Docker 只增加 `NET_ADMIN`，映射 `/dev/net/tun`，不使用 privileged。
- 公网只需要 WireGuard UDP 和受保护的 Bootstrap/Admin 入口；`7001/tcp` 与 `6200/udp` 只在 Overlay 内使用。
- v1.0 不内置 HTTPS。公网 Bootstrap/Admin 必须放在外部 HTTPS 反向代理后；否则 Join Token、Node Token 和管理请求不会被 HTTP 层加密。WireGuard 不保护这条独立公网 HTTP 路径。

## 验收证据

~~~powershell
./scripts/validation/New-AcceptanceRun.ps1 -OutputDirectory evidence/run-001
./scripts/validation/Collect-WindowsEvidence.ps1 -Role Engineer -OutputDirectory evidence/run-001/engineer-a
./scripts/validation/Collect-WindowsEvidence.ps1 -Role Site -OutputDirectory evidence/run-001/site-a
$engineerEvidence = (Get-ChildItem evidence/run-001/engineer-a -Filter '*-engineer-network.json' | Sort-Object LastWriteTimeUtc | Select-Object -Last 1).FullName
$siteEvidence = (Get-ChildItem evidence/run-001/site-a -Filter '*-site-network.json' | Sort-Object LastWriteTimeUtc | Select-Object -Last 1).FullName
./scripts/validation/Set-AcceptanceResult.ps1 -RunDirectory evidence/run-001 -ID T01 -Status PASS -EvidencePath $engineerEvidence,$siteEvidence
./scripts/validation/Test-AcceptanceRun.ps1 -RunDirectory evidence/run-001
~~~

实际记录时必须按执行手册采集所有指定主机证据。工具会校验证据路径、大小、SHA-256 和 Gate 前置条件，但不会替代人工判断证据内容是否真正证明场景。
