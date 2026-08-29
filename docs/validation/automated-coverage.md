# 自动化验证覆盖

权威的 R1–R16 与 Phase 0–10 实现/证据矩阵见 `requirements-evidence.md`；本文补充自动化覆盖细节。

自动化测试覆盖协议 framing、精确错误码、Bootstrap/IPAM/数据库、Control 状态、PacketMux、Session UDP 验证、完整 Session 状态机、进程内 gVisor TCP/UDP/ICMP 往返、Admin 网络迁移、rebootstrap 通知和重复 CIDR 隔离。测试还固定 v1 的 `DEFAULT_ONLY` 拒绝、Session 级注入失败清理、拒绝注入后的监听连续性、限速安全警告、WG `/32` peer、唯一 Server client peer、Admin 日志时间过滤和 Site Console 状态/事件字段。

回归套件还覆盖：

- PREPARE 失败关联、重试和陈旧拒绝隔离；
- netstack 幂等、PacketMux 计数/队列丢包、严格 IPv4 framing、Sender 关闭 race、UDP 空闲回收；
- Site flow 上限、Server WG 密钥并发发布、可用 Overlay 主机地址、`/0` 拒绝和事件模块分类；
- 迁移期间 Session quiesce、迁移后 Session 配置、旧 Control 通知顺序、Overlay 冲突事件、Control rebind 回滚和 Linux 内核变更回滚；
- 全宽随机 SessionID 的无损十进制 JSON、新 Bootstrap 清理陈旧 Session、严格公网/Overlay endpoint、Site 容量拒绝和 Engineer 单 Session GUI 门禁。

验收工具自测证明：没有证据文件不能 PASS；Gate 前置条件被执行；证据 SHA-256 与大小会持久化；记录后篡改可被发现。架构扫描还禁止 Server 采集器导出 WireGuard dump/私钥，检查 Docker data mount、可配置 WireGuard 映射、发布 Compose 权限和预编译二进制入口。

发布验证器会独立解压 ZIP、检查必需项、重算每条 SHA-256、拒绝未纳入清单的文件，并运行包内验收初始化器，保持全部 Gate/T 为 `NOT_RUN`。

前端渲染 QA 的开发 fixture 只提供展示数据，实际使用与内嵌构建相同的 Vue 组件。最近一次检查覆盖 Server Nodes/Sessions/Network/Logs 交互以及 Engineer Site capability/LastSeen 和导航；页面有有效 DOM，浏览器无 warning/error。该 UI QA 不声称物理网络操作成功。

以下映射只作为支持证据，不能把真实验收场景标记 PASS：

| 验收区域 | 自动化证据 | 仍需真实证据 |
|---|---|---|
| T03/T05/T13 | `internal/session` manager/runtime 测试 | 管理员 Engineer/Site 与实际路由 |
| T04/T16 | Windows 路由、冲突、reconcile 测试 | 真实主机前后路由清单 |
| T07/T08/T09 | gVisor 主机套接字往返 | 两台 Windows 间 PLC/服务 |
| T10 | 多 CIDR 状态机与 PacketMux | 两个物理现场子网 |
| T11/T12 | 并发 Session 与重复 CIDR flow key | 四节点载荷隔离 |
| T15 | SQLite 关闭开放 Session 与 Control 重连 | 流量中 Server 重启 |
| T17/T18 | Admin 更新/rebootstrap/迁移回滚测试 | 真实适配器和 `wg0` 迁移 |

Gate A–D 和 T01–T18 在执行手册证据齐全前保持 `NOT_RUN`。
