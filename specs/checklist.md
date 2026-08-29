# 检查清单

> 对应规格书第 18–20 章。每项检查须在对应实现完成后逐条核验；任何一项不通过都视为偏离规格。
>
> `[x]` 表示代码/静态策略/自动化验证已经完成；Gate 与 T01–T18 仍只接受真实主机证据，保持 `[ ]` 不代表自动化测试失败。

## 关键技术 Gate

- [ ] Gate A：wireguard-go 能通过 MuxTun 使用唯一 Wintun，正常 Overlay 通信
- [ ] Gate B：Remote Packet 被 MuxTun 截获后，普通 UDP Socket 的外层 Overlay Packet 能通过同一 Wintun 再进入 WireGuard，不死锁、不无限循环
- [ ] Gate C：Site 收到 Session Raw IPv4 后能注入 gVisor netstack；TCP/UDP Forwarder 能建立 host socket 到现场目标并完成双向数据搬运
- [ ] Gate D：gVisor netstack 能把返回数据重新构造成 Engineer 看到的原始目标 IP 流，并通过 Session UDP 反向封装回 Engineer；Ping Relay 能正确返回 ICMP Echo Reply

## 架构不可变约束（规格书第 0/20 章）

- [x] 每个 Engineer/Site 进程只创建一块名为 RemLink 的 Wintun；无第二块 Subnet Adapter、无 Transit CIDR
- [x] Windows WireGuard 使用嵌入进程的 wireguard-go；无 WireGuardNT 独立接口
- [x] wireguard-go 必须通过 MuxTun 包装唯一 Wintun
- [x] v1 不调用 WinNAT、不启用 Windows IP Forwarding，也不修改第三方 NAT
- [x] Server 使用 Linux Kernel WireGuard；Go Server 不逐包中转 Overlay
- [x] Server Peer AllowedIPs 只放 Node Overlay /32；绝不把现场 Remote CIDR 加进 Server WireGuard AllowedIPs
- [x] 所有 Engineer/Site 的 WireGuard 公网 Peer 都只有 Server；无 P2P 代码
- [x] 无二层代码（ARP/以太网帧/TAP/Bridge/DCP/LLDP）
- [x] PacketMux 只做 IPv4 CIDR 分类，不解析 TCP/UDP/ICMP 或应用层协议
- [x] Remote Packet 外层传输使用普通 UDP Socket over Overlay；未手写外层 IP/UDP 协议栈
- [x] Remote CIDR 由 Engineer Session 动态下发；Site 配置文件不保存现场网段
- [x] 不同 Site 的 Remote CIDR 可完全相同（实现未引入人为限制）
- [x] 一个 Engineer 同时只能有一个 Site Session
- [x] Site Subnet Gateway 复用 gVisor netstack（固定 commit），无自研 TCP/IP 栈
- [x] 无 S7Proxy/ModbusProxy/HTTPProxy/RDPProxy 等应用协议专用模块；PingRelay 是唯一 ICMP 模块
- [x] 所有 Windows 网络修改集中在 platform/windows；GUI/业务层无散落 PowerShell
- [x] 所有依赖固定版本（Go module/NPM/Wintun DLL/gVisor commit）；生产构建无 latest 浮动依赖
- [ ] 先完成 Phase 1–7 网络 Gate，再投入完整 GUI
- [x] 敏感字段（WG PrivateKey/NodeToken）使用 DPAPI 保护落盘，无明文 YAML 私钥
- [x] Engineer/Site YAML 可保存首次注册 Join Token；CLI/环境变量可覆盖 YAML
- [x] Wintun DLL go:embed 打入 EXE，Engineer/Site 各自释放到本端 EXE 目录，不使用共享 ProgramData
- [x] Engineer、Site、Server 使用三个独立目录和 ZIP，互不包含其他端可执行文件
- [x] THIRD_PARTY_NOTICES 随发布包提供（WireGuard/Wintun/gVisor/参考代码许可证）
- [x] 绝不自动关闭 Windows Firewall；防火墙规则仅创建带 RemLink 前缀、可识别且可回滚的规则
- [x] v1 无 0.0.0.0/0 Exit Node 模式

## 验收测试（规格书第 19 章）

- [ ] T01：Engineer 与 Site 都在线，仅有一个 RemLink 虚拟网卡
- [ ] T02：Engineer ping Site Overlay IP，通过 Server 中转成功
- [ ] T03：Engineer 输入 192.168.13.0/24，本机无冲突，Session 可建立
- [ ] T04：Engineer 本地本身是 192.168.13.0/24，建立前拒绝 CIDR_LOCAL_CONFLICT
- [ ] T05：Site 对 192.168.13.0/24 无路由，拒绝 SITE_NO_ROUTE
- [ ] T06：Site 同时运行 Hyper-V/Docker/已有 WinNAT，Remote Session 仍可正常建立；不读取/修改第三方 NAT
- [ ] T07：Engineer ping 现场 PLC，ICMP Echo Relay 成功；延迟可比直接链路略高
- [ ] T08：TCP 102 / 502 / HTTP / RDP 通过通用 TCP Relay 正常连接；无应用协议专用代码
- [ ] T09：普通单播 UDP 流量通过 UDP flow relay 往返成功；idle flow 可回收
- [ ] T10：Session 同时下发 192.168.13.0/24 与 192.168.21.0/24，两网段均可访问
- [ ] T11：Engineer-A→Site-A、Engineer-B→Site-B；两 Site 都是 192.168.13.0/24，同时访问成功互不串流
- [ ] T12：同 Site 两个 Engineer 访问不同/相同目标设备，SessionID + Overlay IP 隔离正确
- [ ] T13：Engineer 尝试再连接第二个 Site，拒绝 ENGINEER_SESSION_EXISTS
- [ ] T14：Engineer Wi-Fi 切手机热点，WireGuard/Control 自动恢复
- [ ] T15：Server 重启，Node 自动重连；旧 Session CLOSED，用户可重新连接
- [ ] T16：Engineer 崩溃后重启，清理残留 Remote Route
- [ ] T17：Server 修改某 Node Overlay IP，Node 自动 Re-bootstrap 后使用新 IP
- [ ] T18：Server 修改整个 Overlay CIDR，所有 Session 关闭、Nodes 重新分配/配置；Site 无需重建系统 NAT

## 功能完整性检查

- [x] Session 状态机七状态（CREATING/PREPARING_SITE/READY/ACTIVE/STOPPING/CLOSED/FAILED）按规格流转
- [x] SessionHeader 20 字节头（RMLK/Version/Type/Flags/SessionID/PayloadLen/Reserved）双向对称封装
- [x] Session 双向收包校验完整（SessionID/外层源/内层源/内层目 CIDR/IPv4 Total Length）
- [x] Session UDP Listener 仅绑定 Overlay 地址，不监听公网或 0.0.0.0
- [x] Bootstrap API 三接口 + Admin Web API 八接口按规格实现
- [x] Control WebSocket 13 种消息按规格实现
- [x] 14 个错误码全量落地（以权威规格实际枚举为准）
- [x] SQLite 六张表 + migration 工具化（无散落 CREATE TABLE）
- [x] Overlay CIDR 修改七步流程完整实现（含 REBOOTSTRAP_REQUIRED）
- [x] Heartbeat 参数按默认值实现（5s 间隔；ONLINE ≤15s / UNSTABLE 15–30s / OFFLINE >30s；退避 1/2/5/10/30s）
- [x] Flow limits 默认值（TCP 2048 / UDP 4096 / UDP idle 60s）且可配置
- [x] Site PREPARE 路由检查覆盖 DIRECT/ROUTED/DEFAULT_ONLY/NO_ROUTE/OVERLAY_CONFLICT
- [x] Engineer 本地冲突检查使用 net/netip prefix overlap（非字符串比较）
- [x] Session 统计每 5 秒上报；Upload/Download 按 Engineer 视角定义
- [x] 11 个日志模块分类正确；高频 packet 日志默认关闭、DEBUG 禁止逐包打印 Payload
- [x] Web UI 五页面（Dashboard/Nodes/Sessions/Network/Logs）功能齐备
- [x] Engineer GUI 功能齐备（Site 列表/多 CIDR 输入/冲突预检/Session 详情/断开）
- [x] 端口占用符合基线：51820/udp（公网 WG）、8080/tcp（Web+Bootstrap）、7001/tcp（仅 Overlay Control）、6200/udp（仅 Overlay Session）
- [x] Docker Compose 只授予 NET_ADMIN（非 privileged）；启动 Preflight 检查内核 WireGuard
- [x] MuxTun Close 幂等且使全部 goroutine 退出
