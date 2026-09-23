<script setup lang="ts">
import { computed } from 'vue'
import type { NetworkConfig } from '../types'
const props = defineProps<{ modelValue: NetworkConfig; busy?: boolean; saved?: boolean; full?: boolean }>()
defineEmits<{ 'update:modelValue': [value: NetworkConfig]; save: []; rotate: [] }>()
const form = computed(() => props.modelValue)
</script>

<template>
  <section class="panel network-form" :class="{ full }">
    <div class="section-title"><div><h2>网络运行状况 & 配置</h2><p>配置版本 {{ form.config_version }}</p></div></div>
    <div class="health-list"><span>Overlay 网络 <b><i class="online"></i>健康</b></span><span>节点连通性 <b><i class="online"></i>正常</b></span><span>会话状态 <b><i class="online"></i>正常</b></span></div>
    <div class="fields"><label>Overlay CIDR<input v-model="form.overlay_cidr" /></label><label>Server Overlay IP<input v-model="form.server_overlay_ip" /></label><label>WireGuard Port<input v-model.number="form.wireguard_port" type="number" /></label><label>Session UDP Port<input v-model.number="form.session_udp_port" type="number" /></label><label>MTU<input v-model.number="form.mtu" type="number" /></label></div>
    <button class="primary save" :disabled="busy" @click="$emit('save')">{{ saved ? '已保存' : '保存网络配置' }}</button>
    <div v-if="full" class="join-token-result"><strong>当前 Join Token</strong><input :value="form.join_token || '读取中…'" readonly aria-label="当前 Join Token" /><small>仅用于新设备首次注册；普通保存和部署不会轮换。</small></div>
    <button v-if="full" class="secondary-action" :disabled="busy" @click="$emit('rotate')">轮换 Join Token</button>
    <p class="form-note">Overlay 变更将关闭现有会话并要求在线节点重新 Bootstrap。</p>
  </section>
</template>
