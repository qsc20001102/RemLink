<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import type { NodeRecord, SessionRecord } from '../types'
import Icon from '../Icon.vue'

const props = defineProps<{ nodes: NodeRecord[]; sessions: SessionRecord[]; serverIP: string }>()
type PlacedNode = { node: NodeRecord; x: number; y: number; size: number }
const hoveredId = ref('')
const canvas = ref<HTMLElement | null>(null)
const canvasWidth = ref(1000)
const canvasHeight = ref(500)
let observer: ResizeObserver | undefined
onMounted(() => {
  observer = new ResizeObserver(entries => {
    const rect = entries[0]?.contentRect
    if (rect) { canvasWidth.value = rect.width; canvasHeight.value = rect.height }
  })
  if (canvas.value) observer.observe(canvas.value)
})
onBeforeUnmount(() => observer?.disconnect())
const engineers = computed(() => props.nodes.filter(node => node.type === 'engineer'))
const sites = computed(() => props.nodes.filter(node => node.type === 'site'))
const activeSessions = computed(() => props.sessions.filter(session => session.status === 'ACTIVE'))
const activeCounts = computed(() => {
  const counts = new Map<string, number>()
  for (const session of activeSessions.value) {
    counts.set(session.engineer_node_id, (counts.get(session.engineer_node_id) || 0) + 1)
    counts.set(session.site_node_id, (counts.get(session.site_node_id) || 0) + 1)
  }
  return counts
})
const relatedIds = computed(() => {
  const ids = new Set<string>()
  for (const session of activeSessions.value) {
    if (session.engineer_node_id === hoveredId.value) ids.add(session.site_node_id)
    if (session.site_node_id === hoveredId.value) ids.add(session.engineer_node_id)
  }
  return ids
})
const focusedNode = computed(() => props.nodes.find(node => node.node_id === hoveredId.value))
const dense = computed(() => Math.max(engineers.value.length, sites.value.length) > 14)

function place(nodes: NodeRecord[], side: 'engineer' | 'site'): PlacedNode[] {
  const count = nodes.length
  if (!count) return []
  const columns = Math.max(1, Math.ceil(count / 9))
  const rows = Math.ceil(count / columns)
  const verticalGap = rows > 1 ? canvasHeight.value * 0.63 / (rows - 1) : 54
  const horizontalGap = columns > 1 ? canvasWidth.value * (side === 'engineer' ? 0.26 : 0.265) / (columns - 1) : 54
  const size = Math.max(8, Math.min(43, verticalGap * 0.82, horizontalGap * 0.8))
  const left = side === 'engineer' ? 6.5 : 67
  const span = side === 'engineer' ? 26 : 26.5
  return nodes.map((node, index) => {
    const column = Math.floor(index / rows)
    const row = index % rows
    const rowsInColumn = Math.min(rows, count - column * rows)
    const x = left + (columns === 1 ? span * 0.5 : column * span / (columns - 1))
    const y = rowsInColumn === 1 ? 51 : 20 + row * 63 / (rowsInColumn - 1)
    return { node, x, y, size }
  })
}

const placed = computed(() => [...place(engineers.value, 'engineer'), ...place(sites.value, 'site')])
function path(point: PlacedNode) {
  const x = point.x * 10
  const y = point.y * 5
  const first = point.node.type === 'engineer' ? x + 135 : x - 135
  const second = point.node.type === 'engineer' ? 395 : 605
  return ['M', x, y, 'C', first, y, second, 250, 500, 250].join(' ')
}
function activeCount(node: NodeRecord) {
  return activeCounts.value.get(node.node_id) || 0
}
function related(node: NodeRecord) {
  return relatedIds.value.has(node.node_id)
}
function statusLabel(status: NodeRecord['status']) {
  return status === 'ONLINE' ? '在线' : status === 'UNSTABLE' ? '不稳定' : '离线'
}
function formatTime(value?: string) {
  return value ? new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' }).format(new Date(value)) : '暂无记录'
}
</script>

<template>
  <section class="topology panel">
    <div class="topology-header">
      <div><h2>网络拓扑</h2><p>实时展示全部设备、接入状态与当前会话</p></div>
      <div class="topology-meta">
        <span><Icon name="engineer" />Engineer <b>{{ engineers.length }}</b></span>
        <span><Icon name="site" />Site <b>{{ sites.length }}</b></span>
        <span class="topology-legend"><i class="online"></i>在线 <i class="unstable"></i>不稳定 <i class="offline"></i>离线</span>
      </div>
    </div>
    <div ref="canvas" class="topology-canvas" :class="{ dense, crowded: nodes.length > 80 }" aria-label="展示全部 Engineer 和 Site 设备的实时网络拓扑">
      <div class="topology-grid" aria-hidden="true"></div>
      <div class="topology-side-label engineer-label">ENGINEER <strong>{{ engineers.length }}</strong></div>
      <div class="topology-side-label site-label">SITE <strong>{{ sites.length }}</strong></div>
      <svg class="topology-links" viewBox="0 0 1000 500" preserveAspectRatio="none" aria-hidden="true">
        <g v-for="point in placed" :key="point.node.node_id" :class="['link-group', point.node.status.toLowerCase(), point.node.type, { selected: point.node.node_id === hoveredId, related: related(point.node), engaged: activeCount(point.node) > 0 }]">
          <path class="link-halo" :d="path(point)" />
          <path class="link-track" :d="path(point)" />
          <path v-if="point.node.status !== 'OFFLINE'" class="link-flow" :d="path(point)" />
        </g>
      </svg>
      <div class="topology-core">
        <div class="core-orbit orbit-outer"></div><div class="core-orbit orbit-middle"></div><div class="core-orbit orbit-inner"></div>
        <div class="server-card"><span class="server-icon"><Icon name="server" /></span><b>RemLink Server</b><small>{{ serverIP || 'Overlay 地址待配置' }}</small><span class="server-health"><i></i>中心节点在线</span></div>
      </div>
      <button v-for="point in placed" :key="point.node.node_id" type="button" class="topology-node"
        :class="[point.node.type, point.node.status.toLowerCase(), { focused: point.node.node_id === hoveredId, related: related(point.node), engaged: activeCount(point.node) > 0 }]"
        :style="{ left: point.x + '%', top: point.y + '%', '--node-size': point.size + 'px' }"
        :aria-label="point.node.name + '，' + statusLabel(point.node.status) + '，' + point.node.overlay_ip"
        @mouseenter="hoveredId=point.node.node_id" @mouseleave="hoveredId=''"
        @focus="hoveredId=point.node.node_id" @blur="hoveredId=''">
        <span class="topology-node-shape"><Icon :name="point.node.type" /></span>
        <span class="topology-node-name">{{ point.node.name }}</span>
      </button>
      <div v-if="focusedNode" class="topology-inspector" aria-live="polite">
        <div class="inspector-title"><span class="inspector-role"><Icon :name="focusedNode.type" /></span><div><b>{{ focusedNode.name }}</b><small>{{ focusedNode.type === 'engineer' ? 'Engineer' : 'Site' }} · {{ focusedNode.overlay_ip || '地址待分配' }}</small></div><span class="inspector-status" :class="focusedNode.status.toLowerCase()">{{ statusLabel(focusedNode.status) }}</span></div>
        <dl><div><dt>当前连接</dt><dd>{{ focusedNode.status === 'OFFLINE' ? '设备离线' : activeCount(focusedNode) ? activeCount(focusedNode) + ' 条实时会话' : '已接入 · 暂无会话' }}</dd></div><div><dt>WG 最近握手</dt><dd>{{ formatTime(focusedNode.wg_handshake) }}</dd></div><div><dt>最后在线</dt><dd>{{ formatTime(focusedNode.last_seen) }}</dd></div><div><dt>客户端版本</dt><dd>{{ focusedNode.version || '—' }}</dd></div></dl>
      </div>
      <p v-if="!nodes.length" class="topology-empty">暂无注册设备</p>
    </div>
    <div class="topology-footer"><span>OVERLAY NETWORK</span><strong>{{ serverIP || '—' }}</strong><span class="topology-footer-line"></span><span>{{ activeSessions.length }} 条实时会话 · {{ nodes.length }} 台设备</span></div>
  </section>
</template>
