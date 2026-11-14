<script setup lang="ts">
import { ref, watch, onUnmounted } from 'vue'
import { NModal, NSpace, NInput, NButton, NAlert } from 'naive-ui'
import { useRealtimeStore } from '@/stores/realtime'
import { useSoftPhone } from '@/shared/composables/useSoftPhone'

const show = defineModel<boolean>('show', { default: false })
const realtime = useRealtimeStore()
const phone = useSoftPhone()
const { registered, status, error, incoming, peer, session, remoteAudio } = phone
const number = ref('')
watch(() => JSON.stringify(realtime.softPhone), () => {
  const config = realtime.softPhone
  if (config.enabled) phone.connect(config)
  else phone.disconnect()
}, { immediate: true })
watch(incoming, value => { if (value) show.value = true })
onUnmounted(phone.disconnect)
</script>

<template>
  <NModal v-model:show="show" preset="card" title="Телефон" style="width: 460px">
    <NSpace vertical>
      <div>{{ status }}<span v-if="peer"> — {{ peer }}</span></div>
      <NAlert v-if="error" type="error">{{ error }}</NAlert>
      <NInput v-model:value="number" placeholder="Телефон или внутренний номер" :disabled="!!session" />
      <NSpace>
        <NButton v-if="!registered" @click="phone.connect(realtime.softPhone)">Подключить</NButton>
        <NButton :disabled="!registered || !!session" @click="phone.call(number)">Позвонить</NButton>
        <NButton v-if="incoming" type="primary" @click="phone.answer">Ответить</NButton>
        <NButton v-if="session" type="error" @click="phone.hangup">Завершить</NButton>
        <NButton v-if="registered && !session" @click="phone.disconnect">Отключить</NButton>
      </NSpace>
    </NSpace>
  </NModal>
  <!-- Плеер остаётся смонтированным при закрытии окна во время разговора. -->
  <audio ref="remoteAudio" autoplay controls v-show="!!session" aria-label="Звук звонка" />
</template>
