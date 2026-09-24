<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Icon from './Icon.vue'
import SessionPanel from './components/SessionPanel.vue'
import LogPanel from './components/LogPanel.vue'
import { formatTime } from './format'
import engineerMark from '../../branding/engineer.svg'
import siteMark from '../../branding/site.svg'
import { checkCIDRs, createSession, disconnectSession, getState, saveSiteCIDRs } from './api'
import type { EngineerState, SiteSummary } from './types'
import { errorLabel, statusLabel } from './zh-cn'

const state = ref<EngineerState | null>(null)
const page = ref<'connection' | 'session' | 'logs' | 'settings'>('connection')
const selectedSiteID = ref('')
const cidrs = ref<string[]>([])
const cidrInput = ref('')
const busy = ref(false)
const error = ref('')
const siteQuery = ref('')
const editing = ref(false)
const preflightState = ref<'checking' | 'pass' | 'fail'>('checking')
const preflightMessage = ref('正在检查本地路由…')
let timer = 0
let unsubscribe: (() => void) | undefined
let preflightRevision = 0

const active = computed(() => state.value?.session.status === 'ACTIVE')
const sessionBusy = computed(() => {
  const status = state.value?.session.status
  return status === 'CREATING' || status === 'PREPARING_SITE' || status === 'READY' || status === 'ACTIVE' || status === 'STOPPING'
})
const selectedSite = computed<SiteSummary | undefined>(() => state.value?.sites.find(site => site.node_id === selectedSiteID.value))
const editLocked = computed(() => busy.value || editing.value || sessionBusy.value || !selectedSite.value)
const canConnect = computed(() => !busy.value && !editing.value && !sessionBusy.value && state.value?.serverConnected && state.value.controlConnected && preflightState.value === 'pass' && selectedSite.value?.online && selectedSite.value.remote_subnet_capability && cidrs.value.length > 0)
const onlineCount = computed(() => state.value?.sites.filter(site => site.online).length ?? 0)
const filteredSites = computed(() => {
  const query = siteQuery.value.trim().toLocaleLowerCase()
  return state.value?.sites.filter(site => `${site.name} ${site.overlay_ip} ${site.node_id}`.toLocaleLowerCase().includes(query)) ?? []
})
const pageCopy = computed(() => ({
  connection: ['远程连接', '选择现场并配置远程网段，安全访问现场设备。'],
  session: ['当前会话', '查看会话状态、远程网段与数据传输情况。'],
  logs: ['运行日志', '按级别和关键词查找客户端运行事件。'],
  settings: ['客户端设置', '查看本机运行配置与网络信息。'],
}[page.value]))
const connectHint = computed(() => {
  if (sessionBusy.value) return '会话进行中，断开后可修改现场和网段。'
  if (!state.value?.serverConnected || !state.value.controlConnected) return '等待服务器与控制通道连接后再建立会话。'
  if (!selectedSite.value) return '请先从左侧列表选择一个现场。'
  if (!selectedSite.value.online) return '当前现场离线，请选择在线现场。'
  if (!selectedSite.value.remote_subnet_capability) return '当前现场暂不支持远程网段访问。'
  if (!cidrs.value.length) return '请添加需要访问的远程网段。'
  if (preflightState.value !== 'pass') return '本地冲突预检通过后即可连接。'
  return '网段已就绪，可以建立远程会话。'
})

async function addCIDR() {
  if (editLocked.value) return
  const value = cidrInput.value.trim()
  if (!value || cidrs.value.includes(value)) return
  editing.value = true
  cidrs.value.push(value)
  cidrInput.value = ''
  try { if (await runPreflight()) await persistCurrentProfile() }
  finally { editing.value = false }
}
async function removeCIDR(value: string) {
  if (editLocked.value) return
  editing.value = true
  cidrs.value = cidrs.value.filter(item => item !== value)
  try {
    const passed = await runPreflight()
    if (passed || cidrs.value.length === 0) await persistCurrentProfile()
  } finally { editing.value = false }
}
async function runPreflight() {
  const revision = ++preflightRevision
  if (cidrs.value.length === 0) {
    preflightState.value = 'fail'; preflightMessage.value = '请为当前现场添加至少一个网段'
    return false
  }
  preflightState.value = 'checking'; preflightMessage.value = '正在检查本地路由…'
  try {
    await checkCIDRs(Array.from(cidrs.value))
    if (revision !== preflightRevision) return false
    preflightState.value = 'pass'; preflightMessage.value = '通过，无冲突'; return true
  } catch (cause) {
    if (revision === preflightRevision) { preflightState.value = 'fail'; preflightMessage.value = errorLabel(cause) }
    return false
  }
}
async function persistCurrentProfile() {
  const siteID = selectedSiteID.value
  const profile = Array.from(cidrs.value)
  if (!siteID || !state.value) return
  try {
    await saveSiteCIDRs(siteID, profile)
    if (profile.length === 0) delete state.value.siteCIDRs[siteID]
    else state.value.siteCIDRs[siteID] = profile
  } catch (cause) {
    error.value = errorLabel(cause)
  }
}
function siteCIDRCount(siteID: string) { return state.value?.siteCIDRs[siteID]?.length ?? 0 }
async function loadSelectedSiteProfile() {
  if (sessionBusy.value) return
  cidrInput.value = ''
  cidrs.value = Array.from(state.value?.siteCIDRs[selectedSiteID.value] ?? [])
  await runPreflight()
}
async function refresh() {
  state.value = await getState()
  if (['CREATING', 'PREPARING_SITE', 'READY', 'ACTIVE', 'STOPPING'].includes(state.value.session.status) && state.value.session.cidrs.length > 0) {
    cidrs.value = Array.from(state.value.session.cidrs)
  }
  if (!selectedSiteID.value || (!sessionBusy.value && !selectedSite.value)) {
    selectedSiteID.value = state.value.sites.find(site => site.online)?.node_id ?? ''
  }
}
async function connect() {
  if (!canConnect.value) return
  busy.value = true; error.value = ''
  try { await createSession(selectedSiteID.value, cidrs.value); await refresh() } catch (cause) { error.value = errorLabel(cause) } finally { busy.value = false }
}
async function disconnect() {
  if (!active.value || busy.value) return
  busy.value = true; error.value = ''
  try { await disconnectSession(); await refresh() } catch (cause) { error.value = errorLabel(cause) } finally { busy.value = false }
}
onMounted(async () => {
  try {
    await refresh()
    await runPreflight()
    const safeRefresh = () => { void refresh().catch(cause => { error.value = errorLabel(cause) }) }
    timer = window.setInterval(safeRefresh, 2000)
    unsubscribe = window.runtime?.EventsOn('remlink:state', safeRefresh)
  } catch (cause) {
    error.value = errorLabel(cause)
  }
})
onBeforeUnmount(() => { clearInterval(timer); unsubscribe?.() })
watch(selectedSiteID, () => { void loadSelectedSiteProfile() })
</script>

<template>
  <div v-if="state" class="app-shell">
    <aside class="sidebar">
      <div class="brand"><img class="app-mark" :src="engineerMark" alt="" /><div>RemLink<small>Engineer 工作台</small></div></div>
      <nav aria-label="主导航">
        <button v-for="item in ([{ key: 'connection', icon: 'link', label: '连接' }, { key: 'session', icon: 'session', label: '会话' }, { key: 'logs', icon: 'logs', label: '日志' }, { key: 'settings', icon: 'settings', label: '设置' }] as const)" :key="item.key" class="nav-item" :class="{ selected: page === item.key }" :aria-current="page === item.key ? 'page' : undefined" @click="page = item.key"><Icon :name="item.icon"/><span>{{ item.label }}</span></button>
      </nav>
      <div class="sidebar-foot"><span class="version">RemLink Engineer v{{ state.version }}</span><span>IPv4 · netstack</span></div>
    </aside>

    <main>
      <header class="status-strip" aria-label="网络状态">
        <div><Icon name="server"/><span>服务器</span><strong><i :class="state.serverConnected ? 'healthy' : 'offline'"></i>{{ state.serverConnected ? '已连接' : '未连接' }}</strong></div>
        <div><Icon name="link"/><span>Overlay</span><strong class="mono">{{ state.overlayIP || '配置中' }}</strong></div>
        <div><Icon name="check"/><span>控制通道</span><strong><i :class="state.controlConnected ? 'healthy' : 'offline'"></i>{{ state.controlConnected ? '在线' : '离线' }}</strong></div>
      </header>

      <div class="workspace">
        <div class="title-row"><h1>{{ pageCopy[0] }}</h1><p>{{ pageCopy[1] }}</p></div>
        <div v-if="error" class="error-banner" role="alert"><span>{{ error }}</span><button class="icon-button" aria-label="关闭错误提示" @click="error = ''"><Icon name="close"/></button></div>

        <template v-if="page === 'connection'">
          <section class="connection-path" aria-label="当前连接路径">
            <div class="endpoint"><img :src="engineerMark" alt="" /><div><b>Engineer</b><small class="mono">{{ state.overlayIP || '等待分配地址' }}</small></div></div>
            <div class="path-line" :class="{ connected: state.serverConnected && state.controlConnected }" aria-hidden="true"><i></i></div>
            <div class="endpoint server-endpoint"><Icon name="server"/><div><b>RemLink Server</b><small>{{ state.serverConnected ? '中心服务器' : '等待连接' }}</small></div></div>
            <div class="path-line" :class="{ connected: active && state.serverConnected && state.controlConnected }" aria-hidden="true"><i></i></div>
            <div class="endpoint site-endpoint"><img :src="siteMark" alt="" /><div><b :title="sessionBusy ? state.session.siteName : selectedSite?.name">{{ (sessionBusy ? state.session.siteName : selectedSite?.name) || '选择现场' }}</b><small>{{ active ? '远程会话已建立' : sessionBusy ? statusLabel(state.session.status) : selectedSite?.overlay_ip || '尚未选择现场' }}</small></div></div>
            <span class="path-status"><i :class="active && state.serverConnected && state.controlConnected ? 'healthy' : 'offline'"></i>{{ active ? (state.serverConnected && state.controlConnected ? '已连接' : '连接异常') : sessionBusy ? '处理中' : '未连接' }}</span>
          </section>

          <div class="setup-grid">
            <section class="panel sites-panel" aria-labelledby="sites-title">
              <div class="panel-heading"><div class="heading-group"><h2 id="sites-title">现场节点</h2><span class="muted">在线 {{ onlineCount }} / {{ state.sites.length }}</span></div><div class="site-search"><Icon name="search"/><input v-model="siteQuery" type="search" placeholder="搜索现场名称或 IP" aria-label="搜索现场名称或 IP" /></div></div>
              <div class="site-table-scroll">
                <table class="site-table">
                  <thead><tr><th scope="col">现场 / Overlay IP</th><th scope="col">状态</th><th scope="col">远程访问</th><th scope="col">最近在线</th></tr></thead>
                  <tbody><tr v-for="site in filteredSites" :key="site.node_id" :class="{ selected: selectedSiteID === site.node_id, locked: busy || editing || (sessionBusy && selectedSiteID !== site.node_id) }">
                    <td><button class="site-select" :disabled="busy || editing || (sessionBusy && selectedSiteID !== site.node_id)" :aria-pressed="selectedSiteID === site.node_id" @click="selectedSiteID = site.node_id"><b :title="site.name">{{ site.name }}</b><small class="mono">{{ site.overlay_ip || '未分配地址' }}</small></button></td>
                    <td><span class="node-status" :class="site.online ? 'good-text' : 'muted'"><i :class="site.online ? 'healthy' : 'offline'"></i>{{ site.online ? '在线' : '离线' }}</span></td>
                    <td><span :class="site.remote_subnet_capability ? '' : 'muted'">{{ site.remote_subnet_capability ? '可用' : '不可用' }}</span><small class="cell-detail">已存 {{ siteCIDRCount(site.node_id) }} 个网段</small><small class="compact-last-seen" :title="site.last_seen">最近 {{ formatTime(site.last_seen) }}</small></td>
                    <td class="mono muted">{{ formatTime(site.last_seen) }}</td>
                  </tr></tbody>
                </table>
                <div v-if="!filteredSites.length" class="empty-state"><Icon name="monitor"/><b>{{ state.sites.length ? '没有匹配的现场' : '暂无现场节点' }}</b><span>{{ state.sites.length ? '试试其他名称或 Overlay IP。' : '现场端上线后，将自动出现在这里。' }}</span><button v-if="siteQuery" class="text-action" @click="siteQuery = ''">清除搜索</button></div>
              </div>
              <p class="panel-footnote">{{ sessionBusy ? '会话期间已锁定现场选择' : '选择现场后，自动加载已保存的远程网段' }}</p>
            </section>

            <section class="panel cidr-panel" aria-labelledby="cidr-title">
              <div class="panel-heading"><h2 id="cidr-title">远程网段</h2><span class="selected-site-label" :title="selectedSite?.name">{{ selectedSite?.name || '请先选择现场' }}</span></div>
              <div class="cidr-body">
                <p class="profile-hint">每个现场独立保存，切换时自动加载。</p>
                <label for="cidr">远程 IPv4 网段（CIDR）</label>
                <form @submit.prevent="addCIDR"><input id="cidr" v-model="cidrInput" placeholder="例如 192.168.13.0/24" :disabled="editLocked" autocomplete="off" spellcheck="false"/><button type="submit" class="secondary" :disabled="editLocked || !cidrInput.trim()"><Icon name="plus"/>添加</button></form>
                <div class="cidr-list">
                  <div v-for="cidr in cidrs" :key="cidr"><Icon name="link"/><code>{{ cidr }}</code><button class="icon-button" :disabled="editLocked" @click="removeCIDR(cidr)" :aria-label="`删除 ${cidr}`"><Icon name="close"/></button></div>
                  <p v-if="!cidrs.length" class="cidr-empty">{{ selectedSite ? '尚未配置网段，请在上方添加。' : '选择现场后可配置远程网段。' }}</p>
                </div>
                <div class="preflight" :class="preflightState" role="status"><Icon :name="preflightState === 'pass' ? 'check' : preflightState === 'fail' ? 'alert' : 'session'"/><span>本地冲突预检：<strong>{{ preflightMessage }}</strong></span><button class="text-action" :disabled="editLocked || preflightState === 'checking'" @click="runPreflight">再次检查</button></div>
                <div class="connect-action"><button class="primary" :disabled="!canConnect" aria-describedby="connect-hint" @click="connect"><Icon name="link"/>{{ busy && !active ? '正在建立…' : '连接现场' }}</button><p id="connect-hint">{{ connectHint }}</p></div>
              </div>
            </section>
          </div>
        </template>

        <SessionPanel v-if="page === 'connection' || page === 'session'" :session="state.session" :busy="busy" @disconnect="disconnect"/>
        <LogPanel v-show="page === 'connection' || page === 'logs'" :logs="state.logs" :expanded="page === 'logs'" :visible="page === 'connection' || page === 'logs'" @expand="page = 'logs'"/>

        <section v-if="page === 'settings'" class="panel settings-panel" aria-labelledby="settings-title">
          <div class="panel-heading"><h2 id="settings-title">运行配置</h2><span class="badge neutral">只读</span></div>
          <dl><dt>Server 公网地址</dt><dd class="mono">{{ state.serverURL || '未配置' }}</dd><dt>Overlay 地址</dt><dd class="mono">{{ state.overlayIP || '等待分配' }}</dd><dt>客户端版本</dt><dd>{{ state.version }}</dd><dt>数据面</dt><dd>IPv4 · 单 Wintun · wireguard-go</dd><dt>现场网关</dt><dd>gVisor netstack</dd></dl>
          <div class="settings-note"><Icon name="settings"/><div><b>由本机配置与 Server Bootstrap 管理</b><p>网络地址、密钥与 Node Token 不在界面中复制或明文保存；Overlay 变更由 Server 触发重新 Bootstrap。</p></div></div>
        </section>
      </div>
    </main>
  </div>
  <div v-else class="loading" role="status"><img :src="engineerMark" alt=""/><h1>RemLink Engineer</h1><p>{{ error || '正在启动工作台…' }}</p></div>
</template>
