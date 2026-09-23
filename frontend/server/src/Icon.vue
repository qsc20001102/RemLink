<script setup lang="ts">defineProps<{name:string}>()</script>
<template><svg viewBox="0 0 24 24" aria-hidden="true"><g fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
  <template v-if="name==='overview'"><path d="M3.5 11L12 4l8.5 7v9H14v-5h-4v5H3.5z"/></template>
  <template v-else-if="name==='nodes'"><circle cx="12" cy="5" r="2.2"/><circle cx="5" cy="18" r="2.2"/><circle cx="19" cy="18" r="2.2"/><path d="M12 7.2v4M7 16.5l5-5 5 5"/></template>
  <template v-else-if="name==='sessions'"><rect x="4" y="4" width="16" height="13" rx="2"/><path d="M8 20h8M12 17v3M8 9h8M8 12h5"/></template>
  <template v-else-if="name==='network'"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c3 3.2 3 14.8 0 18M12 3c-3 3.2-3 14.8 0 18"/></template>
  <template v-else-if="name==='logs'"><path d="M6 3.5h9l3 3V20.5H6zM15 3.5v3h3M9 11h6M9 15h6"/></template>
  <template v-else-if="name==='edit'"><path d="M4 20l4.2-1 10.5-10.5-3.2-3.2L5 15.8zM13.8 7l3.2 3.2"/></template>
  <template v-else-if="name==='trash'"><path d="M4 7h16M9 7V4h6v3M7 7l1 13h8l1-13M10 11v5M14 11v5"/></template>
  <template v-else-if="name==='plus'"><circle cx="12" cy="12" r="9"/><path d="M12 8v8M8 12h8"/></template>
  <template v-else-if="name==='engineer'"><rect x="3.5" y="4.5" width="17" height="11" rx="1.8"/><path d="M2.5 19.5h19l-1.5-4h-16zM9 19.5h6M9 8.5c1.7-1.6 4.3-1.6 6 0M10.7 10.3c.7-.6 1.9-.6 2.6 0M12 12h.01"/></template>
  <template v-else-if="name==='site'"><path d="M3.5 20V10.5l5 2.5V9.5l5 2.5V7l7 3.5V20zM3.5 20h17M7 16h.01M11.5 16h.01M16 16h.01M16 3.5c1.7-1.3 3.4-1.3 5 0M17 5.7c1-.7 2-.7 3 0"/></template>
  <template v-else-if="name==='server'"><rect x="4" y="3.5" width="16" height="5" rx="1.5"/><rect x="4" y="9.5" width="16" height="5" rx="1.5"/><rect x="4" y="15.5" width="16" height="5" rx="1.5"/><path d="M7.5 6h.01M7.5 12h.01M7.5 18h.01M12 6h5M12 12h5M12 18h5"/></template>
  <template v-else-if="name==='arrow'"><path d="M5 12h14m-6-6 6 6-6 6"/></template>
  <template v-else><path d="M5 12.5l4.2 4.2L19 7"/></template>
</g></svg></template>
