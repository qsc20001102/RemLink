import type { EngineerState } from './types'

const dev = import.meta.env.DEV
const demoState: EngineerState = {
  serverConnected: true,
  controlConnected: true,
  serverURL: 'https://remlink.example.com',
  version: '1.0.0',
  overlayIP: '10.88.0.2',
  sites: [
    { node_id: 'site-qingdao', name: '青岛现场 01', overlay_ip: '10.88.0.25', online: true, remote_subnet_capability: true, last_seen: new Date().toISOString() },
    { node_id: 'site-shanghai', name: '上海实验室', overlay_ip: '10.88.0.31', online: false, remote_subnet_capability: true },
  ],
  siteCIDRs: {
    'site-qingdao': ['192.168.17.0/24'],
    'site-shanghai': ['192.168.107.0/24'],
  },
  session: {
    id: '4488624737516445881', siteName: '青岛现场 01', cidrs: ['192.168.17.0/24'], status: 'ACTIVE',
    uploadBytes: 1321205, downloadBytes: 2940838, uploadPackets: 12345, downloadPackets: 15402, latencyMS: 28,
    startedAt: new Date(Date.now() - 18 * 60_000).toISOString(),
  },
  logs: [
    { time: new Date(Date.now() - 50_000).toISOString(), level: 'INFO', message: 'Overlay 隧道已建立，连接路径正常' },
    { time: new Date(Date.now() - 34_000).toISOString(), level: 'INFO', message: 'Remote CIDR 已生效：192.168.17.0/24' },
    { time: new Date(Date.now() - 12_000).toISOString(), level: 'INFO', message: '192.168.17.5：ICMP 回复，时延 28ms' },
  ],
}

const native = () => window.go?.main?.EngineerApp

function normalizeState(value: EngineerState): EngineerState {
  return {
    ...value,
    sites: value.sites ?? [],
    siteCIDRs: Object.fromEntries(Object.entries(value.siteCIDRs ?? {}).map(([siteID, cidrs]) => [siteID, Array.from(cidrs ?? [])])),
    logs: value.logs ?? [],
    session: { ...value.session, cidrs: value.session?.cidrs ?? [] },
  }
}

export async function saveSiteCIDRs(siteNodeID: string, cidrs: string[]): Promise<void> {
  const current = native()
  if (current) return current.SaveSiteCIDRs(siteNodeID, cidrs)
  if (!dev) return nativeUnavailable()
  if (cidrs.length === 0) delete demoState.siteCIDRs[siteNodeID]
  else demoState.siteCIDRs[siteNodeID] = Array.from(cidrs)
}

function nativeUnavailable(): never {
  throw new Error('RemLink native runtime is unavailable; production Demo fallback is disabled')
}

export async function getState(): Promise<EngineerState> {
  const current = native()
  if (current) return normalizeState(await current.GetState())
  if (!dev) return nativeUnavailable()
  return structuredClone(demoState)
}

export async function createSession(siteNodeID: string, cidrs: string[]): Promise<string> {
  const current = native()
  if (current) return current.CreateSession(siteNodeID, cidrs)
  if (!dev) return nativeUnavailable()
  demoState.session = { ...demoState.session, id: 'pending', siteName: demoState.sites.find(site => site.node_id === siteNodeID)?.name ?? '', cidrs: Array.from(cidrs), status: 'CREATING' }
  setTimeout(() => { demoState.session.status = 'ACTIVE'; demoState.session.id = '4488624737516445881' }, 450)
  return 'dev-request'
}

export async function disconnectSession(): Promise<void> {
  const current = native()
  if (current) return current.DisconnectSession()
  if (!dev) return nativeUnavailable()
  demoState.session.status = 'IDLE'
  demoState.session.id = ''
}

export async function checkCIDRs(cidrs: string[]): Promise<void> {
  const current = native()
  if (current) return current.CheckCIDRs(cidrs)
  if (!dev) return nativeUnavailable()
}
