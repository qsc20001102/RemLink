# Phase 1 验证记录

- 日期：2026-08-25
- 状态：进行中；Gate A 尚未通过
- 主机：Windows amd64，非管理员 Codex 进程
- Go 工具链：1.26.7

## 已完成自动化证据

| 检查 | 结果 |
|---|---|
| Wintun 0.14.1 ZIP 官方 SHA-256 | PASS |
| 内嵌 AMD64 DLL SHA-256 | PASS（`e5da8447...20dafce`） |
| 将内嵌 Wintun 安装到指定便携目录并校验/修复 | PASS |
| 官方 Go 绑定报告 Wintun 0.14.1 | PASS |
| MuxTun `tun.Device` 编译期断言 | PASS |
| MuxTun 代理与幂等关闭测试 | PASS |
| wireguard-go UAPI 黄金配置测试 | PASS |
| 全量 Go test/vet、Windows 构建、Linux 交叉构建、模块校验 | PASS |

旧版 `ProgramData` 路径证据已作废。便携目录改造后，从系统临时目录启动位于 `.codex-qa\portable-runtime-probe` 的运行库探针，实际加载路径为该探针 EXE 旁的 `wintun.dll`；其 SHA-256 为固定值 `e5da8447...20dafce`。单元测试同时验证了指定便携目录下的原子安装、复用和异常文件修复。

确认主机不存在 `RemLink` 适配器或 `10.88.0.0/16` 路由后尝试管理员适配器探针；它在明确的管理员预检处以 `administrator privileges are required` 停止，因此没有创建或修改网络适配器。

## Gate A 仍需证据

- 在管理员进程运行 `phase1-node --adapter-probe`，确认一个可复用 `RemLink`、指定 Overlay IPv4 和 MTU 1280。
- 用 `deploy/phase1-server` 准备 Linux 内核 WireGuard `wg0`。
- 在两台管理员权限 Windows 节点运行内嵌 wireguard-go POC，Server peer 各自只用唯一 Overlay `/32`。
- 采集 `wg show`、Windows 适配器清单和双向 Overlay ping。
- 确认每节点恰好一个 RemLink Wintun 且没有 WireGuardNT。

完成全部物理证据前，Phase 1 和 Gate A 必须保持未勾选。
