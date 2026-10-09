<script setup lang="ts">
import { ref, watch } from 'vue'
import Icon from '../Icon.vue'
import { calculateSubnet } from '../subnet'

defineProps<{ canApply: boolean }>()
const emit = defineEmits<{ apply: [cidr: string] }>()
const dialog = ref<HTMLDialogElement | null>(null)
const ip = ref('')
const mask = ref('')
const result = ref('')
const error = ref('')

function open() { dialog.value?.showModal() }
function close() { dialog.value?.close() }
function calculate() {
  result.value = ''
  error.value = ''
  try { result.value = calculateSubnet(ip.value, mask.value) }
  catch (cause) { error.value = cause instanceof Error ? cause.message : String(cause) }
}
function apply() {
  if (!result.value) return
  emit('apply', result.value)
  close()
}
watch([ip, mask], () => { result.value = ''; error.value = '' })
defineExpose({ open })
</script>

<template>
  <dialog ref="dialog" class="subnet-dialog" aria-labelledby="subnet-title" aria-describedby="subnet-description">
    <div class="panel-heading"><h2 id="subnet-title">网段计算器</h2><button type="button" class="icon-button" aria-label="关闭网段计算器" @click="close"><Icon name="close"/></button></div>
    <form class="subnet-form" @submit.prevent="calculate">
      <p id="subnet-description">输入目标设备的 IPv4 地址和子网掩码，计算远程网段中要填写的 CIDR。</p>
      <label for="device-ip">目标设备 IP 地址</label>
      <input id="device-ip" v-model="ip" placeholder="例如 192.168.13.10" autocomplete="off" spellcheck="false" inputmode="decimal" required autofocus />
      <label for="device-mask">子网掩码</label>
      <input id="device-mask" v-model="mask" placeholder="例如 255.255.255.0" autocomplete="off" spellcheck="false" inputmode="decimal" required />
      <p v-if="error" class="subnet-error" role="alert">{{ error }}</p>
      <div v-if="result" class="subnet-result" role="status"><span>配置中填写的网段（CIDR）</span><code>{{ result }}</code><small>填入后点击“添加”，再进行本地冲突预检。</small></div>
      <div class="subnet-actions"><button type="submit" class="secondary">计算网段</button><button type="button" class="primary" :disabled="!result || !canApply" @click="apply">填入远程网段</button></div>
      <p v-if="!canApply" class="subnet-note">选择现场且会话结束后，可将计算结果填入远程网段。</p>
    </form>
  </dialog>
</template>
