<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { formatDateTime } from '../../api/orders'
import {
  listSyncJobs,
  runSyncJob,
  updateSyncJob,
  type SyncJob,
} from '../../api/settings'

const loading = ref(false)
const syncJobs = ref<SyncJob[]>([])

async function load() {
  loading.value = true
  try {
    syncJobs.value = (await listSyncJobs()) || []
  } catch (e: any) {
    ElMessage.error(e.message || '加载失败')
  } finally {
    loading.value = false
  }
}

function jobTypeLabel(t: string) {
  if (t === 'kdzs_orders') return '电商订单（快递助手）'
  if (t === 'store_orders') return '门店订单（StoreCore）'
  return t
}

async function toggleJob(job: SyncJob, enabled: boolean) {
  try {
    await updateSyncJob(job.id, { enabled })
    ElMessage.success(enabled ? '已开启定时同步' : '已关闭定时同步')
    await load()
  } catch (e: any) {
    ElMessage.error(e.message || '保存失败')
  }
}

async function saveInterval(job: SyncJob, minutes: number | undefined) {
  if (minutes == null || minutes < 5) return
  try {
    await updateSyncJob(job.id, { intervalMinutes: minutes })
    ElMessage.success('间隔已更新')
    await load()
  } catch (e: any) {
    ElMessage.error(e.message || '保存失败')
  }
}

async function onRunJob(job: SyncJob) {
  try {
    const stats = await runSyncJob(job.id)
    ElMessage.success(
      `同步完成：新增 ${stats.created || 0}，更新 ${stats.updated || 0}，刷新 ${stats.refreshed || 0}`,
    )
    await load()
  } catch (e: any) {
    ElMessage.error(e.message || '同步失败')
    await load()
  }
}

onMounted(load)
</script>

<template>
  <div v-loading="loading" class="page">
    <section class="block">
      <div class="head">
        <div>
          <h3>同步设置</h3>
          <p>配置定时同步任务，也可手动立即同步。开启后按间隔自动拉取订单并刷新未完结单状态。</p>
        </div>
      </div>
      <el-table :data="syncJobs" stripe empty-text="暂无同步任务">
        <el-table-column label="任务" min-width="180">
          <template #default="{ row }">
            <div>{{ row.name || jobTypeLabel(row.jobType) }}</div>
            <div class="muted">{{ jobTypeLabel(row.jobType) }}</div>
          </template>
        </el-table-column>
        <el-table-column label="启用" width="100">
          <template #default="{ row }">
            <el-switch :model-value="row.enabled" @change="(v: boolean) => toggleJob(row, v)" />
          </template>
        </el-table-column>
        <el-table-column label="间隔(分钟)" width="160">
          <template #default="{ row }">
            <el-input-number
              :model-value="row.intervalMinutes"
              :min="5"
              :max="1440"
              size="small"
              @change="(v: number | undefined) => saveInterval(row, v)"
            />
          </template>
        </el-table-column>
        <el-table-column label="上次运行" min-width="240">
          <template #default="{ row }">
            <div>{{ formatDateTime(row.lastRunAt) }}</div>
            <div v-if="row.lastRunAt" class="muted">
              {{ row.lastRunOk ? '成功' : '失败' }}
              <span v-if="row.lastError"> · {{ row.lastError }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="120" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="onRunJob(row)">立即同步</el-button>
          </template>
        </el-table-column>
      </el-table>
    </section>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 16px; }
.block {
  background: #fff;
  border-radius: 8px;
  padding: 16px 20px 20px;
  border: 1px solid #f0f0f0;
}
.head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 14px;
  gap: 12px;
}
.head h3 { margin: 0 0 6px; font-size: 16px; }
.head p { margin: 0; color: #8c8c8c; font-size: 13px; line-height: 1.5; }
.muted { color: #8c8c8c; font-size: 12px; margin-top: 2px; }
</style>
