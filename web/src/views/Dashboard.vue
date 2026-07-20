<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { fetchDashboard, labelSource, labelStatus, syncKDZS, syncStore } from '../api/orders'

const router = useRouter()
const loading = ref(false)
const byStatus = ref<Record<string, number>>({})
const bySource = ref<Record<string, number>>({})
const syncing = ref('')

async function load() {
  loading.value = true
  try {
    const data = await fetchDashboard() as { byStatus?: Record<string, number>; bySource?: Record<string, number> }
    byStatus.value = data.byStatus || {}
    bySource.value = data.bySource || {}
  } catch (e: any) {
    ElMessage.error(e.message || '加载失败')
  } finally {
    loading.value = false
  }
}

async function doSyncKDZS() {
  syncing.value = 'kdzs'
  try {
    const stats = await syncKDZS({ pageSize: 50 }) as Record<string, number>
    ElMessage.success(`电商同步完成（待推单+待发货）：新增 ${stats.created || 0}，更新 ${stats.updated || 0}`)
    await load()
  } catch (e: any) {
    ElMessage.error(e.message || '同步失败')
  } finally {
    syncing.value = ''
  }
}

async function doSyncStore() {
  syncing.value = 'store'
  try {
    const stats = await syncStore({ pageSize: 50 }) as Record<string, number>
    ElMessage.success(`门店同步完成：新增 ${stats.created || 0}，更新 ${stats.updated || 0}`)
    await load()
  } catch (e: any) {
    ElMessage.error(e.message || '同步失败')
  } finally {
    syncing.value = ''
  }
}

onMounted(load)
</script>

<template>
  <div v-loading="loading" class="page">
    <div class="hero">
      <div>
        <h1>订单中心</h1>
        <p>汇聚电商、门店、手工与小程序订单，统一完成自营 / 代发 / 采购发货分配与物流回传。</p>
      </div>
      <div class="actions">
        <el-button type="primary" :loading="syncing === 'kdzs'" @click="doSyncKDZS">同步电商订单</el-button>
        <el-button :loading="syncing === 'store'" @click="doSyncStore">同步门店订单</el-button>
        <el-button @click="router.push('/orders')">查看订单</el-button>
      </div>
    </div>

    <div class="grid">
      <section>
        <h3>按状态</h3>
        <div class="chips">
          <div v-for="(cnt, key) in byStatus" :key="key" class="chip" @click="router.push({ path: '/orders', query: { status: key } })">
            <span>{{ labelStatus(String(key)) }}</span>
            <strong>{{ cnt }}</strong>
          </div>
          <div v-if="!Object.keys(byStatus).length" class="empty">暂无订单</div>
        </div>
      </section>
      <section>
        <h3>按来源</h3>
        <div class="chips">
          <div v-for="(cnt, key) in bySource" :key="key" class="chip" @click="router.push({ path: '/orders', query: { sourceChannel: key } })">
            <span>{{ labelSource(String(key)) }}</span>
            <strong>{{ cnt }}</strong>
          </div>
          <div v-if="!Object.keys(bySource).length" class="empty">暂无订单</div>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 20px; }
.hero {
  display: flex; justify-content: space-between; gap: 24px; align-items: flex-start;
  padding: 24px 28px;
  background: linear-gradient(135deg, #0f2744 0%, #163a5f 55%, #1d4e89 100%);
  color: #fff; border-radius: 12px;
}
.hero h1 { margin: 0 0 8px; font-size: 28px; font-weight: 700; }
.hero p { margin: 0; max-width: 560px; opacity: 0.88; line-height: 1.6; }
.actions { display: flex; gap: 8px; flex-wrap: wrap; }
.grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
section { background: #fff; border-radius: 10px; padding: 18px 20px; border: 1px solid #eef0f3; }
h3 { margin: 0 0 14px; font-size: 15px; color: #334155; }
.chips { display: flex; flex-wrap: wrap; gap: 10px; }
.chip {
  min-width: 120px; padding: 12px 14px; border-radius: 8px; background: #f8fafc;
  border: 1px solid #e2e8f0; cursor: pointer; display: flex; justify-content: space-between; gap: 12px;
}
.chip:hover { border-color: #93c5fd; background: #eff6ff; }
.chip strong { color: #0f172a; }
.empty { color: #94a3b8; font-size: 13px; }
@media (max-width: 900px) {
  .hero { flex-direction: column; }
  .grid { grid-template-columns: 1fr; }
}
</style>
