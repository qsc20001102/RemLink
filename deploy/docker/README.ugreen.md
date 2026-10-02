# 绿联 NAS 镜像导入

`remlink-server-1.0.8-amd64.tar.gz` 是完整的 Docker 镜像归档，包含 Debian、WireGuard 工具、服务端和内嵌管理页。根目录为 `manifest.json`、镜像配置 JSON，以及各层的 `layer.tar`。无需在 NAS 上安装 Go、Node 或构建镜像。

镜像标签为 `remlink/server:1.0.8`，架构为 `linux/amd64`，用于 Intel / AMD x86-64 NAS。

## 导入和启动

1. 把 `remlink-server-1.0.8-amd64.tar.gz` 上传到 NAS，在 Docker 应用的镜像导入功能中选择该文件。导入后选择 `remlink/server:1.0.8`。
2. 在 NAS 的部署目录放入真正的 `server.yaml` 文件，可从随附的 `server.example.yaml` 复制。修改 `server.wg_endpoint` 为客户端可达的 NAS 公网域名/IP 与 UDP 端口，修改 `server.admin_token` 为自己的管理令牌。局域网测试可先使用 NAS 局域网 IP。创建 `data` 文件夹。
3. 使用随附的 `compose.yaml` 创建项目；相对路径以项目目录为准。也可手动创建容器，按下表设置。

| 设置 | 值 |
| --- | --- |
| 镜像 | `remlink/server:1.0.8`，使用本地镜像，禁止强制拉取 |
| TCP 端口映射 | NAS `8080` → 容器 `8080`，NAS 端口可按需调整 |
| UDP 端口映射 | NAS `51820` → 容器 `51820` |
| 配置文件挂载 | NAS 上的 `server.yaml` → `/etc/remlink/server.yaml`，只读 |
| 数据目录挂载 | NAS 上的 `data` 文件夹 → `/app/data`，可读写 |
| 能力 | `cap_drop: ALL`，`cap_add: NET_ADMIN` |
| 设备 | `/dev/net/tun:/dev/net/tun` |
| 内核参数 | `net.ipv4.ip_forward: "1"` |
| 安全选项 | `no-new-privileges:true` |
| 重启策略 | `unless-stopped` |

保留镜像默认启动命令，运行配置使用 `server.yaml`。编排通过 `bind.create_host_path: false` 要求配置文件预先存在，避免缺失时自动创建同名目录；部署前仍需确认 `server.yaml` 是文件。容器启动后访问 `http://NAS的IP:8080`，使用配置中的管理令牌登录。

编排明确指定 `linux/amd64`，停止宽限为 20 秒，给 Server 最多 10 秒的 HTTP 关闭过程及数据库、日志收尾留出时间。Docker 控制台日志使用 `json-file`，单文件达到 `10m` 后轮转，最多保留 3 个；`data/logs` 中的应用日志由 Server 自身管理。

镜像内置健康检查，无需在 Compose 重复定义：每 15 秒执行一次 `remlink-server -config /etc/remlink/server.yaml -healthcheck`，超时 3 秒，启动宽限 10 秒，连续失败 4 次标记不健康。`restart: unless-stopped` 处理容器进程退出，不会仅因健康状态变成 `unhealthy` 自动重启。

宿主机内核必须支持 WireGuard，且存在 `/dev/net/tun`。镜像包含运行工具，内核支持来自 NAS 系统。入口会检查这些条件；如果日志提示 `kernel WireGuard or CAP_NET_ADMIN is unavailable`，先检查 NAS 内核和容器能力设置。如果界面不能设置能力或 sysctl，使用 Compose 项目创建容器。

只发布管理 HTTP 和 WireGuard UDP 端口；`7001/tcp` 和 `6200/udp` 属于 Overlay 内部通信。跨公网连接时，在路由器转发 WireGuard UDP 端口；管理 HTTP 通过可信网络或已有 HTTPS 反向代理访问。

升级时保留原 `server.yaml` 和完整 `data`，导入新镜像后重新创建容器。数据库中的网络设置优先于 YAML；更改 WireGuard 端口时同步调整配置的公网端口和 UDP 映射。

## 命令行校验

在 NAS 上也可通过 SSH 执行：

```sh
sha256sum -c remlink-server-1.0.8-amd64.tar.gz.sha256
docker load -i remlink-server-1.0.8-amd64.tar.gz
docker image inspect remlink/server:1.0.8 --format '{{.Os}}/{{.Architecture}}'
# 以下命令在 compose.yaml、server.yaml 和 data 所在目录执行
docker compose -f compose.yaml up -d --pull never
docker compose -f compose.yaml exec server remlink-server -config /etc/remlink/server.yaml -healthcheck
```

Docker 官方支持直接加载 gzip 压缩的镜像 TAR：[docker image load](https://docs.docker.com/reference/cli/docker/image/load/)。

## 维护者重新打包

在源码根目录使用 PowerShell 7、Python 3.10+、Go 和 Node.js：

```powershell
./scripts/build-docker-package.ps1 -Version 1.0.8 -OutputDirectory dist/nas-image-build
python scripts/export-nas-image.py --runtime-image dist/remlink-server-1.0.8-image.tar --context dist/nas-image-build/RemLink-Server-v1.0.8-docker-build --output dist/remlink-server-1.0.8-amd64.tar.gz
```

第一步执行前端类型检查、生产构建、Go 测试和 vet，再编译当前 Linux 服务端。第二步复用已有 RemLink 镜像的 Debian 与系统依赖层，丢弃旧应用层，写入本次编译的服务端、预检脚本和第三方声明，输出可导入的镜像归档及 SHA256。配置与持久数据通过挂载提供。

本流程需要已有的 RemLink Docker 镜像 TAR，该文件不随源码提交。更新系统依赖时，应先用 `Dockerfile.release` 构建并 `docker save` 导出新的运行镜像，再传给 `--runtime-image`。重新运行前使用新的输出路径，脚本会拒绝覆盖同名交付物。

打包器会读回输出，校验配置摘要、全部镜像层摘要、发布文件校验和、应用文件内容、Linux 架构和可执行权限。这些检查不替代 NAS 上的镜像导入、容器启动与实际 WireGuard 链路验证。
