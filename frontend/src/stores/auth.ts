import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { requestJSON } from '@/shared/api/http'
import type { User } from '@/shared/types/domain'

interface MeResponse {
  user: User
  permissions: string[]
}

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(null)
  const permissions = ref<string[]>([])
  const initialized = ref(false)
  const loading = ref(false)

  const isAuthenticated = computed(() => user.value !== null)
  const isSuperAdmin = computed(() => Boolean(user.value?.roles?.includes('superadmin')))

  function hasPerm(perm: string): boolean {
    if (!user.value) return false
    if (isSuperAdmin.value) return true
    return permissions.value.includes(perm)
  }

  async function fetchMe(): Promise<boolean> {
    try {
      const data = await requestJSON<MeResponse>('/api/v1/me')
      user.value = data.user
      permissions.value = data.permissions ?? []
      initialized.value = true
      return true
    } catch {
      user.value = null
      permissions.value = []
      initialized.value = true
      return false
    }
  }

  async function login(username: string, password: string): Promise<void> {
    loading.value = true
    try {
      const res = await requestJSON<MeResponse>('/api/v1/auth/login', {
        method: 'POST',
        body: JSON.stringify({ username, password })
      })
      user.value = res.user
      permissions.value = res.permissions ?? []
      initialized.value = true
    } finally {
      loading.value = false
    }
  }

  async function logout(): Promise<void> {
    try {
      await requestJSON('/api/v1/auth/logout', { method: 'POST' })
    } finally {
      user.value = null
      permissions.value = []
    }
  }

  return {
    user,
    permissions,
    initialized,
    loading,
    isAuthenticated,
    isSuperAdmin,
    hasPerm,
    fetchMe,
    login,
    logout
  }
})
