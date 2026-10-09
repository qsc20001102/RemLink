<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import type { EngineerState } from '../types'
import { formatTime } from '../format'
import { levelLabel } from '../zh-cn'

const props = defineProps<{ logs: EngineerState['logs']; expanded: boolean; visible: boolean; docked?: boolean }>()
defineEmits<{ toggle: [] }>()
const level = ref('')
const query = ref('')
const follow = ref(true)
const viewport = ref<HTMLElement>()
const latest = computed(() => props.logs.at(-1))
const titleID = computed(() => props.docked ? 'live-logs-title' : 'logs-title')
const listID = computed(() => props.docked ? 'live-logs-list' : 'logs-list')
const filtered = computed(() => props.logs.filter(log => (!level.value || log.level === level.value) && log.message.toLocaleLowerCase().includes(query.value.trim().toLocaleLowerCase())))
watch([filtered, follow, () => props.visible, () => props.expanded], async () => {
  await nextTick()
  if (follow.value && props.visible && viewport.value) viewport.value.scrollTop = viewport.value.scrollHeight
}, { immediate: true })
</script>

<template>
  <section class="panel logs-panel" :class="{ expanded }" :aria-labelledby="titleID">
    <div class="panel-heading">
      <div class="heading-group"><h2 :id="titleID">{{ docked ? '实时日志' : '运行日志' }}</h2><span class="muted">{{ logs.length }} 条</span></div>
      <div v-if="!expanded" class="latest-log" role="status">
        <template v-if="latest"><time :datetime="latest.time" :title="latest.time">{{ formatTime(latest.time) }}</time><span class="log-level" :class="latest.level.toLowerCase()">{{ levelLabel(latest.level) }}</span><span class="latest-log-message" :title="latest.message">{{ latest.message }}</span></template>
        <span v-else class="muted">暂无日志，等待新的运行事件。</span>
      </div>
      <div class="log-actions">
        <label v-if="expanded" class="follow-toggle"><input v-model="follow" type="checkbox" />跟随最新</label>
        <button v-if="docked" type="button" class="text-action" :aria-expanded="expanded" :aria-controls="listID" @click="$emit('toggle')">{{ expanded ? '折叠' : '展开' }}<span aria-hidden="true">{{ expanded ? '▾' : '▴' }}</span></button>
      </div>
    </div>
    <div v-if="expanded" :id="listID" class="log-detail">
      <div class="log-filters">
        <input v-model="query" type="search" placeholder="搜索日志内容" aria-label="搜索日志内容" />
        <select v-model="level" aria-label="日志级别"><option value="">全部级别</option><option value="INFO">信息</option><option value="WARN">警告</option><option value="ERROR">错误</option></select>
        <span class="muted">{{ filtered.length }} 条匹配</span>
      </div>
      <div ref="viewport" class="logs-scroll" tabindex="0" aria-label="日志列表">
        <table class="log-table">
          <thead><tr><th scope="col">时间</th><th scope="col">级别</th><th scope="col">内容</th></tr></thead>
          <tbody><tr v-for="(log, index) in filtered" :key="index"><td><time :datetime="log.time" :title="log.time">{{ formatTime(log.time) }}</time></td><td><span class="log-level" :class="log.level.toLowerCase()">{{ levelLabel(log.level) }}</span></td><td>{{ log.message }}</td></tr></tbody>
        </table>
        <p v-if="!filtered.length" class="empty-state">{{ logs.length ? '没有匹配的日志，请调整筛选条件。' : '暂无日志，新的运行事件将在这里显示。' }}</p>
      </div>
    </div>
  </section>
</template>
