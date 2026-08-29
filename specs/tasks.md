# 开发任务

> 对应规格书第 18 章分阶段开发计划。执行方式：不要一次性写完整个项目；网络数据面 POC 先于完整 GUI；每一阶段通过明确验收后再进入下一阶段。任何"为了能跑"而改变核心路由模型的实现必须先修改 spec，不允许隐藏偏离。

- [x] Phase 0: 仓库与基础模型
  - [x] 建立 Go monorepo 目录结构（cmd/server、cmd/engineer、cmd/site、internal/*、frontend/、deploy/docker/、docs/）
  - [x] internal/logging：slog + 成熟滚动文件库，11 个模块分类（CORE/BOOTSTRAP/WG/IPAM/CONTROL/SESSION/ROUTE/NETSTACK/TUN/SUBNET/SYSTEM），高频 packet 日志默认关闭
  - [x] internal/config：YAML 配置加载（Engineer/Site 支持首次注册 Join Token 便捷配置，参考规格书附录 B）
  - [x] internal/protocol：SessionHeader（RMLK，20 字节头）编解码、Control WebSocket 消息类型定义、14 个错误码常量（以权威规格实际枚举为准）
  - [x] CI 基础：go build / go test / go vet；不写 GUI
  - [x] 验收：monorepo 可构建，协议模型单元测试通过

- [ ] Phase 1: 单 Wintun + wireguard-go POC（Gate A）
  - [x] internal/platform/windows：Wintun Adapter Manager（创建/复用名为 RemLink 的 Wintun，配置 Overlay IP/Prefix/MTU 1280）
  - [x] internal/overlay/clientwg/muxtun.go：MuxTun 实现 tun.Device（本阶段为直通模式，Read/Write 代理真实 Wintun，Name/MTU/Events/BatchSize 按原语义代理，Close 幂等）
  - [x] internal/overlay/clientwg/device.go + adapter.go：wireguard-go Device 集成（go.mod 锁定 wireguard-go commit）
  - [x] Wintun DLL go:embed 并首次运行释放到 Engineer/Site 各自 EXE 所在目录
  - [x] 搭建手动配置的测试 Server wg0（POC 配置与操作手册已提供；真实主机执行计入 Gate A）
  - [ ] 验收（Gate A）：两个 Windows 节点通过 Server ping 对方 Overlay IP；系统中只有一块 RemLink 虚拟网卡

- [x] Phase 2: Server IPAM + Bootstrap
  - [x] internal/database：SQLite + migration 工具化（settings/nodes/sessions/session_cidrs/session_stats/event_logs 六张表）
  - [x] Bootstrap API：GET /api/v1/server/info、POST /api/v1/bootstrap/register、POST /api/v1/bootstrap/config
  - [x] Join Token 管理（可轮换，存 settings）
  - [x] internal/ipam：地址分配/持久化/释放/手动修改（Server 地址唯一权威）
  - [x] internal/overlay/serverwg：wgctrl 创建 wg0（10.88.0.1/16，ListenPort 51820）、Peer 动态编排（/32 AllowedIPs）、IPv4 forwarding + wg0→wg0 放行、不做 SNAT
  - [x] Windows 端 internal/identity：Node Identity Store（NodeID/NodeName/WG KeyPair/NodeToken），敏感字段 DPAPI 落盘
  - [x] 验收：10 个模拟 Node 注册，地址唯一且重启不变；Peer 动态创建/删除测试通过

- [x] Phase 3: Control Plane
  - [x] internal/control：Overlay Control WebSocket Hub（ws://10.88.0.1:7001/control，仅 Overlay 监听）
  - [x] HELLO/WELCOME 握手、HEARTBEAT（5s 间隔；ONLINE ≤15s / UNSTABLE 15–30s / OFFLINE >30s）
  - [x] NODE_LIST 下发 Engineer（Site 在线列表：Name、Overlay IP、Online、RemoteSubnetCapability、LastSeen）
  - [x] 能力上报（capabilities、OS、版本、netstack 状态、flow capacity）
  - [x] 断线指数退避重连（1s, 2s, 5s, 10s, 30s 上限）
  - [x] 验收：Control Hub 集成测试验证节点上下线状态传播与 Engineer Site 列表

- [ ] Phase 4: Engineer PacketMux POC
  - [x] MuxTun Read 出方向分类：Overlay CIDR → wireguard-go；Remote CIDR → SubnetSender 有界队列；其他 → 丢弃 + 限速日志
  - [x] internal/platform/windows/route：RouteManager（AddRemote/RemoveRemote/Conflicts/Lookup，带 ownership metadata）
  - [x] Engineer 本地 CIDR 冲突检查（net/netip prefix overlap，收集非 RemLink 直连/静态/VPN 前缀；0.0.0.0/0 不作为冲突依据）
  - [ ] 验收：给 Wintun 添加测试 Remote CIDR 路由后能截获完整 IPv4 Packet，Overlay 普通流量不受影响

- [ ] Phase 5: 同网卡 UDP 再入路径（Gate B）
  - [x] internal/subnet/sender.go：SubnetSender（普通 UDP Socket 绑定 Engineer Overlay IP → Site OverlayIP:6200，不手写外层 IP/UDP Header）
  - [x] internal/subnet/header.go + validator.go：SessionHeader 编解码与双向收包校验（Magic/Version/SessionID/PayloadLen/外层源/内层源目 CIDR）
  - [x] internal/subnet/listener.go：Engineer 端 Session UDP Listener（仅绑定 OverlayIP:6200）
  - [x] MuxTun 并发模型：有界 channel + 独立 Sender goroutine、Read 不阻塞、全 Remote batch 时继续读取防 busy loop、统一 Wintun writer 序列化、buffer pool
  - [ ] 验收（Gate B）：外层 UDP 再入同一 Wintun 无死锁/无限循环，经 wireguard-go 发往 Server

- [ ] Phase 6: Site gVisor netstack POC（Gate C）
  - [x] internal/subnetgateway/netstack：gVisor netstack 集成（pinned commit，channel.Endpoint，InjectInbound 入 / Read 出）
  - [x] internal/subnetgateway/tcprelay：TCP Forwarder（接收 SYN → host net.Dial("tcp", target) → io.CopyBuffer 双向搬运）
  - [x] internal/subnetgateway/udprelay：UDP flow mapping（SessionID + 五元组，host UDPConn，idle timeout GC，默认 60s 可配置）
  - [x] Site 端 Session UDP Listener（仅 OverlayIP:6200）+ SubnetGateway.InjectIPv4 接入
  - [ ] 验收（Gate C）：TCP Forwarder 通过 host net.Dial 连接测试目标；UDP Forwarder 双向收发

- [ ] Phase 7: 双向 Session 回传 + Ping Gate（Gate D，关键 Gate）
  - [x] netstack egress Raw IPv4 → SessionTransport 对称 SessionHeader 封装（Outer SRC=Site Overlay IP → DST=Engineer Overlay IP）回传 Engineer
  - [x] Engineer SessionListener 校验（SessionID/Outer Source/Inner Source CIDR/Inner Destination）后经统一 Wintun inbound writer 注入
  - [x] internal/subnetgateway/pingrelay：ICMP Echo Relay（x/net/icmp 或受控系统 ping；保留原 ID/Sequence 构造 Echo Reply）
  - [ ] 验收（Gate D）：TCP/UDP 往返闭环；Engineer ping 现场 IP 得到正确 Echo Reply（T07/T08/T09 场景）

- [ ] Phase 8: 完整 Session 状态机
  - [x] Server Session Manager：CREATING/PREPARING_SITE/READY/ACTIVE/STOPPING/CLOSED/FAILED 全状态流转
  - [x] 全链路消息：CREATE_SESSION → PREPARE_SESSION → PREPARE_RESULT → SESSION_CONFIG → ROUTES_READY → SESSION_ACTIVE；STOP_SESSION 双向
  - [x] Site PREPARE：每 CIDR Windows Route Lookup（DIRECT/ROUTED/DEFAULT_ONLY/NO_ROUTE/OVERLAY_CONFLICT）+ netstack capability + flow capacity（TCP 2048/UDP 4096 可配置）检查
  - [x] 错误码全量落地（含 SESSION_TIMEOUT/FLOW_LIMIT_REACHED/NETSTACK_UNAVAILABLE/SESSION_INJECT_FAILED）
  - [x] SESSION_STATS：Engineer 视角 Upload/Download 累计，每 5 秒上报 Server 并持久化
  - [x] Reconcile：Engineer 启动清理残留 Remote Route；Site 启动重建 SubnetGateway 并清理遗留 flow；Server 重启 Session 全部 CLOSED
  - [ ] 验收：T03/T04/T05/T13/T16 场景通过

- [ ] Phase 9: Engineer GUI + Server Web UI
  - [x] Engineer Wails GUI（frontend/engineer + Vue 3）：Server/Overlay/Control 状态、Site 列表、多 CIDR 输入、本地冲突预检、Session 详情（SessionID/流量/包数/时延/日志）、断开按钮；Active 时禁止选择第二个 Site
  - [x] Server Web UI（frontend/server + Vue 3 + Vite + Go embed）：Dashboard/Nodes/Sessions/Network/Logs 五页面
  - [x] Admin Web API 全量实现（nodes/sessions/network/logs 的 CRUD 与强制断开；可选单一 Admin Token）
  - [ ] 验收：UI 操作可驱动完整业务流程；UI 中不复制网络业务逻辑

- [ ] Phase 10: 多节点与重复现场网段
  - [x] 多 Engineer 多 Site 并发在线与会话（自动化状态机测试完成；真实四节点证据仍归 T11/T12）
  - [x] 重复现场网段隔离验证：SessionID + Engineer Overlay IP 区分（实现级隔离测试完成；T11/T12 仍待真实环境）
  - [ ] 端到端验收测试 T01–T18 全量执行（见 checklist.md）
  - [x] 打包与部署：deploy/docker Compose 基线（仅 NET_ADMIN、/dev/net/tun、Preflight 检查）、THIRD_PARTY_NOTICES
  - [ ] 验收：Engineer-A→Site-A 与 Engineer-B→Site-B 同时访问相同 192.168.13.0/24 互不串流

# 任务依赖

- Phase 0 → Phase 1 → Phase 2 → Phase 3 → Phase 4 → Phase 5 → Phase 6 → Phase 7 → Phase 8 → Phase 9 → Phase 10（规格书要求每阶段通过验收后进入下一阶段）
- 例外：Phase 1 的 Windows 侧 POC 与 Phase 2 的 Server 侧可部分并行（Phase 1 验收用手动配置的 kernel WireGuard Server）
- Gate A=Phase 1 验收、Gate B=Phase 5 验收、Gate C=Phase 6 验收、Gate D=Phase 7 验收；Gate D 是全项目最重要关卡，未通过前不投入 Phase 9 GUI
