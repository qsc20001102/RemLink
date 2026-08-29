# RemLink v1.0 T01–T18 验收执行手册

本文是证据模板，不代表真实环境已经通过。每次运行先执行 `scripts/validation/New-AcceptanceRun.ps1`；在指定主机证据齐全前，场景保持 `NOT_RUN`。每个 PASS 都必须在同一运行目录内包含时间戳、节点名、应用日志和指定主机快照。

只能通过 `Set-AcceptanceResult.ps1` 记录结果；脚本要求证据文件位于运行目录内，并保存 SHA-256 和大小。每批记录后及归档前执行 `Test-AcceptanceRun.ps1`。验证器会拒绝缺失、移动或修改过的证据并执行 Gate 前置条件，但不会代替人工判断所选截图是否真正证明场景。

~~~powershell
./scripts/validation/Set-AcceptanceResult.ps1 -RunDirectory evidence/run-001 -ID T01 -Status PASS -EvidencePath engineer-a/inventory.json,site-a/inventory.json
./scripts/validation/Test-AcceptanceRun.ps1 -RunDirectory evidence/run-001
~~~

## 必需拓扑

- 一台 Linux Server：内核 WireGuard、公网可达 UDP endpoint，只公开受保护的 Bootstrap/Admin 入口和 `51820/udp`。
- Engineer-A、Engineer-B、Site-A、Site-B：分别位于受支持 Windows 系统，RemLink 进程均以管理员运行。
- Site-A 和 Site-B 各自可达一个 `192.168.13.0/24` 测试网段；Site-A 另外可达 `192.168.21.0/24`。
- 目标提供 ICMP Echo、TCP 102、TCP 502、HTTP、RDP 和普通 UDP echo；不能用 RemLink 内部的协议专用代码替代。

在相关场景前后，分别在 Windows 节点运行 `Collect-WindowsEvidence.ps1`，在 Server 运行 `Collect-ServerEvidence.sh`。第三方 WinNAT 只做只读快照。Server 采集器故意只使用公开 WireGuard 视图，绝不执行可能暴露私钥或预共享密钥的 `wg show ... dump`。

## 场景步骤与通过条件

| ID | 执行与必需证据 | 通过条件 |
|---|---|---|
| T01 | 启动 Engineer/Site，采集 Admin 节点页和 Windows inventory | 两者 ONLINE；每节点恰好一个 `RemLink` 适配器；无 WireGuardNT |
| T02 | Engineer 执行 `ping <Site Overlay IP>`，采集 ping 和 Server `wg show` | 经 Server hub 回复成功，WireGuard 计数增加 |
| T03 | Engineer 连接 Site，CIDR 为 `192.168.13.0/24`；采 GUI 与 Session 日志 | Session 到 ACTIVE，且只出现一条自有 Remote route |
| T04 | Engineer 本地接入 `192.168.13.0/24` 后请求相同远程网段 | 在 Server 创建前以 `CIDR_LOCAL_CONFLICT` 拒绝 |
| T05 | 删除 Site 到 `192.168.13.0/24` 的全部非默认路由后请求 | PREPARE 以 `SITE_NO_ROUTE` 失败；无 ACTIVE Session 或泄漏 flow |
| T06 | T03/T07–T09 前后分别快照 Hyper-V/Docker/WinNAT | 远程访问成功，第三方 NAT 快照完全一致 |
| T07 | 对 PLC/测试目标执行 `Test-RemoteTargets.ps1` | 四个 Echo Reply 保留目标身份，无注入错误 |
| T08 | 探测 TCP 102/502/80/3389，并对每项运行一次真实连接 | 全部服务经通用 TCP relay 连接；源码审计无协议代理 |
| T09 | 向 UDP echo 发送不同数据报，等待超过 idle timeout 后再发 | 两次均成功；间隔后 flow 数回到基线 |
| T10 | 一个 Session 同时请求 `192.168.13.0/24` 和 `192.168.21.0/24` | 两网段目标均通过 ICMP 及至少一项 TCP/UDP |
| T11 | Engineer-A→Site-A 与 Engineer-B→Site-B 同时连接；两个 Site 都声明 `192.168.13.0/24` | 两个 SessionID 保持 ACTIVE；载荷标记只回到来源 Engineer |
| T12 | 两个 Engineer 同时连接 Site-A，以不同标记访问同一 endpoint/port | Site flow/事件正确区分 SessionID 和 Engineer Overlay IP，无串流 |
| T13 | Engineer-A 已 ACTIVE 时请求第二个 Site | UI 禁止选择；构造请求被 Server 以 `ENGINEER_SESSION_EXISTS` 拒绝 |
| T14 | 持续 ping/TCP 时把 Engineer Wi-Fi 切到手机热点 | WireGuard 与 Control 在有限中断后重连；用户可重建不会自动恢复的 Session |
| T15 | ACTIVE 流量期间重启 Server | 数据库非终态 Session 变 CLOSED；节点自动重连并能新建 Session |
| T16 | 强制终止 Engineer，管理员重启，采集前后路由 | 接受新 Session 前 reconcile 删除自有陈旧 Remote route |
| T17 | 在 Admin UI 修改一个在线 Node 的 Overlay IP | Session 关闭；Node 公开 Bootstrap；新 IP 下单适配器重连 |
| T18 | 在 Admin UI 修改完整 Overlay CIDR | 全部 Session 关闭；`wg0`、数据库分配、Node 适配器、Control 和 Bootstrap 使用递增配置；Site NAT 快照不变 |

## 建议证据目录

~~~text
evidence/run-001/
  acceptance-run.json
  T01-T18-runbook.md
  server/
  engineer-a/
  engineer-b/
  site-a/
  site-b/
  targets/
  screenshots/
~~~

文件名应包含场景 ID、主机和 UTC 时间。严禁把 Join Token、Admin token、Node Token、WireGuard 私钥、预共享密钥或数据包载荷放入证据。需要展示配置时只截取非敏感字段。

## Gate 记录

- Gate A 使用 T01/T02 证据，并增加双向 Overlay ping。
- Gate B 还需持续远程流量，证明同一个适配器承载普通外层 UDP 且无 busy loop/deadlock。
- Gate C 需要目标侧 TCP/UDP echo 和 Site netstack 日志。
- Gate D 需要返回 raw IPv4 身份以及 T07/T08/T09 证据。

不能仅根据单元/进程内测试标记 Gate 或场景 PASS。自动化测试证明实现不变量，本手册证明指定物理部署行为。

## 一次完整执行顺序

1. 初始化运行目录，记录版本、包哈希、拓扑、主机名、时间同步状态和人员。
2. 采集五台主机基线，确认无秘密进入证据。
3. 先执行 Gate A 与 T01/T02，验证单适配器和中心辐射 Overlay。
4. 执行 T03–T10，覆盖路由冲突、Site 路由、NAT 不变、ICMP/TCP/UDP 和多 CIDR。
5. 执行 T11–T13，覆盖并发、重复 CIDR、同 Site 多 Engineer 和单 Engineer 限制。
6. 执行 T14–T18，覆盖网络切换、Server 重启、异常退出、节点 IP 与 Overlay 迁移。
7. 采集结束快照、应用日志和目标侧证据，记录每项 PASS/FAIL/NOT_RUN。
8. 执行验证器；修复证据路径/哈希问题，但不得为通过验证器而改写真实结果。
9. 将完整运行目录只读归档，并单独保存发布包和外部 ZIP 哈希。
