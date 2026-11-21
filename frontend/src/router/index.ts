import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    redirect: '/crm/calendar'
  },
  {
    path: '/login',
    name: 'login',
    meta: { public: true, title: 'Вход — Теплосфера' },
    component: () => import('@/modules/users/LoginView.vue')
  },
  {
    path: '/chat/shared/:token',
    name: 'chat-shared',
    meta: { public: true, title: 'Публичный диалог — Теплосфера' },
    component: () => import('@/modules/chat/ChatWorkspaceView.vue')
  },
  {
    path: '/crm/calendar',
    name: 'crm-calendar',
    meta: { perm: 'crm.application.view', title: 'Календарь заявок' },
    component: () => import('@/modules/crm/CalendarView.vue')
  },
  {
    path: '/crm/applications',
    name: 'crm-applications',
    meta: { perm: 'crm.application.view', title: 'Заявки и Акты ТО' },
    component: () => import('@/modules/crm/ApplicationsAndActsView.vue')
  },
  {
    path: '/crm/contractors',
    name: 'crm-contractors',
    meta: { perm: 'crm.contractor.view', title: 'Контрагенты и Адреса' },
    component: () => import('@/modules/crm/ContractorsView.vue')
  },
  {
    path: '/crm/timesheet',
    name: 'crm-timesheet',
    meta: { title: 'Табель и план' },
    component: () => import('@/modules/crm/TimesheetAndPlanView.vue')
  },
  {
    path: '/crm/operations',
    name: 'crm-operations',
    meta: { title: 'Договоры, Задачи, Звонки и Отчёты' },
    component: () => import('@/modules/crm/ContractsAndTasksView.vue')
  },
  {
    path: '/docs',
    name: 'docs',
    meta: { perm: 'docs.view', title: 'Документация и Регламенты' },
    component: () => import('@/modules/docs/DocsWorkspaceView.vue')
  },
  {
    path: '/chat',
    name: 'chat',
    meta: { perm: 'chat.use', title: 'Чат с ИИ-ассистентом' },
    component: () => import('@/modules/chat/ChatWorkspaceView.vue')
  },
  {
    path: '/ai',
    name: 'ai',
    meta: { title: 'ИИ: Поиск, Генерация, Модели' },
    component: () => import('@/modules/ai/AiSettingsAndComposeView.vue')
  },
  {
    path: '/admin/users',
    name: 'admin-users',
    meta: { perm: 'admin.user.view', title: 'Пользователи и RBAC' },
    component: () => import('@/modules/users/UsersAdminView.vue')
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: '/crm/calendar'
  }
]

export const router = createRouter({
  history: createWebHistory(),
  routes
})

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  if (!auth.initialized) {
    await auth.fetchMe()
  }

  if (to.meta.public) {
    if (to.name === 'login' && auth.isAuthenticated) {
      return '/crm/calendar'
    }
    return true
  }

  if (!auth.isAuthenticated) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }

  const reqPerm = to.meta.perm as string | undefined
  if (reqPerm && !auth.hasPerm(reqPerm)) {
    return '/docs'
  }

  if (typeof document !== 'undefined' && to.meta.title) {
    document.title = `${String(to.meta.title)} — Теплосфера`
  }
  return true
})

export default router
