import { mount } from '@vue/test-utils'
import { describe, it, expect } from 'vitest'
import BusinessDataDetails from '../modules/crm/BusinessDataDetails.vue'
import type { Application } from '../shared/types/domain'

describe('Imported business data', () => {
  it('shows incomplete source values, business fields and unresolved links without invented values', () => {
    const record = {
      id: 1, incomplete: true, phone_ids: [],
      source_data: { number: 123, contractor_id: null, comment: 'Исходный комментарий', parts: 'Насос' },
      source_relations: { application_phones: [{ unresolved_phone_id: 42, phone: null }] }
    } as unknown as Application
    const view = mount(BusinessDataDetails, { props: { record } })
    expect(view.text()).toContain('123')
    expect(view.text()).toContain('Не указано')
    expect(view.text()).toContain('Исходный комментарий')
    expect(view.text()).toContain('Насос')
    expect(view.text()).toContain('42')
    expect(view.text()).toContain('не заполнена часть реквизитов')
  })
})
