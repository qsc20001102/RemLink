export interface NodeRecord { node_id: string; type: 'engineer' | 'site'; name: string; overlay_ip: string; wg_public_key: string; wg_handshake?: string; status: 'ONLINE' | 'UNSTABLE' | 'OFFLINE'; version: string; os_version: string; last_seen?: string }
export interface Counters { upload_bytes: number; download_bytes: number; upload_packets: number; download_packets: number }
export interface SessionRecord { session_id: string; engineer_node_id: string; site_node_id: string; status: string; cidrs: string[]; created_at: string; active_at?: string; closed_at?: string; error_code?: string; counters: Counters }
export interface NetworkConfig { overlay_cidr: string; server_overlay_ip: string; wireguard_port: number; session_udp_port: number; mtu: number; config_version: number; uptime_seconds?: number; rotate_join_token?: boolean; join_token?: string }
export interface EventRecord { id: number; time: string; level: string; module: string; node_id?: string; session_id?: string; message: string; fields: Record<string, unknown> }
export interface LogFilter { level?: string; module?: string; node_id?: string; session_id?: string; from?: string; to?: string; limit?: number }
