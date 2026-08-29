const statusLabels: Record<string, string> = {
  ONLINE: '在线（ONLINE）', UNSTABLE: '连接不稳定（UNSTABLE）', OFFLINE: '离线（OFFLINE）',
  CREATING: '正在创建（CREATING）', PREPARING_SITE: '正在准备现场端（PREPARING_SITE）',
  READY: '准备就绪（READY）', ACTIVE: '活动中（ACTIVE）', STOPPING: '正在停止（STOPPING）',
  CLOSED: '已关闭（CLOSED）', FAILED: '失败（FAILED）', IDLE: '空闲（IDLE）',
}

const levelLabels: Record<string, string> = { INFO: '信息（INFO）', WARN: '警告（WARN）', ERROR: '错误（ERROR）', DEBUG: '调试（DEBUG）' }
const moduleLabels: Record<string, string> = {
  CORE: '核心（CORE）', BOOTSTRAP: '节点接入（BOOTSTRAP）', WG: 'WireGuard（WG）', IPAM: '地址分配（IPAM）',
  CONTROL: '控制通道（CONTROL）', SESSION: '会话（SESSION）', ROUTE: '路由（ROUTE）', NETSTACK: '网络栈（NETSTACK）',
  TUN: '虚拟网卡（TUN）', SUBNET: '远程网段（SUBNET）', SYSTEM: '系统（SYSTEM）',
}
const reasonLabels: Record<string, string> = {
  SITE_NO_ROUTE: '现场端没有通往远程网段的明确路由（SITE_NO_ROUTE）',
  SESSION_TIMEOUT: '会话建立超时（SESSION_TIMEOUT）',
  SITE_OFFLINE: '现场端离线（SITE_OFFLINE）',
  CIDR_INVALID: '远程网段格式无效（CIDR_INVALID）',
  CIDR_LOCAL_CONFLICT: '远程网段与 Engineer 本地网络冲突（CIDR_LOCAL_CONFLICT）',
  CIDR_OVERLAY_CONFLICT: '远程网段与 Overlay 网段冲突（CIDR_OVERLAY_CONFLICT）',
  NETSTACK_UNAVAILABLE: '现场端 netstack 网关不可用（NETSTACK_UNAVAILABLE）',
  FLOW_LIMIT_REACHED: '现场端连接流数量已达到上限（FLOW_LIMIT_REACHED）',
  SESSION_INJECT_FAILED: '会话数据包注入失败（SESSION_INJECT_FAILED）',
  INVALID_REQUEST: '请求内容无效（INVALID_REQUEST）', NODE_NOT_FOUND: '没有找到指定节点（NODE_NOT_FOUND）',
  NODE_UPDATE_FAILED: '节点更新失败（NODE_UPDATE_FAILED）', SESSION_DISCONNECT_FAILED: '会话断开失败（SESSION_DISCONNECT_FAILED）',
  PEER_REVOKE_FAILED: 'WireGuard 对等节点撤销失败（PEER_REVOKE_FAILED）', NODE_DELETE_FAILED: '节点删除失败（NODE_DELETE_FAILED）',
  INVALID_SESSION_ID: '会话 ID 无效（INVALID_SESSION_ID）', NETWORK_UPDATE_FAILED: '网络配置更新失败（NETWORK_UPDATE_FAILED）',
  JOIN_TOKEN_ROTATE_FAILED: 'Join Token 轮换失败（JOIN_TOKEN_ROTATE_FAILED）',
}

const oldMessages: Record<string, string> = {
  'Node Control connected': '节点 Control 通道已连接',
  'Node rejected Overlay network configuration': '节点拒绝了 Overlay 网络配置',
  'Session preparation started': '会话准备已开始',
  'Node updated': '节点配置已更新',
  'Node revoked': '节点已撤销',
  'Session disconnected by administrator': '管理员已强制断开会话',
  'Network configuration updated': '网络配置已更新',
}

export function statusLabel(value: string) { return statusLabels[value] ?? value }
export function levelLabel(value: string) { return levelLabels[value] ?? value }
export function moduleLabel(value: string) { return moduleLabels[value] ?? value }
export function reasonLabel(value: string) { return reasonLabels[value] ?? value }

export function logMessageLabel(value: string) {
  if (oldMessages[value]) return oldMessages[value]
  let match = /^Session status changed to ([A-Z_]+)$/.exec(value)
  if (match) return `会话状态变更为 ${statusLabel(match[1])}`
  match = /^Node heartbeat status changed to ([A-Z_]+)$/.exec(value)
  if (match) return `节点心跳状态变更为 ${statusLabel(match[1])}`
  return value
}

export function errorLabel(cause: unknown) {
  const value = String(cause).replace(/^Error:\s*/, '')
  for (const [code, label] of Object.entries(reasonLabels)) if (value.includes(code)) return `${label}；原始信息：${value}`
  if (value.includes('valid Bearer Admin Token required')) return '需要有效的管理员 Bearer Token'
  if (value.includes('Failed to fetch')) return '无法连接 Server API，请检查服务地址、端口和防火墙'
  return value
}
