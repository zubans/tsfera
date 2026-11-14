<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, RouterView } from 'vue-router'
import { NConfigProvider, NMessageProvider, ruRU, dateRuRU } from 'naive-ui'
import AppShell from '@/components/AppShell.vue'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const auth = useAuthStore()

const showShell = computed(() => !route.meta.public && auth.isAuthenticated)
</script>

<template>
  <NConfigProvider :locale="ruRU" :date-locale="dateRuRU">
    <NMessageProvider>
      <AppShell v-if="showShell">
        <RouterView />
      </AppShell>
      <RouterView v-else />
    </NMessageProvider>
  </NConfigProvider>
</template>

<style>
* {
  box-sizing: border-box;
}
body {
  margin: 0;
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
  background: #f8fafc;
  color: #0f172a;
}
</style>
