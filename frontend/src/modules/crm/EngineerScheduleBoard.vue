<script setup lang="ts">
import { computed, ref } from 'vue'
import { NTag, NTooltip } from 'naive-ui'
import { formatMoneyRUB } from '@/shared/utils/format'
import type { CalendarEvent, CalendarLoad, EngineerDayLoad, TimeInterval } from '@/shared/types/domain'

// Расписание инженеров на день: колонки — инженеры, время — по вертикали.
// Свободные окна, дежурства и отсутствия приходят с сервера (/calendar/load), карточки — из /applications/calendar.

const props = defineProps<{
  load: CalendarLoad | null
  events: CalendarEvent[]
  order: number[]
  visibleIds: number[]
  canCreate: boolean
  canUpdate: boolean
}>()

const emit = defineEmits<{
  (e: 'open', ev: CalendarEvent): void
  (e: 'create', payload: { engineerId: number; start: Date; end: Date }): void
  (e: 'move', payload: { ev: CalendarEvent; engineerId: number; start: Date; end: Date }): void
}>()

const HOUR_PX = 64
const SNAP_MIN = 15

function clockToMinutes(raw: string | undefined, fallback: number): number {
  const m = /^(\d{1,2}):(\d{2})$/.exec(raw ?? '')
  return m ? Number(m[1]) * 60 + Number(m[2]) : fallback
}

const dayStart = computed(() => {
  const [y, mo, d] = (props.load?.date ?? '').split('-').map(Number)
  return y ? new Date(y, mo - 1, d) : new Date(new Date().setHours(0, 0, 0, 0))
})
const gridFrom = computed(() => clockToMinutes(props.load?.grid_from, 7 * 60))
const gridTo = computed(() => Math.max(clockToMinutes(props.load?.grid_to, 23 * 60), gridFrom.value + 60))
const gridHeight = computed(() => ((gridTo.value - gridFrom.value) / 60) * HOUR_PX)
const hours = computed(() => {
  const out: number[] = []
  for (let m = Math.ceil(gridFrom.value / 60) * 60; m < gridTo.value; m += 60) out.push(m)
  return out
})

function minutesOf(iso: string | Date): number {
  const d = typeof iso === 'string' ? new Date(iso) : iso
  return (d.getTime() - dayStart.value.getTime()) / 60000
}

function pos(from: string | Date, to: string | Date): { top: string; height: string } | null {
  const a = Math.max(minutesOf(from), gridFrom.value)
  const b = Math.min(minutesOf(to), gridTo.value)
  if (b <= a) return null
  return {
    top: `${((a - gridFrom.value) / 60) * HOUR_PX}px`,
    height: `${Math.max(((b - a) / 60) * HOUR_PX, 14)}px`
  }
}

function hhmm(iso: string | Date): string {
  const d = typeof iso === 'string' ? new Date(iso) : iso
  return d.toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })
}

function minutesLabel(min: number): string {
  return `${String(Math.floor(min / 60)).padStart(2, '0')}:${String(min % 60).padStart(2, '0')}`
}

const columns = computed(() => {
  const rank = new Map(props.order.map((id, idx) => [id, idx]))
  const list = (props.load?.engineers ?? []).filter(
    (e) => props.visibleIds.length === 0 || props.visibleIds.includes(e.user_id)
  )
  return [...list].sort((a, b) => {
    if (a.is_service !== b.is_service) return a.is_service ? 1 : -1
    const ra = rank.get(a.user_id) ?? 9999
    const rb = rank.get(b.user_id) ?? 9999
    if (ra !== rb) return ra - rb
    return a.full_name.localeCompare(b.full_name, 'ru')
  })
})

const colorById = computed(() => {
  const m = new Map<number, string>()
  for (const e of props.load?.engineers ?? []) m.set(e.user_id, e.color || '#3b82f6')
  return m
})

interface PlacedEvent {
  ev: CalendarEvent
  secondary: boolean
  overlap: boolean
  style: { top: string; height: string }
}

const eventsByColumn = computed(() => {
  const out = new Map<number, PlacedEvent[]>()
  for (const col of columns.value) {
    const mine = props.events
      .filter((ev) => ev.engineer_id === col.user_id || (ev.executor_ids ?? []).includes(col.user_id))
      .sort((a, b) => a.start.localeCompare(b.start))
    const placed: PlacedEvent[] = []
    mine.forEach((ev, idx) => {
      const style = pos(ev.start, ev.end)
      if (!style) return
      const overlap = mine.some(
        (o, j) => j !== idx && !o.is_duty && !ev.is_duty && new Date(o.start) < new Date(ev.end) && new Date(o.end) > new Date(ev.start)
      )
      placed.push({ ev, secondary: ev.engineer_id !== col.user_id, overlap, style })
    })
    out.set(col.user_id, placed)
  }
  return out
})

function loadLevel(col: EngineerDayLoad): 'none' | 'free' | 'partial' | 'full' {
  if (col.load_percent === null) return 'none'
  if (col.load_percent >= 90) return 'full'
  if (col.load_percent >= 40) return 'partial'
  return 'free'
}

function loadLabel(col: EngineerDayLoad): string {
  if (col.is_service) return 'служебная учётка'
  if (col.on_duty && col.work_minutes === 0) return 'дежурство'
  if (col.load_percent === null) {
    return col.absences.length ? absenceLabel(col.absences[0].kind) : 'выходной'
  }
  return `загрузка ${col.load_percent}%`
}

function absenceLabel(kind: string): string {
  return (
    { vacation: 'отпуск', sick: 'больничный', unpaid: 'б/с', reserve: 'резерв', duty: 'дежурство', work: 'смена' } as Record<string, string>
  )[kind] ?? kind
}

function intervalStyle(iv: TimeInterval) {
  return pos(iv.from, iv.to)
}

function snap(min: number): number {
  return Math.round(min / SNAP_MIN) * SNAP_MIN
}

function dateAt(min: number): Date {
  return new Date(dayStart.value.getTime() + min * 60000)
}

function yToMinutes(e: MouseEvent | DragEvent): number {
  const el = e.currentTarget as HTMLElement
  const rect = el.getBoundingClientRect()
  return gridFrom.value + ((e.clientY - rect.top) / HOUR_PX) * 60
}

function onColumnClick(col: EngineerDayLoad, e: MouseEvent) {
  if (!props.canCreate) return
  const start = snap(yToMinutes(e) - 15)
  emit('create', { engineerId: col.user_id, start: dateAt(start), end: dateAt(start + 60) })
}

function onFreeClick(col: EngineerDayLoad, slot: TimeInterval) {
  if (!props.canCreate) return
  const from = new Date(slot.from)
  const to = new Date(slot.to)
  const end = new Date(Math.min(to.getTime(), from.getTime() + 60 * 60000))
  emit('create', { engineerId: col.user_id, start: from, end })
}

const dragging = ref<{ ev: CalendarEvent; grabOffsetMin: number } | null>(null)
const dropHint = ref<{ userId: number; minutes: number } | null>(null)

function onDragStart(ev: CalendarEvent, e: DragEvent) {
  if (!props.canUpdate) return
  const card = e.currentTarget as HTMLElement
  const rect = card.getBoundingClientRect()
  dragging.value = { ev, grabOffsetMin: ((e.clientY - rect.top) / HOUR_PX) * 60 }
  e.dataTransfer?.setData('text/plain', String(ev.id))
}

function onDragOver(col: EngineerDayLoad, e: DragEvent) {
  if (!dragging.value) return
  dropHint.value = { userId: col.user_id, minutes: snap(yToMinutes(e) - dragging.value.grabOffsetMin) }
}

function onDrop(col: EngineerDayLoad, e: DragEvent) {
  const d = dragging.value
  dragging.value = null
  dropHint.value = null
  if (!d) return
  const duration = (new Date(d.ev.end).getTime() - new Date(d.ev.start).getTime()) / 60000
  const startMin = snap(yToMinutes(e) - d.grabOffsetMin)
  const start = dateAt(startMin)
  if (start.getTime() === new Date(d.ev.start).getTime() && col.user_id === d.ev.engineer_id) return
  emit('move', { ev: d.ev, engineerId: col.user_id, start, end: dateAt(startMin + duration) })
}

function eventStatus(ev: CalendarEvent): string {
  if (ev.is_complete || ev.status === 2) return 'Выполнено'
  if (ev.is_duty) return 'Дежурная'
  return 'Запланировано'
}
</script>

<template>
  <div class="board">
    <div class="board-summary">
      <span>
        {{ load?.totals.applications ?? 0 }} выездов · {{ load?.totals.engineers ?? 0 }} инженеров
        <template v-if="load?.totals.planned_sum"> · план {{ formatMoneyRUB(load.totals.planned_sum) }}</template>
      </span>
      <span class="legend">
        <span><i class="sw sw-planned" />Запланировано</span>
        <span><i class="sw sw-done" />Выполнено</span>
        <span><i class="sw sw-overlap" />Пересечение времени</span>
        <span><i class="sw sw-free" />Свободно</span>
        <span><i class="sw sw-duty" />Дежурство</span>
        <span><i class="sw sw-absent" />Отсутствие, вне смены</span>
      </span>
    </div>

    <div v-if="columns.length === 0" class="empty">Нет инженеров в выбранном подразделении</div>

    <div v-else class="board-scroll">
      <div class="board-grid" :style="{ gridTemplateColumns: `56px repeat(${columns.length}, minmax(200px, 1fr))` }">
        <div class="corner">Время</div>
        <div v-for="col in columns" :key="`h${col.user_id}`" class="col-head" :class="{ service: col.is_service }">
          <div class="col-name">
            <span class="dot" :style="{ background: col.color || '#3b82f6' }" />
            {{ col.full_name }}
          </div>
          <div class="col-sub">{{ col.applications_count }} выездов · выполнено {{ col.completed_count }}</div>
          <div class="col-load" :class="`lvl-${loadLevel(col)}`">
            <div class="bar"><div :style="{ width: `${Math.min(col.load_percent ?? 0, 100)}%` }" /></div>
            <span>{{ loadLabel(col) }}</span>
          </div>
          <div class="col-tags">
            <NTag v-if="col.on_duty" size="tiny" type="warning">Дежурный</NTag>
            <NTag v-if="col.schedule_source === 'default' && !col.is_service" size="tiny" :bordered="false">нет табеля</NTag>
            <span v-if="col.planned_sum && !col.is_service" class="muted">{{ formatMoneyRUB(col.planned_sum) }}</span>
          </div>
        </div>

        <div class="time-col" :style="{ height: `${gridHeight}px` }">
          <div v-for="m in hours" :key="m" class="hour-label" :style="{ top: `${((m - gridFrom) / 60) * HOUR_PX}px` }">
            {{ minutesLabel(m) }}
          </div>
        </div>

        <div
          v-for="col in columns"
          :key="`c${col.user_id}`"
          class="col-body"
          :class="{ 'can-create': canCreate && !col.is_service }"
          :style="{ height: `${gridHeight}px` }"
          @click.self="onColumnClick(col, $event)"
          @dragover.prevent="onDragOver(col, $event)"
          @dragleave="dropHint = null"
          @drop.prevent="onDrop(col, $event)"
        >
          <div v-for="m in hours" :key="m" class="hour-line" :style="{ top: `${((m - gridFrom) / 60) * HOUR_PX}px` }" />
          <template v-for="(iv, i) in col.work" :key="`w${i}`">
            <div v-if="intervalStyle(iv)" class="layer work" :style="intervalStyle(iv)!" />
          </template>
          <template v-for="(iv, i) in col.duty" :key="`d${i}`">
            <div v-if="intervalStyle(iv)" class="layer duty" :style="intervalStyle(iv)!" />
          </template>
          <template v-for="(a, i) in col.absences" :key="`a${i}`">
            <div v-if="intervalStyle(a)" class="layer absent" :style="intervalStyle(a)!">
              <span>{{ absenceLabel(a.kind) }}<template v-if="a.comment">: {{ a.comment }}</template></span>
            </div>
          </template>
          <template v-for="(slot, i) in col.free" :key="`f${i}`">
            <div
              v-if="intervalStyle(slot)"
              class="layer free"
              :class="{ short: slot.short }"
              :style="intervalStyle(slot)!"
              :title="`Свободно ${hhmm(slot.from)}–${hhmm(slot.to)}`"
              @click.stop="onFreeClick(col, slot)"
            >
              <span v-if="!slot.short">+ {{ hhmm(slot.from) }}–{{ hhmm(slot.to) }}</span>
            </div>
          </template>

          <div
            v-if="dropHint && dropHint.userId === col.user_id"
            class="drop-hint"
            :style="{ top: `${((dropHint.minutes - gridFrom) / 60) * HOUR_PX}px` }"
          >
            {{ minutesLabel(Math.max(dropHint.minutes, 0)) }}
          </div>

          <NTooltip v-for="p in eventsByColumn.get(col.user_id)" :key="`e${p.ev.id}`" :disabled="!p.secondary">
            <template #trigger>
              <div
                class="ev"
                :class="{ done: p.ev.is_complete || p.ev.status === 2, overlap: p.overlap, secondary: p.secondary }"
                :style="{
                  ...p.style,
                  borderLeftColor: colorById.get(p.ev.engineer_id) ?? p.ev.color,
                  ...(p.secondary ? { background: `${colorById.get(p.ev.engineer_id) ?? p.ev.color}22` } : {})
                }"
                :draggable="canUpdate && !p.secondary"
                @dragstart="onDragStart(p.ev, $event)"
                @click.stop="emit('open', p.ev)"
              >
                <div class="ev-time">{{ hhmm(p.ev.start) }}–{{ hhmm(p.ev.end) }} · #{{ p.ev.id }}</div>
                <div class="ev-addr">{{ p.ev.address_text || p.ev.contractor_name }}</div>
                <div class="ev-sub">{{ (p.ev.work_types ?? []).join(' + ') }}</div>
                <div class="ev-status">{{ eventStatus(p.ev) }}<template v-if="p.overlap"> · пересечение</template></div>
              </div>
            </template>
            Соисполнитель, основной инженер: {{ p.ev.engineer_name }}
          </NTooltip>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.board {
  background: #fff;
  border: 1px solid #e2e8f0;
  border-radius: 10px;
  overflow: hidden;
}
.board-summary {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: 8px;
  padding: 8px 12px;
  font-size: 13px;
  color: #475569;
  border-bottom: 1px solid #f1f5f9;
}
.legend {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  font-size: 12px;
}
.sw {
  display: inline-block;
  width: 10px;
  height: 10px;
  border-radius: 2px;
  margin-right: 4px;
  vertical-align: -1px;
}
.sw-planned { background: #dbeafe; border: 1px solid #3b82f6; }
.sw-done { background: #dcfce7; border: 1px solid #16a34a; }
.sw-overlap { background: #fff; border: 2px solid #f97316; }
.sw-free { background: #ecfdf5; border: 1px dashed #10b981; }
.sw-duty { background: #fef3c7; }
.sw-absent { background: #e2e8f0; }
.empty {
  padding: 32px;
  text-align: center;
  color: #64748b;
}
.board-scroll {
  overflow: auto;
  max-height: calc(100vh - 230px);
}
.board-grid {
  display: grid;
  position: relative;
}
.corner,
.col-head {
  position: sticky;
  top: 0;
  z-index: 3;
  background: #fff;
  border-bottom: 1px solid #e2e8f0;
  padding: 8px 10px;
}
.corner {
  left: 0;
  z-index: 4;
  font-size: 12px;
  color: #64748b;
  display: flex;
  align-items: flex-end;
}
.col-head {
  border-left: 1px solid #f1f5f9;
}
.col-head.service {
  background: #f8fafc;
}
.col-name {
  font-weight: 600;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-right: 4px;
}
.col-sub,
.muted {
  font-size: 12px;
  color: #64748b;
}
.col-load {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  margin-top: 4px;
}
.col-load .bar {
  flex: 0 0 70px;
  height: 6px;
  border-radius: 3px;
  background: #e2e8f0;
  overflow: hidden;
}
.col-load .bar div {
  height: 100%;
  background: currentColor;
}
.lvl-free { color: #059669; }
.lvl-partial { color: #d97706; }
.lvl-full { color: #dc2626; }
.lvl-none { color: #94a3b8; }
.col-tags {
  display: flex;
  gap: 4px;
  align-items: center;
  margin-top: 4px;
  min-height: 18px;
}
.time-col {
  position: sticky;
  left: 0;
  z-index: 2;
  background: #fff;
  border-right: 1px solid #e2e8f0;
}
.hour-label {
  position: absolute;
  left: 6px;
  font-size: 11px;
  color: #64748b;
  transform: translateY(-50%);
}
.hour-label:first-child {
  transform: none;
}
.col-body {
  position: relative;
  border-left: 1px solid #f1f5f9;
  background: repeating-linear-gradient(135deg, #f8fafc, #f8fafc 6px, #f1f5f9 6px, #f1f5f9 12px);
}
.col-body.can-create {
  cursor: copy;
}
.hour-line {
  position: absolute;
  left: 0;
  right: 0;
  border-top: 1px solid #eef2f7;
  pointer-events: none;
  z-index: 1;
}
.layer {
  position: absolute;
  left: 0;
  right: 0;
  pointer-events: none;
}
.layer.work { background: #fff; }
.layer.duty { background: #fef3c7; }
.layer.absent {
  background: #e2e8f0;
  z-index: 1;
  font-size: 11px;
  color: #475569;
  padding: 4px 6px;
}
.layer.free {
  pointer-events: auto;
  left: 3px;
  right: 3px;
  background: #ecfdf5;
  border: 1px dashed #6ee7b7;
  border-radius: 6px;
  cursor: pointer;
  z-index: 1;
  font-size: 11px;
  color: #047857;
  padding: 3px 6px;
}
.layer.free.short {
  background: #f0fdf4;
  border-color: #bbf7d0;
}
.layer.free:hover {
  background: #d1fae5;
}
.drop-hint {
  position: absolute;
  left: 0;
  right: 0;
  border-top: 2px dashed #2563eb;
  font-size: 11px;
  color: #2563eb;
  z-index: 4;
  pointer-events: none;
}
.ev {
  position: absolute;
  left: 4px;
  right: 4px;
  z-index: 2;
  overflow: hidden;
  border: 1px solid #bfdbfe;
  border-left: 4px solid #3b82f6;
  border-radius: 6px;
  background: #eff6ff;
  padding: 3px 6px;
  font-size: 12px;
  cursor: pointer;
}
.ev.done {
  background: #f0fdf4;
  border-color: #bbf7d0;
}
.ev.overlap {
  outline: 2px solid #f97316;
}
.ev.secondary {
  border-style: dashed;
}
.ev-time {
  font-size: 11px;
  color: #475569;
}
.ev-addr {
  font-weight: 600;
  line-height: 1.25;
}
.ev-sub,
.ev-status {
  font-size: 11px;
  color: #64748b;
}
</style>
