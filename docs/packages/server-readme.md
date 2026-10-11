# RemLink Server 构建目录

本包包含 Linux amd64 服务端、内嵌 Web 管理页、Docker 构建文件和验收工具。

- `linux-amd64/remlink-server`：Linux 二进制。
- `linux-amd64/server.yaml`：原生运行示例，使用前修改公网地址、管理令牌及数据路径。
- `docker/server.yaml`：Docker 配置示例，数据目录为 `/app/data`。
- `docker/compose.release.yaml`：使用本包预编译二进制构建运行镜像。
- `docker/compose.image.yaml`：已有本地镜像时直接运行。
- `BUILD-INFO.json`、`SHA256SUMS.txt`：构建元数据和文件校验和。

`build-all.bat` 默认在本目录旁生成 `remlink-server-版本-amd64.tar.gz` 完整 Docker 镜像，并附带 `compose.yaml`、`server.example.yaml` 和 `README-Server.md`。该镜像包含 `manifest.json` 和镜像层，可直接用 Docker / NAS / 1Panel 的镜像导入功能加载；不需要从本目录构建镜像。

本目录用于构建校验和可选的原生 Linux 部署。若使用 `-SkipArchive`，只生成本目录。另一个入口生成的 `docker-build.tar` 是构建上下文，需要解压并执行其 `build.sh`。

完整步骤见包内 `docs/deployment-and-usage.md`。Docker 命令和 NAS/1Panel 说明位于包内 `docker` 目录。

首次部署才使用示例配置。升级保留原 `server.yaml` 和完整 `data`，避免丢失数据库、节点登记与 WireGuard 密钥。
