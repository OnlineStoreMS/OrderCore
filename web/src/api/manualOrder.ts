import client, { unwrap } from './client'
import type { Order } from './orders'

export interface ParsedAddress {
  name?: string
  phone?: string
  tel?: string
  address?: {
    province?: string
    city?: string
    district?: string
    detail?: string
    str?: string
  }
  shipContent?: string
}

export async function parseManualAddress(rawAddress: string, batch = false) {
  return unwrap<ParsedAddress | ParsedAddress[]>(
    await client.post('/orders/manual/parse-address', { rawAddress, batch }),
  )
}

export async function searchPIMProducts(keyword: string, page = 1, pageSize = 20) {
  return unwrap<{
    list: Array<{
      productId?: number
      productName?: string
      skuId?: number
      skuCode?: string
      specLabel?: string
      specs?: Record<string, string> | string
      price?: number
      pic?: string
    }>
    total: number
  }>(await client.get('/orders/manual/products/pim', { params: { keyword, page, pageSize } }))
}

export async function searchShopProducts(params: {
  platform?: string
  shopId?: string
  /** SKU 规格名称 / 规格编码 */
  keyword?: string
  title?: string
  pageNo?: number
  pageSize?: number
}) {
  return unwrap<{
    total: number
    items: Array<{
      itemId: string
      title: string
      outerId?: string
      picUrl?: string
      platform?: string
      shopId?: string
      shopName?: string
      skus?: Array<{
        skuId: string
        propertiesName?: string
        outerId?: string
        price?: string
        picUrl?: string
      }>
    }>
  }>(await client.get('/orders/manual/products/shop', { params }))
}

export async function lookupManualCustomer(phone: string) {
  return unwrap<{
    id?: number
    displayName?: string
    primaryPhone?: string
    addresses?: Array<{
      id: number
      contactName?: string
      phone?: string
      province?: string
      city?: string
      district?: string
      detail?: string
      isDefault?: number
    }>
  } | null>(await client.get('/orders/manual/customers', { params: { phone } }))
}

export async function listManualCustomerAddresses(customerId: number) {
  return unwrap<
    Array<{
      id: number
      contactName?: string
      phone?: string
      province?: string
      city?: string
      district?: string
      detail?: string
      isDefault?: number
    }>
  >(await client.get(`/orders/manual/customers/${customerId}/addresses`))
}

export interface RecipientSearchItem {
  customerId: number
  addressId: number
  displayName?: string
  primaryPhone?: string
  contactName?: string
  phone?: string
  province?: string
  city?: string
  district?: string
  detail?: string
  label?: string
  isDefault?: number
}

export async function searchManualRecipients(keyword: string, page = 1, pageSize = 20) {
  return unwrap<{
    list: RecipientSearchItem[]
    total: number
    page: number
    pageSize: number
  }>(await client.get('/orders/manual/recipients', { params: { keyword, page, pageSize } }))
}

export async function createManualOrder(body: Record<string, unknown>) {
  return unwrap<Order>(await client.post('/orders/manual', body))
}

export async function createManualOrdersBatch(body: Record<string, unknown>) {
  return unwrap<{ orders: Order[]; total: number }>(await client.post('/orders/manual/batch', body))
}
