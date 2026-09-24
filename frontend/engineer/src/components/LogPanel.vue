<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import type { EngineerState } from '../types'
import { formatTime } from '../format'
import { levelLabel } from '../zh-cn'

const props = defineProps<{ logs: EngineerState['logs']; expanded: boolean; visible: boolean }>()
defineEmits<{ expand: [] }>()
const level = ref('')
const query = ref('')
const follow = ref(true)
const viewport = ref<HTMLElement>()
const filtered = computed(() => props.logs.filter(log => (!level.value || log.level === level.value) && log.message.toLocaleLowerCase().includes(query.value.trim().toLocaleLowerCase())))
const displayed = computed(() => props.expanded ? filtered.value : props.logs.slice(-4))
watch([displayed, follow, () => props.visible, () => props.expanded], async () => {
  await nextTick()
  if (follow.value && props.visible && viewport.value) viewport.value.scrollTop = viewport.value.scrollHeight
}, { immediate: true })
</script>

<template>
  <section class="panel logs-panel" :class="{ expanded }" aria-labelledby="logs-title">
    <div class="panel-heading">
      <div class="heading-group"><h2 id="logs-title">{{ expanded ? '运行日志' : '实时日志' }}</h2><span class="muted">{{ logs.length }} 条</span></div>
      <label v-if="expanded" class="follow-toggle"><input v-model="follow" type="checkbox" />跟随最新</label>
      <button v-else class="text-action" @click="$emit('expand')">查看全部 <span aria-hidden="true">→</span></button>
    </div>
    <div v-if="expanded" class="log-filters">
      <input v-model="query" type="search" placeholder="搜索日志内容" aria-label="搜索日志内容" />
      <select v-model="level" aria-label="日志级别"><option value="">全部级别</option><option value="INFO">信息</option><option value="WARN">警告</option><option value="ERROR">错误</option></select>
      <span class="muted">{{ filtered.length }} 条匹配</span>
    </div>
    <div ref="viewport" class="logs-scroll" tabindex="0" aria-label="日志列表">
      <table class="log-table">
        <thead><tr><th scope="col">时间</th><th scope="col">级别</th><th scope="col">内容</th></tr></thead>
        <tbody><tr v-for="(log, index) in displayed" :key="index"><td><time :datetime="log.time" :title="log.time">{{ formatTime(log.time) }}</time></td><td><span class="log-level" :class="log.level.toLowerCase()">{{ levelLabel(log.level) }}</span></td><td>{{ log.message }}</td></tr></tbody>
      </table>
      <p v-if="!displayed.length" class="empty-state">{{ logs.length ? '没有匹配的日志，请调整筛选条件。' : '暂无日志，新的运行事件将在这里显示。' }}</p>
    </div>
  </section>
</template>
