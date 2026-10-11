import type { ClientSettings } from '../../shared/types'
import type { SiteState } from './types'
const dev = import.meta.env.DEV
const native = () => window.go?.main?.SiteApp
function unavailable(): never { throw new Error('RemLink native runtime is unavailable; production Demo fallback is disabled') }
let demoSettings: ClientSettings = { serverURL: '', nodeName: '', joinToken: '', netstack: { tcpFlowLimit: 2048, udpFlowLimit: 4096, udpIdleSeconds: 60 } }
if (dev) { try { demoSettings = JSON.parse(localStorage.getItem('remlink-site-demo-settings') || 'null') || demoSettings } catch { /* Start with empty development settings. */ } }
const demoState: SiteState = { configured: !!demoSettings.serverURL, connecting: false, serverConnected: !!demoSettings.serverURL, controlConnected: !!demoSettings.serverURL, serverURL: demoSettings.serverURL, nodeName: demoSettings.nodeName, overlayIP: demoSettings.serverURL ? '10.88.0.25' : '', connectionError: '', version: '1.1.0', gatewayReady: !!demoSettings.serverURL, sessions: [], logs: [] }
export async function getState(): Promise<SiteState> { const app = native(); if (app) { const state = await app.GetState(); return { ...state, sessions: state.sessions ?? [], logs: state.logs ?? [] } }; if (!dev) return unavailable(); return structuredClone(demoState) }
export async function getSettings(): Promise<ClientSettings> { const app = native(); if (app) return app.GetSettings(); if (!dev) return unavailable(); return structuredClone(demoSettings) }
export async function saveSettings(value: ClientSettings): Promise<void> { const app = native(); if (app) return app.SaveSettings(value); if (!dev) return unavailable(); demoSettings = structuredClone(value); localStorage.setItem('remlink-site-demo-settings', JSON.stringify(value)); Object.assign(demoState, { configured: true, serverConnected: true, controlConnected: true, gatewayReady: true, serverURL: value.serverURL, nodeName: value.nodeName, overlayIP: '10.88.0.25' }) }
export async function reconnectServer(): Promise<void> { const app = native(); if (app) return app.ReconnectServer(); if (!dev) return unavailable(); Object.assign(demoState, { serverConnected: true, controlConnected: true, gatewayReady: true }) }
export async function hideToTray(): Promise<void> { const app = native(); if (app) return app.HideToTray(); if (!dev) return unavailable() }
export async function quitApp(): Promise<void> { const app = native(); if (app) return app.QuitApp(); if (!dev) return unavailable() }
