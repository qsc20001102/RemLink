# RemLink v1.0 三端部署与使用指南

本文覆盖 Linux Server、Windows Engineer、Windows Site 三端从准备、注册、联通、使用到备份升级的完整流程。根目录 DOCX 是需求权威来源；本文只描述当前代码和发布包已经提供的能力，不把自动化测试等同于物理环境验收。

## 1. 部署边界与端口

RemLink 是中心辐射结构：

- Server 运行在 Linux amd64，使用内核 WireGuard `wg0`，负责注册、Overlay 地址分配、Control、Session 编排和管理页面。
- Engineer 运行在 Windows amd64，提供 GUI；每台 Engineer 同时只允许一个非终态远程 Session。
- Site 运行在另一台 Windows amd64，提供控制台和进程内 gVisor netstack 网关。它用本机普通套接字访问现场目标，因此 PLC 不需要返回 Overlay 的路由。
- 两类 Windows 节点都只创建并复用一个名为 `RemLink` 的 Wintun。不要在同一 Windows 主机同时部署 Engineer 和 Site。
- Overlay 流量全部经 Server 中转，不建立 P2P，不使用 WinNAT、Windows IP Forwarding、SNAT/MASQUERADE，也不在 Server 为现场 LAN 配置 WireGuard `AllowedIPs`。

| 端口 | 作用 | 暴露范围 |
|---|---|---|
| `8080/tcp` | Web UI、Bootstrap、Admin API | 仅可信管理网；公网部署应由外部 HTTPS 反向代理保护 |
| `51820/udp` | WireGuard 公网入口 | Engineer 和 Site 必须可达 |
| `7001/tcp` | Overlay Control WebSocket | 只监听 Server Overlay IP，不做公网映射 |
| `6200/udp` | Overlay Session 数据报 | 只在 Overlay 内使用，不做公网映射 |

`7001/tcp` 和 `6200/udp` 绝不能添加到公网端口映射。

## 2. 环境准备

### 2.1 Linux Server

准备一台 Linux amd64 主机，并确认：

- 有稳定公网 IPv4 或域名；NAT 场景已把 WireGuard UDP 端口转发到 Server。
- 内核支持 WireGuard，存在 `/dev/net/tun`，Docker 和 Compose 插件可用；原生部署还需要 `iproute2`、`iptables` 和 `wireguard-tools`。
- 主机时间同步正常。
- 默认 Overlay `10.88.0.0/16` 与任一 Windows 主机本地直连网段不冲突。
- 现场 CIDR 不与 Overlay、Engineer 本地网段或保留地址冲突，且不使用 `0.0.0.0/0`。

先完成宿主机检查：

~~~bash
uname -m
test -c /dev/net/tun && echo "TUN 就绪"
sudo modprobe wireguard
sudo ip link add dev wg-probe type wireguard
sudo ip link del dev wg-probe
docker version
docker compose version
~~~

若临时 `wg-probe` 创建失败，先修复内核支持，不要用 privileged 容器绕过预检。

### 2.2 Windows Engineer 与 Site

每台 Windows amd64 主机需要管理员权限，并且能访问 Server 的 HTTPS/HTTP Bootstrap 地址和 WireGuard 公网 UDP 地址。主机不能有与 Overlay 冲突的本地直连网段，也不能运行另一个占用 `RemLink` 适配器的 RemLink 角色。

Site 还必须从 Windows 本机访问每个现场目标网段。普通默认路由不算 Site 路由能力；目标应为直连网段或有明确非默认路由：

~~~powershell
Get-NetRoute -AddressFamily IPv4 | Sort-Object DestinationPrefix,RouteMetric | Format-Table DestinationPrefix,NextHop,InterfaceAlias,RouteMetric
~~~

## 3. 获取并核验发布包

三端发布物完全分开，构建后得到三个互不包含对方程序的 ZIP：

- `RemLink-Engineer-v1.0.0-windows-amd64.zip`：只包含 Engineer EXE、`engineer.yaml`、文档和校验工具。
- `RemLink-Site-v1.0.0-windows-amd64.zip`：只包含 Site EXE、`site.yaml`、文档和校验工具。
- `RemLink-Server-v1.0.0-linux-amd64.zip`：只包含 Linux Server、Docker 部署目录、文档和验收工具。

Engineer 与 Site 是便携式目录程序。配置、身份、日志和首次释放的 `wintun.dll` 都以各自 EXE 所在目录为根，不依赖当前工作目录，也不写入 `C:\ProgramData\RemLink`。Engineer 还会在同目录生成不含秘密的 `site-profiles.json`，按 Site 记忆 Remote CIDR。不得把两个 Windows 包合并到同一个目录。

先比对可信渠道公布的 ZIP SHA-256，再解压。Windows 可校验整个发布目录：

~~~powershell
.\RemLink-Engineer-v1.0.0-windows-amd64\scripts\validation\Test-ReleasePackage.ps1 -PackagePath .\RemLink-Engineer-v1.0.0-windows-amd64 -Role Engineer
.\RemLink-Site-v1.0.0-windows-amd64\scripts\validation\Test-ReleasePackage.ps1 -PackagePath .\RemLink-Site-v1.0.0-windows-amd64 -Role Site
.\RemLink-Server-v1.0.0-linux-amd64\scripts\validation\Test-ReleasePackage.ps1 -PackagePath .\RemLink-Server-v1.0.0-linux-amd64 -Role Server
~~~

Linux 可在包顶层执行：

~~~bash
cd RemLink-Server-v1.0.0-linux-amd64
sha256sum -c SHA256SUMS.txt
~~~

当前构建未做 Authenticode 签名；若组织策略要求签名，应先完成内部签名发布流程。

## 4. 部署 Server

### 4.1 推荐：发布包 Docker Compose

将发布包固定放到 `/opt/remlink`，因为持久数据位于 `docker/data`：

~~~bash
sudo mkdir -p /opt/remlink
sudo cp -a RemLink-Server-v1.0.0-linux-amd64/. /opt/remlink/
cd /opt/remlink/docker
sudo cp .env.example .env
sudo chmod 600 .env
sudo mkdir -p data
~~~

中国大陆网络把复制命令改为 `sudo cp .env.china.example .env`。该模板使用清华 TUNA 的 Debian 主仓库和安全仓库、强制 IPv4，并关闭可能导致连接重置的 apt HTTP pipelining。Debian 12 容器的软件源是 DEB822 文件，Dockerfile 会按构建参数替换 URI。TUNA 提示安全镜像可能存在同步延迟；如果官方安全源在你的网络中稳定，可把 `REMLINK_APT_SECURITY_MIRROR` 留空。参考 [TUNA Debian 镜像说明](https://mirrors.tuna.tsinghua.edu.cn/help/debian/)。

编辑 `.env`：

~~~dotenv
REMLINK_WG_ENDPOINT=vpn.example.com:51820
REMLINK_WG_PORT=51820
REMLINK_HTTP_BIND=127.0.0.1
REMLINK_ADMIN_TOKEN=替换为足够长的随机管理令牌
REMLINK_APT_FORCE_IPV4=1
REMLINK_APT_DEBIAN_MIRROR=
REMLINK_APT_SECURITY_MIRROR=
~~~

- `REMLINK_WG_ENDPOINT` 必须是 Windows 实际可达的公网 `主机:端口`。
- `REMLINK_WG_PORT` 必须与 endpoint 端口、`server.yaml` 的 `wireguard_port`、NAT 和防火墙完全一致。
- `REMLINK_HTTP_BIND=127.0.0.1` 用于同机 HTTPS 反向代理；可信管理网直连 HTTP 时改为该管理接口的具体 IP。仅在明确接受风险时使用 `0.0.0.0`。
- `REMLINK_ADMIN_TOKEN` 建议始终设置，不能提交到版本库、截图或验收证据。
- `REMLINK_APT_FORCE_IPV4=1` 让镜像构建阶段的 apt 避开常见 IPv6 黑洞；确认构建网络只有 IPv6 时才设为 `0`。
- 两个 `REMLINK_APT_*_MIRROR` 只影响镜像构建，不影响 Ubuntu 宿主机软件源；中国模板已填入 TUNA URI，通用模板保持空值并使用 Debian 官方源。

`server.yaml` 默认内容如下；初次使用非默认 WireGuard 端口时同步修改：

~~~yaml
server:
  http_listen: "0.0.0.0:8080"
  control_listen: "10.88.0.1:7001"
  wireguard_port: 51820
data:
  directory: "/app/data"
network:
  overlay_cidr: "10.88.0.0/16"
  server_overlay_ip: "10.88.0.1"
  session_udp_port: 6200
  mtu: 1280
~~~

校验并启动：

~~~bash
cd /opt/remlink/docker
sudo docker compose --env-file .env -f compose.release.yaml config --quiet
sudo docker compose --env-file .env -f compose.release.yaml up --build -d
sudo docker compose --env-file .env -f compose.release.yaml ps
sudo docker compose --env-file .env -f compose.release.yaml logs --tail=100 server
curl --fail http://127.0.0.1:8080/api/v1/server/info
~~~

容器只保留 `NET_ADMIN`、映射 `/dev/net/tun`，不启用 privileged。预检失败时根据日志修复 TUN、内核 WireGuard、转发或 capability 问题，不要扩大权限。

防火墙应允许 Windows 来源访问 `51820/udp`。直接使用 HTTP 时只允许可信管理网访问 `8080/tcp`；反向代理时只开放 `443/tcp` 并保留 `REMLINK_HTTP_BIND=127.0.0.1`；不要开放 `7001` 和 `6200`。

### 4.2 HTTPS 反向代理

RemLink v1.0 本身不终止 TLS。公网 Bootstrap 若直接使用 HTTP，Join Token、Node Token 和 Admin 请求不会被 HTTP 层加密；WireGuard 不能保护这条独立公网路径。

可用 Nginx、Caddy 或组织网关终止 HTTPS。Nginx 最小代理段如下，证书按实际配置：

~~~nginx
server {
    listen 443 ssl;
    server_name remlink.example.com;
    ssl_certificate /etc/ssl/remlink/fullchain.pem;
    ssl_certificate_key /etc/ssl/remlink/privkey.pem;
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
    }
}
~~~

Windows YAML 的 `server` 随后填写 `https://remlink.example.com`，不能附加路径、查询参数、片段或 URL 用户名密码。

### 4.3 获取和轮换 Join Token

首次启动成功后获取当前 Token：

~~~bash
cd /opt/remlink/docker
sudo docker compose --env-file .env -f compose.release.yaml exec server remlink-server -config /etc/remlink/server.yaml -print-join-token
~~~

Token 在轮换前可登记多个节点，不是每使用一次自动失效。全部预期节点注册后立即轮换：

~~~bash
sudo docker compose --env-file .env -f compose.release.yaml exec server remlink-server -config /etc/remlink/server.yaml -rotate-join-token
~~~

也可在管理页面“网络”页查看当前 Join Token 或主动轮换；网络页刷新后仍可查看当前值。普通部署与保存网络配置不会轮换 Join Token。已注册节点使用各自的 Node Token，轮换 Join Token 仅使旧值无法用于新的首次注册。

### 4.4 可选：原生 Linux 服务

~~~bash
sudo apt-get update
sudo apt-get install -y ca-certificates iproute2 iptables wireguard-tools
sudo install -m 0755 linux-amd64/remlink-server /usr/local/bin/remlink-server
sudo install -d -m 0750 /etc/remlink /var/lib/remlink
sudo cp linux-amd64/server.yaml /etc/remlink/server.yaml
sudo chmod 0640 /etc/remlink/server.yaml
~~~

把 `/etc/remlink/server.yaml` 的 `data.directory` 改为 `/var/lib/remlink`。创建 root 专用的 `/etc/remlink/remlink.env`：

~~~dotenv
REMLINK_WG_ENDPOINT=vpn.example.com:51820
REMLINK_ADMIN_TOKEN=替换为足够长的随机管理令牌
~~~

创建 `/etc/systemd/system/remlink-server.service`：

~~~ini
[Unit]
Description=RemLink Server
After=network-online.target
Wants=network-online.target
[Service]
Type=simple
User=root
WorkingDirectory=/var/lib/remlink
EnvironmentFile=/etc/remlink/remlink.env
ExecStart=/usr/local/bin/remlink-server -config /etc/remlink/server.yaml
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
[Install]
WantedBy=multi-user.target
~~~

~~~bash
sudo chmod 600 /etc/remlink/remlink.env
sudo systemctl daemon-reload
sudo systemctl enable --now remlink-server
sudo systemctl status remlink-server
sudo journalctl -u remlink-server -n 100 --no-pager
curl --fail http://127.0.0.1:8080/api/v1/server/info
sudo /usr/local/bin/remlink-server -config /etc/remlink/server.yaml -print-join-token
~~~

原生进程需管理 `wg0`、路由和转发规则；当前基线以 root 运行，但不关闭主机防火墙，也不配置 SNAT。

## 5. 部署 Engineer

将 Engineer ZIP 单独解压到 Engineer 主机。建议把解压后的顶层目录固定为 `C:\RemLink\Engineer`，然后以管理员身份打开 PowerShell：

~~~powershell
Set-Location C:\RemLink\Engineer
notepad .\engineer.yaml
~~~

配置文件直接包含首次注册所需的 Join Token：

~~~yaml
server: "https://remlink.example.com"
node_name: "Engineer-Shanghai-01"
join_token: "粘贴从 Server 获取的 Join Token"
~~~

`join_token` 以明文保存在本机 YAML 中，仅在尚无 `identity.json` 时用于首次注册。`NodeID`、`NodeToken` 和 WireGuard 私钥仍由程序管理，不能加入 YAML。默认文件位置：

- 配置：`C:\RemLink\Engineer\engineer.yaml`
- 身份：`C:\RemLink\Engineer\identity.json`
- 日志：`C:\RemLink\Engineer\logs\engineer.jsonl`
- Wintun：`C:\RemLink\Engineer\wintun.dll`，由 EXE 首次释放并校验 SHA-256，不与 Site 共用文件。

保存配置后直接首次启动：

~~~powershell
.\RemLinkEngineer.exe
~~~

程序使用 Windows 机器级 DPAPI 保护注册后的身份。确认管理页显示节点 ONLINE 后，关闭 GUI并重新启动一次：

~~~powershell
.\RemLinkEngineer.exe
~~~

生产运行仍需管理员权限。Engineer 是交互式 GUI，不要注册为 SYSTEM 后台任务。

## 6. 部署 Site

将 Site ZIP 单独解压到另一台 Site 主机。建议把解压后的顶层目录固定为 `C:\RemLink\Site`，然后以管理员身份打开 PowerShell：

~~~powershell
Set-Location C:\RemLink\Site
notepad .\site.yaml
~~~

~~~yaml
server: "https://remlink.example.com"
node_name: "Qingdao-Site-01"
join_token: "粘贴从 Server 获取的 Join Token"
netstack:
  tcp_flow_limit: 2048
  udp_flow_limit: 4096
  udp_idle_seconds: 60
~~~

Site YAML 不允许保存现场 LAN CIDR；CIDR 由每次 Engineer Session 动态下发。三个 netstack 数值必须为正，`udp_idle_seconds` 范围为 1–86400 秒。

保存配置后直接首次启动：

~~~powershell
.\RemLinkSite.exe
~~~

看到以下状态后按 `Ctrl+C` 停止，再不带 Token 重启：

~~~text
OverlayIP=<分配地址> WireGuard=CONNECTED
Control=CONNECTED
RemoteSubnet=READY SubnetGateway=GVISOR_NETSTACK/READY
~~~

Site 的配置位于 `C:\RemLink\Site\site.yaml`，身份位于 `C:\RemLink\Site\identity.json`，日志位于 `C:\RemLink\Site\logs\site.jsonl`，Wintun 位于 `C:\RemLink\Site\wintun.dll`。这些文件均属于 Site 包目录，不与 Engineer 共用。Engineer/Site 的 Token 取值优先级均为命令行 `-join-token`、环境变量 `REMLINK_JOIN_TOKEN`、YAML `join_token`；日常部署只填写 YAML 即可。

如需随系统启动，先在前台完成注册和联通验证，停止前台进程，再注册最高权限任务：

~~~powershell
$action = New-ScheduledTaskAction -Execute 'C:\RemLink\Site\RemLinkSite.exe' -Argument '-config "C:\RemLink\Site\site.yaml"' -WorkingDirectory 'C:\RemLink\Site'
$trigger = New-ScheduledTaskTrigger -AtStartup
$principal = New-ScheduledTaskPrincipal -UserId 'SYSTEM' -LogonType ServiceAccount -RunLevel Highest
Register-ScheduledTask -TaskName 'RemLink Site' -Action $action -Trigger $trigger -Principal $principal
Start-ScheduledTask -TaskName 'RemLink Site'
Get-ScheduledTaskInfo -TaskName 'RemLink Site'
~~~

不要同时运行前台 Site 和计划任务。SYSTEM 可读取同机机器级 DPAPI 身份；身份复制到另一台机器后不能解密，迁移主机必须重新注册。

## 7. 首次联通检查

Server：

~~~bash
cd /opt/remlink/docker
sudo docker compose --env-file .env -f compose.release.yaml ps
sudo docker compose --env-file .env -f compose.release.yaml exec server wg show wg0
sudo docker compose --env-file .env -f compose.release.yaml logs --tail=100 server
~~~

应看到两个 Windows peer 的最近握手和计数；每个 peer 在 Server 只应有自己的 Overlay `/32`。

分别在 Engineer 和 Site：

~~~powershell
$adapter = @(Get-NetAdapter -Name RemLink -ErrorAction Stop)
if ($adapter.Count -ne 1) { throw "RemLink 适配器数量不是 1" }
$adapter | Format-List Name,Status,InterfaceDescription,ifIndex
Get-NetIPAddress -InterfaceAlias RemLink -AddressFamily IPv4
Get-NetIPConfiguration -InterfaceAlias RemLink
~~~

应只有一个 `RemLink` 适配器，地址属于 Overlay，MTU 为 1280。不要安装 WireGuardNT 第二适配器。

浏览器打开受保护的 Server URL。若配置了管理令牌，在左侧令牌框输入后点击“应用”。依次检查：

- “节点”：Engineer/Site 为 ONLINE，Overlay IP 唯一，WG Handshake 与 LastSeen 更新。
- “会话”：首次部署为空。
- “网络”：Overlay、Server IP、端口和 MTU 正确。
- “日志”：没有持续 ERROR。

全部节点登记后轮换 Join Token。

## 8. 建立并使用 Session

1. 在 Site 本机先验证目标，例如 `ping 192.168.13.10` 和 `Test-NetConnection 192.168.13.10 -Port 502`。目标网段必须有直连或明确非默认路由。
2. 启动 Engineer，确认“服务器已连接”和“Control 在线”。
3. 选择 ONLINE 且 Remote Subnet“可用”的 Site。
4. 输入规范 CIDR，例如 `192.168.13.0/24`；可添加多个。
5. 等待本地冲突预检显示“通过，无冲突”。失败时处理 Engineer 已有直连/路由，不能强行绕过。
6. 点击“连接现场”。正常状态为“正在创建（`CREATING`）”→“正在准备现场端（`PREPARING_SITE`）”→“准备就绪（`READY`）”→“活动中（`ACTIVE`）”。
7. ACTIVE 后用原生工具访问目标：

~~~powershell
ping 192.168.13.10
Test-NetConnection 192.168.13.10 -Port 102
Test-NetConnection 192.168.13.10 -Port 502
Test-NetConnection 192.168.13.10 -Port 80
~~~

TCP、UDP 和受约束的 ICMP Echo 走通用 netstack/主机套接字，不依赖协议专用代理。Site 日志记录 Session 和路由结果，不记录数据包载荷。

8. 结束后点击“断开会话”，确认 Engineer 远程路由删除。异常退出后，下次启动会清理本程序拥有的陈旧路由。

每个 Engineer 同时只能有一个非终态 Session；切换 Site 前先断开。多个 Engineer 可访问同一 Site，两个 Site 也可各自使用相同现场 CIDR，SessionID 会隔离数据流。

Engineer 的 Remote CIDR 按 Site NodeID 独立保存。选择现场时只加载并发送该现场的网段，例如现场 A 可保存 `192.168.17.0/24`，现场 B 可保存 `192.168.107.0/24`，不需要来回删除和重建。Site 心跳超过离线阈值后，Server 会以 `SITE_OFFLINE` 自动关闭相关会话并通知 Engineer 清理本地路由。

## 9. 日常管理

- “节点”页可改名称或 Overlay IP、撤销节点。修改在线节点 IP 会关闭相关 Session 并触发重新 Bootstrap；撤销后旧 Node Token 失效。
- “会话”页显示 SessionID、两端节点、CIDR、状态、计数和持续时间，可强制断开 ACTIVE Session。
- “日志”页可按时间、级别、模块、Node ID 和 Session ID 过滤。
- Server 文件日志：Docker 为 `/opt/remlink/docker/data/logs/server.jsonl`；原生为 `/var/lib/remlink/logs/server.jsonl`。

三端面向操作员的日志消息、状态、级别、模块和常见错误均显示中文。协议状态、模块名与错误码会保留在括号中，例如“现场端没有通往远程网段的明确路由（`SITE_NO_ROUTE`）”，便于按文档和接口继续检索。Server 管理页会把旧版本已经写入数据库的常见英文事件即时翻译为中文；数据库原始记录不会被批量改写。JSON 文件日志的 `time`、`level`、`module`、`session_id` 等字段名保持稳定，供脚本和采集系统使用，`msg` 内容改为中文。

同机重新登记时，先停止角色进程/任务，把对应 `identity.json` 移到受控备份位置，再使用新 Join Token；不要编辑 DPAPI 内容。

修改完整 Overlay 前，先检查所有 Windows 主机无本地冲突。保存后 Server 会暂停新 Session、关闭现有 Session、更新数据库与 `wg0`，通知节点重新 Bootstrap，再切换监听。

Docker 修改 WireGuard 端口后，还要把 `.env` 的 `REMLINK_WG_PORT` 和 `REMLINK_WG_ENDPOINT` 改成同一端口并重建映射：

~~~bash
cd /opt/remlink/docker
sudo docker compose --env-file .env -f compose.release.yaml up -d --force-recreate
~~~

重建期间短暂离线，`docker/data` 中数据库和密钥保留。

## 10. 备份、恢复与升级

Docker 权威数据均在 `docker/data`。一致性备份：

~~~bash
cd /opt/remlink/docker
sudo docker compose --env-file .env -f compose.release.yaml stop server
sudo tar -C /opt/remlink/docker -czf /安全备份目录/remlink-data-$(date +%F-%H%M%S).tgz data
sudo docker compose --env-file .env -f compose.release.yaml start server
~~~

同时备份 `server.yaml` 和受保护的 `.env`。恢复时先停止 Server，恢复到原位置并保持权限，再启动检查节点重连。原生部署对应备份 `/var/lib/remlink`、`/etc/remlink/server.yaml` 和 `remlink.env`。

Windows `identity.json` 是机器级 DPAPI 密文，只能在生成它的主机恢复，不能用于跨机器迁移。更换主机应撤销旧节点并重新登记。

从旧版 `ProgramData` 目录模型升级时，必须先停止对应 Windows 进程。在同一台主机上，可把旧的 `C:\ProgramData\RemLink\Engineer\identity.json` 或 `C:\ProgramData\RemLink\Site\identity.json` 手工复制到新包 EXE 旁；机器级 DPAPI 密文仍可解密。日志可按需归档，不要把 Engineer 身份复制给 Site，也不要跨主机复制。确认新包已正常重连后，再决定是否归档旧目录；新版本不会继续读写旧目录。

升级顺序：

1. 核验新包，断开 Session，备份 Server 数据。
2. 停止三端。
3. 固定使用 `/opt/remlink` 时保留 `docker/data` 和本机 `docker/.env`，替换其余发布文件；原生部署替换 Server 二进制。
4. 分别替换 Engineer、Site 包目录中的 EXE；保留同目录的 YAML、`identity.json`、`logs` 和已验证的 `wintun.dll`。不要用另一端的包覆盖当前目录。
5. 先启动 Server，再 Site，最后 Engineer。
6. 核对版本、节点 ONLINE、`wg show wg0`、日志和一条测试 Session。

Server 重启会关闭数据库中遗留的非终态 Session；升级后应新建 Session，不要期待旧 Session 自动恢复。

## 11. 常见故障

| 现象或错误 | 处理 |
|---|---|
| endpoint 端口不匹配 | `REMLINK_WG_ENDPOINT` 端口必须等于已保存的 `wireguard_port`；同时核对 `REMLINK_WG_PORT`、NAT、防火墙 |
| 容器预检失败 | 检查 `/dev/net/tun`、内核 WireGuard、`NET_ADMIN` 和 IPv4 forwarding；不要改 privileged |
| 构建长时间停在 `apt-get` | 中国大陆先使用 `.env.china.example`；再运行 `docker run --rm debian:bookworm-slim sh -c "apt-get -o Acquire::ForceIPv4=true -o Acquire::Retries=2 -o Acquire::http::Timeout=30 update"`；若仍超时，修复 Docker daemon DNS/代理或用 `docker build --network=host` |
| Admin 401 | 输入与 `REMLINK_ADMIN_TOKEN` 完全一致的 Bearer token |
| `JOIN_TOKEN_INVALID` | 安全获取当前 Token；不要继续使用已轮换值 |
| `NODE_AUTH_FAILED` | 身份被撤销、损坏或复制到其他机器；隔离旧身份并重新登记 |
| `OVERLAY_LOCAL_CONFLICT` | Overlay 与 Windows 本地直连网段重叠；恢复或选择无冲突 Overlay |
| `CIDR_LOCAL_CONFLICT` | Engineer 本机已有覆盖远程 CIDR 的网络/路由；处理后重新预检 |
| `SITE_NO_ROUTE` | Site 只有默认路由或无路由；增加真实直连/静态路由并先在 Site 验证 |
| `NETSTACK_UNAVAILABLE` / `FLOW_LIMIT_REACHED` | 检查 Site 就绪、流量上限和长连接；按容量调整配置并重启 |
| `SESSION_INJECT_FAILED` | 当前 Session 会关闭；检查适配器、路由和日志后新建 |
| `ENGINEER_SESSION_EXISTS` | 先断开当前非终态 Session |
| 节点 ONLINE 但业务不通 | 依次检查 Site 本机目标、Server `wg show`、Session ACTIVE、Engineer 路由、Site 日志和主机防火墙 |

## 12. 验收、停用

发布包中初始化验收：

~~~powershell
.\scripts\validation\New-AcceptanceRun.ps1 -OutputDirectory C:\RemLink-Evidence\run-001
~~~

按 `docs/validation/T01-T18-runbook.md` 采证，用 `Set-AcceptanceResult.ps1` 记录，再运行：

~~~powershell
.\scripts\validation\Test-AcceptanceRun.ps1 -RunDirectory C:\RemLink-Evidence\run-001
~~~

证据不存在、哈希不一致或前置 Gate 未通过时，不得标记 PASS。至少验证单适配器、Overlay 双向连通、目标 ICMP/TCP/UDP、断线重连、Server 重启、陈旧路由清理、节点 IP/Overlay 迁移和重复现场网段隔离。

停用时：Engineer 先断开 Session；Site 计划任务先执行 `Stop-ScheduledTask -TaskName 'RemLink Site'`，永久取消再执行 `Unregister-ScheduledTask -TaskName 'RemLink Site'`；Server 停止后归档 data 和配置；撤销不再使用的节点并轮换 Join Token。

当前包没有 Windows 卸载器。持久 `RemLink` Wintun 适配器是设计行为；若只暂停使用，可在确认没有 RemLink 进程后禁用。删除 Engineer 或 Site 包目录会同时删除该端身份和日志；执行前必须备份，并确认明确放弃该主机身份。不要用不明脚本删除第三方网络适配器。
