<script setup lang="ts">
import { ref, computed, onMounted, h } from 'vue'
import {
  NTabs,
  NTabPane,
  NDataTable,
  NButton,
  NSpace,
  NModal,
  NForm,
  NFormItem,
  NInput,
  NSelect,
  NSwitch,
  NCheckboxGroup,
  NCheckbox,
  NTag,
  NAlert,
  NDivider,
  type DataTableColumns
} from 'naive-ui'
import { requestJSON, ApiError } from '@/shared/api/http'
import { formatDateTime } from '@/shared/utils/format'
import { useAuthStore } from '@/stores/auth'
import CrmCatalogsPanel from '@/modules/crm/CrmCatalogsPanel.vue'
import type {
  User,
  Role,
  PermissionDef,
  APIToken,
  LoginEvent,
  InventoryItem,
  Subdivision
} from '@/shared/types/domain'

const auth = useAuthStore()
const users = ref<User[]>([])
const roles = ref<Role[]>([])
const perms = ref<PermissionDef[]>([])
const tokens = ref<APIToken[]>([])
const loginEvents = ref<LoginEvent[]>([])
const inventoryItems = ref<InventoryItem[]>([])
const subdivisions = ref<Subdivision[]>([])
const errorMsg = ref('')

const inventoryCategory = ref<'work_clothes' | 'instrument' | 'engineer_equipment'>('work_clothes')
const newInventoryName = ref('')

const pwdModalOpen = ref(false)
const pwdUserId = ref<number | null>(null)
const newPassword = ref('')

const userModalOpen = ref(false)
const editUserId = ref<number | null>(null)
const userForm = ref({
  username: '',
  password: '',
  email: '' as string | null,
  first_name: '',
  second_name: '',
  last_name: '',
  subdivision_id: null as number | null,
  corporate_phone_number: '',
  color: '#3b82f6',
  is_engineer: false,
  is_dispatcher: false,
  office_equipment: '',
  training: '',
  car_rubber: '',
  car_model: '',
  car_fuel_card_number: '',
  car_government_number: '',
  car_sts_number: '',
  car_contract_number: '',
  work_clothes: [] as string[],
  instruments: [] as string[],
  equipments: [] as string[],
  avatar_path: '',
  roles: [] as string[],
  active: true
})

const tokenModalOpen = ref(false)
const newTokenName = ref('')
const newTokenWrite = ref(false)
const createdPlainToken = ref('')

const roleModalOpen = ref(false)
const roleForm = ref({ name: '', description: '', permissions: [] as string[] })

const workClothesOptions = computed(() =>
  inventoryItems.value
    .filter((i) => i.category === 'work_clothes')
    .map((i) => ({ label: i.name, value: i.name }))
)
const instrumentOptions = computed(() =>
  inventoryItems.value
    .filter((i) => i.category === 'instrument')
    .map((i) => ({ label: i.name, value: i.name }))
)
const engineerEquipmentOptions = computed(() =>
  inventoryItems.value
    .filter((i) => i.category === 'engineer_equipment')
    .map((i) => ({ label: i.name, value: i.name }))
)
const filteredInventory = computed(() =>
  inventoryItems.value.filter((i) => i.category === inventoryCategory.value)
)

async function loadAll() {
  const [u, r, p, t, l, inv, sub] = await Promise.allSettled([
    requestJSON<User[]>('/api/v1/admin/users'),
    requestJSON<Role[]>('/api/v1/admin/roles'),
    requestJSON<PermissionDef[]>('/api/v1/admin/permissions'),
    requestJSON<APIToken[]>('/api/v1/tokens'),
    requestJSON<LoginEvent[]>('/api/v1/admin/logins?limit=50'),
    requestJSON<InventoryItem[]>('/api/v1/admin/inventory'),
    requestJSON<Subdivision[]>('/api/v1/crm/subdivisions')
  ])
  if (u.status === 'fulfilled') users.value = u.value ?? []
  if (r.status === 'fulfilled') roles.value = r.value ?? []
  if (p.status === 'fulfilled') perms.value = p.value ?? []
  if (t.status === 'fulfilled') tokens.value = t.value ?? []
  if (l.status === 'fulfilled') loginEvents.value = l.value ?? []
  if (inv.status === 'fulfilled') inventoryItems.value = inv.value ?? []
  if (sub.status === 'fulfilled') subdivisions.value = sub.value ?? []
}

onMounted(loadAll)

function openCreateUser() {
  editUserId.value = null
  userForm.value = {
    username: '',
    password: '',
    email: null,
    first_name: '',
    second_name: '',
    last_name: '',
    subdivision_id: auth.user?.subdivision_id ?? null,
    corporate_phone_number: '',
    color: '#3b82f6',
    is_engineer: false,
    is_dispatcher: false,
    office_equipment: '',
    training: '',
    car_rubber: '',
    car_model: '',
    car_fuel_card_number: '',
    car_government_number: '',
    car_sts_number: '',
    car_contract_number: '',
    work_clothes: [],
    instruments: [],
    equipments: [],
    avatar_path: '',
    roles: [],
    active: true
  }
  userModalOpen.value = true
}

function openEditUser(u: User) {
  editUserId.value = u.id
  userForm.value = {
    username: u.username,
    password: '',
    email: u.email ?? null,
    first_name: u.first_name,
    second_name: u.second_name,
    last_name: u.last_name,
    subdivision_id: u.subdivision_id ?? null,
    corporate_phone_number: u.corporate_phone_number,
    color: u.color || '#3b82f6',
    is_engineer: u.is_engineer,
    is_dispatcher: u.is_dispatcher,
    office_equipment: u.office_equipment || '',
    training: u.training || '',
    car_rubber: u.car_rubber || '',
    car_model: u.car_model || '',
    car_fuel_card_number: u.car_fuel_card_number || '',
    car_government_number: u.car_government_number || '',
    car_sts_number: u.car_sts_number || '',
    car_contract_number: u.car_contract_number || '',
    work_clothes: [...(u.work_clothes || [])],
    instruments: [...(u.instruments || [])],
    equipments: [...(u.equipments || [])],
    avatar_path: u.avatar_path || '',
    roles: [...(u.roles || [])],
    active: u.active
  }
  userModalOpen.value = true
}

async function saveUser() {
  errorMsg.value = ''
  try {
    const payload = {
      ...userForm.value,
      email: userForm.value.email?.trim() ? userForm.value.email.trim() : null
    }
    if (editUserId.value) {
      await requestJSON(`/api/v1/admin/users/${editUserId.value}`, {
        method: 'PUT',
        body: JSON.stringify(payload)
      })
    } else {
      await requestJSON('/api/v1/admin/users', {
        method: 'POST',
        body: JSON.stringify(payload)
      })
    }
    userModalOpen.value = false
    await loadAll()
  } catch (e) {
    errorMsg.value = e instanceof ApiError ? e.message : 'Ошибка сохранения пользователя'
  }
}

async function changeUserPassword() {
  if (!pwdUserId.value || !newPassword.value) return
  try {
    await requestJSON(`/api/v1/admin/users/${pwdUserId.value}/password`, {
      method: 'POST',
      body: JSON.stringify({ password: newPassword.value })
    })
    pwdModalOpen.value = false
    newPassword.value = ''
  } catch (e) {
    errorMsg.value = e instanceof ApiError ? e.message : 'Ошибка смены пароля'
  }
}

async function createInventoryItem() {
  if (!newInventoryName.value.trim()) return
  await requestJSON('/api/v1/admin/inventory', {
    method: 'POST',
    body: JSON.stringify({ category: inventoryCategory.value, name: newInventoryName.value.trim() })
  })
  newInventoryName.value = ''
  await loadAll()
}

async function deleteInventoryItem(id: number) {
  await requestJSON(`/api/v1/admin/inventory/${id}`, { method: 'DELETE' })
  await loadAll()
}

async function createToken() {
  const res = await requestJSON<{ id: number; token: string }>('/api/v1/tokens', {
    method: 'POST',
    body: JSON.stringify({ name: newTokenName.value, can_read: true, can_write: newTokenWrite.value, ttl_days: 90 })
  })
  createdPlainToken.value = res.token
  newTokenName.value = ''
  await loadAll()
}

async function deleteToken(id: number) {
  await requestJSON(`/api/v1/tokens/${id}`, { method: 'DELETE' })
  await loadAll()
}

function openRole(r: Role) {
  roleForm.value = { name: r.name, description: r.description, permissions: [...(r.permissions || [])] }
  roleModalOpen.value = true
}

async function saveRole() {
  await requestJSON(`/api/v1/admin/roles/${encodeURIComponent(roleForm.value.name)}`, {
    method: 'PUT',
    body: JSON.stringify(roleForm.value)
  })
  roleModalOpen.value = false
  await loadAll()
}

const userColumns: DataTableColumns<User> = [
  { title: 'ID', key: 'id', width: 60 },
  { title: 'Логин', key: 'username' },
  { title: 'ФИО', key: 'full_name' },
  {
    title: 'Профиль',
    key: 'flags',
    render: (u) =>
      h(NSpace, { size: 4 }, () => [
        u.is_engineer ? h(NTag, { size: 'small', type: 'info' }, () => 'Инженер') : null,
        u.is_dispatcher ? h(NTag, { size: 'small', type: 'warning' }, () => 'Диспетчер') : null,
        !u.active ? h(NTag, { size: 'small', type: 'error' }, () => 'Отключён') : null
      ])
  },
  {
    title: 'Роли',
    key: 'roles',
    render: (u) => (u.roles || []).join(', ')
  },
  { title: 'Последний вход', key: 'last_login_at', render: (u) => formatDateTime(u.last_login_at) },
  {
    title: 'Действия',
    key: 'actions',
    width: 210,
    render: (u) =>
      h(NSpace, { size: 6 }, () => [
        auth.hasPerm('admin.user.update') &&
          h(NButton, { size: 'tiny', onClick: () => openEditUser(u) }, () => 'Редактировать'),
        auth.hasPerm('admin.user.change_password') &&
          h(
            NButton,
            {
              size: 'tiny',
              secondary: true,
              onClick: () => {
                pwdUserId.value = u.id
                newPassword.value = ''
                pwdModalOpen.value = true
              }
            },
            () => 'Пароль'
          )
      ])
  }
]
</script>

<template>
  <NSpace vertical :size="12">
    <div style="display: flex; justify-content: space-between; align-items: center">
      <h2 style="margin: 0">Пользователи, роли RBAC и токены доступа</h2>
      <NSpace>
        <NButton type="primary" @click="openCreateUser">+ Новый сотрудник</NButton>
        <NButton secondary @click="tokenModalOpen = true">+ API-токен</NButton>
      </NSpace>
    </div>

    <NAlert v-if="errorMsg" type="error" closable @close="errorMsg = ''">{{ errorMsg }}</NAlert>

    <NTabs type="line" animated>
      <NTabPane name="users" tab="Сотрудники">
        <NDataTable :columns="userColumns" :data="users" :pagination="{ pageSize: 20 }" />
      </NTabPane>

      <NTabPane name="roles" tab="Роли и разрешения RBAC">
        <NSpace vertical>
          <div style="display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr)); gap: 12px">
            <div v-for="r in roles" :key="r.name" style="background: #fff; border: 1px solid #e2e8f0; border-radius: 8px; padding: 12px">
              <div style="display: flex; justify-content: space-between; align-items: center">
                <strong>{{ r.name }}</strong>
                <NTag v-if="r.name === 'superadmin'" type="error" size="small">Защищена</NTag>
                <NButton v-else size="tiny" @click="openRole(r)">Права ({{ r.permissions?.length || 0 }})</NButton>
              </div>
              <div style="font-size: 12px; color: #64748b; margin-top: 4px">{{ r.description }}</div>
            </div>
          </div>
        </NSpace>
      </NTabPane>

      <NTabPane name="tokens" tab="API-токены">
        <NSpace vertical>
          <NAlert v-if="createdPlainToken" type="success" closable @close="createdPlainToken = ''">
            Сохраните токен сейчас (показывается 1 раз): <code>{{ createdPlainToken }}</code>
          </NAlert>
          <NDataTable
            :data="tokens"
            :columns="[
              { title: 'Название', key: 'name' },
              { title: 'Чтение', key: 'can_read', render: (t: APIToken) => (t.can_read ? 'Да' : 'Нет') },
              { title: 'Запись', key: 'can_write', render: (t: APIToken) => (t.can_write ? 'Да' : 'Нет') },
              { title: 'Истекает', key: 'expires_at', render: (t: APIToken) => formatDateTime(t.expires_at) },
              {
                title: 'Действия',
                key: 'act',
                render: (t: APIToken) => h(NButton, { size: 'tiny', type: 'error', onClick: () => deleteToken(t.id) }, () => 'Отозвать')
              }
            ]"
          />
        </NSpace>
      </NTabPane>

      <NTabPane name="audit" tab="Аудит входов">
        <NDataTable
          :data="loginEvents"
          :columns="[
            { title: 'Событие', key: 'event' },
            { title: 'Пользователь', key: 'username' },
            { title: 'IP', key: 'ip' },
            { title: 'Причина', key: 'reason' },
            { title: 'Время', key: 'created_at', render: (e: LoginEvent) => formatDateTime(e.created_at) }
          ]"
        />
      </NTabPane>

      <NTabPane name="inventory" tab="Справочник оснащения">
        <NSpace justify="space-between" style="margin-bottom: 12px">
          <NSelect
            v-model:value="inventoryCategory"
            style="width: 260px"
            :options="[
              { label: 'Спецодежда', value: 'work_clothes' },
              { label: 'Инструмент', value: 'instrument' },
              { label: 'Оборудование инженера', value: 'engineer_equipment' }
            ]"
          />
          <NSpace v-if="auth.hasPerm('admin.user.update')">
            <NInput v-model:value="newInventoryName" placeholder="Наименование..." style="width: 240px" @keyup.enter="createInventoryItem" />
            <NButton type="primary" @click="createInventoryItem">Добавить</NButton>
          </NSpace>
        </NSpace>
        <NDataTable
          :data="filteredInventory"
          :row-key="(i: InventoryItem) => i.id"
          :columns="[
            { title: 'ID', key: 'id', width: 70 },
            { title: 'Наименование', key: 'name' },
            {
              title: '',
              key: 'del',
              width: 100,
              render: (i: InventoryItem) =>
                auth.hasPerm('admin.user.update')
                  ? h(NButton, { size: 'tiny', type: 'error', ghost: true, onClick: () => void deleteInventoryItem(i.id) }, () => 'Удалить')
                  : '—'
            }
          ]"
        />
      </NTabPane>

      <NTabPane name="crm-catalogs" tab="Источники трафика, Анкеты и Баннеры">
        <CrmCatalogsPanel />
      </NTabPane>
    </NTabs>

    <NModal v-model:show="userModalOpen" preset="card" :title="editUserId ? 'Редактирование сотрудника' : 'Новый сотрудник'" style="width: 720px; max-width: 96vw">
      <NForm label-placement="left" label-width="190">
        <NFormItem v-if="!editUserId" label="Логин"><NInput v-model:value="userForm.username" /></NFormItem>
        <NFormItem v-if="!editUserId" label="Пароль"><NInput v-model:value="userForm.password" type="password" /></NFormItem>
        <NFormItem label="Email"><NInput v-model:value="userForm.email" placeholder="user@teplosfera.local" /></NFormItem>
        <NFormItem label="Фамилия"><NInput v-model:value="userForm.last_name" /></NFormItem>
        <NFormItem label="Имя"><NInput v-model:value="userForm.first_name" /></NFormItem>
        <NFormItem label="Отчество"><NInput v-model:value="userForm.second_name" /></NFormItem>
        <NFormItem label="Подразделение">
          <NSelect
            v-model:value="userForm.subdivision_id"
            clearable
            :options="subdivisions.map((s) => ({ label: s.name, value: s.id }))"
          />
        </NFormItem>
        <NFormItem label="Корп. телефон"><NInput v-model:value="userForm.corporate_phone_number" /></NFormItem>
        <NFormItem label="Путь к аватару"><NInput v-model:value="userForm.avatar_path" placeholder="/uploads/avatars/..." /></NFormItem>
        <NFormItem label="Цвет в календаре"><NInput v-model:value="userForm.color" /></NFormItem>
        <NFormItem label="Инженер / Диспетчер">
          <NSpace>
            <NSwitch v-model:value="userForm.is_engineer" /> Инженер
            <NSwitch v-model:value="userForm.is_dispatcher" /> Диспетчер
            <NSwitch v-model:value="userForm.active" /> Активен
          </NSpace>
        </NFormItem>
        <NDivider>Служебный транспорт и допуски</NDivider>
        <NFormItem label="Авто (модель / госномер)">
          <NSpace>
            <NInput v-model:value="userForm.car_model" placeholder="Модель" />
            <NInput v-model:value="userForm.car_government_number" placeholder="А123БВ77" />
          </NSpace>
        </NFormItem>
        <NFormItem label="Резина / Топл. карта">
          <NSpace>
            <NInput v-model:value="userForm.car_rubber" placeholder="Шины/размер" />
            <NInput v-model:value="userForm.car_fuel_card_number" placeholder="№ топл. карты" />
          </NSpace>
        </NFormItem>
        <NFormItem label="СТС / Договор авто">
          <NSpace>
            <NInput v-model:value="userForm.car_sts_number" placeholder="СТС" />
            <NInput v-model:value="userForm.car_contract_number" placeholder="№ договора" />
          </NSpace>
        </NFormItem>
        <NFormItem label="Оргтехника"><NInput v-model:value="userForm.office_equipment" /></NFormItem>
        <NFormItem label="Обучение / Медкомиссия"><NInput v-model:value="userForm.training" /></NFormItem>
        <NFormItem label="Спецодежда">
          <NSelect v-model:value="userForm.work_clothes" multiple tag filterable :options="workClothesOptions" />
        </NFormItem>
        <NFormItem label="Инструмент">
          <NSelect v-model:value="userForm.instruments" multiple tag filterable :options="instrumentOptions" />
        </NFormItem>
        <NFormItem label="Оборудование инженера">
          <NSelect v-model:value="userForm.equipments" multiple tag filterable :options="engineerEquipmentOptions" />
        </NFormItem>
        <NFormItem label="Роли">
          <NCheckboxGroup v-model:value="userForm.roles">
            <NSpace>
              <NCheckbox v-for="r in roles" :key="r.name" :value="r.name" :label="r.name" />
            </NSpace>
          </NCheckboxGroup>
        </NFormItem>
        <NSpace justify="end">
          <NButton @click="userModalOpen = false">Отмена</NButton>
          <NButton type="primary" @click="saveUser">Сохранить</NButton>
        </NSpace>
      </NForm>
    </NModal>

    <NModal v-model:show="pwdModalOpen" preset="card" title="Смена пароля сотрудника" style="width: 380px">
      <NForm>
        <NFormItem label="Новый пароль"><NInput v-model:value="newPassword" type="password" /></NFormItem>
        <NSpace justify="end">
          <NButton @click="pwdModalOpen = false">Отмена</NButton>
          <NButton type="primary" @click="changeUserPassword">Сменить</NButton>
        </NSpace>
      </NForm>
    </NModal>

    <NModal v-model:show="tokenModalOpen" preset="card" title="Выпустить API-токен" style="width: 420px">
      <NForm>
        <NFormItem label="Название"><NInput v-model:value="newTokenName" placeholder="Мобильный клиент / Агент" /></NFormItem>
        <NFormItem label="Разрешить запись (мутации)"><NSwitch v-model:value="newTokenWrite" /></NFormItem>
        <NButton type="primary" block @click="createToken">Выпустить</NButton>
      </NForm>
    </NModal>

    <NModal v-model:show="roleModalOpen" preset="card" :title="`Права роли: ${roleForm.name}`" style="width: 680px">
      <NForm>
        <NFormItem label="Описание"><NInput v-model:value="roleForm.description" /></NFormItem>
        <NFormItem label="Разрешения">
          <NCheckboxGroup v-model:value="roleForm.permissions">
            <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 6px; max-height: 360px; overflow: auto">
              <NCheckbox v-for="p in perms" :key="p.key" :value="p.key" :label="`${p.key} — ${p.description}`" />
            </div>
          </NCheckboxGroup>
        </NFormItem>
        <NSpace justify="end">
          <NButton @click="roleModalOpen = false">Отмена</NButton>
          <NButton type="primary" @click="saveRole">Сохранить роль</NButton>
        </NSpace>
      </NForm>
    </NModal>
  </NSpace>
</template>
