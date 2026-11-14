<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { NCard, NForm, NFormItem, NInput, NButton, NAlert } from 'naive-ui'
import { useAuthStore } from '@/stores/auth'
import { ApiError } from '@/shared/api/http'

const auth = useAuthStore()
const router = useRouter()

const username = ref('')
const password = ref('')
const errorMsg = ref('')

async function onSubmit() {
  errorMsg.value = ''
  try {
    await auth.login(username.value, password.value)
    await router.push('/crm/calendar')
  } catch (e) {
    errorMsg.value = e instanceof ApiError ? e.message : 'Не удалось выполнить вход'
  }
}
</script>

<template>
  <div class="login-wrap">
    <NCard title="Теплосфера — вход в систему" style="width: 390px">
      <NAlert v-if="errorMsg" type="error" style="margin-bottom: 12px">{{ errorMsg }}</NAlert>
      <NForm @submit.prevent="onSubmit">
        <NFormItem label="Логин">
          <NInput v-model:value="username" placeholder="Логин сотрудника" />
        </NFormItem>
        <NFormItem label="Пароль">
          <NInput v-model:value="password" type="password" show-password-on="click" placeholder="Пароль" />
        </NFormItem>
        <NButton type="primary" attr-type="submit" block :loading="auth.loading">Войти</NButton>
      </NForm>
    </NCard>
  </div>
</template>

<style scoped>
.login-wrap {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #0f172a 0%, #1e293b 100%);
}
</style>
