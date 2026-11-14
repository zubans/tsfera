import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { NSelect } from 'naive-ui'
import { useAuthStore } from '@/stores/auth'
import ReportsPanel from '@/modules/crm/ReportsPanel.vue'
import CrmCatalogsPanel from '@/modules/crm/CrmCatalogsPanel.vue'

const enums = { work_types: [], document_statuses: [], task_types: [], task_statuses: [] }
describe('granular migrated permissions in UI', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    useAuthStore().user = {
      id: 1, username: 'imported', first_name: '', second_name: '', last_name: '', full_name: '',
      is_engineer: false, is_dispatcher: false, color: '', corporate_phone_number: '', active: true, roles: ['legacy_1']
    }
    vi.restoreAllMocks()
    vi.spyOn(globalThis, 'fetch').mockImplementation(async () => new Response('[]', { headers: { 'Content-Type': 'application/json' } }))
  })
  it('shows only the permitted report and requests that report', async () => {
    useAuthStore().permissions = ['crm.report.amount_spare_parts.view']
    const wrapper = mount(ReportsPanel, { props: { subdivisions: [], workers: [], enums } })
    await flushPromises()
    expect(wrapper.findComponent(NSelect).props('options')).toEqual([
      expect.objectContaining({ value: 'amount-spare-parts' })
    ])
    const requests = vi.mocked(fetch).mock.calls.map(([url]) => String(url))
    expect(requests.length).toBeGreaterThan(0)
    expect(requests.every(url => url.includes('/reports/amount-spare-parts'))).toBe(true)
    wrapper.unmount()
  })
  it('does not load reports when no report permission exists', async () => {
    useAuthStore().permissions = ['crm.application.view']
    const wrapper = mount(ReportsPanel, { props: { subdivisions: [], workers: [], enums } })
    await flushPromises()
    expect(fetch).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('catalogue create permission does not expose other catalogue writes', async () => {
    useAuthStore().permissions = ['crm.banner.create']
    const wrapper = mount(CrmCatalogsPanel)
    await flushPromises()
    const buttons = wrapper.findAll('button').map(button => button.text())
    expect(buttons).toContain('+ Баннер')
    expect(buttons).not.toContain('+ Источник')
    expect(buttons).not.toContain('+ Вопрос')
    wrapper.unmount()
  })
})
