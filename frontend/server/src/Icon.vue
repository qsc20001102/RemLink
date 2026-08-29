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
  <template v-else><path d="M5 12.5l4.2 4.2L19 7"/></template>
</g></svg></template>
