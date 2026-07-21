import client, { unwrap, type PageData } from './client'

export interface OrderItem {
  id?: number
  skuId?: number
  skuCode?: string
  productName?: string
  skuSpecs?: string
  picUrl?: string
  quantity: number
  price: number
  totalAmount?: number
}

export interface OrderAddress {
  name?: string
  phone?: string
  province?: string
  city?: string
  district?: string
  address?: string
  fullText?: string
}

export interface OrderShipment {
  id: number
  shipmentNo: string
  expressCompany?: string
  expressNo?: string
  needTracking: boolean
  callbackStatus: string
  callbackMessage?: string
  shippedAt?: string
}

export interface Order {
  id: number
  orderNo: string
  sourceChannel: string
  platform?: string
  platformOrderId?: string
  platformSysTid?: string
  shopId?: string
  shopName?: string
  status: string
  shipStatus?: string
  allocType?: string
  dropshipMode?: string
  supplierId?: number
  supplierName?: string
  factoryId?: string
  factoryName?: string
  purchaseOrderId?: string
  buyerNick?: string
  buyerName?: string
  buyerPhone?: string
  totalAmount?: number
  payAmount?: number
  payTime?: string
  orderedAt?: string
  platformStatus?: string
  platformStatusText?: string
  ecommerceStatus?: string
  ecommerceStatusText?: string
  afterSaleStatus?: string
  afterSaleStatusText?: string
  agentType?: number
  shipEntryLocked?: boolean
  shipLockReason?: string
  skipAutoAlloc?: boolean
  remark?: string
  sellerRemark?: string
  allocRemark?: string
  allocatedAt?: string
  shippedAt?: string
  createdAt?: string
  items?: OrderItem[]
  address?: OrderAddress
  shipments?: OrderShipment[]
  statusLogs?: Array<{
    id: number
    fromStatus?: string
    toStatus: string
    action?: string
    remark?: string
    createdAt?: string
  }>
}

export interface SupplierItem {
  id: number
  code?: string
  name: string
  shortName?: string
  contactName?: string
  phone?: string
}

export interface SupplierBinding {
  id: number
  supplierId: number
  supplierCode?: string
  supplierName: string
  sourceChannel: string
  externalFactoryId: string
  externalFactoryName?: string
  platform?: string
  remark?: string
  status: number
}

export interface FactoryItem {
  id?: string
  factoryId: string
  factoryName: string
  factoryNick?: string
}

const sourceLabels: Record<string, string> = {
  kdzs: '电商',
  wx_mall: '小程序',
  store: '门店',
  xianyu: '闲鱼',
  manual: '手工订单',
}

/** 工作台/筛选项固定订单类型顺序 */
export const orderTypeOptions = [
  { value: 'kdzs', label: '电商', tip: 'StoreSyncAgent' },
  { value: 'wx_mall', label: '小程序', tip: 'MallCore 私域商城' },
  { value: 'store', label: '门店', tip: 'StoreCore' },
  { value: 'xianyu', label: '闲鱼', tip: '后续接入' },
  { value: 'manual', label: '手工订单', tip: '' },
] as const

const statusLabels: Record<string, string> = {
  pending_payment: '待付款',
  pending_alloc: '待分配',
  pending_ship: '待分配', // 历史值兼容
  allocated: '已分配',
  purchasing: '采购中',
  shipped: '已发货', // 历史履约值兼容
  partial_ship: '部分发货',
  completed: '已完成',
  closed: '已关闭',
}

const shipStatusLabels: Record<string, string> = {
  wait_ship: '待发货',
  shipped: '已发货',
}

const allocLabels: Record<string, string> = {
  self_ship: '自营发货',
  dropship: '代发发货',
  purchase_then_ship: '采购发货',
}

const dropshipLabels: Record<string, string> = {
  kdzs_factory: '快递助手厂家代发',
  osms_supplier: 'OSMS供应商代发',
}

const platformLabels: Record<string, string> = {
  FXG: '抖店',
  TB: '淘宝',
  XHS: '小红书',
  PDD: '拼多多',
  KSXD: '快手',
  MANUAL: '手工单',
}

const platformStatusLabels: Record<string, string> = {
  wait_audit: '待推单',
  wait_send: '待发货',
  shipped: '已发货',
  seller_consigned: '已发货',
  completed: '交易完成',
  trade_finished: '交易完成',
}

export function labelSource(v?: string) {
  return (v && sourceLabels[v]) || v || '-'
}
/** 平台展示：FXG / 店铺名 */
export function formatPlatformShop(row: Pick<Order, 'platform' | 'shopName'>) {
  const p = (row.platform || '').trim()
  const shop = (row.shopName || '').trim()
  if (p && shop) return `${p} / ${shop}`
  if (p) return p
  if (shop) return shop
  return '-'
}
export function labelStatus(v?: string) {
  return (v && statusLabels[v]) || v || '-'
}
export function labelShipStatus(v?: string) {
  return (v && shipStatusLabels[v]) || v || '-'
}
export function labelAlloc(v?: string) {
  return (v && allocLabels[v]) || v || '-'
}
export function labelDropship(v?: string) {
  return (v && dropshipLabels[v]) || v || '-'
}
export function labelPlatform(v?: string) {
  return (v && platformLabels[v]) || v || '-'
}
export function labelKDZSStatus(order: Pick<Order, 'platformStatus' | 'platformStatusText' | 'sourceChannel'>) {
  if (order.sourceChannel !== 'kdzs') return '-'
  if (order.platformStatusText) return order.platformStatusText
  if (order.platformStatus && platformStatusLabels[order.platformStatus]) {
    return platformStatusLabels[order.platformStatus]
  }
  return order.platformStatus || '-'
}

export function labelEcommerceStatus(order: Pick<Order, 'ecommerceStatus' | 'ecommerceStatusText' | 'afterSaleStatus' | 'afterSaleStatusText' | 'sourceChannel'>) {
  if (order.sourceChannel !== 'kdzs') return '-'
  const main = order.ecommerceStatusText || order.ecommerceStatus || ''
  const after = order.afterSaleStatusText || order.afterSaleStatus || ''
  if (main && after && after !== '—' && after !== '-') return `${main} / ${after}`
  return main || after || '-'
}

export function labelAgentType(v?: number) {
  if (v === 2) return '厂家代发'
  if (v === 1) return '自营'
  return '-'
}

export function formatDateTime(v?: string | null) {
  if (!v) return '-'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return v
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

export function formatGoods(items?: OrderItem[]) {
  if (!items?.length) return '-'
  return items.map((it) => {
    const name = it.productName || it.skuCode || '商品'
    const specs = it.skuSpecs ? `（${it.skuSpecs}）` : ''
    return `${name}${specs} ×${it.quantity || 1}`
  }).join('；')
}

export function formatAddress(addr?: OrderAddress | null) {
  if (!addr) return '-'
  if (addr.fullText) return addr.fullText
  const parts = [addr.name, addr.phone, addr.province, addr.city, addr.district, addr.address].filter(Boolean)
  return parts.join(' ') || '-'
}

export function formatRemark(order: Pick<Order, 'remark' | 'sellerRemark'>) {
  const parts = [order.remark, order.sellerRemark].map((s) => (s || '').trim()).filter(Boolean)
  return parts.length ? parts.join(' / ') : '-'
}

export async function fetchDashboard() {
  return unwrap(await client.get('/dashboard'))
}

export async function listOrders(params: Record<string, unknown>) {
  return unwrap<PageData<Order>>(await client.get('/orders', { params }))
}

export async function getOrder(id: number) {
  return unwrap<Order>(await client.get(`/orders/${id}`))
}

export async function createManualOrder(body: Record<string, unknown>) {
  return unwrap<Order>(await client.post('/orders/manual', body))
}

export async function allocateOrder(id: number, body: Record<string, unknown>) {
  return unwrap<Order>(await client.post(`/orders/${id}/allocate`, body))
}

export async function revokeAllocateOrder(id: number) {
  return unwrap<Order>(await client.post(`/orders/${id}/revoke-allocate`))
}

export async function shipOrder(id: number, body: Record<string, unknown>) {
  return unwrap<Order>(await client.post(`/orders/${id}/ship`, body))
}

export async function syncKDZS(body: Record<string, unknown> = {}) {
  return unwrap(await client.post('/sync/kdzs', body, { timeout: 180000 }))
}

export async function syncStore(body: Record<string, unknown> = {}) {
  return unwrap(await client.post('/sync/store', body, { timeout: 180000 }))
}

export async function listFactories(params: Record<string, unknown> = {}) {
  return unwrap<{ items: FactoryItem[] }>(await client.get('/kdzs/factories', { params }))
}

export async function listSuppliers(params: Record<string, unknown> = {}) {
  return unwrap<PageData<SupplierItem>>(await client.get('/suppliers', { params }))
}

export async function listBindings() {
  return unwrap<SupplierBinding[]>(await client.get('/supplier-bindings'))
}

export async function createBinding(body: Record<string, unknown>) {
  return unwrap<SupplierBinding>(await client.post('/supplier-bindings', body))
}

export async function updateBinding(id: number, body: Record<string, unknown>) {
  return unwrap<SupplierBinding>(await client.put(`/supplier-bindings/${id}`, body))
}

export async function deleteBinding(id: number) {
  return unwrap(await client.delete(`/supplier-bindings/${id}`))
}
