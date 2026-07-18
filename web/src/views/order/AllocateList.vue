<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { labelAlloc, labelDropship, labelSource, labelStatus, listOrders, type Order } from '../../api/orders'

const router = useRouter()
const loading = ref(false)
const list = ref<Order[]>([])
const total = ref(0)
const filters = reactive({
  page: 1,
  pageSize: 20,
  status: 'pending_ship',
  keyword: '',
})

async function load() {
  loading.value = true
  try {
    const data = await listOrders({ ...filters })
    list.value = data.list || []
    total.value = data.total || 0
  } catch (e: any) {
    ElMessage.error(e.message || '加载失败')
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="toolbar">
      <el-radio-group v-model="filters.status" @change="() => { filters.page = 1; load() }">
        <el-radio-button value="pending_ship">待分配</el-radio-button>
        <el-radio-button value="allocated">已分配</el-radio-button>
        <el-radio-button value="purchasing">采购中</el-radio-button>
        <el-radio-button value="">全部</el-radio-button>
      </el-radio-group>
      <el-input v-model="filters.keyword" clearable placeholder="搜索单号/买家" style="width: 220px" @keyup.enter="() => { filters.page = 1; load() }" />
    </div>

    <el-alert
      type="info"
      :closable="false"
      title="分配说明"
      description="自营发货：本仓发货后填单号回传；代发发货：快递助手厂家代发（推送即可）或 OSMS 供应商代发（线下沟通后填单号）；采购发货：先采购到货再自营发出。"
      show-icon
    />

    <el-table v-loading="loading" :data="list" stripe>
      <el-table-column prop="orderNo" label="内部单号" width="160" />
      <el-table-column label="来源" width="130">
        <template #default="{ row }">{{ labelSource(row.sourceChannel) }}</template>
      </el-table-column>
      <el-table-column prop="platformOrderId" label="平台单号" min-width="140" show-overflow-tooltip />
      <el-table-column label="买家" min-width="120">
        <template #default="{ row }">{{ row.buyerName || row.buyerNick || '-' }}</template>
      </el-table-column>
      <el-table-column label="状态" width="100">
        <template #default="{ row }">{{ labelStatus(row.status) }}</template>
      </el-table-column>
      <el-table-column label="分配" width="110">
        <template #default="{ row }">{{ labelAlloc(row.allocType) }}</template>
      </el-table-column>
      <el-table-column label="代发方式" width="150">
        <template #default="{ row }">{{ labelDropship(row.dropshipMode) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="100" fixed="right">
        <template #default="{ row }">
          <el-button type="primary" link @click="router.push(`/orders/${row.id}`)">去处理</el-button>
        </template>
      </el-table-column>
    </el-table>

    <div class="pager">
      <el-pagination
        v-model:current-page="filters.page"
        :page-size="filters.pageSize"
        :total="total"
        layout="total, prev, pager, next"
        @current-change="load"
      />
    </div>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 12px; }
.toolbar { display: flex; justify-content: space-between; gap: 12px; align-items: center; flex-wrap: wrap; }
.pager { display: flex; justify-content: flex-end; }
</style>
