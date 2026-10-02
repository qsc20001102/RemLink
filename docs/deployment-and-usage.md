# RemLink 三端部署与使用

Server、Engineer、Site 分别部署在 Linux 服务端、工程师 Windows 电脑和现场 Windows 电脑。两个 Windows 角色使用独立目录和身份；不要在同一主机同时占用 `RemLink` 适配器。

## 网络准备

| 默认端口 | 用途 | 部署要求 |
| --- | --- | --- |
| `8080/tcp` | Web、Bootstrap、Admin API | 可信管理网访问，或通过 HTTPS 反向代理 |
| `51820/udp` | WireGuard | 两类 Windows 节点必须可达，NAT 场景需转发 |
| `7001/tcp` | Control WebSocket | 只在 Overlay 内监听，不发布公网 |
| `6200/udp` | Session 数据报 | 只在 Overlay 内监听，不发布公网 |

Server 需要 Linux amd64、内核 WireGuard。Docker 部署还需 `/dev/net/tun`、`NET_ADMIN` 和 `net.ipv4.ip_forward=1`。入口脚本会检查这些条件。

默认 Overlay 为 `10.88.0.0/16`，Server IP 为 `10.88.0.1`。Overlay 不得与 Windows 主机的本地直连网络重叠。Site 本机须有到现场目标网段的直连或明确非默认路由；只有默认路由会返回 `SITE_NO_ROUTE`。

## Server 部署

### 选择发布形式

| 发布物 | 用法 |
| --- | --- |
| `remlink-server-1.0.8-amd64.tar.gz` | 完整镜像归档，Docker 镜像导入或 `docker load`；用 `compose.image.yaml` / 随附 `compose.yaml` 启动 |
| `RemLink-Server-v1.0.8-docker-build.tar` | 预编译构建上下文，解压后执行 `sh build.sh`，再用随附 `compose.yaml` 启动 |
| `RemLink-Server-v1.0.8-linux-amd64.zip` | Linux 二进制和 Docker 部署目录；用包内 `docker/compose.release.yaml` 构建运行镜像 |
| 源码仓库 | 在 `deploy/docker` 用 `compose.yaml` 构建前端、Go 服务端和运行镜像 |

上述版本号为示例；部署时以交付物标签为准。绿联 NAS 与 1Panel 的详细步骤在源码的 `deploy/docker/README.ugreen.md`、`README.1panel.md`；Server ZIP 中对应目录为 `docker`。

### 配置

完整镜像不包含你的运行配置。首次部署将示例复制为 `server.yaml`；升级沿用原配置。

```yaml
server:
  wg_endpoint: "vpn.example.com:51820"
  admin_token: "replace-with-your-existing-admin-token"
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

- `wg_endpoint` 填写 Windows 客户端可达的域名/IP 和 UDP 端口。其端口必须与实际生效的 WireGuard 端口一致。
- `admin_token` 换成自己的管理令牌，并限制宿主机配置文件的读取权限。
- `http_listen` 和 `data.directory` 使用 YAML 值。公网地址与管理令牌也从 YAML 读取。
- Overlay CIDR、Server Overlay IP、WireGuard 端口、Session UDP 端口和 MTU 首次启动使用 YAML；数据库已有配置时使用数据库值。管理页保存网络配置不会回写 YAML。
- 应用不读取旧的 `REMLINK_WG_ENDPOINT`、`REMLINK_ADMIN_TOKEN`。`.env` 只用于源码/发布包镜像构建阶段的 apt 参数。

Docker 挂载配置文件到 `/etc/remlink/server.yaml`（只读），挂载原 `data` 目录到 `/app/data`。相对路径以 Compose 项目目录为准。

### 使用已有镜像

在镜像压缩包所在目录导入：

```sh
sha256sum -c remlink-server-1.0.8-amd64.tar.gz.sha256
docker load -i remlink-server-1.0.8-amd64.tar.gz
```

将随附的 `compose.yaml`、配置和数据放在固定部署目录，例如 `/opt/remlink`：

```text
/opt/remlink/
  compose.yaml
  server.yaml
  data/
```

在该目录执行，下面的首次初始化命令只适用于新部署：

```sh
cp server.example.yaml server.yaml
mkdir -p data
# 编辑 server.yaml 后执行
docker compose -f compose.yaml config --quiet
docker compose -f compose.yaml up -d --pull never
docker compose -f compose.yaml ps
docker compose -f compose.yaml logs --tail=100 server
docker compose -f compose.yaml exec server remlink-server -config /etc/remlink/server.yaml -healthcheck
```

`server.example.yaml` 来自随附部署文件。`server.yaml` 必须预先存在且是文件。镜像已包含启动命令和健康检查；Compose 使用本地镜像。

### 使用构建上下文或 Server ZIP

构建上下文 TAR：

```sh
tar -xf RemLink-Server-v1.0.8-docker-build.tar
cd RemLink-Server-v1.0.8-docker-build
sha256sum -c SHA256SUMS.txt
sh build.sh
```

然后在部署目录使用随附 `compose.yaml`、`server.example.yaml`，按“使用已有镜像”流程启动。

Server ZIP 解压后，在包内 `docker` 目录编辑 `server.yaml` 并创建数据目录：

```sh
mkdir -p data
docker compose -f compose.release.yaml config --quiet
docker compose -f compose.release.yaml up --build -d
```

源码构建在仓库 `deploy/docker` 目录执行 `docker compose -f compose.yaml up --build -d`。两种 Compose 构建方式均可选 `cp .env.china.example .env` 设置 apt 镜像；仅使用已有镜像时无需 `.env`。

### 管理入口和 Join Token

浏览器打开 `http://Server地址:8080`，填写 `server.admin_token`。跨公网使用时，在现有网关配置 HTTPS 反向代理，并让 Windows 配置中的 `server` 指向相应 HTTPS URL；Server 本身不终止 TLS。

在已有镜像的 Compose 项目目录查看或轮换注册令牌：

```sh
docker compose -f compose.yaml exec server remlink-server -config /etc/remlink/server.yaml -print-join-token
docker compose -f compose.yaml exec server remlink-server -config /etc/remlink/server.yaml -rotate-join-token
```

其他部署方式替换 `-f` 后的编排文件名。也可在管理页“网络”查看和轮换 Join Token。轮换前同一个 Join Token 可登记多个节点；已登记节点使用 Node Token，轮换不会撤销它们。

### 原生 Linux

如果直接运行 Server ZIP 中的 `linux-amd64/remlink-server`，安装 `ca-certificates`、`iproute2`、`iptables` 和 `wireguard-tools`。将配置的 `data.directory` 改为实际持久目录，例如 `/var/lib/remlink`。进程需要管理内核 WireGuard 与转发规则的权限：

```sh
sudo ./linux-amd64/remlink-server -config /etc/remlink/server.yaml
```

可用 systemd 管理该进程；停止时发送 SIGTERM，并为应用的 10 秒 HTTP 关闭过程留出时间。

## Engineer 和 Site

将两个 Windows ZIP 分别解压到可持续写入的独立目录，例如 `C:\RemLink\Engineer` 和 `C:\RemLink\Site`。它们的默认配置路径以 EXE 所在目录为准。

Engineer 的 `engineer.yaml`：

```yaml
server: "https://remlink.example.com"
node_name: "Engineer-Shanghai-01"
join_token: "粘贴从 Server 获取的 Join Token"
```

Site 的 `site.yaml`：

```yaml
server: "https://remlink.example.com"
node_name: "Site-Qingdao-01"
join_token: "粘贴从 Server 获取的 Join Token"
netstack:
  tcp_flow_limit: 2048
  udp_flow_limit: 4096
  udp_idle_seconds: 60
```

`server` 只允许 HTTP/HTTPS 根地址，不带额外路径、查询参数或用户名密码。Site 配置不填写现场 CIDR；它由 Engineer 在创建会话时下发。首次注册令牌优先级为 `-join-token`、`REMLINK_JOIN_TOKEN`、YAML `join_token`。

以管理员身份在各自目录运行：

```powershell
# 工程师电脑
.\RemLinkEngineer.exe
# 现场电脑
.\RemLinkSite.exe
```

Site 控制台会显示 `OverlayIP`、`WireGuard=已连接`、`Control=已连接`，以及 `远程网段=就绪 子网网关=gVisor netstack/就绪`。Server 节点页应显示 ONLINE。全部预期节点注册后可以轮换 Join Token。

| 运行文件 | 作用 |
| --- | --- |
| `engineer.yaml` / `site.yaml` | 当前角色配置，Join Token 是明文首次注册便捷项 |
| `identity.json` | DPAPI 保护的节点身份，只能在生成它的 Windows 主机使用 |
| `logs/engineer.jsonl` / `logs/site.jsonl` | 角色日志 |
| `wintun.dll` | 内嵌固定版本运行库，首次释放并校验摘要 |
| `site-profiles.json` | 仅 Engineer 使用，按 Site NodeID 保存远程 CIDR |

Engineer 使用交互式 GUI。Site 如需开机启动，可在前台完成注册后，用 Windows 计划任务以最高权限运行 Site EXE，并指定配置文件和工作目录；不要与前台进程同时运行。

## 连接现场

1. 在 Site 本机验证目标可达，例如 `ping 192.168.13.10` 或 `Test-NetConnection 192.168.13.10 -Port 502`。
2. 在 Engineer 确认服务器和 Control 在线，选择就绪的 Site。
3. 添加一个或多个远程 CIDR，例如 `192.168.13.0/24`。本地冲突检查必须通过。
4. 点击连接，等待会话到 `ACTIVE` 后再用原生工具访问现场目标。
5. 完成后断开会话。切换 Site 前先关闭当前会话。

现场网段按 Site 独立保存。多个 Engineer 可访问同一 Site；不同 Site 允许使用相同 CIDR。Site 离线会关闭相关会话。Server 重启后旧会话关闭，节点可重连，Engineer 需重新创建会话。

## 管理、备份与升级

管理页可查看和修改节点、断开会话、修改网络参数、查看事件日志。修改 WireGuard 端口后，同时修改 YAML 的 `wg_endpoint` 端口和 Compose UDP 映射，再重新创建容器。

升级前停止 Server，备份原 `server.yaml` 和完整 `data`。新镜像导入后在原部署目录重新创建：

```sh
docker compose -f compose.yaml up -d --force-recreate --pull never
```

Server 数据包含数据库 `remlink.db`、WireGuard 密钥 `server-wg.key` 和 `logs/server.jsonl`。不要用新示例覆盖原配置，也不要重新生成旧节点依赖的数据库和密钥。

Windows 升级前退出对应进程，保留本端 YAML、`identity.json`、`logs`、`wintun.dll`，Engineer 还须保留 `site-profiles.json`。更换 Windows 主机时重新注册，不能跨机器复制 DPAPI 身份。

## 常见问题

| 现象 | 检查 |
| --- | --- |
| 配置文件是目录或不存在 | 确认 Compose 项目目录及文件挂载来源 |
| WireGuard 预检失败 | 宿主机内核、TUN、`NET_ADMIN`、IPv4 转发 |
| endpoint 端口不匹配 | YAML 公网端口、数据库实际端口与 UDP 映射 |
| 管理页 401 | 管理令牌是否与 YAML 一致 |
| `JOIN_TOKEN_INVALID` | 当前 Join Token 是否已轮换 |
| `NODE_AUTH_FAILED` | 身份是否被撤销、损坏或迁移到其他机器 |
| `OVERLAY_LOCAL_CONFLICT` | Overlay 是否与 Windows 本地网络重叠 |
| `CIDR_LOCAL_CONFLICT` | Engineer 本地网络/路由是否覆盖远程 CIDR |
| `SITE_NO_ROUTE` | Site 是否存在到目标的非默认路由 |
| `ENGINEER_SESSION_EXISTS` | 当前非终态会话是否已关闭 |
| ONLINE 但业务不通 | Site 目标可达、WireGuard 握手、会话 ACTIVE、Engineer 路由和应用日志 |

实机验收使用源码或 Server ZIP 中的 `docs/validation/T01-T18-runbook.md` 与 `scripts/validation` 工具。先初始化运行目录，再采证、记录结果和校验；自动化测试不能替代指定部署的真实联通结果。
