<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { requestJSON } from '@/shared/api/http'
import ReportsPanel from './ReportsPanel.vue'
import type { Subdivision, UserBrief, CRMEnums } from '@/shared/types/domain'

const subdivisions = ref<Subdivision[]>([])
const workers = ref<UserBrief[]>([])
const enums = ref<CRMEnums>({
  work_types: [],
  document_statuses: [],
  task_types: [],
  task_statuses: []
})

onMounted(async () => {
  const [subRes, wRes, eRes] = await Promise.allSettled([
    requestJSON<Subdivision[]>('/api/v1/crm/subdivisions'),
    requestJSON<UserBrief[]>('/api/v1/crm/workers'),
    requestJSON<CRMEnums>('/api/v1/crm/enums')
  ])
  if (subRes.status === 'fulfilled' && Array.isArray(subRes.value)) subdivisions.value = subRes.value
  if (wRes.status === 'fulfilled' && Array.isArray(wRes.value)) workers.value = wRes.value
  if (eRes.status === 'fulfilled' && eRes.value) enums.value = eRes.value
})
</script>

<template>
  <div class="crm-page">
    <div class="page-head">
      <div>
        <h2 class="page-title">Отчёты</h2>
        <div class="page-meta">Выручка, выработка инженеров, конверсия звонков, источники трафика и просрочки</div>
      </div>
    </div>

    <ReportsPanel :subdivisions="subdivisions" :workers="workers" :enums="enums" />
  </div>
</template>

<style scoped>
.crm-page {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.page-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.page-title {
  margin: 0;
  font-size: 20px;
  font-weight: 700;
  color: #0f172a;
}
.page-meta {
  font-size: 13px;
  color: #64748b;
  margin-top: 2px;
}
</style>
