<script setup lang="ts">
import Icon from '../Icon.vue'
import type { NodeRecord } from '../types'
import StatusDot from './StatusDot.vue'

withDefaults(defineProps<{ nodes: NodeRecord[]; title?: string; emptyLabel?: string }>(), { title: '节点状态', emptyLabel: '尚无注册节点' })
defineEmits<{ edit: [node: NodeRecord]; revoke: [node: NodeRecord] }>()
function formatTime(value?: string) {
  return value ? new Intl.DateTimeFormat('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' }).format(new Date(value)) : '—'
}
</script>

<template>
  <section class="panel table-panel">
    <div class="section-title"><h2>{{ title }}</h2><span>共 {{ nodes.length }} 个节点</span></div>
    <div class="table-wrap"><table>
      <thead><tr><th>名称 / WG 公钥</th><th>类型</th><th>Overlay IP</th><th>应用状态</th><th>WG 最近握手</th><th>版本</th><th>最后在线</th><th>操作</th></tr></thead>
      <tbody><tr v-for="node in nodes" :key="node.node_id">
        <td><b>{{ node.name }}</b><small>{{ node.wg_public_key.slice(0, 16) }}…</small></td>
        <td><span class="node-type"><Icon :name="node.type" />{{ node.type === 'engineer' ? 'Engineer' : 'Site' }}</span></td><td class="mono">{{ node.overlay_ip }}</td>
        <td><StatusDot :status="node.status" /></td><td>{{ formatTime(node.wg_handshake) }}</td><td>{{ node.version || '—' }}</td><td>{{ formatTime(node.last_seen) }}</td>
        <td><button class="icon-button" aria-label="编辑节点" @click="$emit('edit', node)"><Icon name="edit" /></button><button class="icon-button destructive" aria-label="撤销节点" @click="$emit('revoke', node)"><Icon name="trash" /></button></td>
      </tr><tr v-if="!nodes.length"><td colspan="8" class="empty">{{ emptyLabel }}</td></tr></tbody>
    </table></div>
  </section>
</template>
