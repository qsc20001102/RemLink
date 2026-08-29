# RemLink

RemLink 是一套面向工业现场的 IPv4 三层远程接入系统，用于让远程工程师通过中心服务器安全访问现场 PLC、HMI、工控机及其他 IP 设备。

系统由三个完全独立的程序组成：

- **RemLink Server**：运行在 Linux，负责节点注册、Overlay 地址分配、WireGuard 中心转发、会话调度和 Web 管理。
- **RemLink Engineer**：运行在工程师 Windows 电脑，提供图形界面，把指定现场网段路由到当前远程会话。
- **RemLink Site**：运行在现场 Windows 电脑，通过 gVisor 用户态网络栈访问现场局域网设备。

典型数据路径：

```text
Engineer Windows
    │
    │ WireGuard Overlay
    ▼
RemLink Server（Linux 中心节点）
    │
    │ WireGuard Overlay
    ▼
Site Windows ── gVisor netstack ── 现场 LAN ── PLC/HMI/工控设备
```

Engineer、Site 和 Server 的发布包、配置及运行数据彼此独立，不能混放或互相替换。

## 主要功能

- 以 Linux 内核 WireGuard 为中心建立 `10.88.0.0/16` Overlay 网络。
- Engineer 和 Site 各自只创建并复用一个 `RemLink` Wintun 适配器。
- 自动注册节点、分配 Overlay IP、维护心跳及在线状态。
- Engineer 可选择 Site，并为该 Site 设置一个或多个远程 CIDR。
- 不同 Site 的远程网段分别保存，切换现场时不需要反复删除和重新添加。
- 支持多个 Engineer、多个 Site，以及不同 Site 使用相同现场网段。
- Site 使用进程内 gVisor netstack 转发通用 TCP、UDP 和 ICMP Echo 流量。
- Site 离线后，Server 自动关闭相关会话，Engineer 自动清理当前远程路由。
- Server 提供中文 Web 管理页面，可查看节点、会话、网络配置和事件日志。
- Engineer、Site、Server 的面向操作员日志均已中文化，同时保留协议状态和错误码。
- Engineer 和 Site 使用便携目录，配置、身份、日志和 `wintun.dll` 均位于各自 EXE 目录。

## 系统边界

- RemLink 是中心辐射结构，所有 Overlay 流量均经过 Server，不建立节点间 P2P。
- Site 不要求 PLC 或现场设备配置返回 Overlay 的路由。
- Site 必须从本机通过直连路由或明确静态路由到达现场网段；只有默认路由时会返回 `SITE_NO_ROUTE`。
- 每个 Engineer 同时只允许一个非终态远程会话。
- 当前版本只支持 IPv4，不提供二层以太网桥接。
- Server v1.0 不直接终止 TLS；公网管理和注册入口应置于 HTTPS 反向代理之后。

## 项目目录与文件作用

| 路径 | 作用 |
|---|---|
| `cmd/server/` | Linux Server 程序入口和启动编排 |
| `cmd/engineer/` | Windows Engineer/Wails GUI 程序入口 |
| `cmd/site/` | Windows Site 控制台程序入口 |
| `internal/admin/` | Server 管理 API、节点和网络配置管理 |
| `internal/bootstrap/` | 节点首次注册、Token 校验和权威配置下发 |
| `internal/control/` | Server 与 Windows 节点之间的控制通道 |
| `internal/session/` | 远程会话状态机、超时、关闭和统计 |
| `internal/overlay/clientwg/` | Windows wireguard-go、Wintun 和 PacketMux |
| `internal/overlay/serverwg/` | Linux 内核 WireGuard 管理 |
| `internal/subnetgateway/` | Site gVisor netstack 及 TCP/UDP/ICMP 转发 |
| `internal/platform/windows/` | Windows 路由、DPAPI、网卡和 Wintun 运行库 |
| `internal/database/`、`internal/ipam/` | SQLite 持久化和 Overlay 地址分配 |
| `frontend/engineer/` | Engineer Vue 图形界面，构建后嵌入 Wails EXE |
| `frontend/server/` | Server Vue Web 管理页面，构建后嵌入 Server 二进制 |
| `config/*.example.yaml` | Server、Engineer、Site 的安全配置模板 |
| `deploy/docker/` | Server Dockerfile、Compose、预检和中国网络模板 |
| `scripts/build-release.ps1` | 构建和验证三端独立发布包 |
| `scripts/validation/` | 架构、发布包和 T01–T18 验收工具 |
| `scripts/maintenance/` | Git 候选文件和敏感内容检查 |
| `specs/` | Phase 0–10 开发任务、规格和验收清单 |
| `docs/` | 中文部署、实现状态、设计和物理验收文档 |
| `third_party/`、`THIRD_PARTY_NOTICES.md` | 第三方组件许可说明 |
| `.github/workflows/ci.yml` | GitHub Actions 自动构建和测试 |

以下目录是本地生成内容，不应提交到 Git：

- `build/`：临时可执行文件。
- `dist/`：三端发布目录和 ZIP。
- `frontend/node_modules/`：前端依赖。
- `frontend/*/dist/`：前端编译结果。
- `data/`、`logs/`、`runtime/`、`evidence/`：运行数据和验收证据。

## 开发与打包环境

推荐在 Windows PowerShell 7 中执行发布脚本，准备：

- Go：版本以 `go.mod` 为准。
- Node.js：与 `.github/workflows/ci.yml` 中的版本一致。
- npm：使用 `frontend/package-lock.json` 锁定依赖。
- PowerShell 7。
- Git。

检查环境：

```powershell
go version
node --version
npm --version
git --version
$PSVersionTable.PSVersion
```

## 构建三个独立发布包

在项目根目录执行：

```powershell
$version = "1.0.7"
./scripts/build-release.ps1 -Version $version
```

版本号必须是语义化版本，例如 `1.0.7` 或 `1.1.0-beta.1`。发布脚本会自动执行：

1. Git 仓库候选文件和敏感内容检查。
2. 前端依赖清洁安装、Vue 类型检查和生产构建。
3. 前端 Demo 数据泄漏检查。
4. Go 依赖校验、全量测试和 `go vet`。
5. 架构规则与验收工具自测。
6. Windows/Linux 交叉编译。
7. 三端目录隔离、SHA-256 生成和发布包解压复核。

成功后在 `dist/` 得到：

```text
dist/
├─ RemLink-Engineer-v1.0.7-windows-amd64/
├─ RemLink-Engineer-v1.0.7-windows-amd64.zip
├─ RemLink-Site-v1.0.7-windows-amd64/
├─ RemLink-Site-v1.0.7-windows-amd64.zip
├─ RemLink-Server-v1.0.7-linux-amd64/
└─ RemLink-Server-v1.0.7-linux-amd64.zip
```

每个发布目录都包含 `BUILD-INFO.json` 和 `SHA256SUMS.txt`。Engineer 包不会包含 Site 或 Server，Site 包不会包含 Engineer 或 Server，Server 包也不会包含 Windows 客户端。

只运行开发检查、不生成发布包时执行：

```powershell
./scripts/maintenance/Test-RepositoryHygiene.ps1
npm ci --prefix frontend
npm run typecheck --prefix frontend
npm run build --prefix frontend
go test -count=1 ./...
go vet ./...
./scripts/validation/Test-FrontendProduction.ps1
./scripts/validation/Test-Architecture.ps1
./scripts/validation/Test-AcceptanceTools.ps1
```

## 部署前网络准备

| 端口 | 用途 | 是否需要公网或跨网访问 |
|---|---|---|
| `8080/tcp` | Web、Bootstrap、Admin API | 仅可信管理网；公网应使用 HTTPS 反向代理 |
| `51820/udp` | WireGuard 公网入口 | Engineer 和 Site 必须能够访问 |
| `7001/tcp` | Overlay Control WebSocket | 只在 Overlay 内使用，不映射公网 |
| `6200/udp` | Overlay Session 数据 | 只在 Overlay 内使用，不映射公网 |

部署前确认：

- Server 是 Linux amd64，并支持内核 WireGuard 和 `/dev/net/tun`。
- Engineer 和 Site 是 Windows amd64，并以管理员权限运行。
- 默认 Overlay `10.88.0.0/16` 不与任一 Windows 主机的本地网络重叠。
- Server 的 WireGuard UDP 端口已在云安全组、防火墙和 NAT 中放行。
- Site Windows 本机能够直接访问目标 PLC/HMI 网段。

## 部署 Server（Ubuntu + Docker Compose）

### 1. 检查宿主机

```bash
uname -m
test -c /dev/net/tun && echo "TUN 正常"
sudo modprobe wireguard
docker version
docker compose version
```

### 2. 解压和安装 Server 包

以下使用 `1.0.7` 举例，实际部署时替换为构建出的版本：

```bash
unzip RemLink-Server-v1.0.7-linux-amd64.zip
sudo mkdir -p /opt/remlink
sudo cp -a RemLink-Server-v1.0.7-linux-amd64/. /opt/remlink/
cd /opt/remlink/docker
```

### 3. 创建 `.env`

普通网络环境：

```bash
sudo cp .env.example .env
```

中国大陆网络环境：

```bash
sudo cp .env.china.example .env
```

编辑 `.env`。下面是通用模板的配置形式；如果复制的是 `.env.china.example`，请保留其中已经填写的两个镜像地址，不要用下面的空值覆盖：

```dotenv
REMLINK_WG_ENDPOINT=vpn.example.com:51820
REMLINK_WG_PORT=51820
REMLINK_HTTP_BIND=127.0.0.1
REMLINK_ADMIN_TOKEN=替换为足够长的随机管理令牌
REMLINK_APT_FORCE_IPV4=1
REMLINK_APT_DEBIAN_MIRROR=
REMLINK_APT_SECURITY_MIRROR=
```

关键配置说明：

- `REMLINK_WG_ENDPOINT`：Engineer/Site 实际可访问的公网 IP 或域名及 UDP 端口。
- `REMLINK_WG_PORT`：必须与 endpoint、`server.yaml`、防火墙和 NAT 端口一致。
- `REMLINK_HTTP_BIND=127.0.0.1`：Web 只允许本机反向代理访问。
- 需要在可信局域网直接打开 Web 时，可改为 Server 的管理网 IP；只有明确接受风险时才使用 `0.0.0.0`。
- `REMLINK_ADMIN_TOKEN`：Server Web 和 Admin API 的 Bearer Token，不能提交到 Git。
- 中国模板已设置适合中国网络的 Debian 镜像和 IPv4 构建参数。

### 4. 检查 Server YAML

`docker/server.yaml` 默认配置：

```yaml
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
```

没有网络冲突时建议保持默认值。如果修改 `wireguard_port`，必须同步修改 `.env`、云安全组、防火墙和 NAT。

### 5. 启动 Server

```bash
cd /opt/remlink/docker
sudo chmod 600 .env
sudo mkdir -p data
sudo docker compose --env-file .env -f compose.release.yaml config --quiet
sudo docker compose --env-file .env -f compose.release.yaml up --build -d
sudo docker compose --env-file .env -f compose.release.yaml ps
sudo docker compose --env-file .env -f compose.release.yaml logs --tail=100 server
curl --fail http://127.0.0.1:8080/api/v1/server/info
```

### 6. 获取 Join Token

```bash
sudo docker compose --env-file .env -f compose.release.yaml exec server \
  remlink-server -config /etc/remlink/server.yaml -print-join-token
```

Join Token 用于 Engineer 和 Site 第一次注册。节点全部注册完成后建议轮换：

```bash
sudo docker compose --env-file .env -f compose.release.yaml exec server \
  remlink-server -config /etc/remlink/server.yaml -rotate-join-token
```

浏览器访问受保护的 Server 地址，例如 `http://服务器管理网IP:8080` 或反向代理后的 HTTPS 域名。在左侧令牌框输入 `.env` 中的 `REMLINK_ADMIN_TOKEN` 并点击“应用”。

## 部署 Engineer（Windows）

将 Engineer ZIP 单独解压到固定目录，例如：

```text
C:\RemLink\Engineer
```

编辑 EXE 同目录的 `engineer.yaml`：

```yaml
server: "https://remlink.example.com"
node_name: "Engineer-Shanghai-01"
join_token: "粘贴从 Server 获取的 Join Token"
```

如果没有配置 HTTPS 反向代理，在可信网络中可临时使用：

```yaml
server: "http://服务器IP:8080"
```

以管理员身份运行：

```powershell
Set-Location C:\RemLink\Engineer
.\RemLinkEngineer.exe
```

首次启动会注册节点，并在当前目录生成受机器级 DPAPI 保护的 `identity.json`。该身份只能在原 Windows 主机上使用，不能复制到另一台电脑。

## 部署 Site（Windows）

将 Site ZIP 单独解压到另一台现场 Windows 主机，例如：

```text
C:\RemLink\Site
```

编辑 EXE 同目录的 `site.yaml`：

```yaml
server: "https://remlink.example.com"
node_name: "Qingdao-Site-01"
join_token: "粘贴从 Server 获取的 Join Token"

netstack:
  tcp_flow_limit: 2048
  udp_flow_limit: 4096
  udp_idle_seconds: 60
```

配置含义：

- `tcp_flow_limit`：Site 同时处理的 TCP flow 上限。
- `udp_flow_limit`：Site 同时处理的 UDP flow 上限。
- `udp_idle_seconds`：UDP flow 空闲回收时间。
- Site YAML 不配置现场网段；现场 CIDR 由 Engineer 创建会话时动态下发。

以管理员身份运行：

```powershell
Set-Location C:\RemLink\Site
.\RemLinkSite.exe
```

正常状态应包含：

```text
WireGuard=CONNECTED
Control=CONNECTED
RemoteSubnet=READY
SubnetGateway=GVISOR_NETSTACK/READY
```

建立会话前，先在 Site 本机验证目标和路由：

```powershell
Get-NetRoute -AddressFamily IPv4 | Sort-Object DestinationPrefix,RouteMetric
ping 192.168.17.10
Test-NetConnection 192.168.17.10 -Port 502
```

目标网段必须是直连网络或具有明确非默认路由。只有默认路由时，RemLink 会拒绝会话。

## 基础使用流程

1. 启动 Server，确认 `/api/v1/server/info` 正常。
2. 使用 Join Token 分别启动并注册 Site 和 Engineer。
3. 在 Server Web“节点”页面确认两个节点均为“在线（ONLINE）”。
4. 在 Site 本机确认目标 PLC/HMI 的 IP、端口和明确路由可达。
5. 打开 Engineer，选择需要连接的 Site。
6. 为该 Site 添加远程网段，例如 `192.168.17.0/24`。
7. 等待 Engineer 的本地 CIDR 冲突检查通过。
8. 点击“连接现场”，等待状态进入“活动中（ACTIVE）”。
9. 在 Engineer 电脑使用原生工具访问现场设备：

```powershell
ping 192.168.17.10
Test-NetConnection 192.168.17.10 -Port 102
Test-NetConnection 192.168.17.10 -Port 502
Test-NetConnection 192.168.17.10 -Port 80
```

10. 使用完成后点击“断开会话”，确认远程路由被删除。

Engineer 会按 Site Node ID 保存远程 CIDR。例如：

- 现场 A 保存 `192.168.17.0/24`。
- 现场 B 保存 `192.168.107.0/24`。

切换 Site 时只加载对应现场的 CIDR，不需要手工删除其他现场的网段。Site 离线后，当前会话会自动关闭。

## 三端运行文件位置

### Engineer

```text
Engineer目录/
├─ RemLinkEngineer.exe
├─ engineer.yaml
├─ identity.json
├─ site-profiles.json
├─ wintun.dll
└─ logs/engineer.jsonl
```

### Site

```text
Site目录/
├─ RemLinkSite.exe
├─ site.yaml
├─ identity.json
├─ wintun.dll
└─ logs/site.jsonl
```

### Docker Server

```text
/opt/remlink/docker/
├─ .env
├─ server.yaml
├─ compose.release.yaml
└─ data/
   ├─ remlink.db
   ├─ logs/server.jsonl
   └─ WireGuard及Server运行数据
```

不要把 Engineer 和 Site 放在同一个目录，也不要互相复制 `identity.json`。升级 Windows 程序时保留本端 YAML、身份、日志和已校验的 `wintun.dll`。

## 常见问题

| 现象 | 原因或处理方法 |
|---|---|
| Server 页面无法打开 | 检查 `REMLINK_HTTP_BIND`、Docker 端口映射、防火墙和反向代理 |
| 页面提示需要 Bearer Admin Token | 输入 `.env` 中的 `REMLINK_ADMIN_TOKEN` |
| `JOIN_TOKEN_INVALID` | Join Token 已轮换或填写错误，重新从 Server 安全获取 |
| `SITE_NO_ROUTE` / `DEFAULT_ONLY` | Site 没有目标网段的直连或明确静态路由 |
| `CIDR_LOCAL_CONFLICT` | Engineer 本地已有覆盖远程 CIDR 的网卡或路由 |
| `OVERLAY_LOCAL_CONFLICT` | `10.88.0.0/16` 与某台 Windows 主机本地网络冲突 |
| DPAPI `Key not valid for use in specified state` | `identity.json` 来自其他机器/账户状态或已损坏，应隔离旧身份并重新注册 |
| 容器预检提示 WireGuard 或 `CAP_NET_ADMIN` 不可用 | 检查宿主机 WireGuard、`/dev/net/tun`、Compose 的 `NET_ADMIN` 和 IPv4 forwarding |
| 中国网络构建停在 `apt-get` | 使用 `.env.china.example`，并检查 Docker DNS、IPv4 和镜像连接 |

更完整的故障排查、备份、升级和 HTTPS 配置见部署文档。

## 安全注意事项

- Join Token 和 Admin Token 都不得提交到 Git、截图或公开日志。
- Engineer/Site YAML 中只允许明文保存首次注册所需的 Join Token；Node Token 和 WireGuard 私钥由程序及 DPAPI 管理。
- `.env`、`identity.json`、`site-profiles.json`、数据库、日志和私钥均已加入 `.gitignore`。
- 不要对 Server 容器启用 `privileged`；当前 Compose 只授予 `NET_ADMIN` 并映射 `/dev/net/tun`。
- 不要把 `7001/tcp` 和 `6200/udp` 映射到公网。
- 公网 Bootstrap/Admin 必须使用外部 HTTPS 反向代理保护。
- ZIP 和 EXE 应放到 GitHub Release 或其他发布渠道，不应直接提交到源码历史。

提交前执行：

```powershell
./scripts/maintenance/Test-RepositoryHygiene.ps1
git status --short
git diff --check
```

## 文档入口

- [完整三端部署与使用指南](docs/deployment-and-usage.md)
- [当前实现状态](docs/implementation-status.md)
- [T01–T18 物理验收手册](docs/validation/T01-T18-runbook.md)
- [需求与证据矩阵](docs/validation/requirements-evidence.md)
- [自动化测试覆盖说明](docs/validation/automated-coverage.md)
- [Phase 任务清单](specs/tasks.md)
- [开发与验收检查表](specs/checklist.md)

## 当前验证状态

Phase 0–10 的生产代码、前端、自动化测试、发布打包和 Docker 基线均已实现。自动化覆盖 Bootstrap、IPAM、数据库、Control 重连、Session 状态机、数据包验证、gVisor TCP/UDP/ICMP 往返、网络迁移、重复现场 CIDR 隔离及发布包校验。

Gate A–D 和 T01–T18 的正式结果必须在符合拓扑要求的 Linux/Windows 实机环境中按验收手册采集证据。单元测试、浏览器测试和一次人工联通不能替代完整物理验收记录。
