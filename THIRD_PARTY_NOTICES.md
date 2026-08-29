# RemLink 第三方组件声明

RemLink v1.0 使用以下固定版本第三方组件。源码发行包和依赖锁文件记录准确版本及传递依赖图；版权归各自作者所有。

| 组件 | 固定版本 | 许可证 |
|---|---:|---|
| wireguard-go | `ecfc5a8d5446` | MIT |
| WireGuard Go 控制库（wgctrl） | `a9ab2273dd10` | MIT |
| Wintun Go 绑定与 Wintun 运行库 | `0fa3db229ce2` / `0.14.1` | MIT |
| WireGuard for Windows Go 包 | `v1.0.1` | MIT |
| gVisor netstack | `39ed1f5ac29c` | Apache-2.0 |
| coder/websocket | `v1.8.15` | ISC |
| Wails | `v2.13.0` | MIT |
| modernc.org/sqlite | `v1.57.0` | BSD-3-Clause；内置 SQLite 属于公有领域 |
| Vue | `v3.5.41` | MIT |
| Vite | `v8.2.2` | MIT |

完整许可证文本可在对应上游源码发行物和模块/包缓存中查看：

- [wireguard-go](https://git.zx2c4.com/wireguard-go/)
- [Wintun](https://www.wintun.net/)
- [gVisor](https://gvisor.dev/)
- [coder/websocket](https://github.com/coder/websocket)
- [Wails](https://github.com/wailsapp/wails)
- [modernc.org/sqlite](https://gitlab.com/cznic/sqlite)
- [Vue](https://github.com/vuejs/core)
- [Vite](https://github.com/vitejs/vite)

内嵌 `wintun.dll` 是 Wintun 0.14.1 官方签名的 AMD64 二进制文件。RemLink 加载前验证 SHA-256：

`e5da8447dc2c320edc0fc52fa01885c103de8c118481f683643cacc3220dafce`
