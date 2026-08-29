# RemLink Engineer 独立便携包

本目录只属于 Engineer，不包含 Site 或 Server 程序。请把整个目录放在可持续写入的位置，例如 `C:\RemLink\Engineer`；不要只复制 EXE，也不要与 Site 解压到同一目录。

## 首次使用

1. 以管理员身份打开 PowerShell，进入本目录。
2. 编辑 `engineer.yaml`，填写 Server URL、Engineer 名称和 `join_token`。
3. 直接执行：

~~~powershell
.\RemLinkEngineer.exe
~~~

`join_token` 会以明文保存在 YAML 中，只在首次注册时使用；注册后的 Node Token 和 WireGuard 私钥仍由 DPAPI 保护。后续直接以管理员身份运行 `RemLinkEngineer.exe`。默认配置路径始终是 EXE 旁的 `engineer.yaml`，与启动时的当前工作目录无关。

## 本目录中的运行文件

- `identity.json`：NodeID、Node Token 和 WireGuard 私钥等 DPAPI 保护身份，仅可在生成它的 Windows 主机解密。
- `site-profiles.json`：由界面自动生成，按 Site NodeID 保存各现场的 Remote CIDR；不包含密钥或 Token。切换现场时会自动加载对应网段。
- `logs\engineer.jsonl`：Engineer 日志。
- `wintun.dll`：EXE 内嵌的固定版本 Wintun 首次运行释放文件，程序会校验 SHA-256。

备份或升级前先退出 Engineer。升级时保留 `engineer.yaml`、`identity.json` 和 `logs`，只替换通过校验的新 EXE；完整三端流程见 `docs\deployment-and-usage.md`。
