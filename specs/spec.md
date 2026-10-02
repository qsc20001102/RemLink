# RemLink 架构与协议规格

本文件保留系统的架构约束与协议边界。操作流程见 [部署指南](../docs/deployment-and-usage.md)，真实拓扑验收见 [T01–T18 手册](../docs/validation/T01-T18-runbook.md)。接口签名和 JSON 字段以对应源码定义为准，修改实现时需同步维护规格与验证。

## 架构约束

| 角色 | 数据面与职责 |
| --- | --- |
| Server | Linux 内核 WireGuard、IPAM、节点登记、Control、Session 调度、SQLite 和 Web 管理 |
| Engineer | Windows Wails GUI、单 Wintun、内嵌 wireguard-go、PacketMux、自有远程路由和 Session UDP |
| Site | Windows 控制台、单 Wintun、内嵌 wireguard-go、Session UDP、gVisor netstack 与主机套接字 |

- 所有 Windows WireGuard peer 只连接 Server，所有 Overlay 流量经中心转发。
- 每个 Windows 节点只创建并复用一个名为 `RemLink` 的 Wintun，默认 MTU 1280。
- Server peer 的 AllowedIPs 只包含该节点的 Overlay `/32`；Windows 到 Server 的唯一 peer 使用整个 Overlay CIDR。
- 现场 CIDR 位于 Session UDP payload 内，不能加入 Server WireGuard AllowedIPs。
- Windows 不启用 IP forwarding，不创建或修改 WinNAT；Site 用普通主机套接字连接现场设备。
- 支持 IPv4 TCP、单播 UDP 和 ICMP Echo，不提供二层桥接、广播/组播、IPv6、Exit Node 或节点间直连。
- Engineer 同时最多一个非终态会话；Site 可服务多个 Engineer。不同 Site 可以使用相同现场网段。
- Windows 网络修改集中在 `internal/platform/windows`，GUI 不实现路由和网络业务规则。

## 配置与地址

| 参数 | 初始默认值 |
| --- | --- |
| Overlay CIDR | `10.88.0.0/16` |
| Server Overlay IP | `10.88.0.1` |
| HTTP | `0.0.0.0:8080` |
| WireGuard | `51820/udp` |
| Control | `10.88.0.1:7001` |
| Session | `6200/udp`，节点各自 Overlay 地址 |
| MTU | `1280` |

Server 是节点地址的唯一权威来源。登记后地址持久化，重启保持稳定；分配拒绝网络地址、广播地址、Server 地址及已占用地址。撤销节点会移除 peer、使 Node Token 失效并释放地址。

Server 公网地址、管理令牌、HTTP 监听和数据目录从 YAML 读取。网络参数先取数据库记录，没有记录时使用 YAML 初始值。管理页保存网络参数不回写 YAML。`server.wg_endpoint` 的端口必须与生效的 WireGuard 端口一致。

修改节点地址或整个 Overlay 会关闭相关会话，并要求节点重新 Bootstrap。迁移需暂停新会话、更新地址和 peer，在旧 Control 通道仍可用时通知在线节点，再切换监听；失败回滚由 `internal/admin/network.go` 与 Control supervisor 处理。Windows 应用新 Overlay 前检查本地重叠，冲突时报告 `OVERLAY_LOCAL_CONFLICT`。

## 身份与信任边界

首次登记使用 Join Token，成功后使用各节点的 Node Token。NodeID、WireGuard 密钥和身份由程序管理。Join Token 可轮换，轮换不撤销已登记节点。

Windows 身份以机器级 DPAPI 保护，存放在各角色 EXE 所在目录的 `identity.json`。首次注册 Join Token 可配置在 YAML；优先级为命令行 `-join-token`、环境变量 `REMLINK_JOIN_TOKEN`、YAML。Windows 私钥不上传 Server。

Server 私钥存入数据目录，peer 只能使用自己的 Overlay 源地址。管理 API 使用可选的单一 Bearer Admin Token。Server 不内置 TLS，公网 HTTP 入口的保护由部署层 HTTPS 反向代理提供。

## MuxTun 与 PacketMux

`MuxTun` 实现固定 wireguard-go 版本的 `tun.Device`，包装真实 Wintun。Engineer 出方向仅按目标 IPv4 CIDR 分类：

1. Overlay 目标原样交给 wireguard-go。
2. 当前会话的远程 CIDR 放入 Sender 有界队列。
3. 其他目标丢弃并记录限速元数据日志。

Sender 在独立 goroutine 使用普通 UDP socket，绑定 Engineer Overlay IP，向 Site Overlay IP 的 Session 端口发送 `SessionHeader + Raw IPv4`。外层 UDP 目标属于 Overlay，经同一个 Wintun 再次进入 wireguard-go。

Read 不等待 UDP 网络 I/O；一次 batch 全是远程包时继续读取，避免返回 `0, nil` 导致忙循环。wireguard-go 入方向与 Engineer 远程回复注入通过统一 Wintun writer 序列化。Close 必须幂等并结束相关 goroutine。Site 现场数据交给 gVisor，不注入 Windows Wintun 做转发。

实现见 `internal/overlay/clientwg` 与 `internal/subnet`。PacketMux 不解析 TCP/UDP/ICMP 或应用层协议。

## Session

远程 CIDR 由 Engineer 动态下发，Site YAML 不保存现场网段。允许一个会话包含多个 CIDR；拒绝 `/0`、与 Overlay 重叠或与 Engineer 非 RemLink 本地网络/路由重叠的目标。

状态为 `CREATING`、`PREPARING_SITE`、`READY`、`ACTIVE`、`STOPPING`、`CLOSED`、`FAILED`。除 `CLOSED` 与 `FAILED` 外均占用 Engineer 的单会话名额。

建立顺序：本地冲突检查 → `CREATE_SESSION` → Server 校验并生成随机非零 uint64 SessionID → `PREPARE_SESSION` → Site 路由与容量检查 → `PREPARE_RESULT` → `SESSION_CONFIG` → Engineer 安装自有路由 → `ROUTES_READY` → `SESSION_ACTIVE`。

双向 UDP payload 使用相同 20 字节头，多字节数值采用大端序：

| 字段 | 字节数 | 内容 |
| --- | --- | --- |
| Magic | 4 | `RMLK` |
| Version | 1 | `1` |
| Type | 1 | `0x01`，IPv4 |
| Flags | 2 | `0` |
| SessionID | 8 | 非零 uint64 |
| PayloadLen | 2 | 原始 IPv4 长度 |
| Reserved | 2 | `0` |
| Payload | N | 完整原始 IPv4 包 |

接收端校验活动会话、SessionID、外层源 Overlay IP、IPv4 版本与实际长度。Engineer→Site 内层源必须为 Engineer Overlay IP，目的属于远程 CIDR；Site→Engineer 内层源属于远程 CIDR，目的为 Engineer Overlay IP。失败时丢弃，记录限速警告。

协议不加入自研加密、ACK、重传或拥塞控制。外层安全由 WireGuard 提供。显式绑定物理源地址的工业软件可能不满足内层源校验，应选择 RemLink 网卡。

## Site 网关

Site 对每个 CIDR 进行 Windows Route Lookup：`DIRECT`、`ROUTED` 可用；`DEFAULT_ONLY`、`NO_ROUTE` 拒绝；Overlay 冲突也拒绝。默认容量为 TCP 2048、UDP 4096，UDP 空闲回收 60 秒，均通过 Site 配置调整。

- TCP：gVisor 接收 SYN 并维护 Engineer 侧连接，主机 TCP socket 连接现场目标，双向搬运字节。
- UDP：按 SessionID、Engineer IP 与流信息隔离主机 UDP socket，空闲后回收。
- ICMP Echo：Site 探测目标，按原请求 ID/Sequence 构造回复并通过 Session UDP 返回。
- 返回 raw IPv4 的源地址为现场目标，目的为 Engineer Overlay IP；必须经 Session UDP 封装返回，Engineer 校验后注入 Wintun。

现场设备看到 Site 的现场可达地址，因此不需要 Overlay 返回路由。使用固定版本的 gVisor，不实现自研 TCP/IP 栈或应用协议代理。网关接口定义在 `internal/subnetgateway/gateway.go`；路由管理定义在 Windows `route.Manager`，不在文档中另行声明替代接口。

## Control 与 HTTP API

首次登记和重新配置走公开 Bootstrap API；建立 WireGuard 后，控制消息通过仅监听 Server Overlay IP 的 WebSocket `/control`。

| Control 消息 | 方向 |
| --- | --- |
| `HELLO` | Node → Server |
| `WELCOME` | Server → Node |
| `NODE_LIST` | Server → Engineer |
| `CREATE_SESSION` | Engineer → Server |
| `PREPARE_SESSION` | Server → Site |
| `PREPARE_RESULT` | Site → Server |
| `SESSION_CONFIG` | Server → Engineer |
| `ROUTES_READY` | Engineer → Server |
| `SESSION_ACTIVE` | Server → Engineer/Site |
| `STOP_SESSION` | Node → Server，Server → Node |
| `SESSION_STATS` | Node → Server |
| `HEARTBEAT` | 双向 |
| `REBOOTSTRAP_REQUIRED` | Server → Node |

Envelope 和 payload 定义见 `internal/protocol/control.go`。心跳默认每 5 秒；ONLINE 为最近心跳不超过 15 秒，UNSTABLE 为 15–30 秒，OFFLINE 为超过 30 秒。重连退避为 1、2、5、10、30 秒。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/v1/server/info` | Server 基本信息 |
| POST | `/api/v1/bootstrap/register` | 首次登记 |
| POST | `/api/v1/bootstrap/config` | 使用节点身份获取配置 |
| GET | `/api/v1/admin/nodes` | 节点列表 |
| PATCH | `/api/v1/admin/nodes/{id}` | 修改名称/Overlay IP |
| DELETE | `/api/v1/admin/nodes/{id}` | 撤销节点 |
| GET | `/api/v1/admin/sessions` | 会话列表 |
| POST | `/api/v1/admin/sessions/{id}/disconnect` | 断开会话 |
| GET | `/api/v1/admin/network` | 网络参数与当前 Join Token |
| PUT | `/api/v1/admin/network` | 修改参数或轮换 Join Token |
| GET | `/api/v1/admin/logs` | 过滤事件日志 |

HTTP 请求、响应与鉴权实现见 `internal/bootstrap/http.go`、`internal/admin/handler.go`。错误码定义在 `internal/protocol/error_code.go`。Web 管理 API 的 SessionID 使用十进制字符串，避免 JavaScript 对完整 uint64 的精度损失。

## 持久化、恢复和日志

Server SQLite 包含 `settings`、`nodes`、`sessions`、`session_cidrs`、`session_stats`、`event_logs`。表结构和约束由 `internal/database/migrations` 管理。Docker 将数据库、密钥和应用日志放在 `/app/data`。

Engineer 记录自己创建的路由并在启动时清理陈旧路由；Site 重建进程内网关，不操作第三方 NAT。Wintun 适配器可复用。Server 重启关闭数据库中遗留的非终态会话，不透明恢复旧会话。新 Bootstrap 会清理已经丢失本地运行时的旧会话；短暂 Control 重连由节点运行时处理。

日志模块为 CORE、BOOTSTRAP、WG、IPAM、CONTROL、SESSION、ROUTE、NETSTACK、TUN、SUBNET、SYSTEM。高频 packet 日志默认关闭，不记录载荷，只允许限速元数据事件。统计按 Engineer 视角定义上传/下载，周期上报累计计数；Server 不为统计进入 WireGuard 数据路径。

## 构建与验收

Go/npm 依赖由模块和锁文件固定，Wintun DLL 内嵌并在释放时校验摘要。Engineer 与 Site 使用独立便携目录；Engineer 还保存按 Site 区分的 `site-profiles.json`。第三方声明随发布包提供。

Docker 运行使用 `NET_ADMIN`、TUN 和 IPv4 转发，启动预检必须失败时明确退出。三个角色的 ZIP 独立，Docker 构建上下文 TAR 与可导入镜像 TAR 为不同发布形式。

自动化验证覆盖见 [验证说明](../docs/validation/automated-coverage.md)。Gate A–D 与 T01–T18 在真实拓扑中采证，结果以当次运行记录为准。
