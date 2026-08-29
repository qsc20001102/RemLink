# Server Docker 部署

完整生产流程见 `docs/deployment-and-usage.md`；在源码仓库中该文件位于 `../../docs/deployment-and-usage.md`，在发布包中位于 `../docs/deployment-and-usage.md`。

Compose 基线删除全部 Linux capability 后只增加 `NET_ADMIN`，映射 `/dev/net/tun`，从不启用 privileged。入口预检会在启动 Server 前检查 TUN、内核 WireGuard、capability 和 IPv4 forwarding。

## 从发布包部署

在解压后的 `RemLink-Server-v1.0.0-linux-amd64/docker` 目录：

~~~bash
cp .env.example .env
chmod 600 .env
# 编辑 .env 与 server.yaml
mkdir -p data
docker compose --env-file .env -f compose.release.yaml config --quiet
docker compose --env-file .env -f compose.release.yaml up --build -d
docker compose --env-file .env -f compose.release.yaml exec server remlink-server -config /etc/remlink/server.yaml -print-join-token
~~~

`compose.release.yaml` 使用发布包中的 `linux-amd64/remlink-server`，不会要求完整源码。

中国大陆网络建议改为 `cp .env.china.example .env`。该模板把 Debian 主仓库和安全仓库切换到清华 TUNA，同时强制 IPv4、关闭 apt HTTP pipelining，并保留超时/重试。Debian 12 容器使用 `/etc/apt/sources.list.d/debian.sources` 的 DEB822 格式，Dockerfile 会通过构建参数替换其中 URI。TUNA 也提示安全镜像可能有同步延迟；网络允许时可把 `REMLINK_APT_SECURITY_MIRROR` 留空以继续使用 Debian 官方安全源。详情见 [TUNA Debian 帮助](https://mirrors.tuna.tsinghua.edu.cn/help/debian/) 和 [TUNA Debian Security 帮助](https://mirrors.tuna.tsinghua.edu.cn/help/debian-security/)。

## 从源码仓库构建

在仓库 `deploy/docker` 目录：

~~~bash
cp .env.example .env
chmod 600 .env
docker compose --env-file .env -f compose.yaml up --build -d
~~~

源码 Compose 会构建前端和 Go Server。

若构建停在 Debian `apt-get`，先用 `docker run --rm debian:bookworm-slim sh -c "apt-get -o Acquire::ForceIPv4=true -o Acquire::Retries=2 -o Acquire::http::Timeout=30 update"` 验证容器网络。Dockerfile 默认强制 IPv4，并为 apt 配置 30 秒超时和 5 次重试；确认环境只有 IPv6 时才把 `REMLINK_APT_FORCE_IPV4` 改为 `0`。中国大陆环境优先使用 `.env.china.example`；测试命令仍超时时，应修复 Docker daemon 的 DNS/代理或在 Ubuntu 上用 `docker build --network=host`，而不是扩大容器权限。

只映射 `8080/tcp` 和配置的 WireGuard UDP 端口。Control `7001/tcp` 与 Session `6200/udp` 只绑定 Overlay，不发布。数据库、密钥和日志写入 `/app/data`，由宿主机 `./data` 持久化。

v1.0 的 `8080/tcp` 不内置 TLS。将 `REMLINK_HTTP_BIND` 设为 `127.0.0.1` 并使用外部 HTTPS 反向代理，或绑定到可信管理接口；不要把未加密 Bootstrap/Admin 直接暴露到 Internet。

管理页修改 WireGuard 端口后，必须同步更新 `REMLINK_WG_PORT` 和 `REMLINK_WG_ENDPOINT`，再强制重建服务，使宿主机与容器映射跟随数据库中的新端口。`./data` 不会因容器重建丢失。
