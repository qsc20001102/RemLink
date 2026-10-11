<script setup lang="ts">
import type { EventRecord, NodeRecord } from '../types'
import { levelLabel } from '../zh-cn'
import { eventDevices, eventMessage } from '../event-label'
withDefaults(defineProps<{ logs: EventRecord[]; nodes?: NodeRecord[]; bare?: boolean }>(), { nodes: () => [] })
function formatTime(value?: string) { return value ? new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' }).format(new Date(value)) : '—' }
</script>

<template>
  <section :class="bare ? '' : 'panel table-panel'">
    <div v-if="!bare" class="section-title"><h2>最近事件日志</h2><span>按时间倒序</span></div>
    <div class="table-wrap"><table>
      <thead><tr><th>时间</th><th>级别</th><th>设备</th><th>发生的事件</th></tr></thead>
      <tbody><tr v-for="log in logs" :key="log.id"><td>{{ formatTime(log.time) }}</td><td><span class="log-level" :class="log.level.toLowerCase()">{{ levelLabel(log.level).replace(/（[^）]+）$/, '') }}</span></td><td :title="log.node_id">{{ eventDevices(log, nodes) }}</td><td class="event-message" :title="`原始记录：${log.message}\n节点 ID：${log.node_id || '—'}\n会话 ID：${log.session_id || '—'}`">{{ eventMessage(log, nodes) }}</td></tr><tr v-if="!logs.length"><td colspan="4" class="empty">没有匹配日志</td></tr></tbody>
    </table></div>
  </section>
</template>
