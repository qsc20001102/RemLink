<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import Icon from '../../engineer/src/Icon.vue'
import LogPanel from '../../engineer/src/components/LogPanel.vue'
import SettingsForm from '../../shared/SettingsForm.vue'
import CloseDialog from '../../shared/CloseDialog.vue'
import { serializeRefresh } from '../../shared/state-refresh'
import siteMark from '../../branding/site.svg'
import { errorLabel, statusLabel } from '../../engineer/src/zh-cn'
import { getState, getSettings, saveSettings, reconnectServer, hideToTray, quitApp } from './api'
import type { SiteState } from './types'
import type { ClientSettings } from '../../shared/types'

const state = ref<SiteState | null>(null), settings = ref<ClientSettings | null>(null)
const page = ref<'connection' | 'settings'>('connection'), busy = ref(false), error = ref(''), closeDialog = ref(false), logsExpanded = ref(false)
let timer = 0, unsubscribe: (() => void) | undefined, unsubscribeClose: (() => void) | undefined
const refresh = serializeRefresh(getState, snapshot => { const first = state.value === null; state.value = snapshot; if (first && !snapshot.configured) page.value = 'settings' })
async function save(value: ClientSettings) { busy.value = true; error.value = ''; try { await saveSettings(value); settings.value = await getSettings(); await refresh(); if (state.value?.configured) page.value = 'connection' } catch (cause) { error.value = errorLabel(cause) } finally { busy.value = false } }
async function retry() { try { await reconnectServer(); await refresh() } catch (cause) { error.value = errorLabel(cause) } }
async function background() { try { await hideToTray(); closeDialog.value = false } catch (cause) { error.value = errorLabel(cause) } }
async function exitApp() { try { await quitApp() } catch (cause) { error.value = errorLabel(cause) } }
onMounted(async () => { unsubscribeClose = window.runtime?.EventsOn('remlink:close-request', () => { closeDialog.value = true }); try { await refresh(); settings.value = await getSettings(); const update = () => { void refresh().catch(cause => { error.value = errorLabel(cause) }) }; timer = window.setInterval(update, 2000); unsubscribe = window.runtime?.EventsOn('remlink:state', update) } catch (cause) { error.value = errorLabel(cause) } })
onBeforeUnmount(() => { clearInterval(timer); unsubscribe?.(); unsubscribeClose?.() })
</script>

<template>
  <div v-if="state" class="app-shell site-app" :class="{ 'logs-expanded': logsExpanded }">
    <aside class="sidebar"><div class="brand"><img class="app-mark" :src="siteMark" alt=""/><div>RemLink<small>Site 现场端</small></div></div>
      <nav aria-label="主导航"><button class="nav-item" :class="{ selected: page === 'connection' }" @click="page = 'connection'"><Icon name="link"/><span>连接</span></button><button class="nav-item" :class="{ selected: page === 'settings' }" @click="page = 'settings'"><Icon name="settings"/><span>设置</span></button></nav>
      <div class="sidebar-foot"><span class="version">RemLink Site v{{ state.version }}</span><span>现场远程接入</span></div></aside>
    <main><header class="status-strip"><div><Icon name="server"/><span>服务器</span><strong><i :class="state.serverConnected ? 'healthy' : 'offline'"></i>{{ state.serverConnected ? '已连接' : state.connecting ? '正在连接' : '未连接' }}</strong></div><div><Icon name="link"/><span>本机地址</span><strong class="mono">{{ state.overlayIP || '等待分配' }}</strong></div><div><Icon name="check"/><span>现场网络</span><strong><i :class="state.gatewayReady ? 'healthy' : 'offline'"></i>{{ state.gatewayReady ? '已就绪' : '未就绪' }}</strong></div></header>
      <div class="workspace"><div class="title-row"><h1>{{ page === 'settings' ? '现场端设置' : '现场连接' }}</h1><p>{{ page === 'settings' ? '设置服务器地址、现场名称和注册令牌。' : '查看服务器连接与现场远程接入状态。' }}</p></div>
        <div v-if="error" class="error-banner" role="alert">{{ error }}</div><div v-if="state.connectionError" class="connection-notice" role="alert"><span>{{ errorLabel(state.connectionError) }}</span><button class="secondary" @click="retry">重新连接</button></div>
        <SettingsForm v-if="page === 'settings' && settings" :value="settings" role="Site" :configured="state.configured" :busy="busy" @save="save"/>
        <template v-if="page === 'connection'">
          <div v-if="!state.configured" class="connection-notice"><span>请先完成现场端设置，再连接服务器。</span><button class="primary" @click="page = 'settings'">前往设置</button></div>
          <section class="connection-path"><div class="endpoint"><img :src="siteMark" alt=""/><div><b>{{ state.nodeName || '本机 Site' }}</b><small class="mono">{{ state.overlayIP || '等待分配地址' }}</small></div></div><div class="path-line" :class="{ connected: state.controlConnected }"><i></i></div><div class="endpoint server-endpoint"><Icon name="server"/><div><b>RemLink Server</b><small>{{ state.serverURL ? '已配置服务器' : '尚未配置服务器' }}</small></div></div><span class="path-status"><i :class="state.controlConnected ? 'healthy' : 'offline'"></i>{{ state.controlConnected ? '在线' : state.connecting ? '连接中' : '离线' }}</span></section>
          <section class="panel site-status-panel"><div class="panel-heading"><h2>连接信息</h2><button class="secondary" :disabled="!state.configured || state.connecting" @click="retry">重新连接</button></div><dl><dt>现场名称</dt><dd>{{ state.nodeName || '未设置' }}</dd><dt>Server 公网地址</dt><dd>{{ state.serverURL ? '••••••••' : '未设置' }}</dd><dt>控制通道</dt><dd>{{ state.controlConnected ? '已连接' : '未连接' }}</dd><dt>现场网络</dt><dd>{{ state.gatewayReady ? '已就绪，等待工程师接入' : '等待初始化' }}</dd><dt>当前接入</dt><dd>{{ state.sessions.length }} 个工程师连接</dd></dl></section>
          <section class="panel site-sessions"><div class="panel-heading"><h2>当前工程师连接</h2><span class="muted">{{ state.sessions.length }} 条</span></div><div v-if="!state.sessions.length" class="empty-state"><Icon name="monitor"/><b>暂时没有工程师接入</b><span>工程师建立远程连接后，会自动出现在这里。</span></div><table v-else class="site-table"><thead><tr><th>连接 ID</th><th>状态</th></tr></thead><tbody><tr v-for="session in state.sessions" :key="session.id"><td class="mono">{{ session.id }}</td><td>{{ statusLabel(session.status) }}</td></tr></tbody></table></section>
        </template>
      </div><div class="logs-dock"><LogPanel :logs="state.logs" :expanded="logsExpanded" :visible="true" :docked="true" @toggle="logsExpanded = !logsExpanded"/></div>
    </main>
  </div><div v-else class="loading" role="status"><img :src="siteMark" alt=""/><h1>RemLink Site</h1><p>{{ error || '正在启动现场端…' }}</p></div>
  <CloseDialog :open="closeDialog" @cancel="closeDialog = false" @tray="background" @quit="exitApp"/>
</template>
