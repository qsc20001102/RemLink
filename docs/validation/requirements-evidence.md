# RemLink v1.0 需求与证据矩阵

日期：2026-08-25

本矩阵审计根目录权威 DOCX、`specs/spec.md`、`specs/tasks.md` 和 `specs/checklist.md`，并映射到生产代码与可重复验证。若派生 Markdown 与 DOCX 冲突，以 DOCX 为准。

状态词汇：

- `CODE_TESTED`：存在生产实现，并有可重复自动化测试或策略扫描覆盖该边界。
- `BUILD_VERIFIED`：目标已编译/类型检查，但不声称管理员权限或物理网络行为成功。
- `PHYSICAL_NOT_RUN`：Gate/场景需要真实 Linux/Windows 拓扑，不能从单元、集成、浏览器或交叉构建推导 PASS。

## 需求覆盖

| 需求 | 生产证据 | 自动化/构建证据 | 审计状态 | 仍需物理证据 |
|---|---|---|---|---|
| R1 架构与不可变约束 | `internal/overlay`、`internal/subnet`、`internal/subnetgateway`、Windows 平台层、`serverwg` | 架构扫描；重复 CIDR manager/netstack 测试；双平台发布构建 | `CODE_TESTED` | Gate A–D、T01–T02、T06、T11–T12 |
| R2 Overlay 与 Server IPAM | `internal/ipam`、Admin network/handler、Bootstrap、serverwg | IPAM 稳定分配；原子 Node/network 设置；七步迁移、回滚、通知顺序、Node IP 更新/删除 | `CODE_TESTED` | T17–T18 真实适配器、`wg0`、Control |
| R3 单 Wintun MuxTun/PacketMux | clientwg、Windows Wintun/runtime | 分类、framing、计数、丢包、关闭测试；DLL 并发发布；Windows 构建 | `CODE_TESTED + BUILD_VERIFIED` | Gate A–B、T01–T02 管理员 Windows |
| R4 Remote Subnet Session | `internal/session`、`internal/subnet`、Windows route | 完整状态机、请求关联、超时、多 CIDR、冲突、双向数据报验证、并发 SessionID 和 MaxUint64 JSON | `CODE_TESTED` | T03–T05、T10、T13 与真实路由 |
| R5 Site gVisor 网关 | netstack、TCP/UDP/ping relay | 进程内主机套接字往返、prepare 幂等、容量和 UDP idle-GC | `CODE_TESTED` | Gate C–D、T06–T09 真实目标 |
| R6 Bootstrap、Control、身份 | Bootstrap、Control、nodeagent、identity、DPAPI | 严格 API/config/URL、token/DPAPI、HELLO/心跳/状态/重连/rebootstrap | `CODE_TESTED` | T14–T15 中断 |
| R7 Server | `cmd/server`、database、Admin、serverwg、Server 前端 | 数据库/Admin/Control；Linux 构建；前端生产扫描；Docker 架构与发布入口 | `CODE_TESTED + BUILD_VERIFIED` | Linux WG/Docker 启动与 UI 拓扑 |
| R8 Engineer | `cmd/engineer`、Engineer 前端、Engineer Session、Windows adapter/route | runtime 测试；禁止 Demo fallback；Vue 构建；Windows GUI 构建 | `CODE_TESTED + BUILD_VERIFIED` | 管理员 GUI、路由、Wintun、Gate/T |
| R9 Site | `cmd/site`、Site Session、subnetgateway | Console 字段/事件；route/capacity/injection/relay/reconcile；Windows console 构建 | `CODE_TESTED + BUILD_VERIFIED` | Site Console 与真实 LAN TCP/UDP/ICMP |
| R10 数据库与模型 | database、model、migrations | 六表迁移/store；原子网络/IP 替换；启动关闭开放 Session | `CODE_TESTED` | T15 部署 Server 重启 |
| R11 API 与消息 | Bootstrap HTTP、Admin handler、protocol、Control | 严格 JSON/framing/auth/direction/error 与 handler 测试 | `CODE_TESTED` | T03/T17/T18 指定抓包/API |
| R12 冲突、失败、恢复 | route、nodeagent、Bootstrap reconcile、Session runtime、迁移事务 | 本地/Overlay 冲突、陈旧路由、新 Node 清 Session、重连、超时、注入失败、迁移回滚 | `CODE_TESTED` | T04–T05、T14–T18 |
| R13 日志、统计、可观测性 | logging、eventlog、Session counter/reporter、Admin log API | 模块分类、限速、仅元数据包警告、统计持久化/过滤、秘密扫描 | `CODE_TESTED` | T07–T12 流量计数与日志 |
| R14 安全边界 | Node/Join token 哈希、DPAPI、WG `/32`、严格 listener/validator、Admin token | token/auth、地址/URL、listener/validator、证据秘密扫描 | `CODE_TESTED` | 防火墙、暴露面、AllowedIPs |
| R15 结构、接口、依赖 | monorepo、Go/npm 锁、内嵌 Wintun、第三方声明 | 模块验证、架构扫描、前端清洁安装、双平台构建、发布校验和 | `BUILD_VERIFIED` | 部署策略要求时的签名 |
| R16 阶段与验收 | tasks/checklist、执行手册、证据脚本 | 自测执行证据哈希、范围、前置条件和篡改检测 | `CODE_TESTED`；物理状态独立 | Gate A–D、T01–T18 均 `PHYSICAL_NOT_RUN` |

## 阶段审计

| 阶段 | 实现状态 | 验收状态 |
|---|---|---|
| Phase 0 | 仓库、模型、日志、配置、协议、CI 已实现并测试 | 自动化验收完成 |
| Phase 1 | Wintun/MuxTun/wireguard-go 与内嵌 DLL；Windows 可构建 | Gate A `PHYSICAL_NOT_RUN` |
| Phase 2 | 数据库、IPAM、Bootstrap、Server WG、身份 store | 模拟 Node/IPAM 完成；部署由后续物理测试覆盖 |
| Phase 3 | Control Hub、心跳、节点列表、capability、重连 | 自动化集成完成 |
| Phase 4 | PacketMux、自有路由与冲突逻辑 | 物理 Wintun 拦截 `PHYSICAL_NOT_RUN` |
| Phase 5 | 普通 UDP 重入传输和验证 | Gate B `PHYSICAL_NOT_RUN` |
| Phase 6 | gVisor netstack TCP/UDP 网关 | Gate C `PHYSICAL_NOT_RUN` |
| Phase 7 | 对称返回传输和 ICMP Echo relay | Gate D `PHYSICAL_NOT_RUN` |
| Phase 8 | 双边 Session 状态机、错误、统计、reconcile | T03/T04/T05/T13/T16 `PHYSICAL_NOT_RUN` |
| Phase 9 | Engineer GUI、Server 五页 UI、Admin API | UI 驱动真实工作流 `PHYSICAL_NOT_RUN` |
| Phase 10 | 并发、重复 CIDR、打包、Docker、证据工具 | T01–T18 与最终重复子网拓扑 `PHYSICAL_NOT_RUN` |

## 复现命令

在仓库根目录运行：

~~~powershell
go mod verify
go test -count=1 ./...
go vet ./...
npm ci --prefix frontend
npm run typecheck --prefix frontend
npm run build --prefix frontend
./scripts/validation/Test-FrontendProduction.ps1
./scripts/validation/Test-Architecture.ps1
./scripts/validation/Test-AcceptanceTools.ps1
./scripts/build-release.ps1 -Version 1.0.0
~~~

发布构建会在生成二进制和校验和前重复关键测试。真实结果必须先用 `New-AcceptanceRun.ps1` 初始化，采证后用 `Set-AcceptanceResult.ps1` 记录，最后运行 `Test-AcceptanceRun.ps1`。只有证据文件在该运行目录内且哈希通过时，Gate/T 才允许标记 PASS。
