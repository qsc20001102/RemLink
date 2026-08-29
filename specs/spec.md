# RemLink v1.0（Userspace Netstack 版）实施规格

> 权威设计来源：仓库根目录《RemLink_v1.0_技术设计与AI开发规格书_Netstack版.docx》（版本日期 2026-08-19）。本文件将其转化为 spec-driven 开发格式；若实现中发现本文件与 docx 原文有冲突，以 docx 为准并回改本文件。禁止为了让代码跑通而绕开核心约束。

## 背景与目标

工业自动化远程调试中，工程师需要在异地直接访问现场设备 IP（如 192.168.13.10 的 PLC、HMI HTTP、RDP），而端口代理需逐个配置、协议适配无法泛化。RemLink 通过中心式 IPv4 L3 虚拟组网 + Engineer 动态下发远程 CIDR + Site 端 gVisor netstack userspace 子网网关，让 Windows 普通 IP 应用无需任何协议适配即可访问现场地址，且允许多个现场使用完全相同的网段。

## 变更内容

- 全新构建 RemLink v1.0 三端产品（绿地项目，全部为新增能力）：
  - **Server**：Ubuntu / Docker / Linux Kernel WireGuard + Go（wgctrl 编排）+ SQLite + Vue 3 Web UI
  - **Engineer**：Windows Wails GUI + wireguard-go（嵌入进程）+ 单一 RemLink Wintun + PacketMux
  - **Site**：Windows Console + wireguard-go（嵌入进程）+ 单一 RemLink Wintun + gVisor netstack SubnetGateway
- 架构基线：**单 Wintun + wireguard-go + PacketMux + gVisor netstack**；不使用 WinNAT、Windows IP Forwarding、Transit CIDR、WireGuardNT
- 按 Phase 0–10 分阶段实施，Gate A–D 为关键验收关卡（见 tasks.md）
- 仅 IPv4；Remote Subnet v1 支持 TCP / 单播 UDP / ICMP Echo，不实现二层

## 影响范围

- 受影响规格：无（首个 change）
- 受影响代码：全新 Go monorepo `remlink/`（目录结构见 R15）
- 固定版本外部依赖：Wintun DLL、wireguard-go（锁定 commit）、gVisor netstack（锁定 commit）、wgctrl、SQLite driver、Wails、Vue 3 + Vite、golang.org/x/net/icmp

## 新增需求

### 需求： R1 总体架构与不可变约束

系统 必须 采用中心式 Hub-and-Spoke 架构：所有 Engineer/Site 节点只与 Server 建立 WireGuard 连接，数据始终经 Server Hub 中转；节点只配置 Server 公网 IP/端口，不配置彼此公网地址。

系统 必须 遵守以下不可变决策：

| 决策项 | v1.0 最终选择 |
|---|---|
| 虚拟组网 | 中心式 Hub-and-Spoke，无 P2P |
| Windows 虚拟网卡 | 每个 Engineer/Site 只有 1 块名为 RemLink 的 Wintun |
| Windows WireGuard | wireguard-go 嵌入进程，不用 WireGuardNT |
| Server WireGuard | Linux Kernel WireGuard |
| 数据层 | IPv4 L3 捕获；Remote Subnet v1 = TCP/UDP/ICMP Echo |
| 远程子网 | Engineer 建立 Session 时动态下发；Site 配置不保存现场 CIDR |
| Site Subnet Gateway | gVisor netstack userspace relay，不依赖 WinNAT/IP Forwarding |
| Engineer 并发 | 一个 Engineer 同时最多连接一个 Site |
| Site 并发 | 底层允许多个 Engineer 同时连接同一个 Site |
| 现场网段重复 | 允许不同 Site 使用完全相同的 192.168.x.0/24 |
| 二层 | 明确不做 ARP/以太网帧/DCP/LLDP/TAP/Bridge |
| 服务端网络地址 | Server Web UI 配置；Node IP 由 Server IPAM 自动分配 |

明确不在 v1 范围：二层协议、P2P/STUN/TURN/UDP 打洞、IPv6、IP 广播/组播、SCTP/GRE/ESP、用户注册/多租户/RBAC/OAuth、HA/Cluster/K8s、自研 TCP/IP 栈/VPN/网卡驱动/NAT、一个 Engineer 同时连多个 Site。

#### 场景： 两个 Site 使用相同现场网段

- **当** Site-A 与 Site-B 都使用 192.168.13.0/24 作为现场网段，且分别被 Engineer-A、Engineer-B 通过 Session 访问
- **则** 两条 Session 同时工作互不串流；Server WireGuard 不出现路由冲突（Server 只按 Overlay /32 选路，现场 CIDR 位于 Session UDP Payload 内）

#### 场景： 尝试 P2P 或二层能力

- **当** 任何实现引入 P2P 直连、打洞、ARP/TAP/Bridge 代码
- **则** 违反不可变约束，不被接受

### 需求： R2 Overlay 虚拟组网与 Server IPAM

系统 必须 提供 Overlay 虚拟组网与集中式 IPAM：

- 默认 Overlay CIDR 为 10.88.0.0/16（仅初始值，Server Web UI 必须允许修改）；Server 地址默认取该网段第一个可用地址（如 10.88.0.1）；Engineer 与 Site 使用统一 Node Pool。
- 每个 Node 的地址在 Server WireGuard Peer 上以 /32 AllowedIPs 登记；客户端到 Server 的唯一 Peer 使用整个 Overlay CIDR 作为 AllowedIPs。
- Server 是 Overlay 地址唯一权威来源，客户端不得自行填写虚拟 IP。
- Node 首次注册时由 IPAM 分配地址，后续重启保持同一地址；地址不得为网络地址、广播地址、Server 地址或已占用地址。
- Web UI 可手动修改 Node IP；修改后触发该 Node 重新 Bootstrap/重建 WireGuard。
- 删除 Node 后撤销 WireGuard Peer、Node Token，并释放地址。

修改 Overlay CIDR 时系统 必须 按 7 步流程执行：Web UI 提交并校验新 CIDR → 所有 Active Session 进入 STOPPING/CLOSED → IPAM 为现有 Node 重新分配/迁移地址 → 重建 wg0 地址和 Peer AllowedIPs → 在线 Node 经旧 Control 通道收 REBOOTSTRAP_REQUIRED（通道断则自动走公网 Bootstrap API）→ Windows Node 更新 Adapter IP/路由并重启 wireguard-go → Site 重建 NetworkConfig 与 Session Gateway（不修改 Windows NAT/Forwarding）。

Node 必须 在应用新 Overlay CIDR 前检查其与本机现有直连网络是否重叠；发现冲突时不得强行启用，向 Server 报告 OVERLAY_LOCAL_CONFLICT。

#### 场景： Node 地址稳定

- **当** 已注册 Node 重启后再次 Bootstrap
- **则** 获得与之前相同的 Overlay IP

#### 场景： Node 本地网络与 Overlay 冲突

- **当** Node 本机直连网络与 Overlay CIDR 重叠
- **则** 拒绝启用新配置并上报 OVERLAY_LOCAL_CONFLICT

### 需求： R3 Windows 单虚拟网卡数据面（MuxTun / PacketMux）

Engineer 与 Site 必须 都只创建一块名称固定为 RemLink 的 Wintun L3 Adapter（默认 MTU 1280），同时承载 Overlay 节点通信和 Engineer 的 Remote Subnet 路由。不存在第二块 Subnet Adapter，也不存在 Transit CIDR。

系统 必须 将真实 Wintun 包装为 MuxTun（实现 wireguard-go 的 tun.Device 接口，Read/Write 代理给真实 Wintun）后交给 wireguard-go Device，使 RemLink 能在"Windows 网络栈 ↔ WireGuard 引擎"边界做 L3 分流。MuxTun 必须尽量薄。

Engineer 出方向 PacketMux 必须 仅根据目标 IPv4 地址分类：

- 目标属于 Overlay CIDR：原包不修改，直接交给 wireguard-go。
- 目标属于当前 Active Session 的任一 Remote CIDR：不交给 wireguard-go，将原始 IPv4 bytes 放入 SubnetSender 的有界队列。
- 其他目标：丢弃并做限速日志。

SubnetSender 必须 使用普通 Go UDP Socket（绑定 Engineer Overlay IP，目标为 Site OverlayIP:6200）发送 `RemLinkHeader + 原始 IPv4 Packet`，由 Windows TCP/IP 生成外层 IP/UDP Header；该外层 UDP Packet 因目标属于 Overlay CIDR 再次进入同一 Wintun，被 PacketMux 判定为 Overlay 流量交给 wireguard-go（防循环：外层目标为 Site Overlay IP，不属于 Remote CIDR，只进入一次 WireGuard）。

MuxTun 并发要求：

- PacketMux Read 不得在发送 Remote Packet 时直接阻塞等待 UDP 网络 I/O；使用有界 channel + 独立 Sender goroutine。
- 当一次 Wintun batch 全部为 Remote Packet 时，Read 应继续读取，直到能向 wireguard-go 返回至少一个 Overlay Packet 或收到关闭/错误事件，避免返回 0,nil 造成 busy loop。
- Wintun Write 由 wireguard-go 入方向和 Site Session 注入共用时，通过统一 Writer/锁序列化。
- packet buffer 尽量复用池；先保证正确性，再做 batch 优化。

#### 场景： Remote 包外层再入 Wintun

- **当** Engineer 应用访问 192.168.13.10，PacketMux 拦截后由 SubnetSender 以 UDP 发往 Site Overlay IP:6200
- **则** 外层 UDP 再次进入同一 Wintun 后被识别为 Overlay 流量交给 wireguard-go，无死锁、无无限循环

### 需求： R4 Remote Subnet Session

Session 业务规则：

- Remote CIDR 由 Engineer 用户在连接 Site 时输入并下发；Site 本地配置不预存现场网段。
- 一个 Engineer 同时最多一个处于 CREATING/PREPARING_SITE/ACTIVE 的 Site Session。
- 一个 Session 可携带多个 IPv4 CIDR，数据结构从第一天就使用数组。
- Site 底层允许同时服务多个 Engineer；通过 SessionID + Engineer Overlay IP 区分。
- 两个不同 Site 的 Remote CIDR 可以完全相同。
- 目标 CIDR 不得与 RemLink Overlay CIDR 重叠，不得与 Engineer 本地非 RemLink 网络发生任何前缀重叠。
- v1 禁止 0.0.0.0/0 Exit Node 模式。

Session 状态机 必须 为：CREATING → PREPARING_SITE → READY → ACTIVE → STOPPING → CLOSED，以及 FAILED（任一关键检查失败）。

Session 建立顺序 必须 为：Engineer GUI 选择 Site 并输入 CIDR → Engineer 本地冲突检查（失败不请求 Server）→ CREATE_SESSION → Server 校验（已有 Session/Site 在线/CIDR 合法性）→ Server 生成随机 64-bit SessionID、状态 PREPARING_SITE、向 Site 下发 PREPARE_SESSION → Site 对每个 CIDR 做 Windows Route Lookup 并检查 SubnetGateway 能力与 flow capacity → Site 返回 PREPARE_RESULT（含 subnet_gateway=netstack）→ Server 向 Engineer 返回 SESSION_CONFIG（SessionID、Site Overlay IP、Remote CIDRs、MTU、Session UDP Port）→ Engineer 添加 Windows Remote Routes 并启动 SessionTransport → Engineer 发送 ROUTES_READY → Server 置 ACTIVE 并通知 Site。

Session 数据包格式（两个方向统一）SHALL 为：

| 字段 | 长度 | 说明 |
|---|---|---|
| Magic | 4 bytes | ASCII: RMLK |
| Version | 1 byte | v1 = 1 |
| Type | 1 byte | v1 固定 0x01 = IPv4 |
| Flags | 2 bytes | v1 置 0 |
| SessionID | 8 bytes | Server 生成的 uint64 |
| PayloadLen | 2 bytes | 原始 IPv4 Packet 长度 |
| Reserved | 2 bytes | 置 0 |
| Payload | N bytes | 完整原始 IPv4 Packet |

不加入 TCP 风格序号/ACK/重传/拥塞控制；不做 TCP-over-TCP。WireGuard 已提供外层完整性和加密，不再增加自研加密或可靠 UDP。

Session 双向收包校验 必须 满足：

- Engineer 与 Site 都只在自己的 OverlayIP:6200 上监听 Session UDP，不监听公网地址或 0.0.0.0。
- 根据 SessionID 查找 Active Session。
- UDP 外层源 Overlay IP 必须等于该 Session 对端绑定的 Overlay IP（双向都校验）。
- Engineer→Site 方向内层 IPv4 Source 默认必须等于 Engineer Overlay IP（应用显式绑定其他物理源地址的流量不保证可用）。
- Engineer→Site：内层 Destination 必须属于 Session Remote CIDR；Site→Engineer：内层 Source 必须属于 Session Remote CIDR，Destination 必须等于 Engineer Overlay IP。
- IPv4 Total Length、版本和实际 Payload 长度必须一致。
- 校验失败直接丢弃并做限速安全日志。

#### 场景： Engineer 已有 Active Session 再连第二个 Site

- **当** Engineer 在 ACTIVE Session 期间请求连接另一个 Site
- **则** 拒绝并返回 ENGINEER_SESSION_EXISTS

#### 场景： 目标 CIDR 与本地网络冲突

- **当** Engineer 本机存在 192.168.0.0/16，用户输入 192.168.13.0/24
- **则** 本地冲突检查失败（前缀重叠），不向 Server 发起请求

### 需求： R5 Site Userspace Subnet Gateway（gVisor netstack）

Site 收到 Session UDP 后 必须 取出原始 IPv4 Packet，直接交给 gVisor netstack SubnetGateway（通过 channel.Endpoint.InjectInbound），不注入 Windows Wintun 做内核转发。gVisor netstack 负责 TCP/UDP 连接状态和报文重建；RemLink 不实现 TCP 状态机。现场侧实际出站使用 Windows 普通 host socket（net.Dial / UDPConn），目标设备看到的源地址是 Site 的现场可达地址。

- v1 不创建 Transit CIDR，不创建 RemLinkNAT，不要求 PLC/网关认识 Overlay 网段。
- 能力边界：v1 明确支持 TCP、单播 UDP 和 ICMP Echo（Ping）；IP 广播/组播、SCTP、GRE、ESP 及其他非 TCP/UDP IP 协议不在保证范围。
- SubnetGateway 接口：

```go
type SubnetGateway interface {
    Prepare(ctx context.Context, cfg SessionConfig) error
    InjectIPv4(ctx context.Context, sessionID uint64, packet []byte) error
    CloseSession(ctx context.Context, sessionID uint64) error
}
// v1 default / only backend: GVisorNetstackBackend
```

- 优先直接依赖 upstream gVisor netstack 并固定 commit；可参考 Tailscale wgengine/netstack（BSD-3-Clause）实现模式，但不得引入整个 Tailscale 控制面。未来 Kernel/NAT Backend 必须保持接口不变。
- TCP Relay：gVisor netstack 接收 Engineer 的 SYN，创建 Engineer-facing TCP endpoint；RemLink 使用 host net.Dial("tcp", target) 建立 Site→PLC 连接，以 io.CopyBuffer 双向搬运字节；不做应用协议识别。
- UDP Relay：按 SessionID + Engineer 源/目标五元组维护轻量 flow mapping；每个 flow 使用 host UDPConn 与现场目标通信，收到回复后写回 gVisor UDP endpoint；flow 使用可配置 idle timeout 做 GC。
- 返回包必须使用 Session 封装（对称封装，v1 强制设计）：gVisor netstack 生成面向 Engineer 的 IPv4 响应包（SRC=现场 IP → DST=Engineer Overlay IP）后，Site 必须将其再封装成 Session UDP（Outer SRC=Site Overlay IP，Outer DST=Engineer Overlay IP，Payload=SessionHeader+RawIPv4）；不得把该 Raw Packet 直接交给 WireGuard（Server 对 Site Peer 的 AllowedIPs 只有 /32，源地址会被 cryptokey routing 拒绝）。Engineer SessionListener 校验后将 Raw IPv4 写入本机 Wintun 入站方向；Windows 最终看到 SRC=现场 IP、DST=Engineer Overlay IP。
- ICMP Echo Relay：v1 只支持 Echo Request/Reply；收到 Echo Request 后由 PingRelay 使用成熟 ICMP 库或受控系统 ping 从 Site 探测目标；目标响应后构造与原请求 ID/Sequence 对应的 Echo Reply Raw IPv4 经 SessionTransport 返回 Engineer；其他 ICMP 类型不支持。
- Site PREPARE 时 必须 对每个目标 CIDR 做 Windows Route Lookup：DIRECT（直连）允许；ROUTED（明确静态/动态路由）允许；DEFAULT_ONLY（仅默认路由）默认拒绝；NO_ROUTE 拒绝（SITE_NO_ROUTE）；OVERLAY_CONFLICT 拒绝。
- 建议初始 flow 上限（均可配置）：TCP 2048 flows、UDP 4096 flows、UDP idle timeout 60s。

#### 场景： 现场设备返回路径

- **当** PLC 192.168.13.10 响应 Site host socket 的连接
- **则** Site 经 gVisor netstack 生成面向 Engineer 的 Raw IPv4，再以 SessionHeader 封装经 UDP 发往 Engineer Overlay IP；PLC 无需任何 Overlay 返回路由

### 需求： R6 控制面与节点协议

系统 必须 分离 Bootstrap 与正常 Control：

- Bootstrap（公网 HTTP）：`http://SERVER_IP:8080/api/v1/bootstrap/...`
- WireGuard：`SERVER_IP:51820/udp`
- Control（仅 Overlay）：`ws://10.88.0.1:7001/control`

节点第一次启动无 WireGuard 时使用公网 Bootstrap API 获取 Overlay IP、Server WireGuard PublicKey 和 Endpoint；建立 WireGuard 后所有控制消息走 Overlay 内 Control WebSocket。

Node 身份 必须 包含：NodeID（UUID，首次运行生成并持久化）、NodeType（engineer/site）、NodeName、WireGuard PrivateKey（本地生成，仅本机保存）、WireGuard PublicKey（注册时提交 Server）、NodeToken（Server 注册成功后生成，用于后续身份验证；不是用户系统）。

Windows 本地敏感字段（WG PrivateKey、NodeToken）SHALL 优先使用 Windows DPAPI 保护后落盘，不以明文 YAML 保存私钥；为简化自用部署，首次注册 Join Token 可以明文保存在 Engineer/Site YAML 中。Server PrivateKey 保存到数据目录并设置严格文件权限。

Server 必须 维护可轮换 Join Token：新 Node 注册必须提供 Join Token；注册成功后改用 NodeToken。v1 不实现 HTTPS，公网 Bootstrap/管理 HTTP 的机密性不由 TLS 提供，作为部署边界在文档中说明。

Heartbeat 参数（默认值）：interval 5s；ONLINE=最近心跳 ≤15s；UNSTABLE=15–30s；OFFLINE=>30s；Reconnect backoff=1s,2s,5s,10s,30s 上限。

### 需求： R7 Server 设计

Server 必须 承担：Bootstrap API 和 Web UI、IPAM/Node Registry/Node Token、Linux Kernel WireGuard interface 和 Peer 编排（wgctrl）、Control WebSocket Hub、Session Manager、节点/Session 统计聚合、SQLite 持久化和日志查询；不逐包处理 WireGuard Overlay 数据。

Server wg0 必须 配置 Address 10.88.0.1/16（随 Overlay CIDR）、ListenPort 51820；每个 Node Peer 的 AllowedIPs 为其 Overlay /32。Linux 必须启用 IPv4 forwarding 并允许 wg0→wg0 转发；Server 不对 Overlay 做 SNAT。v1 不同时实现 kernel WireGuard 与 wireguard-go 两套 Server Backend。

Docker Compose 基线 必须 只授予 NET_ADMIN（不用 privileged: true），映射 /dev/net/tun、51820/udp、8080/tcp，挂载 ./data:/app/data，sysctls net.ipv4.ip_forward=1。镜像启动 Preflight 必须检查内核 WireGuard 能否创建接口；失败时明确报错并退出，不静默切换。

Web UI 必须 提供五个页面：

| 页面 | 核心内容 |
|---|---|
| Dashboard | Uptime、Overlay CIDR、在线 Engineer/Site、Active Session、流量 |
| Nodes | 名称、类型、Overlay IP、WG PublicKey 摘要、Handshake、App 状态、版本、LastSeen |
| Sessions | Engineer、Site、Remote CIDRs、状态、上下行流量、持续时间、强制断开 |
| Network | Overlay CIDR、Server IP、WG Port、Join Token 轮换 |
| Logs | 按时间/级别/模块/Node/Session 过滤事件 |

### 需求： R8 Engineer 设计

Engineer 进程（RemLinkEngineer.exe）SHALL 包含：Wails GUI、Bootstrap/Control Client、Node Identity Store、Wintun Adapter Manager、MuxTun/PacketMux、wireguard-go Device、Session Manager、SessionTransport（UDP Sender + Listener）、RouteManager、Stats、Logging。

GUI 必须 提供：Server 公网地址/Overlay 状态/本机虚拟 IP/Control 状态/版本显示；全部 Site 列表（Name、Overlay IP、Online、RemoteSubnetCapability、LastSeen）；选择 Site 后输入一个或多个 CIDR；连接前执行本地 CIDR 冲突检查；连接后显示 SessionID、目标 Site、CIDR、上传/下载、包数、时延和日志；Active Session 时禁止再选择第二个 Site。

Session READY 后 Engineer 必须 为每个 Remote CIDR 增加指向 RemLink Adapter 的 Windows 路由，由 RouteManager 统一创建并带 RemLink ownership metadata/本地状态记录以便异常恢复。不得通过降低 Metric 强抢冲突路由；发现 Remote CIDR 与本机现有非 RemLink 直连/静态/VPN 前缀重叠时直接拒绝建立 Session。

应用显式绑定网卡的情况：普通 Socket 由 Windows 根据 Remote Route 选择 RemLink Adapter；显式绑定物理网卡或固定源 IP 的工业软件不完全透明，应在其网络接口选择中选择 RemLink Adapter；v1 不增加源地址改写。

### 需求： R9 Site 设计

Site 进程（RemLinkSite.exe，Console）SHALL 包含：Bootstrap/Control Client、Node Identity Store、Wintun Adapter Manager、MuxTun/PacketMux、wireguard-go Device、UDP Session Listener :6200（Overlay only）、gVisor Netstack SubnetGateway、TCP/UDP/Ping Relay、Route Inspector、Flow Manager/Stats、Logging。

Site 启动能力检测 必须 按 7 步执行：加载 Node Identity 并获取 Server NetworkConfig → 检查 Overlay CIDR 与本地直连网络冲突 → 创建/复用 RemLink Wintun 并配置 Overlay IP/Prefix/MTU → 启动 wireguard-go 并等待 Server Overlay 可达 → 初始化 gVisor netstack SubnetGateway（TCP/UDP forwarder 与 flow limits；无需检查或修改 WinNAT）→ 启动仅绑定 OverlayIP:6200 的 Session UDP Listener（同一端口承载双向）→ 连接 Control WebSocket 并报告能力、OS、版本、netstack 状态与 flow capacity。

Console 输出 必须 展示 Server、Node、Overlay IP、WireGuard/Control/Remote Subnet/SubnetGateway 状态及 SESSION/ROUTE 事件。

### 需求： R10 数据库与数据模型

Server 必须 使用单 SQLite 数据库文件（如 /app/data/remlink.db），database/sql + 稳定 SQLite Driver，迁移采用成熟 migration 工具或嵌入 SQL migration 文件，不在代码中散落 CREATE TABLE。

核心表 必须 包含：

| 表 | 关键字段 |
|---|---|
| settings | key, value, updated_at |
| nodes | node_id, type, name, overlay_ip, wg_public_key, node_token_hash, status, version, os_version, last_seen |
| sessions | session_id, engineer_node_id, site_node_id, status, created_at, active_at, closed_at, error_code |
| session_cidrs | session_id, cidr |
| session_stats | session_id, tx_bytes, rx_bytes, tx_packets, rx_packets, updated_at |
| event_logs | time, level, module, node_id, session_id, message, fields_json |

Windows Node 不使用 Server SQLite；Engineer 与 Site 必须作为完全独立的便携式包发布，持久状态放在各自 EXE 所在目录，且不得共用运行文件。敏感字段用 DPAPI，至少保存 NodeID、NodeToken、WireGuard PrivateKey、Server URL、NodeName、最后一次 NetworkConfig 版本，以及 RemLink 创建过的路由状态用于 Reconcile。

### 需求： R11 API 与消息定义

Public Bootstrap API 必须 提供：GET /api/v1/server/info（Server ID、版本、WG Endpoint、Bootstrap 信息）、POST /api/v1/bootstrap/register（首次 Node 注册）、POST /api/v1/bootstrap/config（已注册 Node 用 NodeID+NodeToken 拉取最新 NetworkConfig）。

Admin Web API 必须 提供：GET /api/v1/admin/nodes；PATCH /api/v1/admin/nodes/{id}（改名/改 Overlay IP）；DELETE /api/v1/admin/nodes/{id}（撤销 Node）；GET /api/v1/admin/sessions；POST /api/v1/admin/sessions/{id}/disconnect（强制断开）；GET /api/v1/admin/network；PUT /api/v1/admin/network；GET /api/v1/admin/logs。v1 不做用户系统；如需保护仅实现可选的单一 Admin Token，不设计账户/角色体系。

Control WebSocket 消息 必须 包含：

| Type | 方向 | 关键字段 |
|---|---|---|
| HELLO | Node→Server | node_id, node_token, config_version, capabilities |
| WELCOME | Server→Node | server_time, network_config_version |
| NODE_LIST | Server→Engineer | sites[] |
| CREATE_SESSION | Engineer→Server | site_node_id, target_cidrs[] |
| PREPARE_SESSION | Server→Site | session_id, engineer_overlay_ip, target_cidrs[] |
| PREPARE_RESULT | Site→Server | ok, route_results[], subnet_gateway_status, tcp_capacity, udp_capacity, error |
| SESSION_CONFIG | Server→Engineer | session_id, peer_overlay_ip, cidrs[], mtu, udp_port |
| ROUTES_READY | Engineer→Server | session_id |
| SESSION_ACTIVE | Server→Engineer/Site | session_id |
| STOP_SESSION | 任意→Server / Server→Node | session_id, reason |
| SESSION_STATS | Engineer/Site→Server | cumulative counters |
| HEARTBEAT | 双向 | timestamp/status |
| REBOOTSTRAP_REQUIRED | Server→Node | config_version, reason |

错误码基线 必须 包含：SERVER_UNREACHABLE、JOIN_TOKEN_INVALID、NODE_AUTH_FAILED、OVERLAY_LOCAL_CONFLICT、ENGINEER_SESSION_EXISTS、SITE_OFFLINE、CIDR_INVALID、CIDR_LOCAL_CONFLICT、CIDR_OVERLAY_CONFLICT、SITE_NO_ROUTE、NETSTACK_UNAVAILABLE、FLOW_LIMIT_REACHED、SESSION_TIMEOUT、SESSION_INJECT_FAILED。

### 需求： R12 路由冲突、异常与恢复

Engineer 本地 CIDR 冲突算法 必须 使用 net/netip 做真正的 Prefix overlap（不使用字符串比较）：收集 Windows 有效路由和所有非 RemLink 直连前缀；默认路由 0.0.0.0/0 不作为冲突依据；任何更具体的现有本地/VPN 路由只要与 Remote CIDR 有重叠即拒绝。

Reconcile 规则：

- Engineer 启动时检查 RemLink Adapter 上由自身记录创建但没有对应 Active Session 的 Remote Routes，删除残留。
- Site 启动时初始化/重建 userspace SubnetGateway；不检查、不创建、不删除任何 Windows NAT。
- Site 清理上次异常退出遗留的 userspace flow/session 状态；Windows 系统路由、NAT、Forwarding 不需要恢复。
- Wintun Adapter 可复用，不要求每次退出删除；卸载/显式清理时再删除。

Server 重启后所有 Remote Session 必须 直接标记 CLOSED，不实现透明 Session Resume；Node 的 wireguard-go/Control 自动重连，Engineer 用户重新点击连接，Site userspace flow 随 Session 关闭并清理。

网络切换（Wi-Fi→网线/热点）时由 WireGuard 正常 roaming/重新握手处理；客户端只配置 Server 固定 Endpoint，Server Peer Endpoint 由 WireGuard 根据握手学习；Control WebSocket 断线使用指数退避重连。

### 需求： R13 日志、统计与可观测性

日志模块 必须 分为：CORE、BOOTSTRAP、WG、IPAM、CONTROL、SESSION、ROUTE、NETSTACK、TUN、SUBNET、SYSTEM。高频 packet 日志默认关闭；DEBUG 也禁止逐包打印 Payload，只允许限速采样（防止 PLC 下载/RDP 时写满磁盘）。

Session 流量统计 必须 以 Engineer 视角定义：Upload=Engineer→Site LAN，Download=Site LAN→Engineer；Engineer PacketMux 在拦截 Remote 出包时累计上传，Engineer SessionListener 在校验并注入 Remote Reply 前累计下载；每 5 秒向 Server 上报累计值。Server 不为了统计进入 WireGuard packet path；节点层 WireGuard 总流量可由 wgctrl 读取作为辅助指标。

### 需求： R14 安全边界

- WireGuard 负责公网数据通道加密、认证和完整性；RemLink 不重复实现。
- 每个 Node 独立 WireGuard KeyPair；PrivateKey 不上传 Server。
- Server WireGuard Peer 只允许该 Node 的 /32 Overlay IP，防止节点伪造其他 Overlay Source。
- Engineer 与 Site 的 Session UDP Listener 都只绑定 Overlay 地址，并按方向校验 SessionID、Outer Source、Inner Source/Target CIDR、Inner Destination。
- Join Token 仅用于首次加入；NodeToken 用于应用层身份。
- v1 按需求不实现 HTTPS；公网暴露时可由外部反向代理/防火墙补充，但不是 v1 内核功能。
- 绝不自动关闭 Windows Firewall；如需要规则，只创建带 RemLink 前缀、可识别且可回滚的规则。

### 需求： R15 项目结构、关键接口与依赖管理

Monorepo 结构 必须 为：

```
remlink/
├─ cmd/
│  ├─ server/
│  ├─ engineer/
│  └─ site/
├─ internal/
│  ├─ model/
│  ├─ protocol/
│  ├─ config/
│  ├─ identity/
│  ├─ ipam/
│  ├─ control/
│  ├─ session/
│  ├─ subnet/
│  │  ├─ header.go
│  │  ├─ sender.go
│  │  ├─ listener.go
│  │  └─ validator.go
│  ├─ overlay/
│  │  ├─ clientwg/
│  │  │  ├─ device.go
│  │  │  ├─ muxtun.go
│  │  │  └─ adapter.go
│  │  └─ serverwg/
│  ├─ platform/
│  │  ├─ windows/
│  │  │  ├─ route/
│  │  │  ├─ netinfo/
│  │  │  ├─ socket/
│  │  │  └─ dpapi/
│  │  └─ linux/
│  │     └─ netlink/
│  ├─ subnetgateway/
│  │  ├─ netstack/
│  │  ├─ tcprelay/
│  │  ├─ udprelay/
│  │  └─ pingrelay/
│  ├─ database/
│  ├─ logging/
│  └─ stats/
├─ frontend/
│  ├─ server/
│  └─ engineer/
├─ deploy/docker/
└─ docs/
```

关键接口 必须 为：

```go
type RouteManager interface {
    AddRemote(prefix netip.Prefix, ifIndex uint32) error
    RemoveRemote(prefix netip.Prefix) error
    Conflicts(prefix netip.Prefix) ([]RouteConflict, error)
    Lookup(dst netip.Addr) (RouteInfo, error)
}

type SubnetGateway interface {
    Prepare(ctx context.Context, cfg SessionConfig) error
    InjectIPv4(ctx context.Context, sessionID uint64, packet []byte) error
    CloseSession(ctx context.Context, sessionID uint64) error
}

type SessionTransport interface {
    SendIPv4(sessionID uint64, peer netip.Addr, packet []byte) error
}
```

MuxTun 约束：必须实现所固定 wireguard-go 版本的 tun.Device 接口，go.mod 锁定 wireguard-go commit/version，升级前先跑 MuxTun Windows POC；base Device 的 Name/MTU/Events/BatchSize 按原语义代理；Read 方向负责 OS→WireGuard 分类；Write 方向负责 WireGuard→Windows 原样写入（Engineer SessionReceiver 的 Remote Reply 也通过统一 Wintun inbound writer 注入；Site netstack 数据不注入 Windows Wintun）；Close 必须可幂等，并使所有 goroutine 退出。

依赖管理：所有 Go module、NPM package、Wintun DLL、gVisor commit 均固定版本/commit，禁止生产构建使用 latest 浮动依赖（gVisor netstack API 不保证稳定，升级必须经过 POC）；Wintun DLL 使用 go:embed 打入 EXE，首次运行释放到当前角色 EXE 所在目录，不得通过 `%ProgramData%` 在 Engineer 与 Site 之间共享；发布包提供 THIRD_PARTY_NOTICES（WireGuard/Wintun/gVisor 及参考复用代码许可证，如直接采用 Tailscale 代码片段遵循其 BSD-3-Clause）；Windows Engineer 与 Site 都要求管理员权限（创建 Wintun、配置接口/路由；Site v1 不修改 WinNAT 或 IP Forwarding）。

### 需求： R16 阶段化开发与验收

开发 必须 按 Phase 0–10 顺序执行（详见 tasks.md），每阶段通过明确验收后才进入下一阶段；先完成 Phase 1–7 网络 Gate，再投入完整 GUI。Gate A–D 为关键关卡：

- Gate A：wireguard-go 能通过 MuxTun 使用唯一 Wintun，正常 Overlay 通信。
- Gate B：Remote Packet 被 MuxTun 截获后，普通 UDP Socket 的外层 Overlay Packet 能通过同一 Wintun 再进入 WireGuard，不死锁、不无限循环。
- Gate C：Site 收到 Session Raw IPv4 后能注入 gVisor netstack；TCP/UDP Forwarder 能建立 host socket 到现场目标并完成双向数据搬运。
- Gate D：gVisor netstack 能把返回数据重新构造成 Engineer 看到的原始目标 IP 流，并通过 Session UDP 反向封装回 Engineer；Ping Relay 能正确返回 ICMP Echo Reply。

最终验收 必须 通过规格书第 19 章 T01–T18 全部场景（详见 checklist.md）。测试 TCP/102、TCP/502、HTTP、RDP、ICMP Echo、UDP 的目的是证明通用 TCP/UDP/Ping userspace gateway 成立；生产代码中禁止出现 S7Proxy、ModbusProxy、HTTPProxy 等应用协议专用模块；PingRelay 是唯一明确的 ICMP Echo 诊断模块。

## 修改需求

无（绿地项目）。

## 删除需求

无（绿地项目）。
