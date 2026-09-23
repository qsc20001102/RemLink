# RemLink v1.0 实现状态

日期：2026-08-25

Phase 0–10 的实现工作均已进入代码。`specs/tasks.md` 明确区分“已实现子任务”和“需要外部主机的验收项”。

## 已完成的代码与自动化测试

- 稳定协议：20 字节 Session 头、13 种 Control 消息、14 个精确错误码。
- Linux 内核 WireGuard 编排、SQLite 迁移、IPAM、Bootstrap、Join/Node Token、Control Hub、心跳状态与重连策略。
- 单 Wintun wireguard-go Client、PacketMux、自有 Windows 路由、本地重叠检查、普通 Overlay UDP Session 传输和严格数据包验证。
- 单进程 gVisor netstack 网关、通用 TCP/UDP relay 与受约束 ICMP Echo relay；不使用 WinNAT、Windows forwarding 或协议专用代理。
- Site TCP/UDP flow 上限和 UDP 空闲超时可配置，默认 `2048/4096/60s`，并通过 capability 上报。
- 七状态双边 Session 编排、超时、统计、reconcile、rebootstrap、Admin 审计事件和七步 Overlay 网络迁移。
- 网络迁移会原子暂停 Session 创建、关闭现有 Session、发布新 Bootstrap、在旧 Control 仍在线时通知节点、切换 `wg0`/监听，并在失败时恢复内核和应用状态。
- Windows 节点在旧 Control 在线时预检新 Overlay；本地重叠会保持旧配置以报告 `OVERLAY_LOCAL_CONFLICT`，Server 记录 ERROR 事件。
- PREPARE 拒绝与 CREATE 请求关联，陈旧结果不能终止后续请求；完全相同的 netstack PREPARE 重试幂等。
- Site 和 Server 信任边界都拒绝 `DEFAULT_ONLY`。数据包注入失败只关闭受影响 Session 并报告 `SESSION_INJECT_FAILED`，节点监听仍可用。
- 被拒 Session 数据报和 PacketMux 丢包只输出元数据、限速安全警告；默认不记录高频数据包 DEBUG，也没有接收载荷字节的日志 API。
- PacketMux 在拦截/注入边界计数；UDP relay 空闲回收、Sender 关闭与 netstack 重试路径有 race/幂等覆盖。
- Wails Engineer GUI 和内嵌 Vue Server Web UI 已实现；Server 五个管理页及指定 API 完整。节点显示 WG 握手，Session 显示持续时间，日志支持五维过滤，网络页可持续查看当前 Join Token，Engineer 显示 Site capability 与 LastSeen。
- 浏览器侧 SessionID 使用十进制字符串，避免 JavaScript 舍入随机 `uint64`。新的 Node Bootstrap 会关闭 Server 侧本地运行时已经丢失的 Session；短 Control 重连保留运行时。
- Engineer GUI 对所有非终态单 Session 状态做操作门禁；生产启动和输入默认失败关闭；Server 运行表刷新不会覆盖正在编辑的网络配置。
- 多 Engineer、多 Site 并发与重复 CIDR flow 隔离已实现。
- 配置、Bootstrap、IPAM、Client WG、Session 和 Admin 信任边界都拒绝 Overlay 网络/广播地址以及 `/0` Exit Node。Server 事件模块限制为规格定义的 11 个名称。
- Server WireGuard 私钥并发发布原子；成功的 Linux 内核变更不会被误报为取消。
- 固定依赖、CI、Windows/Linux 发布脚本、受限 Docker Compose、启动预检、第三方声明和自包含 T01–T18 证据工具已提供。
- Docker 使用 `./data:/app/data` 持久化和对称 `REMLINK_WG_PORT` 映射，并支持限制 HTTP bind。源码 Compose 与发布包预编译二进制 Compose 分离。
- Server 的 Join Token 打印/轮换 CLI 在 Session 恢复清理之前退出，不会因运维读取 Token 而关闭活动 Session。
- Admin 空节点/Session/事件列表统一编码为 `[]`，Server 前端也会把旧版 `null` 响应归一为空数组，避免首次正确鉴权后的概览白屏。
- 发布验证器会解压 ZIP、检查必需项、重算全部校验和、拒绝未覆盖文件，并冒烟运行包内验收初始化器，不伪造结果。
- Engineer、Site、Server 生成三个独立目录和 ZIP，发布验证器会拒绝混入其他角色的可执行文件；Windows 两端默认以各自 EXE 目录保存 YAML、DPAPI 身份、日志和校验后的 Wintun DLL，不再写入或共享 `ProgramData` 文件。
- Engineer/Site YAML 支持明文 `join_token` 作为自用部署便捷项；命令行和 `REMLINK_JOIN_TOKEN` 仍可覆盖 YAML，注册后的 Node Token 与 WireGuard 私钥继续由 DPAPI 保护。
- Engineer 按 Site NodeID 在 EXE 旁的 `site-profiles.json` 独立记忆 Remote CIDR；选择现场时只加载该现场网段。Site 心跳进入 OFFLINE 后，Server 自动以 `SITE_OFFLINE` 关闭相关会话并释放 Engineer 会话门禁。
- Server、Engineer、Site 的操作员日志与界面状态已中文化；稳定协议状态、模块名和错误码以括号形式保留，Server 管理页还能翻译旧数据库中的常见英文事件。
- Engineer 发布构建固定使用 Wails `desktop,production` 标签，并写入 `BUILD-INFO.json`；架构检查会拒绝退回缺少标签、启动时只弹 Wails 错误框的普通 Go 构建。

## 待外部物理验收

Gate A–D 与 T01–T18 当前为 `NOT_RUN`，不是失败也不是通过。它们需要 Linux 内核 WireGuard、管理员权限 Windows Engineer/Site、真实路由、重复现场网络、目标服务、热点切换以及进程/网络重启。执行方式见 `docs/validation/T01-T18-runbook.md`。

## 本地验证说明

本地基线已执行 npm 清洁安装、Vue 类型检查和生产构建、`go mod verify`、全量 Go test、`go vet`、架构策略扫描、Windows/Linux 交叉构建、发布校验和验证以及包内验收初始化冒烟。

渲染 QA 使用已安装 Microsoft Edge 与 Playwright：Engineer 的 ACTIVE→断开→IDLE 与 Settings 导航通过；Server 的 SESSION 日志过滤和 Join Token 轮换通过；两个页面均无框架错误覆盖和浏览器 warning/error。

本机没有 Docker，因此不在本地声称镜像已构建；Linux CI 负责源码镜像构建。由于本机没有 C 编译器，race 测试由 Linux CI 承担。Windows EXE 未做 Authenticode 签名；工作区没有 commit 时 `BUILD-INFO.json` 会记录 `unknown`。这些限制均不能记为已通过的 Gate 或物理验收结果。

本次中文文档与发布包部署入口修改后，应以最新一次 `scripts/build-release.ps1` 生成的 Engineer、Site、Server 三个独立 ZIP、各包 `SHA256SUMS.txt` 和实际命令输出为准，不沿用旧包哈希。
