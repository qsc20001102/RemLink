import type { EventRecord, NodeRecord } from './types'
import { logMessageLabel, reasonLabel } from './zh-cn'

function field(log: EventRecord, key: string) { const value = log.fields?.[key]; return typeof value === 'string' ? value : '' }
function lookup(id: string, nodes: NodeRecord[]) { return nodes.find(node => node.node_id === id)?.name || '' }
export function eventDevices(log: EventRecord, nodes: NodeRecord[]) {
  const engineer = field(log, 'engineer_name') || lookup(field(log, 'engineer_node_id'), nodes)
  const site = field(log, 'site_name') || lookup(field(log, 'site_node_id'), nodes)
  if (engineer || site) return `${engineer || 'Engineer 设备'} → ${site || 'Site 设备'}`
  return field(log, 'node_name') || field(log, 'name') || lookup(log.node_id || '', nodes) || (log.node_id ? '已移除设备' : 'Server')
}
export function eventMessage(log: EventRecord, nodes: NodeRecord[]) {
  const message = logMessageLabel(log.message)
  const name = eventDevices(log, nodes)
  const engineer = field(log, 'engineer_name') || lookup(field(log, 'engineer_node_id'), nodes) || 'Engineer 设备'
  const site = field(log, 'site_name') || lookup(field(log, 'site_node_id'), nodes) || 'Site 设备'
  const pair = `${engineer} 与 ${site}`
  const code = field(log, 'error_code') || field(log, 'reason')
  const reason = code ? `；原因：${reasonLabel(code).replace(/（[^）]+）$/, '')}` : ''
  if (message.includes('管理员已强制断开会话')) return `管理员已断开 ${pair} 的远程连接`
  if (message.includes('会话准备已开始')) return `${engineer} 正在请求连接 ${site}`
  if (message.includes('会话状态变更为')) {
    const status = field(log, 'session_status') || /（([A-Z_]+)）/.exec(message)?.[1] || ''
    const descriptions: Record<string, string> = { CREATING: `${engineer} 正在请求连接 ${site}`, PREPARING_SITE: `${site} 正在准备接受 ${engineer} 的连接`, READY: `${site} 已准备就绪，等待 ${engineer} 连接`, ACTIVE: `${pair} 已建立远程连接`, STOPPING: `${pair} 正在断开远程连接`, CLOSED: `${pair} 的远程连接已结束`, FAILED: `${engineer} 连接 ${site} 失败` }
    return (descriptions[status] || `${pair} 的连接状态已更新`) + reason
  }
  if (message.includes('设备已断开服务器连接')) return `${name} 已断开服务器连接`
  if (message.includes('节点 Control 通道已连接')) return `${name} 已上线并连接服务器`
  if (message.includes('节点心跳状态变更为')) {
    if (message.includes('OFFLINE')) return `${name} 已离线`
    if (message.includes('UNSTABLE')) return `${name} 的连接不稳定`
    return `${name} 已恢复在线`
  }
  if (message.includes('节点已撤销')) return `管理员已撤销 ${name}`
  if (message.includes('节点配置已更新')) return `${name} 的设备名称或网络设置已更新`
  if (message.includes('节点拒绝了 Overlay 网络配置')) return `${name} 的网络配置更新失败`
  return message
}
