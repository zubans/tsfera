export interface User {
  id: number
  username: string
  email?: string | null
  first_name: string
  second_name: string
  last_name: string
  full_name: string
  subdivision_id?: number | null
  is_engineer: boolean
  is_dispatcher: boolean
  color: string
  corporate_phone_number: string
  office_equipment?: string
  training?: string
  car_rubber?: string
  car_model?: string
  car_fuel_card_number?: string
  car_government_number?: string
  car_sts_number?: string
  car_contract_number?: string
  work_clothes?: string[]
  instruments?: string[]
  equipments?: string[]
  avatar_path?: string
  active: boolean
  roles: string[]
  last_login_at?: string | null
}

export interface InventoryItem {
  id: number
  category: string
  name: string
  deleted: boolean
  created_at: string
}

export interface Role {
  name: string
  description: string
  is_system: boolean
  permissions: string[]
}

export interface PermissionDef {
  key: string
  group: string
  resource: string
  action: string
  description: string
}

export interface APIToken {
  id: number
  name: string
  can_read: boolean
  can_write: boolean
  expires_at?: string | null
  last_used_at?: string | null
  created_at: string
}

export interface LoginEvent {
  id: number
  event: string
  username: string
  ip: string
  reason?: string
  created_at: string
}

export interface ContractorPhone {
  id?: number
  phone: string
  formatted?: string
  note: string
}

export interface AddressEquipment {
  id: number
  address_id: number
  equipment_id: number
  equipment_name: string
  serial_number: string
  note: string
}

export interface ContractorAddress {
  id: number
  contractor_id: number
  address: string
  city?: string
  street?: string
  house?: string
  apartment?: string
  comment?: string
  equipments?: AddressEquipment[]
}

export interface LegalEntity {
  contractor_id?: number
  name: string
  director_name: string
  actual_address: string
  legal_address: string
  bank_name: string
  inn: string
  kpp: string
  bik: string
  checking_account: string
  corresponding_account: string
  okpo: string
  ogrn: string
  updated_at?: string
}

export interface TrafficSource {
  id: number
  name: string
  traffic_source_type_id: number
  sort: number
  active: boolean
  created_at?: string
  updated_at?: string
}

export interface CertificateExecutor {
  user_id: number
  full_name?: string
  sum: number
}

export interface Contractor {
  client_category_id?: number
  id: number
  name: string
  first_name?: string
  second_name?: string
  last_name?: string
  subdivision_id?: number | null
  type: number
  is_legal_entity?: boolean
  traffic_source_id?: number | null
  legal_entity?: LegalEntity | null
  note: string
  phones?: ContractorPhone[]
  emails?: { id?: number; email: string }[]
  addresses?: ContractorAddress[]
}

export interface SimilarContractorMatch {
  contractor_id: number
  contractor_name: string
  matched_on: string
  contact_value: string
}

export interface Equipment {
  id: number
  type: string
  model: string
  manufacturer: string
  name: string
}

export interface CalendarEvent {
  id: number
  title: string
  start: string
  end: string
  contractor_id: number
  contractor_name: string
  address_id: number
  address_text: string
  phones: string[]
  engineer_id: number
  engineer_name: string
  color: string
  status: number
  is_complete: boolean
  is_duty: boolean
  work_types: string[]
  equipments: string[]
  serial_numbers: string[]
  acts_total_amount: number
  planned_sum?: number
  work_window_from?: string | null
  work_window_to?: string | null
  executor_ids?: number[]
  traffic_source_id?: number | null
  analytic_id?: number | null
  analytic_rating?: number | null
  special_notes: string
  notes_viewed: boolean
  rating?: number | null
}

export interface CertificateTelemetry {
  smoke_channel_vacuum?: string
  gas_pressure_in_the_pipeline?: string
  burner_gas_pressure?: string
  working_pressure?: string
  heating_expansion_tank_pressure?: string
  DHW_expansion_tank_pressure?: string
  DHW_flow?: string
  partial_heat_setting?: string
  voltage?: string
  type_of_coolant?: string
  coolant_parameters?: string
  contract_eligible?: boolean
  has_air_conditioner?: boolean
}

export interface Certificate {
  equipments?: { id: number; equipment_id: number; serial_number: string }[]
  number?: number
  source_data?: Record<string, unknown>
  source_relations?: Record<string, unknown>
  id: number
  application_id?: number | null
  maintenance_contract_id?: number | null
  without_maintenance_contract?: boolean
  contractor_id: number
  contractor_name?: string
  address_id: number
  address_text?: string
  engineer_id: number
  engineer_name?: string
  work_date: string
  total_amount: number
  paid_amount: number
  payment_method: string
  receipt_number?: string
  cost_of_parts?: number
  notes_viewed?: boolean
  work_types: string[]
  spare_parts: { name: string; quantity: number; price: number }[]
  executors?: CertificateExecutor[]
  analytic_id?: number | null
  analytic_rating?: number | null
  results: CertificateTelemetry
  special_notes: string
  other_note: string
  recommendation: string
  comment: string
  status?: number
}

export interface Application {
  phone_ids?: number[]
  workflow_status?: number
  incomplete?: boolean
  number?: number
  source_data?: Record<string, unknown>
  source_relations?: Record<string, unknown>
  id: number
  contractor_id: number
  contractor_name?: string
  address_id: number
  address_text?: string
  engineer_id: number
  engineer_name?: string
  engineer_color?: string
  subdivision_id?: number | null
  traffic_source_id?: number | null
  planned_sum?: number
  work_window_from?: string | null
  work_window_to?: string | null
  executor_ids?: number[]
  executor_names?: string[]
  analytic_id?: number | null
  analytic_rating?: number | null
  plan_datetime_from: string
  plan_datetime_to: string
  status: number
  is_complete: boolean
  is_duty: boolean
  work_types: string[]
  price_note: string
  comment: string
  special_notes: string
  notes_viewed: boolean
  rating?: number | null
  rating_comment?: string
  acts_total_amount?: number
}

export interface ContractEquipment {
  id?: number
  equipment_id: number
  equipment_name?: string
  serial_number: string
}

export interface MaintenanceContract {
  id: number
  contractor_id: number
  contractor_name?: string
  address_id: number
  address_text?: string
  application_id?: number | null
  number: string
  category?: string
  price: number
  signed_at: string
  valid_from: string
  valid_to: string
  status: number
  note: string
  has_newer_contract?: boolean
  needs_prolongation?: boolean
  equipments?: ContractEquipment[]
}

export interface CRMTask {
  missing_deadline?: boolean
  id: number
  contractor_id?: number | null
  contractor_name?: string
  address_id?: number | null
  address_text?: string
  application_id?: number | null
  contract_id?: number | null
  prev_task_id?: number | null
  task_type: string
  title: string
  description: string
  status: number
  task_result?: string
  priority: number
  assigned_user_id?: number | null
  assigned_name?: string
  plan_datetime_to: string
  execution_datetime?: string | null
  is_urgent: boolean
  note: string
}

export interface TaskPoolSetting {
  id: number
  name: string
  task_types: string[]
  priority: number
  time_from: string
  time_to: string
  is_online: boolean
  with_subdivision: boolean
  for_dispatcher: boolean
  for_engineer: boolean
  tasks_max_count: number
  transferred_tasks_max_count: number
  active: boolean
}

export interface CallRecord {
  id: number
  external_call_id: string
  abonent_id?: string
  ext_tracking_id?: string
  direction: string
  status: string
  caller_phone: string
  callee_phone: string
  contractor_id?: number | null
  contractor_name?: string
  user_id?: number | null
  user_name?: string
  traffic_source_id?: number | null
  traffic_source_name?: string
  caller_type?: number
  call_result?: number
  type_of_appeal?: string
  comment?: string
  answer_time?: string | null
  duration_sec: number
  started_at: string
}

export interface MissCallPoolItem {
  id: number
  call_id: number
  user_id: number
  is_viewed: boolean
  caller_phone: string
  callee_phone: string
  started_at: string
  contractor_id?: number | null
  contractor_name?: string
  created_at: string
}

export interface AnalyticQuestionOption {
  id: number
  name: string
  sort: number
}

export interface AnalyticQuestion {
  id: number
  name: string
  analytic_document_type_id: number
  sort: number
  active: boolean
  options: AnalyticQuestionOption[]
}

export interface AnalyticAnswer {
  question_id: number
  question?: string
  option_ids: number[]
  option_names?: string[]
}

export interface Analytic {
  id: number
  number: string
  certificate_id: number
  rating: number
  comment: string
  status: number
  answers: AnalyticAnswer[]
  contractor_id?: number
  contractor_name?: string
  engineer_id?: number
  engineer_name?: string
  created_at?: string
}

export interface Banner {
  id: number
  name: string
  text: string
  file_id?: number | null
  file_url?: string
  active: boolean
}

export interface AppSetting {
  key: string
  name: string
  data: unknown
  updated_at?: string
}

export interface DaDataAddressSuggestion {
  value: string
  unrestricted_value: string
  postal_code?: string
  city?: string
  street?: string
  house?: string
  flat?: string
}

export interface RefItem {
  id: number
  kind: string
  name: string
  deleted: boolean
  created_at: string
}

export interface NotificationItem {
  id: number
  kind: string
  title: string
  message: string
  entity_type: string
  entity_id: number
  is_read: boolean
  created_at: string
}

export interface DocDocument {
  id: number
  space: string
  doc_type: string
  title: string
  slug: string
  content: string
  status: string
  tags: string[]
  version: number
  visibility: string
  outline?: { level: number; title: string; anchor: string; line: number }[]
  entity_links?: { id: number; entity_type: string; entity_id: number; entity_title?: string; entity_path?: string; note: string }[]
  attachments?: { id: number; filename: string; mime_type: string; size_bytes: number }[]
  updated_at: string
}

export interface DocComment {
  id: number
  document_id: number
  author_name: string
  body: string
  quote: string
  resolved: boolean
  created_at: string
}

export interface DocRevision {
  id: number
  version: number
  title: string
  diff_summary: string
  compressed: boolean
  created_at: string
}

export interface AIProvider {
  id: number
  name: string
  kind: string
  base_url: string
  has_api_key: boolean
  active: boolean
  is_default: boolean
}

export interface AIModel {
  id: number
  provider_id: number
  provider_name?: string
  name: string
  model_id: string
  capability: string
  context_window: number
  is_default: boolean
  open_to_all: boolean
  active: boolean
}

export interface SearchResult {
  source_type: string
  source_id: string
  title: string
  anchor?: string
  snippet: string
  path: string
  score: number
}

export interface SourceRef {
  source_type: string
  source_id: string
  title: string
  anchor?: string
  snippet?: string
  path?: string
}

export interface ToolStep {
  name: string
  arguments?: string
  summary: string
  error?: string
  duration_ms: number
}

export interface ChatMessage {
  id: number
  role: 'user' | 'assistant'
  content: string
  steps?: ToolStep[]
  sources?: SourceRef[]
  tokens_used?: number
  created_at: string
}

export interface Conversation {
  id: number
  title: string
  mode: 'agent' | 'rag'
  model_id: number
  entity_type?: string
  entity_id?: number
  messages?: ChatMessage[]
  updated_at: string
}

export interface ConversationShare {
  id: number
  conversation_id: number
  share_token: string
  revoked: boolean
  expires_at?: string | null
  created_at: string
}

export interface UserBrief {
  id: number
  username: string
  full_name: string
  color: string
  subdivision_id?: number | null
  is_engineer: boolean
  is_dispatcher: boolean
  active: boolean
}

export interface EnumOption {
  id: number
  key: string
  label: string
  legacy?: boolean
}

export interface CRMEnums {
  work_types: string[]
  document_statuses: { id: number; label: string }[]
  task_types: { id: string; label: string }[]
  task_statuses: { id: number; label: string }[]
  contract_categories?: EnumOption[]
  payment_options?: EnumOption[]
  task_results?: EnumOption[]
  call_types_of_appeal?: EnumOption[]
  caller_types?: EnumOption[]
  call_results?: EnumOption[]
  traffic_source_types?: EnumOption[]
  call_directions?: EnumOption[]
  call_statuses?: EnumOption[]
}

export interface SubdivisionPhone {
  id?: string
  name?: string
  phone: string
  is_main?: boolean
  is_duty: boolean
}

export interface SubdivisionPaymentOption {
  payment_option_id: number
  key?: string
  file_id?: number | null
  file_url?: string
  is_main: boolean
}

export interface Subdivision {
  id: number
  name: string
  address: string
  supervisor_user_id?: number | null
  phones?: SubdivisionPhone[]
  payment_options?: SubdivisionPaymentOption[]
}

export interface SipAccount {
  id: number
  name: string
  domain: string
  login: string
  abonent_id?: string
  protocol: string
  server: string
  port: number
  subdivision_id?: number | null
  user_id?: number | null
  active: boolean
}

export interface SipUserSelectOption {
  id: number
  abonent_id: string
  name: string
  user_id?: number | null
  full_name: string
}

export interface ChangeLogItem {
  id: number
  entity_type: string
  entity_id: number
  action: string
  user_id: number
  user_name?: string
  changes: unknown
  created_at: string
}

export interface AmountActsReportRow {
  engineer_id: number
  engineer_name: string
  subdivision_id?: number | null
  subdivision_name?: string
  certificates_count: number
  total_amount: number
  planned_sum_total?: number
  sum_total?: number
  paid_amount: number
  debt_amount: number
  contracts_amount?: number
  by_payment_option?: Record<string, number>
}

export interface ExpiredTaskReportRow {
  task_id: number
  title: string
  task_type: string
  assigned_user_id?: number | null
  assigned_name: string
  contractor_name: string
  plan_datetime_to: string
  overdue_minutes: number
}

export interface SubdivisionSumReportRow {
  subdivision_id: number
  subdivision_name: string
  planned_sum_total: number
  sum_total: number
  by_payment_option: Record<string, number>
}

export interface AmountSparePartsReport {
  total_cost_of_parts: number
  certificates_count: number
}

export interface CallConversionRow {
  type_of_appeal: string
  label: string
  total: number
  results: Record<number, number>
  percents: Record<number, number>
  other_count: number
  other_percent: number
}

export interface CallConversionReport {
  rows: CallConversionRow[]
  target_row: CallConversionRow
  summary_row: CallConversionRow
}

export interface ContractorReportRow {
  contractor_id: number
  name: string
  phones: string[]
  addresses: string[]
  certificates_count: number
  fact_sum: number
  cost_of_parts: number
}

export interface EngineerReportRow {
  engineer_id: number
  engineer_name: string
  certificates_count: number
  zero_sum_count: number
  zero_percent: number
  type_job_cert_count: number
  type_job_fact_sum: number
  planned_sum: number
  fact_sum: number
}

export interface TrafficSourceReportRow {
  traffic_source_id?: number
  name: string
  calls: number
  applications: number
  application_conversion: number
  closed_applications: number
  closed_application_conversion: number
  sum: number
  sum_conversion: number
}

export interface TrafficSourceReport {
  rows: TrafficSourceReportRow[]
  summary_row: TrafficSourceReportRow
}

export interface SpaceSummary {
  space: string
  count: number
}

export interface DocRevisionDetail extends DocRevision {
  content: string
}

export interface ModelAccessPolicy {
  model_id: number
  open_to_all: boolean | null
  user_ids: number[]
}

export interface AIUsageLog {
  id: number
  user_id: number
  model_id: number
  model_name: string
  operation: string
  prompt_tokens: number
  completion_tokens: number
  latency_ms: number
  status: string
  error_text?: string
  created_at: string
}
