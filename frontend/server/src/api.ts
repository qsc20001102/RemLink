import type { EventRecord, LogFilter, NetworkConfig, NodeRecord, SessionFilter, SessionRecord } from './types'

const now = Date.now()
const demoNodes: NodeRecord[] = [
  { node_id: 'eng-01', type: 'engineer', name: 'Engineer-01', overlay_ip: '10.88.0.10', wg_public_key: 'E7y0lQYJb0fYfS0uA8GpR0JtXj4Ff7r2O8lD...', wg_handshake: new Date(now - 6000).toISOString(), status: 'ONLINE', version: '1.0.0', os_version: 'Windows 11', last_seen: new Date(now - 4000).toISOString() },
  { node_id: 'eng-02', type: 'engineer', name: 'Engineer-02', overlay_ip: '10.88.0.11', wg_public_key: 'Q1r9...', status: 'ONLINE', version: '1.0.0', os_version: 'Windows 11', last_seen: new Date(now - 8000).toISOString() },
  { node_id: 'site-a', type: 'site', name: 'Site-A', overlay_ip: '10.88.0.20', wg_public_key: 'L8c2...', status: 'UNSTABLE', version: '1.0.0', os_version: 'Windows Server 2022', last_seen: new Date(now - 22_000).toISOString() },
  { node_id: 'site-b', type: 'site', name: 'Site-B', overlay_ip: '10.88.0.21', wg_public_key: 'M5x4...', status: 'OFFLINE', version: '1.0.0', os_version: 'Windows 10', last_seen: new Date(now - 2_400_000).toISOString() },
]
const demoSessions: SessionRecord[] = [
  { session_id: '8648912340291133', engineer_node_id: 'eng-01', site_node_id: 'site-a', status: 'ACTIVE', cidrs: ['192.168.10.0/24'], created_at: new Date(now - 1_220_000).toISOString(), active_at: new Date(now - 1_200_000).toISOString(), counters: { upload_bytes: 1258291, download_bytes: 3586129, upload_packets: 8912, download_packets: 14022 } },
  { session_id: '7066248371127201', engineer_node_id: 'eng-02', site_node_id: 'site-a', status: 'ACTIVE', cidrs: ['192.168.20.0/24'], created_at: new Date(now - 550_000).toISOString(), active_at: new Date(now - 530_000).toISOString(), counters: { upload_bytes: 712004, download_bytes: 1153434, upload_packets: 4220, download_packets: 6741 } },
  { session_id: '6194730274181054', engineer_node_id: 'eng-01', site_node_id: 'site-b', status: 'CLOSED', cidrs: ['192.168.30.0/24'], created_at: new Date(now - 7_200_000).toISOString(), active_at: new Date(now - 7_180_000).toISOString(), closed_at: new Date(now - 5_400_000).toISOString(), counters: { upload_bytes: 2430410, download_bytes: 8617031, upload_packets: 5010, download_packets: 9332 } },
]
let demoNetwork: NetworkConfig = { overlay_cidr: '10.88.0.0/16', server_overlay_ip: '10.88.0.1', wireguard_port: 51820, session_udp_port: 6200, mtu: 1280, config_version: 1, uptime_seconds: 48376, join_token: 'demo-current-join-token' }
const demoEvents: EventRecord[] = [
  { id: 5, time: new Date(now - 5000).toISOString(), level: 'INFO', module: 'CONTROL', node_id: 'eng-01', message: '节点上线，Overlay 10.88.0.10', fields: {} },
  // Keep two legacy English records in development so the presentation-layer
  // translator is exercised against events persisted by older Server builds.
  { id: 4, time: new Date(now - 12_000).toISOString(), level: 'INFO', module: 'SESSION', session_id: '8648912340291133', message: 'Session status changed to ACTIVE', fields: {} },
  { id: 3, time: new Date(now - 31_000).toISOString(), level: 'WARN', module: 'CONTROL', node_id: 'site-a', message: 'Node heartbeat status changed to UNSTABLE', fields: {} },
  { id: 2, time: new Date(now - 80_000).toISOString(), level: 'ERROR', module: 'CONTROL', node_id: 'site-b', message: '节点离线', fields: {} },
]

const dev = import.meta.env.DEV
let token = localStorage.getItem('remlink-admin-token') ?? ''
export function setAdminToken(value: string) { token = value.trim(); localStorage.setItem('remlink-admin-token', token) }
async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, { ...init, headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}), ...init?.headers } })
  if (!response.ok) {
    const body = await response.json().catch(() => ({}))
    const code = body?.error?.code
    const message = body?.error?.message ?? `HTTP ${response.status}`
    throw new Error(code ? `${code}: ${message}` : message)
  }
  return response.status === 204 ? undefined as T : response.json()
}
export const api = {
  nodes: () => dev ? Promise.resolve(structuredClone(demoNodes)) : request<NodeRecord[]>('/api/v1/admin/nodes'),
  sessions: (filter: SessionFilter = {}) => {
    const params = new URLSearchParams()
    for (const key of ['kind','session_id','engineer_node_id','site_node_id','status','from','to','limit'] as const) if (filter[key]) params.set(key, String(filter[key]))
    if (!dev) return request<SessionRecord[]>(`/api/v1/admin/sessions?${params}`)
    const from = filter.from ? Date.parse(filter.from) : Number.NEGATIVE_INFINITY
    const to = filter.to ? Date.parse(filter.to) : Number.POSITIVE_INFINITY
    return Promise.resolve(structuredClone(demoSessions.filter(session => {
      const history = session.status === 'CLOSED' || session.status === 'FAILED'
      const time = Date.parse(history ? session.closed_at || session.created_at : session.created_at)
      return (!filter.kind || (filter.kind === 'history' ? history : !history)) &&
        (!filter.session_id || session.session_id === filter.session_id) &&
        (!filter.engineer_node_id || session.engineer_node_id === filter.engineer_node_id) &&
        (!filter.site_node_id || session.site_node_id === filter.site_node_id) &&
        (!filter.status || session.status === filter.status) && time >= from && time <= to
    }).slice(0, filter.limit ?? 1000)))
  },
  network: () => dev ? Promise.resolve(structuredClone(demoNetwork)) : request<NetworkConfig>('/api/v1/admin/network'),
  logs: (filter: LogFilter = {}) => {
    const params = new URLSearchParams({ limit: String(filter.limit ?? 200) })
    for (const key of ['level','module','node_id','session_id','from','to'] as const) if (filter[key]) params.set(key, String(filter[key]))
    if (!dev) return request<EventRecord[]>(`/api/v1/admin/logs?${params}`)
    const from = filter.from ? Date.parse(filter.from) : Number.NEGATIVE_INFINITY
    const to = filter.to ? Date.parse(filter.to) : Number.POSITIVE_INFINITY
    return Promise.resolve(structuredClone(demoEvents.filter(event => (!filter.level || event.level === filter.level) && (!filter.module || event.module === filter.module) && (!filter.node_id || event.node_id === filter.node_id) && (!filter.session_id || String(event.session_id ?? '') === filter.session_id) && Date.parse(event.time) >= from && Date.parse(event.time) <= to)))
  },
  patchNode: async (id: string, patch: Partial<Pick<NodeRecord, 'name' | 'overlay_ip'>>) => dev ? Object.assign(demoNodes.find(node => node.node_id === id)!, patch) : request<NodeRecord>(`/api/v1/admin/nodes/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify(patch) }),
  deleteNode: async (id: string) => dev ? demoNodes.splice(demoNodes.findIndex(node => node.node_id === id), 1) : request<void>(`/api/v1/admin/nodes/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  disconnect: async (id: string) => dev ? Object.assign(demoSessions.find(session => session.session_id === id)!, { status: 'CLOSED' }) : request(`/api/v1/admin/sessions/${id}/disconnect`, { method: 'POST' }),
  updateNetwork: async (network: NetworkConfig) => {
    if (dev) {
      const { rotate_join_token: rotate, join_token: _, ...input } = network
      demoNetwork = { ...input, config_version: network.config_version + 1, join_token: rotate ? 'demo-join-token-after-rotation' : demoNetwork.join_token }
      return structuredClone(demoNetwork)
    }
    const { config_version: _, uptime_seconds: __, join_token: ___, ...input } = network
    return request<NetworkConfig>('/api/v1/admin/network', { method: 'PUT', body: JSON.stringify(input) })
  },
}
