/// <reference types="vite/client" />

import type { EngineerState } from './types'

declare global {
  interface Window {
    go?: {
      main?: {
        EngineerApp?: {
          GetState(): Promise<EngineerState>
          CreateSession(siteNodeID: string, cidrs: string[]): Promise<string>
          SaveSiteCIDRs(siteNodeID: string, cidrs: string[]): Promise<void>
          DisconnectSession(): Promise<void>
          CheckCIDRs(cidrs: string[]): Promise<void>
        }
      }
    }
    runtime?: {
      EventsOn(name: string, callback: (payload: unknown) => void): () => void
    }
  }
}
