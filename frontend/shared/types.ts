export interface ClientSettings {
  serverURL: string
  nodeName: string
  joinToken: string
  netstack: { tcpFlowLimit: number; udpFlowLimit: number; udpIdleSeconds: number }
}
export interface NativeDesktop {
  GetSettings(): Promise<ClientSettings>
  SaveSettings(value: ClientSettings): Promise<void>
  RenameNode(name: string): Promise<void>
  ReconnectServer(): Promise<void>
  HideToTray(): Promise<void>
  QuitApp(): Promise<void>
}
