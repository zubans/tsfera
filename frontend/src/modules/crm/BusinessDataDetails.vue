<script setup lang="ts">
import { computed } from 'vue'
import type { Application, Certificate } from '@/shared/types/domain'
const props = defineProps<{ record: Application | Certificate }>()
const labels: Record<string, string> = {
  application_phones: 'Контактные телефоны заявки', unresolved_phone_id: 'Отсутствующая в исходных данных запись телефона', phone: 'Номер телефона', equipment_name: 'Наименование оборудования', id: 'Идентификатор', number: 'Номер документа', contractor_id: 'Контрагент', address_id: 'Адрес',
  executor_user_id: 'Ответственный сотрудник', date: 'Дата работ', date_from: 'Начало', date_to: 'Окончание',
  comment: 'Комментарий', results: 'Результаты замеров', recommendation: 'Рекомендация', sum: 'Сумма',
  parts: 'Описание запчастей', cost_of_parts: 'Стоимость запчастей', receipt_number: 'Номер чека',
  maintenance_contract_id: 'Договор обслуживания', application_for_technical_work_id: 'Заявка',
  payment_option_id: 'Способ оплаты', other_note: 'Дополнительные замечания', notes_viewed: 'Замечания прочитаны',
  create_user_id: 'Создал', update_user_id: 'Изменил', created_at: 'Дата создания', updated_at: 'Дата изменения',
  document_status_id: 'Статус документа', status_id: 'Статус распределения', subdivision_id: 'Подразделение',
  traffic_source_id: 'Источник обращения', planned_sum: 'Плановая сумма', rating: 'Оценка',
  is_complete: 'Завершена', is_duty: 'Дежурная', work_window_from: 'Окно работ: начало', work_window_to: 'Окно работ: конец',
  without_maintenance_contract: 'Без договора обслуживания', phone_ids: 'Телефоны заявки', equipments: 'Оборудование акта',
  equipment_id: 'Оборудование', serial_number: 'Серийный номер', parent_id: 'Документ', phone_id: 'Телефон', user_id: 'Сотрудник',
  tab_application_for_technical_work_equipment: 'Оборудование заявки', tab_application_for_technical_work_executor: 'Соисполнители заявки',
  tab_application_for_technical_work_types_of_job: 'Работы заявки', tab_certificate_of_completion_executor: 'Соисполнители акта',
  tab_certificate_of_completion_types_of_job: 'Работы акта', tab_certificate_of_completion_special_note: 'Особые отметки',
  type_of_job_id: 'Вид работ', special_note_id: 'Отметка'
}
function display(value: unknown): string {
  if (value === null || value === undefined) return 'Не указано'
  if (typeof value === 'boolean') return value ? 'Да' : 'Нет'
  if (typeof value === 'object') return JSON.stringify(value, null, 2)
  return String(value)
}
const fields = computed(() => Object.entries(props.record.source_data ?? {}))
const relations = computed(() => Object.entries(props.record.source_relations ?? {}))
</script>
<template>
  <p v-if="'incomplete' in record && record.incomplete">В исходном документе не заполнена часть реквизитов. Отсутствующие значения сохранены без подстановки.</p>
  <p v-if="'phone_ids' in record">Телефоны заявки (идентификаторы): {{ record.phone_ids?.join(', ') || 'Не указаны' }}</p>
  <div v-if="'equipments' in record && record.equipments?.length">
    <strong>Оборудование акта</strong>
    <p v-for="item in record.equipments" :key="item.id">Оборудование № {{ item.equipment_id }} · Серийный номер: {{ item.serial_number || 'Не указан' }}</p>
  </div>
  <p v-if="fields.length">Исходные реквизиты на момент переноса:</p>
  <table v-if="fields.length" class="business-data">
    <tbody><tr v-for="[key, value] in fields" :key="key"><th>{{ labels[key] || key }}</th><td><pre>{{ display(value) }}</pre></td></tr></tbody>
  </table>
  <section v-for="[key, value] in relations" :key="key"><strong>{{ labels[key] || key }}</strong><pre>{{ display(value) }}</pre></section>
  <p v-if="!fields.length">Дополнительных перенесённых реквизитов нет.</p>
</template>
<style scoped>
.business-data { width: 100%; border-collapse: collapse; }
th, td { padding: 6px; border-bottom: 1px solid #ddd; text-align: left; vertical-align: top; }
pre { white-space: pre-wrap; overflow-wrap: anywhere; margin: 0; font: inherit; }
section { margin-top: 16px; }
</style>
