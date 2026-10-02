# RemLink Server 独立发布包

本包包含 Linux amd64 服务端、内嵌 Web 管理页、Docker 构建文件和验收工具。

- `linux-amd64/remlink-server`：Linux 二进制。
- `linux-amd64/server.yaml`：原生运行示例，使用前修改公网地址、管理令牌及数据路径。
- `docker/server.yaml`：Docker 配置示例，数据目录为 `/app/data`。
- `docker/compose.release.yaml`：使用本包预编译二进制构建运行镜像。
- `docker/compose.image.yaml`：已有本地镜像时直接运行。
- `BUILD-INFO.json`、`SHA256SUMS.txt`：构建元数据和文件校验和。

本 ZIP 需要解压部署。若收到完整镜像 `tar.gz`，可用 Docker 镜像导入；若收到 `docker-build.tar`，则先解压并执行其 `build.sh`。

完整步骤见包内 `docs/deployment-and-usage.md`。Docker 命令和 NAS/1Panel 说明位于包内 `docker` 目录。

首次部署才使用示例配置。升级保留原 `server.yaml` 和完整 `data`，避免丢失数据库、节点登记与 WireGuard 密钥。
