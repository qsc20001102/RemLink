<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
const props = defineProps<{ open: boolean; busy?: boolean }>()
const emit = defineEmits<{ cancel: []; tray: []; quit: [] }>()
const dialog = ref<HTMLElement | null>(null)
let previousFocus: HTMLElement | null = null
watch(() => props.open, async open => { if (open) { previousFocus = document.activeElement as HTMLElement; await nextTick(); dialog.value?.focus() } else { previousFocus?.focus() } })
function keydown(event: KeyboardEvent) {
  if (event.key === 'Escape') { emit('cancel'); return }
  if (event.key !== 'Tab') return
  const buttons = Array.from(dialog.value?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') || [])
  if (!buttons.length) return
  const first = buttons[0], last = buttons[buttons.length - 1]
  if (event.shiftKey && (document.activeElement === first || document.activeElement === dialog.value)) { event.preventDefault(); last.focus() }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
}
</script>
<template>
  <div v-if="open" class="desktop-modal-backdrop">
    <section ref="dialog" class="desktop-close-dialog" role="dialog" aria-modal="true" aria-labelledby="close-dialog-title" tabindex="-1" @keydown="keydown">
      <h2 id="close-dialog-title">关闭窗口后如何运行？</h2><p>隐藏到系统托盘会保持后台连接。直接退出会断开当前连接并关闭程序。</p>
      <div><button class="secondary" :disabled="busy" @click="$emit('cancel')">取消</button><button class="primary" :disabled="busy" @click="$emit('tray')">隐藏到托盘</button><button class="secondary danger-action" :disabled="busy" @click="$emit('quit')">直接退出</button></div>
    </section>
  </div>
</template>
