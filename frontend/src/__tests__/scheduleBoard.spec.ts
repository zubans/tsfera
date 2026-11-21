import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import EngineerScheduleBoard from '@/modules/crm/EngineerScheduleBoard.vue'
import type { CalendarEvent, CalendarLoad, EngineerDayLoad } from '@/shared/types/domain'

function at(h: number, m = 0): string {
  return new Date(2026, 10, 21, h, m).toISOString()
}

function engineer(over: Partial<EngineerDayLoad>): EngineerDayLoad {
  return {
    user_id: 0,
    full_name: '',
    color: '#3b82f6',
    is_service: false,
    applications_count: 0,
    completed_count: 0,
    planned_sum: 0,
    work: [],
    duty: [],
    absences: [],
    busy: [],
    free: [],
    work_minutes: 0,
    busy_minutes: 0,
    load_percent: null,
    on_duty: false,
    schedule_source: 'timesheet',
    ...over
  }
}

const load: CalendarLoad = {
  date: '2026-11-21',
  grid_from: '07:00',
  grid_to: '21:00',
  min_free_minutes: 60,
  totals: { applications: 1, engineers: 2, planned_sum: 5000 },
  engineers: [
    engineer({
      user_id: 1,
      full_name: 'Петров А. В.',
      color: '#16a34a',
      applications_count: 1,
      work: [{ from: at(8), to: at(17) }],
      free: [{ from: at(11), to: at(17), short: false }],
      load_percent: 33,
      work_minutes: 540
    }),
    engineer({ user_id: 2, full_name: 'Сидоров И. П.', absences: [{ kind: 'vacation', from: at(0), to: at(24), comment: '' }] }),
    engineer({ user_id: 3, full_name: 'Офис К. К.', is_service: true })
  ]
}

const event = {
  id: 117093,
  title: '',
  start: at(8),
  end: at(11),
  contractor_id: 1,
  contractor_name: 'ООО Тепло',
  address_id: 1,
  address_text: 'Кольчугино, ул. Вокзальная, 32И',
  phones: [],
  engineer_id: 1,
  engineer_name: 'Петров А. В.',
  color: '#16a34a',
  status: 1,
  is_complete: false,
  is_duty: false,
  work_types: ['Диагностика'],
  equipments: [],
  serial_numbers: [],
  acts_total_amount: 0,
  executor_ids: [2],
  special_notes: '',
  notes_viewed: true
} as CalendarEvent

function mountBoard() {
  return mount(EngineerScheduleBoard, {
    props: { load, events: [event], order: [], visibleIds: [], canCreate: true, canUpdate: true }
  })
}

describe('EngineerScheduleBoard', () => {
  it('рисует колонки инженеров с загрузкой, служебную учётку — последней', () => {
    const wrapper = mountBoard()
    const heads = wrapper.findAll('.col-head').map((h) => h.text())
    expect(heads).toHaveLength(3)
    expect(heads[0]).toContain('Петров А. В.')
    expect(heads[0]).toContain('загрузка 33%')
    expect(heads[1]).toContain('отпуск')
    expect(heads[2]).toContain('служебная учётка')
    expect(wrapper.text()).toContain('1 выездов · 2 инженеров')
  })

  it('показывает заявку у основного инженера и у соисполнителя', () => {
    const wrapper = mountBoard()
    const cards = wrapper.findAll('.ev')
    expect(cards).toHaveLength(2)
    expect(cards[0].text()).toContain('#117093')
    expect(cards.filter((c) => c.classes('secondary'))).toHaveLength(1)
  })

  it('клик по свободному окну создаёт заявку на этого инженера и время', async () => {
    const wrapper = mountBoard()
    const free = wrapper.find('.layer.free')
    expect(free.text()).toContain('11:00–17:00')
    await free.trigger('click')
    const created = wrapper.emitted('create')?.[0]?.[0] as { engineerId: number; start: Date; end: Date }
    expect(created.engineerId).toBe(1)
    expect(created.start.getHours()).toBe(11)
    expect(created.end.getHours()).toBe(12)
  })

  it('фильтр исполнителей скрывает колонки', () => {
    const wrapper = mount(EngineerScheduleBoard, {
      props: { load, events: [event], order: [], visibleIds: [2], canCreate: true, canUpdate: true }
    })
    expect(wrapper.findAll('.col-head')).toHaveLength(1)
    expect(wrapper.find('.col-head').text()).toContain('Сидоров')
  })
})
