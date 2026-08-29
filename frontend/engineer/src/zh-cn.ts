const statusLabels: Record<string, string> = {
  IDLE: '空闲（IDLE）', CREATING: '正在创建（CREATING）', PREPARING_SITE: '正在准备现场端（PREPARING_SITE）',
  READY: '准备就绪（READY）', ACTIVE: '活动中（ACTIVE）', STOPPING: '正在停止（STOPPING）',
  CLOSED: '已关闭（CLOSED）', FAILED: '失败（FAILED）',
}
const levelLabels: Record<string, string> = { INFO: '信息', WARN: '警告', ERROR: '错误' }
const errorLabels: Record<string, string> = {
  SITE_NO_ROUTE: '现场端没有通往远程网段的明确路由', SESSION_TIMEOUT: '会话建立超时', SITE_OFFLINE: '现场端离线',
  CIDR_INVALID: '远程网段格式无效', CIDR_LOCAL_CONFLICT: '远程网段与本地网络冲突',
  CIDR_OVERLAY_CONFLICT: '远程网段与 Overlay 网段冲突', NETSTACK_UNAVAILABLE: '现场端 netstack 网关不可用',
  FLOW_LIMIT_REACHED: '现场端连接流数量已达到上限', SESSION_INJECT_FAILED: '会话数据包注入失败',
  ENGINEER_SESSION_EXISTS: 'Engineer 已存在未结束的会话',
}

export function statusLabel(value: string) { return statusLabels[value] ?? value }
export function levelLabel(value: string) { return levelLabels[value] ?? value }
export function errorLabel(cause: unknown) {
  const value = String(cause).replace(/^Error:\s*/, '')
  for (const [code, label] of Object.entries(errorLabels)) if (value.includes(code)) return `${label}（${code}）`
  if (value.includes('administrator privileges are required')) return '需要以管理员身份运行，才能管理 RemLink Wintun 网卡'
  if (value.includes('decrypt WireGuard private key')) return '无法解密 WireGuard 私钥：identity.json 不是由当前 Windows 系统生成，请重新注册节点'
  if (value.includes('Join Token is required')) return '首次注册需要在 engineer.yaml 中填写 Join Token'
  if (value.includes('conflicts with existing route')) return `远程网段与现有本地路由冲突；原始信息：${value}`
  if (value.includes('Site has no route to Remote CIDR')) return '现场端没有通往远程网段的明确路由（SITE_NO_ROUTE）'
  if (value.includes('native runtime is unavailable')) return 'RemLink 原生运行时不可用，请使用正式 Engineer.exe 启动'
  return value
}
