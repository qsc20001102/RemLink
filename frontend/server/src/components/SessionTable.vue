<script setup lang="ts">
import type { NodeRecord, SessionRecord } from '../types'
import StatusDot from './StatusDot.vue'
import { reasonLabel } from '../zh-cn'

const props = defineProps<{ sessions: SessionRecord[]; nodes: NodeRecord[]; title?: string; emptyLabel?: string; history?: boolean }>()
defineEmits<{ disconnect: [session: SessionRecord] }>()
function formatBytes(value: number) { return value < 1024 ** 2 ? `${(value / 1024).toFixed(1)} KB` : `${(value / 1024 ** 2).toFixed(2)} MB` }
function nodeName(id: string) { return props.nodes.find(node => node.node_id === id)?.name ?? id }
function formatTime(value?: string) { return value ? new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }).format(new Date(value)) : '—' }
function duration(session: SessionRecord) {
  const start = Date.parse(session.active_at || session.created_at), end = session.closed_at ? Date.parse(session.closed_at) : Date.now()
  const seconds = Math.max(0, Math.floor((end - start) / 1000)), hours = Math.floor(seconds / 3600), minutes = Math.floor(seconds % 3600 / 60)
  return hours ? `${hours}小时 ${minutes}分` : `${minutes}分 ${seconds % 60}秒`
}
</script>

<template>
  <section class="panel table-panel" :class="{ 'history-panel': history }">
    <div class="section-title"><div class="session-heading"><h2>{{ title || '会话' }}</h2><span>共 {{ sessions.length }} 条</span></div><slot name="filters" /></div>
    <div class="table-wrap"><table>
      <thead><tr><th>会话 ID</th><th>Engineer</th><th>Site</th><th>远程网段</th><th>状态</th><th>上传 / 下载</th><th>持续时间</th><th>{{ history ? '结束时间' : '操作' }}</th></tr></thead>
      <tbody><tr v-for="session in sessions" :key="session.session_id">
        <td class="mono">{{ String(session.session_id).slice(0, 14) }}</td><td>{{ nodeName(session.engineer_node_id) }}</td><td>{{ nodeName(session.site_node_id) }}</td><td class="mono">{{ session.cidrs.join(', ') }}</td>
        <td><StatusDot :status="session.status" /><small v-if="session.error_code" class="red">{{ reasonLabel(session.error_code) }}</small></td><td>{{ formatBytes(session.counters.upload_bytes) }} / {{ formatBytes(session.counters.download_bytes) }}</td><td>{{ duration(session) }}</td>
        <td v-if="history">{{ formatTime(session.closed_at) }}</td><td v-else><button class="disconnect" :disabled="session.status !== 'ACTIVE'" @click="$emit('disconnect', session)">强制断开</button></td>
      </tr><tr v-if="!sessions.length"><td colspan="8" class="empty">{{ emptyLabel || '当前没有会话' }}</td></tr></tbody>
    </table></div>
  </section>
</template>
