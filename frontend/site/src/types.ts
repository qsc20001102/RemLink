export interface SiteState {
  configured: boolean; connecting: boolean; serverConnected: boolean; controlConnected: boolean
  serverURL: string; nodeName: string; overlayIP: string; connectionError: string; version: string; gatewayReady: boolean
  sessions: Array<{ id: string; status: string; reason: string }>
  logs: Array<{ time: string; level: 'INFO' | 'WARN' | 'ERROR'; message: string }>
}
