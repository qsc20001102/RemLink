# RemLink Engineer 独立便携包

本目录只属于 Engineer，不包含 Site 或 Server 程序。请把整个目录放在可持续写入的位置，例如 `C:\RemLink\Engineer`；不要只复制 EXE，也不要与 Site 解压到同一目录。

## 首次使用

1. 以管理员身份打开 PowerShell，进入本目录。
2. 编辑 `engineer.yaml`，填写 Server URL、Engineer 名称和 `join_token`。
3. 直接执行：

~~~powershell
.\RemLinkEngineer.exe
~~~

`join_token` 会以明文保存在 YAML 中，用于首次注册、切换 Server 和同步改名；注册后的 Node Token 和 WireGuard 私钥仍由 DPAPI 保护。后续直接以管理员身份运行 `RemLinkEngineer.exe`。默认配置路径始终是 EXE 旁的 `engineer.yaml`，与启动时的当前工作目录无关。

修改 YAML 后退出并重新启动，程序会自动同步 `identity.json`，无需删除文件。修改 `node_name` 会立即更新本地身份；填写当前 Server 的有效 `join_token` 后，也会同步服务器显示名称。切换 `server` 时填写新 Server 的有效 `join_token`，注册成功后自动更新身份；失败时保留原身份。节点 ID、WireGuard 密钥及路由清理记录会保留。

不知道远程网段如何填写时，点击连接页的“网段计算器”，输入目标设备 IP 和子网掩码。例如 `192.168.13.10`、`255.255.255.0` 会得到 `192.168.13.0/24`。点击“填入远程网段”后，再点击“添加”完成冲突预检和保存。

窗口底部的“实时日志”默认显示最新一条状态，切换页面或滚动时仍保持可见。点击右侧“展开”查看当前保留的日志记录，可搜索、按级别筛选或关闭“跟随最新”浏览旧记录；点击“折叠”恢复单行显示。侧栏“日志”仍提供完整日志列表。

## 本目录中的运行文件

- `RemLinkEngineer.svg`、`RemLinkEngineer.ico`：Engineer 专属图标；ICO 已嵌入 EXE，也可用于快捷方式。
- `identity.json`：NodeID、Node Token 和 WireGuard 私钥等 DPAPI 保护身份，仅可在生成它的 Windows 主机解密。
- `site-profiles.json`：由界面自动生成，按 Site NodeID 保存各现场的 Remote CIDR；不包含密钥或 Token。切换现场时会自动加载对应网段。
- `logs\engineer.jsonl`：Engineer 日志。
- `wintun.dll`：EXE 内嵌的固定版本 Wintun 首次运行释放文件，程序会校验 SHA-256。

备份或升级前先退出 Engineer。升级时保留 `engineer.yaml`、`identity.json`、`site-profiles.json` 和 `logs`，只替换通过校验的新 EXE；完整三端流程见 `docs\deployment-and-usage.md`。
