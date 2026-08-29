export type NodeStatus = 'ONLINE' | 'UNSTABLE' | 'OFFLINE'
export type SessionStatus = 'IDLE' | 'CREATING' | 'PREPARING_SITE' | 'READY' | 'ACTIVE' | 'STOPPING' | 'CLOSED' | 'FAILED'

export interface SiteSummary {
  node_id: string
  name: string
  overlay_ip: string
  online: boolean
  remote_subnet_capability: boolean
  last_seen?: string
}

export interface EngineerState {
  serverConnected: boolean
  controlConnected: boolean
  serverURL: string
  version: string
  overlayIP: string
  sites: SiteSummary[]
  siteCIDRs: Record<string, string[]>
  session: {
    id: string
    siteName: string
    cidrs: string[]
    status: SessionStatus
    uploadBytes: number
    downloadBytes: number
    uploadPackets: number
    downloadPackets: number
    latencyMS: number
    startedAt?: string
    reason?: string
  }
  logs: Array<{ time: string; level: 'INFO' | 'WARN' | 'ERROR'; message: string }>
}
