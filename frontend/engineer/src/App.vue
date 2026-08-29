<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Icon from './Icon.vue'
import { checkCIDRs, createSession, disconnectSession, getState, saveSiteCIDRs } from './api'
import type { EngineerState, SiteSummary } from './types'
import { errorLabel, levelLabel, statusLabel } from './zh-cn'

const state = ref<EngineerState | null>(null)
const page = ref<'connection' | 'session' | 'logs' | 'settings'>('connection')
const selectedSiteID = ref('')
const cidrs = ref<string[]>([])
const cidrInput = ref('')
const busy = ref(false)
const error = ref('')
const preflightState = ref<'checking' | 'pass' | 'fail'>('checking')
const preflightMessage = ref('正在检查本地路由…')
let timer = 0
let unsubscribe: (() => void) | undefined

const active = computed(() => state.value?.session.status === 'ACTIVE')
const sessionBusy = computed(() => {
  const status = state.value?.session.status
  return status === 'CREATING' || status === 'PREPARING_SITE' || status === 'READY' || status === 'ACTIVE' || status === 'STOPPING'
})
const selectedSite = computed<SiteSummary | undefined>(() => state.value?.sites.find(site => site.node_id === selectedSiteID.value))
const canConnect = computed(() => !busy.value && !sessionBusy.value && state.value?.serverConnected && state.value.controlConnected && preflightState.value === 'pass' && selectedSite.value?.online && selectedSite.value.remote_subnet_capability && cidrs.value.length > 0)

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`
  if (value < 1024 ** 2) return `${(value / 1024).toFixed(1)} KB`
  return `${(value / 1024 ** 2).toFixed(2)} MB`
}
function formatTime(value?: string) { return value ? new Intl.DateTimeFormat('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' }).format(new Date(value)) : '—' }
async function addCIDR() {
  const value = cidrInput.value.trim()
  if (!value || cidrs.value.includes(value)) return
  cidrs.value.push(value)
  cidrInput.value = ''
  if (await runPreflight()) await persistCurrentProfile()
}
async function removeCIDR(value: string) {
  cidrs.value = cidrs.value.filter(item => item !== value)
  const passed = await runPreflight()
  if (passed || cidrs.value.length === 0) await persistCurrentProfile()
}
async function runPreflight() {
  if (cidrs.value.length === 0) {
    preflightState.value = 'fail'; preflightMessage.value = '请为当前现场添加至少一个网段'
    return false
  }
  preflightState.value = 'checking'; preflightMessage.value = '正在检查本地路由…'
  try { await checkCIDRs(Array.from(cidrs.value)); preflightState.value = 'pass'; preflightMessage.value = '通过，无冲突'; return true }
  catch (cause) { preflightState.value = 'fail'; preflightMessage.value = errorLabel(cause); return false }
}
async function persistCurrentProfile() {
  const siteID = selectedSiteID.value
  if (!siteID || !state.value) return
  try {
    await saveSiteCIDRs(siteID, Array.from(cidrs.value))
    if (cidrs.value.length === 0) delete state.value.siteCIDRs[siteID]
    else state.value.siteCIDRs[siteID] = Array.from(cidrs.value)
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
  if (!selectedSiteID.value) selectedSiteID.value = state.value.sites.find(site => site.online)?.node_id ?? ''
}
async function connect() {
  if (!canConnect.value) return
  busy.value = true; error.value = ''
  try { await createSession(selectedSiteID.value, cidrs.value); await refresh() } catch (cause) { error.value = errorLabel(cause) } finally { busy.value = false }
}
async function disconnect() {
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
  <div class="app-shell" v-if="state">
    <aside class="sidebar">
      <div class="brand">RemLink<span>Engineer</span></div>
      <nav aria-label="主导航">
        <button class="nav-item" :class="{selected:page==='connection'}" @click="page='connection'"><Icon name="link"/>连接</button>
        <button class="nav-item" :class="{selected:page==='session'}" @click="page='session'"><Icon name="session"/>会话</button>
        <button class="nav-item" :class="{selected:page==='logs'}" @click="page='logs'"><Icon name="logs"/>日志</button>
        <button class="nav-item" :class="{selected:page==='settings'}" @click="page='settings'"><Icon name="settings"/>设置</button>
      </nav>
      <div class="sidebar-foot"><span class="version">v{{ state.version }}</span><span>IPv4 · netstack</span></div>
    </aside>

    <main>
      <header class="status-strip">
        <div><i :class="state.serverConnected ? 'healthy' : 'offline'"></i>服务器 <strong>{{ state.serverConnected ? '已连接' : '未连接' }}</strong></div>
        <div><i class="healthy"></i>Overlay <strong>{{ state.overlayIP || '配置中' }}</strong></div>
        <div><i :class="state.controlConnected ? 'healthy' : 'offline'"></i>Control <strong>{{ state.controlConnected ? '在线' : '离线' }}</strong></div>
      </header>

      <div class="workspace">
        <section v-show="page==='connection'" class="connection-path" aria-label="当前连接路径">
          <div class="endpoint"><span class="endpoint-icon"><Icon name="monitor"/></span><div><b>Engineer</b><small>{{ state.overlayIP }}</small></div></div>
          <div class="path-line"><span></span><i></i><i></i><i></i><span></span></div>
          <div class="endpoint site"><span class="endpoint-icon"><Icon name="server"/></span><div><b>{{ selectedSite?.name || '选择现场' }}</b><small>{{ selectedSite?.overlay_ip || '—' }}</small></div></div>
        </section>

        <div v-show="page==='connection'" class="setup-grid">
          <section class="panel sites-panel">
            <div class="panel-heading"><h2>现场节点</h2><span>{{ state.sites.filter(site => site.online).length }} 个在线</span></div>
            <div class="table-head"><span>站点名称</span><span>状态</span><span>Overlay IP</span><span>Remote Subnet</span><span>LastSeen</span></div>
            <button v-for="site in state.sites" :key="site.node_id" class="site-row" :class="{ selected: selectedSiteID === site.node_id, disabled: sessionBusy && selectedSiteID !== site.node_id }" :disabled="sessionBusy && selectedSiteID !== site.node_id" @click="selectedSiteID = site.node_id">
              <span><i :class="site.online ? 'healthy' : 'offline'"></i>{{ site.name }}</span>
              <span :class="site.online ? 'good-text' : 'muted'">{{ site.online ? '在线' : '离线' }}</span>
              <span>{{ site.overlay_ip || '—' }}</span>
              <span :class="site.remote_subnet_capability ? 'good-text' : 'muted'">{{ site.remote_subnet_capability ? `可用 · 已存 ${siteCIDRCount(site.node_id)} 个` : '不可用' }}</span>
              <span>{{ formatTime(site.last_seen) }}</span>
            </button>
            <button class="primary wide" :disabled="!canConnect" @click="connect">{{ busy ? '正在建立…' : '连接现场' }}</button>
          </section>

          <section class="panel cidr-panel">
            <div class="panel-heading"><div><h2>远程网段</h2><small>{{ selectedSite?.name ? `当前现场：${selectedSite.name}` : '请先选择现场' }}</small></div><button class="text-action" @click="addCIDR"><Icon name="plus"/>添加网段</button></div>
            <p class="profile-hint">每个现场独立保存；切换现场时自动加载，建立会话时只发送当前现场的网段。</p>
            <label for="cidr">Remote CIDR</label>
            <form @submit.prevent="addCIDR"><input id="cidr" v-model="cidrInput" placeholder="例如 192.168.13.0/24" :disabled="sessionBusy"/><button class="secondary" :disabled="sessionBusy || !cidrInput.trim()">添加</button></form>
            <div class="cidr-list">
              <div v-for="cidr in cidrs" :key="cidr"><span class="grip">⠿</span><code>{{ cidr }}</code><button :disabled="sessionBusy" @click="removeCIDR(cidr)" :aria-label="`删除 ${cidr}`"><Icon name="close"/></button></div>
              <p v-if="!cidrs.length" class="cidr-empty">当前现场尚未配置远程网段</p>
            </div>
            <div class="preflight" :class="preflightState"><Icon name="check"/><span>本地冲突预检：<strong>{{ preflightMessage }}</strong></span><button :disabled="sessionBusy || preflightState==='checking'" @click="runPreflight">再次检查</button></div>
          </section>
        </div>

        <p v-if="error" class="error-banner">{{ error }}</p>
        <section v-show="page==='connection'||page==='session'" class="session-panel">
          <div class="session-copy"><h2>会话详情</h2><dl><dt>现场端</dt><dd>{{ state.session.siteName || '—' }}</dd><dt>远程网段</dt><dd>{{ state.session.cidrs.join(', ') || '—' }}</dd><dt>会话 ID</dt><dd class="mono">{{ state.session.id || '—' }}</dd><dt>状态</dt><dd class="good-text">{{ statusLabel(state.session.status) }}</dd><dt>结束原因</dt><dd>{{ state.session.reason ? errorLabel(state.session.reason) : '—' }}</dd><dt>开始时间</dt><dd>{{ formatTime(state.session.startedAt) }}</dd></dl></div>
          <div class="metric"><span>上传</span><strong>{{ formatBytes(state.session.uploadBytes) }}</strong><small>{{ state.session.uploadPackets.toLocaleString() }} packets</small></div>
          <div class="metric"><span>下载</span><strong>{{ formatBytes(state.session.downloadBytes) }}</strong><small>{{ state.session.downloadPackets.toLocaleString() }} packets</small></div>
          <div class="metric"><span>时延</span><strong>{{ state.session.latencyMS || '—' }}</strong><small>ms</small></div>
          <button class="danger" :disabled="!active || busy" @click="disconnect">断开会话</button>
        </section>

        <section v-show="page==='connection'||page==='logs'" class="logs-panel">
          <div class="panel-heading"><h2>实时日志</h2><span>自动滚动</span></div>
          <div class="logs"><div v-for="(log, index) in state.logs" :key="index"><time>{{ formatTime(log.time) }}</time><b :class="log.level.toLowerCase()">{{ levelLabel(log.level) }}</b><span>{{ log.message }}</span></div></div>
        </section>
        <section v-if="page==='settings'" class="panel settings-panel">
          <div class="panel-heading"><h2>运行配置</h2><span>由本机配置与 Server Bootstrap 管理</span></div>
          <dl><dt>Server 公网地址</dt><dd class="mono">{{ state.serverURL }}</dd><dt>客户端版本</dt><dd>{{ state.version }}</dd><dt>数据面</dt><dd>IPv4 · 单 Wintun · wireguard-go</dd><dt>现场网关</dt><dd>gVisor netstack</dd></dl>
          <p>网络地址、密钥与 Node Token 不在界面中复制或明文保存；Overlay 变更由 Server 触发重新 Bootstrap。</p>
        </section>
      </div>
    </main>
  </div>
  <div v-else class="loading">{{ error || '正在启动 RemLink Engineer…' }}</div>
</template>

<style>
.cidr-panel .panel-heading>div{display:grid;gap:3px}
.cidr-panel .panel-heading small{color:var(--muted);font-size:10px}
.sites-panel .table-head,.sites-panel .site-row{grid-template-columns:1.2fr .55fr .8fr 1.05fr .7fr;column-gap:8px}
.profile-hint{margin:11px 18px 0;color:#68738a;font-size:11px;line-height:1.5}
.cidr-empty{margin:3px 0;padding:14px;border:1px dashed #cbd2e0;border-radius:6px;color:var(--muted);font-size:11px;text-align:center}
</style>
