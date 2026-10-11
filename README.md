# RemLink

RemLink 是面向工业现场的 IPv4 远程接入系统，让 Windows 工程师电脑通过中心服务器访问现场 PLC、HMI、工控机等 IP 设备。

| 程序 | 运行环境 | 作用 |
| --- | --- | --- |
| Server | Linux amd64 | 节点注册、地址分配、内核 WireGuard 中心转发、会话调度和 Web 管理 |
| Engineer | Windows amd64，管理员权限 | Wails 图形界面，选择现场并设置远程 CIDR，创建和断开会话 |
| Site | Windows amd64，管理员权限 | Wails 图形界面，配置服务器、展示连接状态，通过 gVisor netstack 访问现场网络 |

```text
Engineer → WireGuard → Server → WireGuard → Site → 现场 LAN
```

每个 Windows 节点使用一个 `RemLink` Wintun 适配器。Engineer 同时只允许一个非终态会话；一个 Site 可服务多个 Engineer，不同 Site 可以使用相同现场网段。现场设备无需配置返回 Overlay 的路由，但 Site 本机必须有通往目标网段的直连或明确非默认路由。

Windows 节点的身份、应用内设置和现场网段保存在 `%ProgramData%\RemLink\Engineer`、`%ProgramData%\RemLink\Site`，同一电脑换包继续沿用。新电脑打开先配置服务器地址、设备名称和 Join Token，保存后连接；以后自动连接。旧版身份仍在包内时，在原目录启动一次新版以导入旧数据。两端均可隐藏到系统托盘运行。

当前支持 IPv4 TCP、单播 UDP 和 ICMP Echo。架构约束与协议见 [实施规格](specs/spec.md)。

## 部署入口

- [三端部署与使用](docs/deployment-and-usage.md)：配置、注册、连接现场、备份和升级。
- [绿联 NAS 镜像导入](deploy/docker/README.ugreen.md)：导入完整 `tar.gz` 镜像，并使用 `compose.image.yaml` 启动。
- [1Panel 构建包部署](deploy/docker/README.1panel.md)：上传预编译构建上下文 TAR，在服务器构建镜像。
- [Docker 部署](deploy/docker/README.md)：源码构建、发布包构建和已有镜像三种方式。

Server 的公网 WireGuard 地址和管理令牌填写在 `server.yaml` 的 `server.wg_endpoint`、`server.admin_token`。管理页保存的网络参数存入 SQLite，优先于 YAML 中的初始网络值。

默认端口为 `8080/tcp`（Web、Bootstrap、Admin）和 `51820/udp`（WireGuard）。`7001/tcp` 与 `6200/udp` 用于 Overlay 内部通信。Server 不内置 TLS；跨公网访问管理和注册入口时使用 HTTPS 反向代理。

## 开发与检查

发布脚本使用 PowerShell 7、Git、Go 和 Node.js。Go 版本以 `go.mod` 为准，前端依赖由 `frontend/package-lock.json` 锁定；Docker 源码构建使用 Node.js 24。

在仓库根目录执行。前端构建必须先于 Go 检查，因为两个前端的 `dist` 会嵌入可执行文件：

```powershell
npm ci --prefix frontend
npm run typecheck --prefix frontend
npm run build --prefix frontend
go mod verify
go test -count=1 ./...
go vet ./...
./scripts/maintenance/Test-RepositoryHygiene.ps1
./scripts/validation/Test-FrontendProduction.ps1
./scripts/validation/Test-Architecture.ps1
./scripts/validation/Test-AcceptanceTools.ps1
```

测试范围和实机验收方法见 [自动化验证](docs/validation/automated-coverage.md) 与 [T01–T18 验收手册](docs/validation/T01-T18-runbook.md)。验收结果保存在每次运行的证据目录中。

## 打包

在源码根目录双击 `build-all.bat` 即可一键构建三端 1.1.0，Engineer、Site 输出 ZIP，Server 输出可直接导入 Docker / NAS / 1Panel 的完整镜像 `.tar.gz`。脚本使用 PowerShell 7、Git、Go、Node.js/npm 和 Python 3.11+；执行前端、Go、架构检查、客户端压缩包校验以及 Server 镜像配置、镜像层和应用文件摘要校验，完成或失败后保留窗口。

首次打包前，将已有 RemLink Server Docker 镜像归档放到 `dist/runtime/remlink-server-runtime-amd64.tar.gz`，或通过环境变量 `REMLINK_RUNTIME_IMAGE` 指定路径。打包复用其 Debian 与系统工具层，丢弃旧应用层，写入本次编译的 1.1.0 服务端；本机无需 Docker。镜像归档不随源码提交，更新系统依赖的方法见 [绿联 NAS 部署说明](deploy/docker/README.ugreen.md)。

每次运行输出到独立目录 `dist/releases/v1.1.0-时间戳/`，包含两个 ZIP、一个 Server 镜像 `.tar.gz` 和 SHA256、`compose.yaml`、`server.example.yaml`、`README-Server.md` 及构建目录，不覆盖原包或运行身份。命令行或自动化可使用 `build-all.bat --no-pause`。

生成三个独立发布包：

```powershell
./scripts/build-release.ps1 -Version 1.1.0
```

输出到 `dist`：

```text
RemLink-Engineer-v1.1.0-windows-amd64.zip
RemLink-Site-v1.1.0-windows-amd64.zip
remlink-server-1.1.0-amd64.tar.gz
```

版本可通过 `-Version` 修改，输出位置可通过 `-OutputDirectory` 修改，运行镜像可通过 `-RuntimeImagePath` 指定。每个构建目录包含 `BUILD-INFO.json` 和 `SHA256SUMS.txt`。Engineer、Site 构建使用 Wails 必需的 `desktop,production` 标签。Server 镜像用 `docker load -i remlink-server-1.1.0-amd64.tar.gz` 导入，升级沿用原 `server.yaml` 和 `data` 挂载。

只生成 Engineer 和 Site 的未压缩目录：

```powershell
./scripts/build-release.ps1 -Version 1.1.0 -Roles Engineer,Site -SkipArchive -OutputDirectory dist/optimized-v1.1.0
```

输出目录若含身份、现场网段或日志等运行数据，脚本会停止，需改用其他输出目录。

生成供服务器构建的 Docker 上下文 TAR：

```powershell
./scripts/build-docker-package.ps1 -Version 1.1.0
```

构建上下文 TAR 需要解压并执行 `build.sh`。可直接导入的镜像 `tar.gz` 则由 `scripts/export-nas-image.py` 生成，它需要已有 RemLink 镜像的运行依赖层；具体前置条件和命令见绿联 NAS 部署说明。

## 目录

| 路径 | 内容 |
| --- | --- |
| `cmd/server`、`cmd/engineer`、`cmd/site` | 三端程序入口 |
| `internal` | 配置、身份、数据库、组网、会话和现场网关 |
| `frontend/engineer`、`frontend/site`、`frontend/server` | Vue 前端 |
| `config/*.example.yaml` | 三端配置模板 |
| `deploy/docker` | Dockerfile、Compose、预检和部署说明 |
| `scripts` | 打包、图标、仓库检查和验收工具 |
| `docs` | 使用指南、包内说明和验收手册 |
| `specs/spec.md` | 架构与协议约束 |
| `third_party`、`THIRD_PARTY_NOTICES.md` | 第三方许可与声明 |

`dist`、`build`、依赖缓存、前端编译结果、本地部署文件和运行数据已由 `.gitignore` 排除。内嵌 Wintun DLL 是程序所需依赖，保留在源码中。

提交前检查：

```powershell
./scripts/maintenance/Test-RepositoryHygiene.ps1
git diff --check
git status --short
```
