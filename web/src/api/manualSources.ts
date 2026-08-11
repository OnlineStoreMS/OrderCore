import client, { unwrap } from './client'

export interface ManualOrderSource {
  id: number
  tenantId: number
  name: string
  code?: string
  sort: number
  enabled: boolean
  remark?: string
  createdAt?: string
  updatedAt?: string
}

export async function listManualOrderSources(params?: { enabledOnly?: boolean }) {
  return unwrap<ManualOrderSource[]>(
    await client.get('/manual-order-sources', {
      params: params?.enabledOnly ? { enabledOnly: '1' } : undefined,
    }),
  )
}

export async function createManualOrderSource(body: {
  name: string
  code?: string
  sort?: number
  enabled?: boolean
  remark?: string
}) {
  return unwrap<ManualOrderSource>(await client.post('/manual-order-sources', body))
}

export async function updateManualOrderSource(
  id: number,
  body: {
    name?: string
    code?: string
    sort?: number
    enabled?: boolean
    remark?: string
  },
) {
  return unwrap<ManualOrderSource>(await client.put(`/manual-order-sources/${id}`, body))
}

export async function deleteManualOrderSource(id: number) {
  return unwrap<{ ok: boolean }>(await client.delete(`/manual-order-sources/${id}`))
}
