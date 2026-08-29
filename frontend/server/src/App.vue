<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import Icon from './Icon.vue'
import LogTable from './components/LogTable.vue'
import NetworkForm from './components/NetworkForm.vue'
import NodeTable from './components/NodeTable.vue'
import SessionTable from './components/SessionTable.vue'
import { api, setAdminToken } from './api'
import type { EventRecord, NetworkConfig, NodeRecord, SessionRecord } from './types'
import { errorLabel, levelLabel, moduleLabel } from './zh-cn'

type Page = 'overview' | 'nodes' | 'sessions' | 'network' | 'logs'
const page = ref<Page>('overview')
const nodes = ref<NodeRecord[]>([]), sessions = ref<SessionRecord[]>([]), logs = ref<EventRecord[]>([])
let network = reactive<NetworkConfig>({ overlay_cidr:'', server_overlay_ip:'', wireguard_port:51820, session_udp_port:6200, mtu:1280, config_version:1 })
const busy = ref(false), error = ref(''), saved = ref(false), adminToken = ref(localStorage.getItem('remlink-admin-token') ?? '')
const logLevel = ref(''), logModule = ref(''), logNode = ref(''), logSession = ref(''), logFrom = ref(''), logTo = ref('')
let refreshTimer = 0
const logModules = ['CORE','BOOTSTRAP','WG','IPAM','CONTROL','SESSION','ROUTE','NETSTACK','TUN','SUBNET','SYSTEM']
const nav: Array<{id:Page; label:string; icon:string}> = [{id:'overview',label:'概览',icon:'overview'},{id:'nodes',label:'节点',icon:'nodes'},{id:'sessions',label:'会话',icon:'sessions'},{id:'network',label:'网络',icon:'network'},{id:'logs',label:'日志',icon:'logs'}]
const online = computed(() => nodes.value.filter(node => node.status === 'ONLINE').length)
const unstable = computed(() => nodes.value.filter(node => node.status === 'UNSTABLE').length)
const offline = computed(() => nodes.value.filter(node => node.status === 'OFFLINE').length)
const healthLabel = computed(() => offline.value ? '需关注' : unstable.value ? '不稳定' : '健康')
const activeSessions = computed(() => sessions.value.filter(session => session.status === 'ACTIVE'))
const totalUpload = computed(() => sessions.value.reduce((sum, session) => sum + session.counters.upload_bytes, 0))
const totalDownload = computed(() => sessions.value.reduce((sum, session) => sum + session.counters.download_bytes, 0))
const engineerNodes = computed(() => nodes.value.filter(node => node.type === 'engineer'))
const siteNodes = computed(() => nodes.value.filter(node => node.type === 'site'))
const nodeName = (id:string) => nodes.value.find(node => node.node_id === id)?.name ?? id
function arrayOrEmpty<T>(value:T[]|null|undefined):T[]{return Array.isArray(value)?value:[]}
function formatTime(value?:string){return value?new Intl.DateTimeFormat('zh-CN',{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',second:'2-digit'}).format(new Date(value)):'—'}
function formatBytes(value:number){return value<1024**2?`${(value/1024).toFixed(1)} KB`:`${(value/1024**2).toFixed(2)} MB`}
function formatUptime(value=0){const days=Math.floor(value/86400),hours=Math.floor(value%86400/3600),minutes=Math.floor(value%3600/60);return days?`${days}天 ${hours}小时`:`${hours}小时 ${minutes}分`}
function topologyNodePosition(index:number,count:number){return ((index+0.5)/Math.max(count,1))*100}
function logFilter(){return {level:logLevel.value||undefined,module:logModule.value||undefined,node_id:logNode.value.trim()||undefined,session_id:logSession.value.trim()||undefined,from:logFrom.value?new Date(logFrom.value).toISOString():undefined,to:logTo.value?new Date(logTo.value).toISOString():undefined,limit:200}}
async function load(){busy.value=true;error.value='';try{const [nextNodes,nextSessions,nextNetwork,nextLogs]=await Promise.all([api.nodes(),api.sessions(),api.network(),api.logs(logFilter())]);nodes.value=arrayOrEmpty(nextNodes);sessions.value=arrayOrEmpty(nextSessions);logs.value=arrayOrEmpty(nextLogs);Object.assign(network,nextNetwork)}catch(cause){error.value=errorLabel(cause)}finally{busy.value=false}}
async function refreshRuntime(){if(busy.value)return;try{const [nextNodes,nextSessions,nextLogs]=await Promise.all([api.nodes(),api.sessions(),api.logs(logFilter())]);nodes.value=arrayOrEmpty(nextNodes);sessions.value=arrayOrEmpty(nextSessions);logs.value=arrayOrEmpty(nextLogs)}catch(cause){error.value=errorLabel(cause)}}
async function runAction(action:()=>Promise<unknown>){busy.value=true;error.value='';try{await action();await load()}catch(cause){error.value=errorLabel(cause)}finally{busy.value=false}}
async function editNode(node:NodeRecord){const name=window.prompt('节点名称',node.name);if(name===null)return;const overlay=window.prompt('Overlay IP',node.overlay_ip);if(overlay===null)return;await runAction(()=>api.patchNode(node.node_id,{name,overlay_ip:overlay}))}
async function revokeNode(node:NodeRecord){if(!window.confirm(`撤销节点 ${node.name}？此操作会断开相关会话。`))return;await runAction(()=>api.deleteNode(node.node_id))}
async function disconnect(session:SessionRecord){if(!window.confirm(`强制断开 Session ${session.session_id}？`))return;await runAction(()=>api.disconnect(session.session_id))}
async function saveNetwork(rotate=false){busy.value=true;error.value='';saved.value=false;try{const updated=await api.updateNetwork({...network,rotate_join_token:rotate});Object.assign(network,updated);saved.value=true;setTimeout(()=>saved.value=false,2500)}catch(cause){error.value=errorLabel(cause)}finally{busy.value=false}}
function applyToken(){setAdminToken(adminToken.value);load()}
onMounted(async()=>{await load();refreshTimer=window.setInterval(()=>{void refreshRuntime()},5000)})
onBeforeUnmount(()=>clearInterval(refreshTimer))
</script>

<template>
  <div class="admin-shell">
    <aside class="sidebar">
      <div class="brand"><span class="brand-mark"><i></i><i></i><i></i></span><span>RemLink <b>Server</b></span></div>
      <nav><button v-for="item in nav" :key="item.id" :class="{selected:page===item.id}" @click="page=item.id"><Icon :name="item.icon"/>{{item.label}}</button></nav>
      <div class="token-box"><label>单机访问令牌（可选）</label><div><input v-model="adminToken" type="password" placeholder="Bearer token"/><button @click="applyToken">应用</button></div></div>
      <footer>RemLink Server v1.0.0</footer>
    </aside>
    <main>
      <header><span>服务器版本：<b>v1.0.0</b></span><i></i><span>运行时长：<b>{{formatUptime(network.uptime_seconds)}}</b></span><i></i><span>整体健康状态：<strong>{{healthLabel}}</strong></span></header>
      <div class="content">
        <div class="title-row"><div><h1>{{nav.find(item=>item.id===page)?.label==='概览'?'网络运行概览':nav.find(item=>item.id===page)?.label}}</h1><p v-if="page!=='overview'">RemLink Server 权威配置与运行状态</p></div><button v-if="page==='overview'" class="primary" @click="page='network'"><Icon name="plus"/>节点接入配置</button></div>
        <p v-if="error" class="page-error">{{error}}</p>

        <template v-if="page==='overview'">
          <section class="topology panel">
            <div class="section-title">
              <div class="topology-heading"><h2>拓扑概览</h2><p>Engineer 在左，Site 在右，均独立连接 RemLink Server</p></div>
              <div class="legend"><span><i class="online"></i>在线</span><span><i class="unstable"></i>不稳定</span><span><i class="offline"></i>离线</span></div>
            </div>
            <div class="topology-body">
              <div class="topology-split" :style="{minHeight:`${Math.max(180,Math.max(engineerNodes.length,siteNodes.length)*48)}px`}" aria-label="Engineer 位于左侧、RemLink Server 位于中间、Site 位于右侧的 Overlay 拓扑">
                <div class="topology-side engineer-side">
                  <svg viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
                    <line v-for="(node,index) in engineerNodes" :key="node.node_id" x1="28" :y1="topologyNodePosition(index,engineerNodes.length)" x2="100" y2="50"/>
                  </svg>
                  <span v-for="(node,index) in engineerNodes" :key="node.node_id" class="topology-node engineer-node" :style="{top:`${topologyNodePosition(index,engineerNodes.length)}%`}">
                    <i :class="node.status.toLowerCase()"></i><b>{{node.name}}</b><em>Engineer</em><small>{{node.overlay_ip}}</small>
                  </span>
                  <p v-if="!engineerNodes.length" class="topology-empty">暂无 Engineer</p>
                </div>
                <div class="server-node"><Icon name="nodes"/><div><b>RemLink Server</b><small>{{network.server_overlay_ip}}</small></div></div>
                <div class="topology-side site-side">
                  <svg viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
                    <line v-for="(node,index) in siteNodes" :key="node.node_id" x1="0" y1="50" x2="72" :y2="topologyNodePosition(index,siteNodes.length)"/>
                  </svg>
                  <span v-for="(node,index) in siteNodes" :key="node.node_id" class="topology-node site-node" :style="{top:`${topologyNodePosition(index,siteNodes.length)}%`}">
                    <i :class="node.status.toLowerCase()"></i><b>{{node.name}}</b><em>Site</em><small>{{node.overlay_ip}}</small>
                  </span>
                  <p v-if="!siteNodes.length" class="topology-empty">暂无 Site</p>
                </div>
              </div>
              <dl><dt>在线节点</dt><dd>{{online}}</dd><dt>不稳定节点</dt><dd class="amber">{{unstable}}</dd><dt>离线节点</dt><dd class="red">{{offline}}</dd><dt>活跃会话</dt><dd>{{activeSessions.length}}</dd><dt>累计上传</dt><dd>{{formatBytes(totalUpload)}}</dd><dt>累计下载</dt><dd>{{formatBytes(totalDownload)}}</dd></dl>
            </div>
          </section>
          <div class="overview-list"><NodeTable :nodes="nodes" :compact="true" @edit="editNode" @revoke="revokeNode"/><SessionTable :sessions="activeSessions" :nodes="nodes" @disconnect="disconnect"/><LogTable :logs="logs.slice(0,5)"/></div>
        </template>
        <NodeTable v-else-if="page==='nodes'" :nodes="nodes" @edit="editNode" @revoke="revokeNode"/>
        <SessionTable v-else-if="page==='sessions'" :sessions="sessions" :nodes="nodes" @disconnect="disconnect"/>
        <NetworkForm v-else-if="page==='network'" v-model="network" :busy="busy" :saved="saved" :full="true" @save="saveNetwork(false)" @rotate="saveNetwork(true)"/>
        <section v-else class="panel logs-page"><div class="section-title log-filter-title"><h2>事件日志</h2><div class="filters"><input v-model="logFrom" type="datetime-local" title="起始时间"/><input v-model="logTo" type="datetime-local" title="结束时间"/><select v-model="logLevel"><option value="">全部级别</option><option value="INFO">{{levelLabel('INFO')}}</option><option value="WARN">{{levelLabel('WARN')}}</option><option value="ERROR">{{levelLabel('ERROR')}}</option></select><select v-model="logModule"><option value="">全部模块</option><option v-for="module in logModules" :key="module" :value="module">{{moduleLabel(module)}}</option></select><input v-model="logNode" placeholder="节点 ID"/><input v-model="logSession" placeholder="会话 ID"/><button @click="load">查询</button></div></div><LogTable :logs="logs" :bare="true"/></section>
      </div>
    </main>
  </div>
</template>

<style>
.filters{flex-wrap:wrap;justify-content:flex-end}
.filters input[type="datetime-local"]{width:154px}
.filters input{width:105px}
.log-filter-title{min-height:72px;align-items:flex-start;padding-top:12px;gap:12px}
.join-token-result{margin:10px 15px 0;padding:10px;background:#f0f1ff;border:1px solid #ccd2ff;border-radius:6px;display:grid;gap:7px;color:#3438cc;font-size:9px}
.join-token-result input{width:100%;height:30px;border:1px solid #afbaf0;border-radius:5px;padding:0 9px;background:#fff;font-family:"Cascadia Code",monospace;font-size:9px}
.topology-split{grid-column:1/3;display:grid;grid-template-columns:minmax(0,1fr) 220px minmax(0,1fr);align-items:center;min-width:0}
.topology-side{position:relative;align-self:stretch;min-width:0}
.topology-side svg{position:absolute;inset:0;width:100%;height:100%;overflow:visible}
.topology-side line{stroke:#6d78a9;stroke-width:2;vector-effect:non-scaling-stroke}
.topology-node{position:absolute;width:calc(28% - 10px);transform:translateY(-50%);display:grid;gap:2px;line-height:1.15}
.topology-node>i{position:absolute;top:50%;transform:translateY(-50%);width:13px;height:13px;border:2px solid #fff;border-radius:50%;box-shadow:0 0 0 1px #d9deeb}
.engineer-node{right:72%;padding-right:21px;text-align:right}
.engineer-node>i{right:-6px}
.site-node{left:72%;padding-left:21px;text-align:left}
.site-node>i{left:-6px}
.topology-node>b{font-size:9px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.topology-node>em{color:#59647a;font-size:8px;font-style:normal;font-weight:700}
.topology-node>small{color:var(--muted);font-size:9px;white-space:nowrap}
.topology-empty{position:absolute;top:50%;width:100%;margin:0;transform:translateY(-50%);color:var(--muted);font-size:9px;text-align:center}
.overview-list{display:grid;gap:10px}
@media(max-width:1120px){.topology-split{grid-template-columns:minmax(0,1fr) 160px minmax(0,1fr)}}
</style>
