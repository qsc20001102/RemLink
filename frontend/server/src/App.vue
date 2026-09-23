<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import Icon from './Icon.vue'
import LogTable from './components/LogTable.vue'
import NetworkForm from './components/NetworkForm.vue'
import NodeTable from './components/NodeTable.vue'
import SessionTable from './components/SessionTable.vue'
import TopologyView from './components/TopologyView.vue'
import { api, setAdminToken } from './api'
import type { EventRecord, NetworkConfig, NodeRecord, SessionFilter, SessionRecord } from './types'
import { errorLabel, levelLabel, moduleLabel } from './zh-cn'

type Page = 'overview' | 'nodes' | 'sessions' | 'network' | 'logs'
const page = ref<Page>('overview')
const releaseVersion = import.meta.env.VITE_APP_VERSION || '1.0.0'
const nodes = ref<NodeRecord[]>([]), sessions = ref<SessionRecord[]>([]), historySessions = ref<SessionRecord[]>([]), logs = ref<EventRecord[]>([])
let network = reactive<NetworkConfig>({ overlay_cidr:'', server_overlay_ip:'', wireguard_port:51820, session_udp_port:6200, mtu:1280, config_version:1 })
const busy = ref(false), error = ref(''), saved = ref(false), adminToken = ref(localStorage.getItem('remlink-admin-token') ?? '')
const logLevel = ref(''), logModule = ref(''), logNode = ref(''), logSession = ref(''), logFrom = ref(''), logTo = ref('')
function localDateTime(date: Date){const offset = date.getTimezoneOffset() * 60000; return new Date(date.getTime() - offset).toISOString().slice(0,16)}
const historyFrom = ref(localDateTime(new Date(Date.now() - 7 * 86400000)))
const historyTo = ref(''), historyEngineer = ref(''), historySite = ref(''), historyStatus = ref(''), historyId = ref('')
const historyFilter = ref<SessionFilter>({kind:'history',from:new Date(Date.now() - 7 * 86400000).toISOString(),limit:500})
let refreshTimer = 0
const logModules = ['CORE','BOOTSTRAP','WG','IPAM','CONTROL','SESSION','ROUTE','NETSTACK','TUN','SUBNET','SYSTEM']
const nav: Array<{id:Page; label:string; icon:string}> = [{id:'overview',label:'概览',icon:'overview'},{id:'nodes',label:'节点',icon:'nodes'},{id:'sessions',label:'会话',icon:'sessions'},{id:'network',label:'网络',icon:'network'},{id:'logs',label:'日志',icon:'logs'}]
const unstable = computed(() => nodes.value.filter(node => node.status === 'UNSTABLE').length)
const offline = computed(() => nodes.value.filter(node => node.status === 'OFFLINE').length)
const healthLabel = computed(() => offline.value ? '需关注' : unstable.value ? '不稳定' : '健康')
const realtimeSessions = computed(() => sessions.value.filter(session => session.status !== 'CLOSED' && session.status !== 'FAILED'))
const engineerNodes = computed(() => nodes.value.filter(node => node.type === 'engineer'))
const siteNodes = computed(() => nodes.value.filter(node => node.type === 'site'))
function arrayOrEmpty<T>(value:T[]|null|undefined):T[]{return Array.isArray(value)?value:[]}
function formatUptime(value=0){const days=Math.floor(value/86400),hours=Math.floor(value%86400/3600),minutes=Math.floor(value%3600/60);return days?`${days}天 ${hours}小时`:`${hours}小时 ${minutes}分`}
function logFilter(){return {level:logLevel.value||undefined,module:logModule.value||undefined,node_id:logNode.value.trim()||undefined,session_id:logSession.value.trim()||undefined,from:logFrom.value?new Date(logFrom.value).toISOString():undefined,to:logTo.value?new Date(logTo.value).toISOString():undefined,limit:200}}
async function load(){busy.value=true;error.value='';try{const [nextNodes,nextSessions,nextHistory,nextNetwork,nextLogs]=await Promise.all([api.nodes(),api.sessions({kind:'live'}),api.sessions(historyFilter.value),api.network(),api.logs(logFilter())]);nodes.value=arrayOrEmpty(nextNodes);sessions.value=arrayOrEmpty(nextSessions);historySessions.value=arrayOrEmpty(nextHistory);logs.value=arrayOrEmpty(nextLogs);Object.assign(network,nextNetwork)}catch(cause){error.value=errorLabel(cause)}finally{busy.value=false}}
async function refreshRuntime(){if(busy.value)return;try{const [nextNodes,nextSessions,nextHistory,nextLogs]=await Promise.all([api.nodes(),api.sessions({kind:'live'}),page.value==='sessions'?api.sessions(historyFilter.value):Promise.resolve(historySessions.value),api.logs(logFilter())]);nodes.value=arrayOrEmpty(nextNodes);sessions.value=arrayOrEmpty(nextSessions);historySessions.value=arrayOrEmpty(nextHistory);logs.value=arrayOrEmpty(nextLogs)}catch(cause){error.value=errorLabel(cause)}}
async function queryHistory(){historyFilter.value={kind:'history',from:historyFrom.value?new Date(historyFrom.value).toISOString():undefined,to:historyTo.value?new Date(historyTo.value).toISOString():undefined,engineer_node_id:historyEngineer.value||undefined,site_node_id:historySite.value||undefined,status:historyStatus.value||undefined,session_id:historyId.value.trim()||undefined,limit:500};busy.value=true;error.value='';try{historySessions.value=arrayOrEmpty(await api.sessions(historyFilter.value))}catch(cause){error.value=errorLabel(cause)}finally{busy.value=false}}
async function runAction(action:()=>Promise<unknown>){busy.value=true;error.value='';try{await action();await load()}catch(cause){error.value=errorLabel(cause)}finally{busy.value=false}}
async function editNode(node:NodeRecord){const name=window.prompt('节点名称',node.name);if(name===null)return;const overlay=window.prompt('Overlay IP',node.overlay_ip);if(overlay===null)return;await runAction(()=>api.patchNode(node.node_id,{name,overlay_ip:overlay}))}
async function revokeNode(node:NodeRecord){if(!window.confirm(`撤销节点 ${node.name}？此操作会断开相关会话。`))return;await runAction(()=>api.deleteNode(node.node_id))}
async function disconnect(session:SessionRecord){if(!window.confirm(`强制断开 Session ${session.session_id}？`))return;await runAction(()=>api.disconnect(session.session_id))}
async function saveNetwork(rotate=false){if(rotate&&!window.confirm('确定轮换 Join Token？旧 Token 将无法用于新设备注册，已注册设备不受影响。'))return;busy.value=true;error.value='';saved.value=false;try{const current=rotate?await api.network():network;const updated=await api.updateNetwork({...current,rotate_join_token:rotate});Object.assign(network,updated);saved.value=true;setTimeout(()=>saved.value=false,2500)}catch(cause){error.value=errorLabel(cause)}finally{busy.value=false}}
function applyToken(){setAdminToken(adminToken.value);load()}
onMounted(async()=>{await load();refreshTimer=window.setInterval(()=>{void refreshRuntime()},5000)})
onBeforeUnmount(()=>clearInterval(refreshTimer))
</script>

<template>
  <div class="admin-shell">
    <aside class="sidebar">
      <div class="brand"><span class="brand-mark"><i></i><i></i><i></i></span><span>RemLink <b>Server</b><small>工业远程网络管理平台</small></span></div>
      <nav aria-label="主导航"><button v-for="item in nav" :key="item.id" :class="{selected:page===item.id}" :title="item.label" :aria-label="item.label" @click="page=item.id"><Icon :name="item.icon"/>{{item.label}}</button></nav>
      <div class="token-box"><label>单机访问令牌（可选）</label><div><input v-model="adminToken" type="password" placeholder="Bearer token"/><button @click="applyToken">应用</button></div></div>
      <footer>RemLink Server v{{ releaseVersion }}</footer>
    </aside>
    <main>
      <header><span class="header-health"><i :class="healthLabel === '健康' ? 'online' : healthLabel === '不稳定' ? 'unstable' : 'offline'"></i>系统{{healthLabel}}</span><span>运行时长 <b>{{formatUptime(network.uptime_seconds)}}</b></span><span class="header-version">Server v{{ releaseVersion }}</span></header>
      <div class="content" :class="{ 'overview-content': page === 'overview' }">
        <div v-if="page!=='overview'" class="title-row"><div><h1>{{page==='sessions'?'会话记录':nav.find(item=>item.id===page)?.label}}</h1><p>RemLink Server 权威配置与运行状态</p></div></div>
        <p v-if="error" class="page-error">{{error}}</p>

        <template v-if="page==='overview'">
          <TopologyView :nodes="nodes" :sessions="realtimeSessions" :server-i-p="network.server_overlay_ip" />
          <section class="overview-events panel"><div class="section-title"><div><h2>最近事件</h2><p>记录系统关键活动</p></div><button class="text-link" @click="page='logs'">查看全部日志 <Icon name="arrow" /></button></div><LogTable :logs="logs.slice(0,20)" :bare="true" /></section>
        </template>
        <div v-else-if="page==='nodes'" class="node-sections"><NodeTable title="Engineer 节点" empty-label="暂无 Engineer 节点" :nodes="engineerNodes" @edit="editNode" @revoke="revokeNode"/><NodeTable title="Site 节点" empty-label="暂无 Site 节点" :nodes="siteNodes" @edit="editNode" @revoke="revokeNode"/></div>
        <div v-else-if="page==='sessions'" class="session-sections">
          <SessionTable title="实时会话" empty-label="当前没有实时会话" :sessions="realtimeSessions" :nodes="nodes" @disconnect="disconnect"/>
          <SessionTable title="历史会话" empty-label="所选条件下暂无历史会话" :history="true" :sessions="historySessions" :nodes="nodes" @disconnect="disconnect">
            <template #filters><div class="session-filters">
              <input v-model="historyFrom" type="datetime-local" aria-label="历史会话起始时间" title="起始时间"/>
              <input v-model="historyTo" type="datetime-local" aria-label="历史会话结束时间" title="结束时间"/>
              <select v-model="historyEngineer" aria-label="Engineer 筛选"><option value="">全部 Engineer</option><option v-for="node in engineerNodes" :key="node.node_id" :value="node.node_id">{{node.name}}</option></select>
              <select v-model="historySite" aria-label="Site 筛选"><option value="">全部 Site</option><option v-for="node in siteNodes" :key="node.node_id" :value="node.node_id">{{node.name}}</option></select>
              <select v-model="historyStatus" aria-label="会话状态筛选"><option value="">全部状态</option><option value="CLOSED">已关闭</option><option value="FAILED">失败</option></select>
              <input v-model="historyId" inputmode="numeric" placeholder="会话 ID" aria-label="会话 ID 筛选" @keyup.enter="queryHistory"/>
              <button :disabled="busy" @click="queryHistory">查询</button>
            </div></template>
          </SessionTable>
        </div>
        <NetworkForm v-else-if="page==='network'" v-model="network" :busy="busy" :saved="saved" :full="true" @save="saveNetwork(false)" @rotate="saveNetwork(true)"/>
        <section v-else class="panel logs-page"><div class="section-title log-filter-title"><h2>事件日志</h2><div class="filters"><input v-model="logFrom" type="datetime-local" title="起始时间"/><input v-model="logTo" type="datetime-local" title="结束时间"/><select v-model="logLevel"><option value="">全部级别</option><option value="INFO">{{levelLabel('INFO')}}</option><option value="WARN">{{levelLabel('WARN')}}</option><option value="ERROR">{{levelLabel('ERROR')}}</option></select><select v-model="logModule"><option value="">全部模块</option><option v-for="module in logModules" :key="module" :value="module">{{moduleLabel(module)}}</option></select><input v-model="logNode" placeholder="节点 ID"/><input v-model="logSession" placeholder="会话 ID"/><button @click="load">查询</button></div></div><LogTable :logs="logs" :bare="true"/></section>
      </div>
    </main>
  </div>
</template>
