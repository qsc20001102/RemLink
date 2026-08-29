<script setup lang="ts">
import { statusLabel } from '../zh-cn'
defineProps<{ status: string }>()
</script>

<template>
  <span class="status"><i :class="status.toLowerCase()"></i>{{ statusLabel(status) }}</span>
</template>
