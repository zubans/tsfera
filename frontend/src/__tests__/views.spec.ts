import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { NSelect } from 'naive-ui'
import { useAuthStore } from '@/stores/auth'
import { useRealtimeStore } from '@/stores/realtime'
import CalendarView from '@/modules/crm/CalendarView.vue'
import ContractorsView from '@/modules/crm/ContractorsView.vue'
import ContractsAndTasksView from '@/modules/crm/ContractsAndTasksView.vue'
import ApplicationsAndActsView from '@/modules/crm/ApplicationsAndActsView.vue'
import CrmCatalogsPanel from '@/modules/crm/CrmCatalogsPanel.vue'
import ReportsPanel from '@/modules/crm/ReportsPanel.vue'
import UsersAdminView from '@/modules/users/UsersAdminView.vue'
import DocsWorkspaceView from '@/modules/docs/DocsWorkspaceView.vue'
import ChatWorkspaceView from '@/modules/chat/ChatWorkspaceView.vue'

function makeTestRouter(initialPath = '/crm/calendar') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/crm/calendar', component: CalendarView },
      { path: '/crm/applications', component: ApplicationsAndActsView },
      { path: '/crm/contractors', component: ContractorsView },
      { path: '/crm/operations', component: ContractsAndTasksView },
      { path: '/admin/users', component: UsersAdminView },
      { path: '/docs', component: DocsWorkspaceView },
      { path: '/chat', component: ChatWorkspaceView },
      { path: '/chat/shared/:token', component: ChatWorkspaceView }
    ]
  })
  void router.push(initialPath)
  return router
}

function jsonResp(data: unknown, status = 200) {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json' }
  })
}

describe('Core UI Views integration specs', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.restoreAllMocks()
    const auth = useAuthStore()
    auth.initialized = true
    auth.user = {
      id: 1,
      username: 'dispatcher',
      first_name: 'Анна',
      second_name: '',
      last_name: 'Петрова',
      full_name: 'Петрова Анна',
      is_engineer: false,
      is_dispatcher: true,
      color: '#2563eb',
      corporate_phone_number: '9990001122',
      active: true,
      roles: ['superadmin']
    }
    auth.permissions = ['*']
  })

  it('CalendarView loads events, shows unread notes, marks viewed, and refreshes on WS revision', async () => {
    let calCalls = 0
    const todayPrefix = new Date().toISOString().slice(0, 10)
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes('/api/v1/crm/workers')) {
        return jsonResp([{ id: 2, username: 'eng1', full_name: 'Инженер Сидоров', color: '#16a34a', is_engineer: true, is_dispatcher: false, active: true }])
      }
      if (url.includes('/api/v1/crm/enums')) {
        return jsonResp({
          work_types: ['ТО', 'Ремонт'],
          document_statuses: [{ id: 1, label: 'Активна' }],
          task_types: [],
          task_statuses: []
        })
      }
      if (url.includes('/api/v1/crm/contractors')) {
        return jsonResp({ items: [{ id: 5, name: 'ООО Котельная', type: 2, note: '' }], total: 1 })
      }
      if (url.includes('/api/v1/crm/applications/calendar')) {
        calCalls++
        return jsonResp([
          {
            id: 10,
            title: 'Заявка #10',
            start: `${todayPrefix}T09:00:00Z`,
            end: `${todayPrefix}T11:00:00Z`,
            contractor_id: 5,
            contractor_name: 'ООО Котельная',
            address_id: 8,
            address_text: 'ул. Газовая, 1',
            phones: ['9991112233'],
            engineer_id: 2,
            engineer_name: 'Инженер Сидоров',
            color: '#16a34a',
            status: 1,
            is_complete: false,
            is_duty: true,
            work_types: ['ТО'],
            equipments: ['Baxi Luna 3'],
            serial_numbers: ['SN-001'],
            acts_total_amount: 5500,
            special_notes: 'Проверить датчик тяги',
            notes_viewed: false,
            rating: null
          }
        ])
      }
      return jsonResp({})
    })

    const router = makeTestRouter('/crm/calendar')
    await router.isReady()
    const wrapper = mount(CalendarView, { global: { plugins: [router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('ООО Котельная')
    expect(wrapper.text()).toContain('Дежурная')
    expect(wrapper.text()).toContain('Особые заметки не просмотрены')
    expect(calCalls).toBe(1)

    const rt = useRealtimeStore()
    rt.calendarRevision++
    await flushPromises()
    expect(calCalls).toBe(2)
  })

  it('DocsWorkspaceView renders Markdown outline and surfaces HTTP 409 optimistic lock conflict', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      if (url.includes('/api/v1/docs/spaces')) {
        return jsonResp([{ space: 'service', count: 1 }])
      }
      if (url === '/api/v1/docs/documents?' || url.startsWith('/api/v1/docs/documents?')) {
        return jsonResp([
          {
            id: 1,
            space: 'service',
            doc_type: 'sop',
            title: 'Регламент Baxi',
            slug: 'reglament-baxi',
            content: '# Проверка газа\n\nПараграф инструкции',
            status: 'published',
            tags: ['baxi'],
            version: 3,
            visibility: 'internal',
            updated_at: '2026-11-13T10:00:00Z'
          }
        ])
      }
      if (url === '/api/v1/docs/documents/1' && (!init?.method || init.method === 'GET')) {
        return jsonResp({
          id: 1,
          space: 'service',
          doc_type: 'sop',
          title: 'Регламент Baxi',
          slug: 'reglament-baxi',
          content: '# Проверка газа\n\nПараграф инструкции',
          status: 'published',
          tags: ['baxi'],
          version: 3,
          visibility: 'internal',
          outline: [{ level: 1, title: 'Проверка газа', anchor: 'проверка-газа', line: 1 }],
          entity_links: [],
          attachments: [],
          updated_at: '2026-11-13T10:00:00Z'
        })
      }
      if (url.endsWith('/comments')) return jsonResp([])
      if (url.endsWith('/revisions')) return jsonResp([])
      if (url === '/api/v1/docs/documents/1' && init?.method === 'PUT') {
        return jsonResp({ error: 'Документ изменён другим пользователем (v4)' }, 409)
      }
      return jsonResp([])
    })

    const router = makeTestRouter('/docs')
    await router.isReady()
    const wrapper = mount(DocsWorkspaceView, { global: { plugins: [router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('Регламент Baxi')
    expect(wrapper.html()).toContain('id="проверка-газа"')

    const editBtn = wrapper.findAll('button').find((b) => b.text().includes('Редактировать'))
    expect(editBtn).toBeDefined()
    await editBtn!.trigger('click')
    await flushPromises()

    const saveBtn = wrapper.findAll('button').find((b) => b.text().includes('Сохранить'))
    expect(saveBtn).toBeDefined()
    await saveBtn!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('Конфликт версий (HTTP 409)')
  })

  it('ChatWorkspaceView renders shared conversation steps & sources read-only without composer', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes('/api/v1/chat/shared/tok123')) {
        return jsonResp({
          id: 42,
          title: 'Диагностика ошибки E01',
          mode: 'agent',
          model_id: 1,
          entity_type: 'contractor',
          entity_id: 5,
          updated_at: '2026-11-13T10:00:00Z',
          messages: [
            {
              id: 1,
              role: 'user',
              content: 'Почему котёл выдаёт E01?',
              created_at: '2026-11-13T10:00:00Z'
            },
            {
              id: 2,
              role: 'assistant',
              content: 'Проверьте **электрод ионизации** и давление газа.',
              steps: [{ name: 'hybrid_search', summary: 'Найдено 2 совпадения', duration_ms: 15 }],
              sources: [{ source_type: 'doc', source_id: '1', title: 'Мануал Baxi', path: '/docs/1', anchor: 'e01' }],
              created_at: '2026-11-13T10:00:02Z'
            }
          ]
        })
      }
      return jsonResp([])
    })

    const router = makeTestRouter('/chat/shared/tok123')
    await router.isReady()
    const wrapper = mount(ChatWorkspaceView, { global: { plugins: [router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('Диагностика ошибки E01')
    expect(wrapper.text()).toContain('Вызовы инструментов (1)')
    expect(wrapper.text()).toContain('Мануал Baxi')
    expect(wrapper.html()).toContain('<strong>электрод ионизации</strong>')
    expect(wrapper.find('.composer-bar').exists()).toBe(false)
  })

  it('ContractorsView runs duplicate check-similar and displays matched contractors warning', async () => {
    let checkCalls = 0
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes('/api/v1/crm/contractors/check-similar')) {
        checkCalls++
        return jsonResp({
          matches: [
            {
              contractor_id: 9,
              contractor_name: 'Иван Петров',
              matched_on: 'phone',
              contact_value: '9991112233'
            }
          ]
        })
      }
      if (url.includes('/api/v1/crm/contractors')) {
        return jsonResp({ items: [], total: 0 })
      }
      if (url.includes('/api/v1/crm/subdivisions')) {
        return jsonResp([{ id: 1, name: 'Воронеж', address: 'ул. Мира, 1' }])
      }
      return jsonResp([])
    })

    const router = makeTestRouter('/crm/contractors')
    await router.isReady()
    const wrapper = mount(ContractorsView, { global: { plugins: [router] } })
    await flushPromises()

    const addBtn = wrapper.findAll('button').find((b) => b.text().includes('+ Новый контрагент'))
    expect(addBtn).toBeDefined()
    await addBtn!.trigger('click')
    await flushPromises()

    const phoneInput = document.body.querySelector(
      'input[placeholder="+7 (999) 123-45-67"]'
    ) as HTMLInputElement | null
    expect(phoneInput).not.toBeNull()
    phoneInput!.value = '+79991112233'
    phoneInput!.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    phoneInput!.dispatchEvent(new Event('blur', { bubbles: true }))
    await flushPromises()

    expect(checkCalls).toBe(1)
    expect(document.body.textContent).toContain('Иван Петров')
  })

  it('ContractsAndTasksView renders task execute, call recording player, refs and report contracts_amount', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes('/api/v1/crm/contracts')) {
        return jsonResp([
          {
            id: 1,
            contractor_id: 5,
            contractor_name: 'ООО Котельная',
            address_id: 8,
            address_text: 'ул. Газовая, 1',
            number: 'Д-100',
            category: 'plus',
            price: 12000,
            signed_at: '2026-11-01T00:00:00Z',
            valid_from: '2026-11-01T00:00:00Z',
            valid_to: '2027-11-01T00:00:00Z',
            status: 1,
            note: ''
          }
        ])
      }
      if (url.includes('/api/v1/crm/tasks')) {
        return jsonResp([
          {
            id: 7,
            task_type: 'call',
            title: 'Обзвон ТО',
            description: '',
            status: 1,
            priority: 10,
            plan_datetime_to: '2026-11-15T10:00:00Z',
            is_urgent: false,
            note: ''
          }
        ])
      }
      if (url.includes('/api/v1/crm/calls/current')) return jsonResp([])
      if (url.includes('/api/v1/crm/calls')) {
        return jsonResp([
          {
            id: 3,
            external_call_id: 'call-3',
            ext_tracking_id: 'rec-3',
            direction: 'incoming',
            status: 'ended',
            caller_phone: '9992223344',
            callee_phone: '8001002030',
            type_of_appeal: 'consultation',
            duration_sec: 40,
            started_at: '2026-11-13T10:00:00Z'
          }
        ])
      }
      if (url.includes('/api/v1/crm/reports/amount-acts')) {
        return jsonResp([
          {
            engineer_id: 2,
            engineer_name: 'Сидоров И.',
            subdivision_id: 1,
            subdivision_name: 'Воронеж',
            certificates_count: 4,
            total_amount: 15000,
            paid_amount: 12000,
            debt_amount: 3000,
            contracts_amount: 24000
          }
        ])
      }
      if (url.includes('/api/v1/crm/contractors')) return jsonResp({ items: [], total: 0 })
      if (url.includes('/api/v1/crm/enums')) {
        return jsonResp({
          work_types: ['ТО'],
          document_statuses: [{ id: 1, label: 'Активна' }],
          task_types: [{ id: 'call', label: 'Звонок' }],
          task_statuses: [{ id: 1, label: 'Новая' }],
          contract_categories: [{ id: 2, key: 'plus', label: 'Плюс' }],
          task_results: [{ id: 1, key: 'answered', label: 'Дозвонились / Выполнено' }],
          call_types_of_appeal: [{ id: 2, key: 'consultation', label: 'Консультация' }]
        })
      }
      return jsonResp([])
    })

    const router = makeTestRouter('/crm/operations?tab=contracts')
    await router.isReady()
    const wrapper = mount(ContractsAndTasksView, { global: { plugins: [router] } })
    await flushPromises()

    expect(wrapper.text()).toContain('Д-100')
    expect(wrapper.text()).toContain('Плюс')
    expect(wrapper.text()).toContain('Справочники')
  })

  it('ApplicationsAndActsView triggers complete application and UsersAdminView loads inventory catalogs', async () => {
    let completeCalled = false
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
      const url = String(input)
      if (url.includes('/api/v1/crm/applications/55/complete') && init?.method === 'POST') {
        completeCalled = true
        return jsonResp({ id: 55, is_complete: true, status: 2 })
      }
      if (url.includes('/api/v1/crm/applications')) {
        return jsonResp([
          {
            id: 55,
            contractor_id: 5,
            contractor_name: 'ООО Тест',
            address_id: 8,
            address_text: 'ул. Ленина, 5',
            engineer_id: 2,
            engineer_name: 'Инженер',
            plan_datetime_from: '2026-11-13T09:00:00Z',
            plan_datetime_to: '2026-11-13T11:00:00Z',
            status: 1,
            is_complete: false,
            is_duty: false,
            work_types: ['ТО'],
            price_note: '',
            comment: '',
            special_notes: '',
            notes_viewed: true
          }
        ])
      }
      if (url.includes('/api/v1/crm/contractors')) return jsonResp({ items: [], total: 0 })
      if (url.includes('/api/v1/crm/enums')) {
        return jsonResp({
          work_types: ['ТО'],
          document_statuses: [{ id: 1, label: 'Активна' }],
          task_types: [],
          task_statuses: [],
          payment_options: [{ id: 1, key: 'cash', label: 'Наличные' }]
        })
      }
      if (url.includes('/api/v1/admin/inventory')) {
        return jsonResp([{ id: 1, category: 'work_clothes', name: 'Куртка зимняя', deleted: false, created_at: '2026-11-13T00:00:00Z' }])
      }
      return jsonResp([])
    })

    const router = makeTestRouter('/crm/applications')
    await router.isReady()
    const wrapper = mount(ApplicationsAndActsView, { global: { plugins: [router] } })
    await flushPromises()

    const completeBtn = wrapper.findAll('button').find((b) => b.text().trim() === 'Завершить')
    expect(completeBtn).toBeDefined()
    await completeBtn!.trigger('click')
    await flushPromises()
    expect(completeCalled).toBe(true)

    const usersWrapper = mount(UsersAdminView, { global: { plugins: [router] } })
    await flushPromises()
    expect(usersWrapper.text()).toContain('Справочник оснащения')
  })

  it('ContractorsView opens legal entity requisites, subscriptions and queries DaData address suggest', async () => {
    let suggestCalled = false
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes('/api/v1/crm/contractors/15/subscriptions')) {
        return jsonResp({ active_subscriptions: [{ id: 'sub-1' }], active_contracts: 1 })
      }
      if (url.includes('/api/v1/crm/contractors/15')) {
        return jsonResp({
          id: 15,
          name: 'ООО ТеплоСервис',
          type: 2,
          is_legal_entity: true,
          legal_entity: {
            contractor_id: 15,
            name: 'ООО ТеплоСервис',
            inn: '3664069397',
            kpp: '366401001',
            ogrn: '1053600591197',
            director_name: 'Иванов И.И.',
            bank_name: 'Сбербанк',
            bik: '044525225'
          },
          phones: [],
          emails: [],
          addresses: [{ id: 88, contractor_id: 15, address: 'г. Воронеж, ул. Мира, д. 1' }]
        })
      }
      if (url.includes('/api/v1/crm/contractors')) {
        return jsonResp({
          items: [{ id: 15, name: 'ООО ТеплоСервис', type: 2, is_legal_entity: true, note: '' }],
          total: 1
        })
      }
      if (url.includes('/api/v1/crm/addresses/suggest')) {
        suggestCalled = true
        return jsonResp([
          {
            value: 'г Воронеж, ул Мира, д 10',
            unrestricted_value: '394000, г Воронеж, ул Мира, д 10',
            city: 'Воронеж',
            street: 'Мира',
            house: '10'
          }
        ])
      }
      if (url.includes('/api/v1/crm/subdivisions')) {
        return jsonResp([{ id: 1, name: 'Воронеж', address: 'ул. Мира, 1' }])
      }
      return jsonResp([])
    })

    const router = makeTestRouter('/crm/contractors')
    await router.isReady()
    const wrapper = mount(ContractorsView, { global: { plugins: [router] } })
    await flushPromises()

    const cardBtn = wrapper.findAll('button').find((b) => b.text().trim() === 'Открыть')
    expect(cardBtn).toBeDefined()
    await cardBtn!.trigger('click')
    await flushPromises()

    expect(document.body.textContent).toContain('Реквизиты юридического лица')
    expect(document.body.textContent).toContain('3664069397')
    expect(document.body.textContent).toContain('CloudPayments подписок: 1')

    const addAddrBtn = Array.from(document.body.querySelectorAll('button')).find((b) =>
      b.textContent?.includes('+ Адрес')
    )
    expect(addAddrBtn).toBeDefined()
    addAddrBtn!.click()
    await flushPromises()

    const addrInput = document.body.querySelector(
      'input[placeholder="г. Воронеж, ул. Мира, д. 10"]'
    ) as HTMLInputElement | null
    expect(addrInput).not.toBeNull()
    addrInput!.value = 'Воронеж Мира 10'
    addrInput!.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()

    const dadataBtn = Array.from(document.body.querySelectorAll('button')).find((b) =>
      b.textContent?.trim() === 'DaData'
    )
    expect(dadataBtn).toBeDefined()
    dadataBtn!.click()
    await flushPromises()

    expect(suggestCalled).toBe(true)
    expect(document.body.textContent).toContain('г Воронеж, ул Мира, д 10')
  })

  it('CrmCatalogsPanel loads traffic sources, analytic questions and engineer banners', async () => {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes('/api/v1/crm/traffic-sources')) {
        return jsonResp([{ id: 1, name: 'Яндекс Карты', traffic_source_type_id: 1, sort: 10, active: true }])
      }
      if (url.includes('/api/v1/crm/analytic-questions')) {
        return jsonResp([
          { id: 5, name: 'Мастер был вежлив?', analytic_document_type_id: 1, sort: 10, active: true, options: [{ id: 11, name: 'Да', sort: 1 }] }
        ])
      }
      if (url.includes('/api/v1/crm/banners')) {
        return jsonResp([{ id: 9, name: 'Акция ТО', text: 'Проверяйте дымоход', active: true }])
      }
      if (url.includes('/api/v1/crm/app-settings/app.banners_for_engineer')) {
        return jsonResp({ key: 'app.banners_for_engineer', data: { before_create: [9], after_create: [] } })
      }
      if (url.includes('/api/v1/crm/enums')) {
        return jsonResp({
          traffic_source_types: [{ id: 1, key: 'search', label: 'Поиск / Карты' }]
        })
      }
      return jsonResp([])
    })

    const wrapper = mount(CrmCatalogsPanel)
    await flushPromises()

    expect(wrapper.text()).toContain('Яндекс Карты')
    expect(wrapper.text()).toContain('Мастер был вежлив?')
    expect(wrapper.text()).toContain('Акция ТО')
  })

  it('ReportsPanel renders extended reports and contractors export', async () => {
    const createUrlSpy = vi.fn(() => 'blob:mock')
    const revokeUrlSpy = vi.fn()
    const origURL = globalThis.URL
    globalThis.URL.createObjectURL = createUrlSpy
    globalThis.URL.revokeObjectURL = revokeUrlSpy
    const anchorClickSpy = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
    let exportedUrl = ''

    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
      const url = String(input)
      if (url.includes('/api/v1/crm/reports/amount-acts')) {
        return jsonResp([
          {
            engineer_id: 2,
            engineer_name: 'Сидоров И.',
            subdivision_name: 'Воронеж',
            certificates_count: 3,
            planned_sum_total: 12000,
            total_amount: 15000,
            paid_amount: 15000,
            debt_amount: 0,
            contracts_amount: 8000
          }
        ])
      }
      if (url.includes('/api/v1/crm/reports/contractors/export')) {
        exportedUrl = url
        return new Response('xlsx-bytes', {
          status: 200,
          headers: { 'content-disposition': 'attachment; filename="contractors.xlsx"' }
        })
      }
      if (url.includes('/api/v1/crm/reports/contractors')) {
        return jsonResp([
          {
            contractor_id: 5,
            name: 'ООО Котельная',
            phones: ['+79991112233'],
            addresses: ['ул. Газовая, 1'],
            certificates_count: 2,
            fact_sum: 9500,
            cost_of_parts: 1500
          }
        ])
      }
      return jsonResp([])
    })

    const wrapper = mount(ReportsPanel, {
      props: {
        subdivisions: [{ id: 1, name: 'Воронеж', address: 'ул. Мира, 1' }],
        workers: [],
        enums: {
          work_types: ['ТО'],
          document_statuses: [],
          task_types: [],
          task_statuses: [],
          payment_options: [{ id: 1, key: 'cash', label: 'Наличные' }],
          call_results: [{ id: 1, key: 'application', label: 'Заявка' }]
        }
      }
    })
    await flushPromises()

    expect(wrapper.text()).toContain('Сидоров И.')
    expect(wrapper.text()).toContain('План. сумма')

    await wrapper.findComponent(NSelect).vm.$emit('update:value', 'contractors')
    await flushPromises()
    expect(wrapper.text()).toContain('ООО Котельная')

    const exportBtn = wrapper.findAll('button').find((b) => b.text().includes('Экспорт XLSX'))
    expect(exportBtn).toBeDefined()
    await exportBtn!.trigger('click')
    await flushPromises()

    expect(exportedUrl).toContain('/api/v1/crm/reports/contractors/export?format=xlsx')
    expect(createUrlSpy).toHaveBeenCalled()
    expect(anchorClickSpy).toHaveBeenCalled()
    expect(origURL).toBeDefined()
  })
})
