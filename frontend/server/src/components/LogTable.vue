<script setup lang="ts">
import type { EventRecord } from '../types'
import { levelLabel, logMessageLabel, moduleLabel } from '../zh-cn'
defineProps<{ logs: EventRecord[]; bare?: boolean }>()
function formatTime(value?: string) { return value ? new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' }).format(new Date(value)) : '—' }
</script>

<template>
  <section :class="bare ? '' : 'panel table-panel'">
    <div v-if="!bare" class="section-title"><h2>最近事件日志</h2><span>按时间倒序</span></div>
    <div class="table-wrap"><table>
      <thead><tr><th>时间</th><th>级别</th><th>模块</th><th>Node</th><th>Session</th><th>事件</th></tr></thead>
      <tbody><tr v-for="log in logs" :key="log.id"><td>{{ formatTime(log.time) }}</td><td><span class="log-level" :class="log.level.toLowerCase()">{{ levelLabel(log.level) }}</span></td><td>{{ moduleLabel(log.module) }}</td><td>{{ log.node_id || '—' }}</td><td class="mono">{{ log.session_id || '—' }}</td><td>{{ logMessageLabel(log.message) }}</td></tr><tr v-if="!logs.length"><td colspan="6" class="empty">没有匹配日志</td></tr></tbody>
    </table></div>
  </section>
</template>
