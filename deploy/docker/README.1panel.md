# 1Panel 构建与更新

交付物为 `RemLink-Server-v1.0.8-docker-build.tar`，包含预编译 Linux amd64 服务端（内嵌 Web 前端）、Dockerfile、构建脚本、编排和配置示例。这是构建上下文，不是镜像归档，不能直接用于 1Panel 的“镜像导入”。

## 本机打包（维护者）

在源码根目录使用 PowerShell 7、Go、Node.js 和 Git：

```powershell
./scripts/build-docker-package.ps1 -Version 1.0.8
```

本机不需要 Docker Desktop 或 WSL。脚本运行前端类型检查、生产构建、Go 测试和 vet，再交叉编译服务端，输出 `dist/RemLink-Server-v1.0.8-docker-build.tar` 及 SHA256。同名输出目录已存在时可用 `-OutputDirectory dist/rebuild` 指定另一个输出位置，避免混入过期文件。

## 服务器构建

上传 tar 到服务器，在终端执行：

```sh
tar -xf RemLink-Server-v1.0.8-docker-build.tar
cd RemLink-Server-v1.0.8-docker-build
sha256sum -c SHA256SUMS.txt
sh build.sh
```

镜像标签为 `remlink/server:1.0.8`，无时间戳和提交号后缀。其他发布版本由打包参数决定，实际标签见包内 `image.txt` 和 `compose.yaml`。

也可在 1Panel 镜像构建界面选择解压目录的 `Dockerfile`，构建上下文为该解压目录，镜像名称填写 `remlink/server:1.0.8`。

构建需要联网下载 Debian 基础镜像及系统依赖；服务器不需要 Go 或 Node。`build.sh` 默认使用清华 Debian 主仓库和安全仓库，强制 IPv4。可通过 shell 环境变量 `REMLINK_APT_FORCE_IPV4`、`REMLINK_APT_DEBIAN_MIRROR`、`REMLINK_APT_SECURITY_MIRROR` 覆盖；把两个镜像变量显式设为空可使用官方源。脚本不会自动读取编排的 `.env`。在 1Panel 界面直接构建时，Dockerfile 默认使用官方源，可另行设置 `APT_*` 构建参数。

## 编排及持久数据

以已有编排目录为例：

```text
/opt/1panel/docker/compose/remlink/
├── docker-compose.yml
├── server.yaml          # 必须是 YAML 文件
└── data/                # 原数据库、密钥和日志目录
```

构建完成后，在 1Panel 编辑原编排，使用包内 `compose.yaml` 的内容。它没有 `build`，镜像是 `remlink/server:1.0.8`，并使用 `pull_policy: never`。不要启用强制拉取。

挂载保持：

```yaml
    volumes:
      - ./server.yaml:/etc/remlink/server.yaml:ro
      - ./data:/app/data
```

相对路径以编排目录为准，不是镜像构建目录。首次部署才把 `server.example.yaml` 复制为 `server.yaml` 并创建 data；升级必须保留原配置和完整数据。若迁移目录，先停止新旧容器，再完整复制原 server.yaml 和 data，保留原副本直到验证通过。不要同时运行使用新旧数据副本的两个服务端。

如果启动报 `server.yaml: is a directory`，说明挂载到的是文件夹，常见原因是编排目录里原本没有该文件，Docker 自动创建了同名文件夹。停止容器，将误建目录改名备份，再放入真正的配置文件；同时确认 data 是迁移来的原数据。

应用运行配置不再从容器环境变量读取。`.env.example` 仅包含可选 apt 构建参数。

### 从旧版本升级

保留原 server.yaml 和 data，在 YAML 已有的 `server:` 节点下补充以下两项，使用原环境变量里的真实值：

```yaml
server:
  wg_endpoint: "vpn.example.com:51820"
  admin_token: "replace-with-your-existing-admin-token"
  http_listen: "0.0.0.0:8080"
  control_listen: "10.88.0.1:7001"
  wireguard_port: 51820
```

不要新建第二个 server 节点，也不要覆盖旧配置的其他字段。旧 `REMLINK_WG_ENDPOINT`、`REMLINK_ADMIN_TOKEN` 和 `-wg-endpoint` 不再使用。实际配置不复制进镜像；请限制宿主机 YAML 的读取权限。

编排仍保留端口映射，默认 `0.0.0.0:8080:8080/tcp` 和 `51820:51820/udp`。按原部署调整绑定地址；若原来只允许本机访问管理页，保持 `127.0.0.1:8080:8080/tcp`。健康检查读取 YAML 的 HTTP 监听，不再固定 8080。

### 配置优先级

- 管理页仍可修改网络参数，保存到数据库，不回写 YAML，配置文件保持只读挂载。
- 数据库已有网络设置时，Overlay CIDR、Server Overlay IP、WireGuard 端口、Session UDP 端口和 MTU 以数据库为准；没有记录时使用 YAML。
- 公网地址、管理令牌、HTTP 监听和数据路径取自 YAML。
- `server.wg_endpoint` 的端口必须与生效的 WireGuard 端口一致，否则启动报错。管理页改 WireGuard 端口后，须同步修改 YAML 的公网地址端口和编排 UDP 映射，再重新创建容器。
- 修改 YAML 不会覆盖数据库中的网络设置；后续网络调整仍通过管理页操作。

保存编排后重新创建容器，不要仅重启。同一标签重新构建也必须重新创建，例如在编排目录执行：

```sh
docker compose -f docker-compose.yml up -d --force-recreate --pull never
```

检查容器健康状态、日志、管理页、原节点及实际连接。构建包校验不代表 Docker 构建或真实 WireGuard/TUN 链路已经验证。更新前备份原数据；回退时确认镜像与数据格式兼容。
