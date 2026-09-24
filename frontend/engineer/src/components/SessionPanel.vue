<script setup lang="ts">
import { computed } from 'vue'
import type { EngineerState } from '../types'
import { formatBytes, formatTime } from '../format'
import { errorLabel, statusLabel } from '../zh-cn'

const props = defineProps<{ session: EngineerState['session']; busy: boolean }>()
defineEmits<{ disconnect: [] }>()
const tone = computed(() => props.session.status === 'ACTIVE' ? 'success' : props.session.status === 'FAILED' ? 'error' : ['IDLE', 'CLOSED'].includes(props.session.status) ? 'neutral' : 'pending')
</script>

<template>
  <section class="panel session-panel" aria-labelledby="session-title">
    <div class="panel-heading">
      <div class="heading-group"><h2 id="session-title">会话详情</h2><span class="badge" :class="tone">{{ statusLabel(session.status) }}</span></div>
      <button class="danger" :disabled="session.status !== 'ACTIVE' || busy" @click="$emit('disconnect')">断开会话</button>
    </div>
    <div class="session-body">
      <dl class="session-details">
        <dt>现场端</dt><dd>{{ session.siteName || '尚未建立会话' }}</dd>
        <dt>远程网段</dt><dd class="mono">{{ session.cidrs.join(', ') || '—' }}</dd>
        <dt>会话 ID</dt><dd class="mono">{{ session.id || '—' }}</dd>
        <dt>开始时间</dt><dd>{{ formatTime(session.startedAt) }}</dd>
        <dt>结束原因</dt><dd :class="{ 'error-text': session.reason }">{{ session.reason ? errorLabel(session.reason) : '—' }}</dd>
      </dl>
      <div class="session-metrics">
        <div class="metric"><span>上传</span><strong>{{ formatBytes(session.uploadBytes) }}</strong><small>{{ session.uploadPackets.toLocaleString() }} 个数据包</small></div>
        <div class="metric"><span>下载</span><strong>{{ formatBytes(session.downloadBytes) }}</strong><small>{{ session.downloadPackets.toLocaleString() }} 个数据包</small></div>
        <div class="metric"><span>时延</span><strong>{{ session.latencyMS || '—' }}<small v-if="session.latencyMS"> ms</small></strong><small>往返时延</small></div>
      </div>
    </div>
  </section>
</template>
