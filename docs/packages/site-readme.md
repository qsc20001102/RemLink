# RemLink Site 独立便携包

本目录只属于 Site，不包含 Engineer 或 Server 程序。请把整个目录放在可持续写入的位置，例如 `C:\RemLink\Site`；不要只复制 EXE，也不要与 Engineer 解压到同一目录。

## 首次使用

1. 以管理员身份打开 PowerShell，进入本目录。
2. 编辑 `site.yaml`，填写 Server URL、Site 名称、`join_token` 和 netstack 容量。
3. 直接执行：

~~~powershell
.\RemLinkSite.exe
~~~

`join_token` 会以明文保存在 YAML 中，只在首次注册时使用；注册后的 Node Token 和 WireGuard 私钥仍由 DPAPI 保护。确认 Overlay、Control 和 RemoteSubnet 均就绪后停止前台程序。后续直接运行 `RemLinkSite.exe`，或按完整指南注册最高权限启动任务；默认配置路径始终是 EXE 旁的 `site.yaml`。

## 本目录中的运行文件

- `RemLinkSite.svg`、`RemLinkSite.ico`：Site 专属图标；ICO 已嵌入 EXE，也可用于快捷方式。
- `identity.json`：NodeID、Node Token 和 WireGuard 私钥等 DPAPI 保护身份，仅可在生成它的 Windows 主机解密。
- `logs\site.jsonl`：Site 日志。
- `wintun.dll`：EXE 内嵌的固定版本 Wintun 首次运行释放文件，程序会校验 SHA-256。

备份或升级前先停止前台进程和计划任务。升级时保留 `site.yaml`、`identity.json` 和 `logs`，只替换通过校验的新 EXE；完整三端流程见 `docs\deployment-and-usage.md`。
