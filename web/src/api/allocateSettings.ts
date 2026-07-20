import client, { unwrap } from './client'
import type { SupplierItem } from './orders'

export type { SupplierItem }

export interface AllocSettings {
  id?: number
  tenantId?: number
  enabled: boolean
  strategy: string
  createdAt?: string
  updatedAt?: string
}

export interface SkuSupplierRule {
  id: number
  tenantId: number
  skuCode: string
  supplierId: number
  supplierCode?: string
  supplierName: string
  priority: number
  status: number
  remark?: string
  createdAt?: string
  updatedAt?: string
}

export interface SkuSupplierRulePayload {
  skuCode: string
  supplierId: number
  supplierCode?: string
  supplierName: string
  priority?: number
  status?: number
  remark?: string
}

export async function getAllocSettings() {
  return unwrap<AllocSettings>(await client.get('/alloc-settings'))
}

export async function updateAllocSettings(data: { enabled: boolean; strategy?: string }) {
  return unwrap<AllocSettings>(await client.put('/alloc-settings', data))
}

export async function listSkuSupplierRules(keyword?: string) {
  return unwrap<SkuSupplierRule[]>(
    await client.get('/sku-supplier-rules', { params: keyword ? { keyword } : undefined }),
  )
}

export async function createSkuSupplierRule(data: SkuSupplierRulePayload) {
  return unwrap<SkuSupplierRule>(await client.post('/sku-supplier-rules', data))
}

export async function updateSkuSupplierRule(id: number, data: SkuSupplierRulePayload) {
  return unwrap<SkuSupplierRule>(await client.put(`/sku-supplier-rules/${id}`, data))
}

export async function deleteSkuSupplierRule(id: number) {
  return unwrap<{ ok: boolean }>(await client.delete(`/sku-supplier-rules/${id}`))
}
