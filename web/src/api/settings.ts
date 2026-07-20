import client, { unwrap } from './client'

export interface SyncJob {
  id: number
  jobType: string
  name: string
  enabled: boolean
  intervalMinutes: number
  paramsJson?: string
  lastRunAt?: string
  lastRunOk?: boolean
  lastError?: string
  lastStatsJson?: string
}

export interface NotificationChannel {
  id: number
  name: string
  channelType: string
  webhookUrl: string
  secret?: string
  enabled: boolean
  remark?: string
}

export interface PushRule {
  id: number
  supplierId: number
  event: string
  channelId: number
  enabled: boolean
  remark?: string
}

export interface PushLog {
  id: number
  orderId: number
  supplierId?: number
  channelId?: number
  event?: string
  channelType?: string
  status: string
  errorMessage?: string
  sentAt?: string
}

export async function listSyncJobs() {
  return unwrap<SyncJob[]>(await client.get('/sync-jobs'))
}

export async function updateSyncJob(id: number, body: Record<string, unknown>) {
  return unwrap<SyncJob>(await client.put(`/sync-jobs/${id}`, body))
}

export async function runSyncJob(id: number) {
  return unwrap<Record<string, number>>(await client.post(`/sync-jobs/${id}/run`))
}

export async function listChannels() {
  return unwrap<NotificationChannel[]>(await client.get('/notification-channels'))
}

export async function createChannel(body: Record<string, unknown>) {
  return unwrap<NotificationChannel>(await client.post('/notification-channels', body))
}

export async function updateChannel(id: number, body: Record<string, unknown>) {
  return unwrap<NotificationChannel>(await client.put(`/notification-channels/${id}`, body))
}

export async function deleteChannel(id: number) {
  return unwrap(await client.delete(`/notification-channels/${id}`))
}

export async function testChannel(id: number) {
  return unwrap(await client.post(`/notification-channels/${id}/test`))
}

export async function listPushRules() {
  return unwrap<PushRule[]>(await client.get('/push-rules'))
}

export async function createPushRule(body: Record<string, unknown>) {
  return unwrap<PushRule>(await client.post('/push-rules', body))
}

export async function updatePushRule(id: number, body: Record<string, unknown>) {
  return unwrap<PushRule>(await client.put(`/push-rules/${id}`, body))
}

export async function deletePushRule(id: number) {
  return unwrap(await client.delete(`/push-rules/${id}`))
}

export async function pushOrder(id: number, event = 'manual_push') {
  return unwrap(await client.post(`/orders/${id}/push`, {}, { params: { event } }))
}

export async function listPushLogs(orderId?: number) {
  return unwrap<PushLog[]>(await client.get('/push-logs', { params: orderId ? { orderId } : {} }))
}
