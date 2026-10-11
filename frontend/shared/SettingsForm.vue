<script setup lang="ts">
import { reactive, watch } from 'vue'
import type { ClientSettings } from './types'
const props = defineProps<{ value: ClientSettings; role: 'Engineer' | 'Site'; configured: boolean; busy?: boolean }>()
const emit = defineEmits<{ save: [value: ClientSettings] }>()
const form = reactive<ClientSettings>({ ...props.value, netstack: { ...props.value.netstack } })
watch(() => props.value, value => Object.assign(form, value, { netstack: { ...value.netstack } }))
function submit() { emit('save', { ...form, netstack: { ...form.netstack } }) }
</script>

<template>
  <section class="panel client-settings-panel" aria-labelledby="client-settings-title">
    <div class="panel-heading"><h2 id="client-settings-title">{{ configured ? '连接设置' : '先配置，再开始连接' }}</h2></div>
    <p class="first-setup-note" v-if="!configured">这是这台电脑第一次使用 RemLink {{ role }}。填写以下信息后，程序会自动尝试连接服务器。</p>
    <form class="client-settings-form" @submit.prevent="submit">
      <label for="client-name">{{ role }} 名称<input id="client-name" v-model="form.nodeName" required maxlength="128" placeholder="例如：上海工程师电脑 / 青岛现场" :disabled="busy"/><small>这个名称会显示在 Server 的设备列表和日志中。</small></label>
      <label for="client-server">Server 公网地址<input id="client-server" v-model="form.serverURL" type="password" required placeholder="填写服务器的 HTTP 或 HTTPS 地址" autocomplete="off" autocapitalize="none" :spellcheck="false" :disabled="busy"/><small>输入和已保存的地址均隐藏，可以直接修改。</small></label>
      <label for="client-join">Join Token<input id="client-join" v-model="form.joinToken" type="password" :required="!configured" placeholder="填写服务器提供的注册令牌" autocomplete="off" autocapitalize="none" :spellcheck="false" :disabled="busy"/><small>首次注册、修改服务器或设备名称时，需要有效的 Join Token。</small></label>
      <p class="settings-save-note">设置保存在这台电脑上，更换软件包后会继续使用。</p>
      <button class="primary" type="submit" :disabled="busy">{{ busy ? '正在保存…' : '保存并连接' }}</button>
    </form>
  </section>
</template>
