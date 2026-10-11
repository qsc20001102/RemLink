/// <reference types="vite/client" />

import type { EngineerState } from './types'
import type { NativeDesktop } from '../../shared/types'
import type { SiteState } from '../../site/src/types'

declare global {
  interface Window {
    go?: {
      main?: {
        SiteApp?: NativeDesktop & { GetState(): Promise<SiteState> }
        EngineerApp?: NativeDesktop & {
          OpenConfigDirectory(): Promise<void>
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
