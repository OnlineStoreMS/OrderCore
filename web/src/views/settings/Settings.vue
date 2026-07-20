<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  createChannel,
  createPushRule,
  deleteChannel,
  deletePushRule,
  listChannels,
  listPushRules,
  listSyncJobs,
  runSyncJob,
  testChannel,
  updateChannel,
  updatePushRule,
  updateSyncJob,
  type NotificationChannel,
  type PushRule,
  type SyncJob,
} from '../../api/settings'
import { listSuppliers, type SupplierItem } from '../../api/orders'

const loading = ref(false)
const syncJobs = ref<SyncJob[]>([])
const channels = ref<NotificationChannel[]>([])
const rules = ref<PushRule[]>([])
const suppliers = ref<SupplierItem[]>([])

const channelVisible = ref(false)
const editingChannelId = ref<number | null>(null)
const channelForm = reactive({
  name: '',
  channelType: 'feishu_webhook',
  webhookUrl: '',
  secret: '',
  enabled: true,
  remark: '',
})

const ruleVisible = ref(false)
const editingRuleId = ref<number | null>(null)
const ruleForm = reactive({
  supplierId: 0,
  event: 'order_allocated',
  channelId: undefined as number | undefined,
  enabled: true,
  remark: '',
})

async function load() {
  loading.value = true
  try {
    const [jobs, chs, rs, sup] = await Promise.all([
      listSyncJobs(),
      listChannels(),
      listPushRules(),
      listSuppliers({ page: 1, pageSize: 200 }),
    ])
    syncJobs.value = jobs || []
    channels.value = chs || []
    rules.value = rs || []
    suppliers.value = sup.list || []
  } catch (e: any) {
    ElMessage.error(e.message || '加载失败')
  } finally {
    loading.value = false
  }
}

function jobTypeLabel(t: string) {
  if (t === 'kdzs_orders') return '电商订单'
  if (t === 'store_orders') return '门店订单'
  return t
}

function channelTypeLabel(t: string) {
  if (t === 'feishu_webhook') return '飞书机器人'
  if (t === 'wecom_webhook') return '企微机器人'
  return t
}

function supplierLabel(id: number) {
  if (!id) return '全部供应商（默认）'
  const s = suppliers.value.find((x) => x.id === id)
  return s ? `${s.name}${s.code ? ` (${s.code})` : ''}` : `供应商 #${id}`
}

function channelName(id: number) {
  return channels.value.find((x) => x.id === id)?.name || `#${id}`
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

async function saveInterval(job: SyncJob, minutes: number) {
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
    ElMessage.success(`同步完成：新增 ${stats.created || 0}，更新 ${stats.updated || 0}，刷新 ${stats.refreshed || 0}`)
    await load()
  } catch (e: any) {
    ElMessage.error(e.message || '同步失败')
    await load()
  }
}

function openCreateChannel() {
  editingChannelId.value = null
  Object.assign(channelForm, {
    name: '',
    channelType: 'feishu_webhook',
    webhookUrl: '',
    secret: '',
    enabled: true,
    remark: '',
  })
  channelVisible.value = true
}

function openEditChannel(row: NotificationChannel) {
  editingChannelId.value = row.id
  Object.assign(channelForm, {
    name: row.name,
    channelType: row.channelType,
    webhookUrl: row.webhookUrl,
    secret: '',
    enabled: row.enabled,
    remark: row.remark || '',
  })
  channelVisible.value = true
}

async function submitChannel() {
  if (!channelForm.name || !channelForm.webhookUrl) {
    ElMessage.warning('请填写名称与 Webhook')
    return
  }
  try {
    if (editingChannelId.value) {
      await updateChannel(editingChannelId.value, { ...channelForm })
      ElMessage.success('已更新')
    } else {
      await createChannel({ ...channelForm })
      ElMessage.success('已创建')
    }
    channelVisible.value = false
    await load()
  } catch (e: any) {
    ElMessage.error(e.message || '保存失败')
  }
}

async function onTestChannel(row: NotificationChannel) {
  try {
    await testChannel(row.id)
    ElMessage.success('测试消息已发送')
  } catch (e: any) {
    ElMessage.error(e.message || '测试失败')
  }
}

async function onDeleteChannel(row: NotificationChannel) {
  await ElMessageBox.confirm(`删除渠道「${row.name}」？`, '确认')
  try {
    await deleteChannel(row.id)
    ElMessage.success('已删除')
    await load()
  } catch (e: any) {
    ElMessage.error(e.message || '删除失败')
  }
}

function openCreateRule() {
  editingRuleId.value = null
  Object.assign(ruleForm, {
    supplierId: 0,
    event: 'order_allocated',
    channelId: channels.value[0]?.id,
    enabled: true,
    remark: '',
  })
  ruleVisible.value = true
}

function openEditRule(row: PushRule) {
  editingRuleId.value = row.id
  Object.assign(ruleForm, {
    supplierId: row.supplierId,
    event: row.event,
    channelId: row.channelId,
    enabled: row.enabled,
    remark: row.remark || '',
  })
  ruleVisible.value = true
}

async function submitRule() {
  if (!ruleForm.channelId) {
    ElMessage.warning('请选择推送渠道')
    return
  }
  try {
    if (editingRuleId.value) {
      await updatePushRule(editingRuleId.value, { ...ruleForm })
      ElMessage.success('已更新')
    } else {
      await createPushRule({ ...ruleForm })
      ElMessage.success('已创建')
    }
    ruleVisible.value = false
    await load()
  } catch (e: any) {
    ElMessage.error(e.message || '保存失败')
  }
}

async function onDeleteRule(row: PushRule) {
  await ElMessageBox.confirm('删除该推送规则？', '确认')
  try {
    await deletePushRule(row.id)
    ElMessage.success('已删除')
    await load()
  } catch (e: any) {
    ElMessage.error(e.message || '删除失败')
  }
}

onMounted(load)
</script>

<template>
  <div v-loading="loading" class="page">
    <section class="block">
      <div class="head">
        <div>
          <h3>定时同步</h3>
          <p>按间隔自动同步电商/门店订单，并刷新未完结订单的最新状态（含电商订单状态、售后等）。</p>
        </div>
      </div>
      <el-table :data="syncJobs" stripe>
        <el-table-column label="任务" min-width="160">
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
              @change="(v: number) => saveInterval(row, v)"
            />
          </template>
        </el-table-column>
        <el-table-column label="上次运行" min-width="220">
          <template #default="{ row }">
            <div>{{ row.lastRunAt || '-' }}</div>
            <div v-if="row.lastRunAt" class="muted">
              {{ row.lastRunOk ? '成功' : '失败' }}
              <span v-if="row.lastError"> · {{ row.lastError }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="120">
          <template #default="{ row }">
            <el-button link type="primary" @click="onRunJob(row)">立即同步</el-button>
          </template>
        </el-table-column>
      </el-table>
    </section>

    <section class="block">
      <div class="head">
        <div>
          <h3>推送渠道</h3>
          <p>配置飞书机器人 / 企业微信群机器人 Webhook，用于推送订单给供应商。</p>
        </div>
        <el-button type="primary" @click="openCreateChannel">新建渠道</el-button>
      </div>
      <el-table :data="channels" stripe>
        <el-table-column prop="name" label="名称" min-width="140" />
        <el-table-column label="方式" width="120">
          <template #default="{ row }">{{ channelTypeLabel(row.channelType) }}</template>
        </el-table-column>
        <el-table-column prop="webhookUrl" label="Webhook" min-width="220" show-overflow-tooltip />
        <el-table-column label="启用" width="80">
          <template #default="{ row }">
            <el-tag size="small" :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '是' : '否' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="200" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="onTestChannel(row)">测试</el-button>
            <el-button link type="primary" @click="openEditChannel(row)">编辑</el-button>
            <el-button link type="danger" @click="onDeleteChannel(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </section>

    <section class="block">
      <div class="head">
        <div>
          <h3>供应商推送规则</h3>
          <p>订单分配成功后，按供应商匹配规则推送；供应商 ID=0 为默认规则。</p>
        </div>
        <el-button type="primary" @click="openCreateRule">新建规则</el-button>
      </div>
      <el-table :data="rules" stripe>
        <el-table-column label="供应商" min-width="180">
          <template #default="{ row }">{{ supplierLabel(row.supplierId) }}</template>
        </el-table-column>
        <el-table-column label="事件" width="140">
          <template #default="{ row }">{{ row.event === 'order_allocated' ? '订单分配' : row.event }}</template>
        </el-table-column>
        <el-table-column label="渠道" min-width="140">
          <template #default="{ row }">{{ channelName(row.channelId) }}</template>
        </el-table-column>
        <el-table-column label="启用" width="80">
          <template #default="{ row }">
            <el-tag size="small" :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '是' : '否' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="140" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEditRule(row)">编辑</el-button>
            <el-button link type="danger" @click="onDeleteRule(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </section>

    <el-dialog v-model="channelVisible" :title="editingChannelId ? '编辑渠道' : '新建渠道'" width="560px">
      <el-form label-width="110px">
        <el-form-item label="名称" required><el-input v-model="channelForm.name" /></el-form-item>
        <el-form-item label="推送方式">
          <el-select v-model="channelForm.channelType" style="width: 100%">
            <el-option label="飞书机器人" value="feishu_webhook" />
            <el-option label="企微机器人" value="wecom_webhook" />
          </el-select>
        </el-form-item>
        <el-form-item label="Webhook" required>
          <el-input v-model="channelForm.webhookUrl" type="textarea" :rows="2" placeholder="机器人 Webhook 地址" />
        </el-form-item>
        <el-form-item v-if="channelForm.channelType === 'feishu_webhook'" label="签名密钥">
          <el-input v-model="channelForm.secret" placeholder="可选；飞书机器人签名校验" />
        </el-form-item>
        <el-form-item label="启用"><el-switch v-model="channelForm.enabled" /></el-form-item>
        <el-form-item label="备注"><el-input v-model="channelForm.remark" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="channelVisible = false">取消</el-button>
        <el-button type="primary" @click="submitChannel">保存</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="ruleVisible" :title="editingRuleId ? '编辑规则' : '新建规则'" width="520px">
      <el-form label-width="110px">
        <el-form-item label="供应商">
          <el-select v-model="ruleForm.supplierId" filterable style="width: 100%">
            <el-option label="全部供应商（默认）" :value="0" />
            <el-option
              v-for="s in suppliers"
              :key="s.id"
              :label="`${s.name}${s.code ? ' (' + s.code + ')' : ''}`"
              :value="s.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="事件">
          <el-select v-model="ruleForm.event" style="width: 100%">
            <el-option label="订单分配" value="order_allocated" />
          </el-select>
        </el-form-item>
        <el-form-item label="推送渠道" required>
          <el-select v-model="ruleForm.channelId" style="width: 100%">
            <el-option v-for="c in channels" :key="c.id" :label="`${c.name}（${channelTypeLabel(c.channelType)}）`" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="启用"><el-switch v-model="ruleForm.enabled" /></el-form-item>
        <el-form-item label="备注"><el-input v-model="ruleForm.remark" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="ruleVisible = false">取消</el-button>
        <el-button type="primary" @click="submitRule">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 20px; }
.block { display: flex; flex-direction: column; gap: 12px; }
.head { display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; }
.head h3 { margin: 0 0 6px; }
.head p { margin: 0; color: #64748b; font-size: 13px; max-width: 720px; line-height: 1.5; }
.muted { color: #94a3b8; font-size: 12px; }
</style>
