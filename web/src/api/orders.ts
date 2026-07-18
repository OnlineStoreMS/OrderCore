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
  platformStatus?: string
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
  kdzs: '电商(快递助手)',
  wx_mall: '微信小程序',
  store: '门店销售',
  manual: '手工订单',
}

const statusLabels: Record<string, string> = {
  pending_payment: '待付款',
  pending_ship: '待发货',
  allocated: '已分配',
  purchasing: '采购中',
  shipped: '已发货',
  partial_ship: '部分发货',
  completed: '已完成',
  closed: '已关闭',
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

export function labelSource(v?: string) {
  return (v && sourceLabels[v]) || v || '-'
}
export function labelStatus(v?: string) {
  return (v && statusLabels[v]) || v || '-'
}
export function labelAlloc(v?: string) {
  return (v && allocLabels[v]) || v || '-'
}
export function labelDropship(v?: string) {
  return (v && dropshipLabels[v]) || v || '-'
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

export async function shipOrder(id: number, body: Record<string, unknown>) {
  return unwrap<Order>(await client.post(`/orders/${id}/ship`, body))
}

export async function syncKDZS(body: Record<string, unknown> = {}) {
  return unwrap(await client.post('/sync/kdzs', body))
}

export async function syncStore(body: Record<string, unknown> = {}) {
  return unwrap(await client.post('/sync/store', body))
}

export async function listFactories(params: Record<string, unknown> = {}) {
  return unwrap<{ items: FactoryItem[] }>(await client.get('/kdzs/factories', { params }))
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
