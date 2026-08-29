# RemLink 界面设计记录

Engineer 和 Server 在实现前先生成全页面视觉参考，再翻译为 Vue/CSS；概念图没有被直接嵌入产品。两端统一使用冷色近白工作区、深蓝紫操作色、薄荷绿/琥珀/红状态色、细边框、紧凑表格和清晰左侧导航，适合运维桌面。

## 资源

- `engineer-concept.png`：Engineer 连接/Session 概念图。
- `server-concept.png`：Server 运维仪表板概念图。
- `engineer-render.png`：交互 QA 后的 1440×900 实际渲染。
- `server-render.png`：交互 QA 后的 1440×900 实际渲染。

## 还原记录

- 保留：整体壳层、字号层级、连接拓扑线、状态色、表单密度、卡片、表格和主操作。
- 调整：Server 实际渲染补充 Nodes/Sessions/Logs 完整运维内容；Engineer 远程 CIDR 列表支持真实多前缀纵向增长。
- 功能新增：Admin token、运行时长、过滤、编辑/撤销、强制断开、Join Token 轮换、响应式表格滚动和真实心跳 RTT。

浏览器 QA 因交互式 Browser 插件不可用，使用已安装 Microsoft Edge 与项目内 Playwright。1440×900 和 1024×720 的核心交互通过，无控制台/页面/HTTP 错误，也无页面级横向溢出。
