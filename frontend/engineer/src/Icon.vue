<script setup lang="ts">
defineProps<{ name: 'link' | 'session' | 'logs' | 'settings' | 'server' | 'monitor' | 'plus' | 'close' | 'check' }>()
</script>

<template>
  <svg viewBox="0 0 24 24" aria-hidden="true">
    <g fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
      <template v-if="name === 'link'"><circle cx="12" cy="5" r="2.5"/><circle cx="5" cy="18" r="2.5"/><circle cx="19" cy="18" r="2.5"/><path d="M12 7.5v4M7.2 16.4l4.8-4.9 4.8 4.9"/></template>
      <template v-else-if="name === 'session'"><circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3 2"/></template>
      <template v-else-if="name === 'logs'"><path d="M6 3.5h9l3 3V20.5H6z"/><path d="M15 3.5v3h3M9 11h6M9 15h6"/></template>
      <template v-else-if="name === 'settings'"><circle cx="12" cy="12" r="3"/><path d="M12 2.8v2M12 19.2v2M2.8 12h2M19.2 12h2M5.5 5.5l1.4 1.4M17.1 17.1l1.4 1.4M18.5 5.5l-1.4 1.4M6.9 17.1l-1.4 1.4"/></template>
      <template v-else-if="name === 'server'"><rect x="5" y="3.5" width="14" height="7" rx="2"/><rect x="5" y="13.5" width="14" height="7" rx="2"/><path d="M9 7h.01M9 17h.01M13 7h3M13 17h3"/></template>
      <template v-else-if="name === 'monitor'"><rect x="3.5" y="4" width="17" height="12" rx="2"/><path d="M9 20h6M12 16v4"/></template>
      <template v-else-if="name === 'plus'"><path d="M12 5v14M5 12h14"/></template>
      <template v-else-if="name === 'close'"><path d="M6 6l12 12M18 6L6 18"/></template>
      <template v-else><path d="M5 12.5l4.2 4.2L19 7"/></template>
    </g>
  </svg>
</template>
