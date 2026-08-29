# Phase 1 Gate A Server 手工 POC

本目录只用于 Ubuntu/Linux 内核 WireGuard 的一次性 Phase 1 验证，不是 Phase 2 之后的受管 Server。

## Server 准备

1. 安装发行版 WireGuard 工具，确认 `ip link add dev wg-test type wireguard` 成功，然后立即删除测试接口。
2. 用 `wg genkey` 和 `wg pubkey` 生成 Server 密钥对，私钥只留在 Server。
3. 在每个 Windows POC 节点分别生成密钥对，私钥只留在该节点。
4. 把 `wg0.conf.example` 复制到 `/etc/wireguard/wg0.conf`，权限设为 `0600`，替换全部占位符。
5. 启用 IPv4 forwarding，并允许流量从 `wg0` 转发回 `wg0`；不要添加 SNAT/MASQUERADE。
6. 执行 `wg-quick up wg0`，确认 `wg show wg0` 显示 UDP 51820。

每个 Server Peer 的 `AllowedIPs` 只能是该节点 Overlay `/32`，绝不能添加远端 Site LAN CIDR。

## Windows 节点

在每个节点的管理员 PowerShell 中运行。两个只读/局部探针可单独执行：

~~~powershell
go run ./cmd/phase1-node --runtime-probe
go run ./cmd/phase1-node --adapter-probe --address 10.88.0.2/16
~~~

适配器探针需要管理员权限，会配置并保留可复用的 `RemLink` 适配器，但会关闭活动 Wintun 会话，不会删除适配器。

~~~powershell
$secret = Read-Host '节点私钥' -AsSecureString
$pointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secret)
try {
  $env:REMLINK_POC_PRIVATE_KEY = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($pointer)
} finally {
  [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($pointer)
}
go run ./cmd/phase1-node --address 10.88.0.2/16 --server-endpoint <SERVER_PUBLIC_IP>:51820 --server-public-key <SERVER_PUBLIC_KEY_BASE64>
~~~

节点 B 使用 `10.88.0.3/16`。两个进程都报告 wireguard-go 就绪后，执行双向 Overlay ping。

## Gate A 证据

- 每台 Windows 节点 `Get-NetAdapter -Name RemLink` 恰好返回一个适配器。
- `Get-NetIPConfiguration -InterfaceAlias RemLink` 显示分配地址与 MTU 1280。
- `wg show wg0` 显示两个 `/32` peer 的近期握手和计数。
- 双向 Overlay ping 经 Server 成功。
- 抓包或 Server 计数证明路径为 Node A → Server `wg0` → Node B，不存在 P2P endpoint。
