<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import {
  formatAddress,
  formatDateTime,
  formatRemark,
  labelAgentType,
  labelAlloc,
  labelDropship,
  labelKDZSStatus,
  labelPlatform,
  labelStatus,
  listOrders,
  type Order,
  type OrderItem,
} from '../../api/orders'
import { dateShortcuts, defaultOrderedRange } from '../../utils/date'

const router = useRouter()
const loading = ref(false)
const list = ref<Order[]>([])
const total = ref(0)
const [defaultStart, defaultEnd] = defaultOrderedRange()
const filters = reactive({
  page: 1,
  pageSize: 20,
  status: 'pending_ship',
  keyword: '',
  orderedRange: [defaultStart, defaultEnd] as [string, string] | null,
})

async function load() {
  loading.value = true
  try {
    const params: Record<string, unknown> = {
      page: filters.page,
      pageSize: filters.pageSize,
      status: filters.status || undefined,
      keyword: filters.keyword || undefined,
    }
    if (filters.orderedRange?.length === 2) {
      params.orderedAtStart = filters.orderedRange[0]
      params.orderedAtEnd = filters.orderedRange[1]
    }
    const data = await listOrders(params)
    list.value = data.list || []
    total.value = data.total || 0
  } catch (e: any) {
    ElMessage.error(e.message || '加载失败')
  } finally {
    loading.value = false
  }
}

function onFilterChange() {
  filters.page = 1
  load()
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="toolbar">
      <el-radio-group v-model="filters.status" @change="onFilterChange">
        <el-radio-button value="pending_ship">待分配</el-radio-button>
        <el-radio-button value="allocated">已分配</el-radio-button>
        <el-radio-button value="purchasing">采购中</el-radio-button>
        <el-radio-button value="">全部</el-radio-button>
      </el-radio-group>
      <el-date-picker
        v-model="filters.orderedRange"
        type="datetimerange"
        range-separator="至"
        start-placeholder="下单开始"
        end-placeholder="下单结束"
        value-format="YYYY-MM-DD HH:mm:ss"
        :shortcuts="dateShortcuts"
        clearable
        style="width: 360px"
        @change="onFilterChange"
      />
      <el-input v-model="filters.keyword" clearable placeholder="搜索单号/买家" style="width: 220px" @keyup.enter="onFilterChange" />
    </div>

    <el-alert
      type="info"
      :closable="false"
      title="分配说明"
      description="自营发货：本仓发货后填单号回传；代发发货：快递助手厂家代发（推送即可）或 OSMS 供应商代发（线下沟通后填单号）；采购发货：先采购到货再自营发出。"
      show-icon
    />

    <el-table v-loading="loading" :data="list" stripe>
      <el-table-column label="平台" width="90">
        <template #default="{ row }">{{ labelPlatform(row.platform) }}</template>
      </el-table-column>
      <el-table-column prop="platformOrderId" label="平台单号" min-width="150" show-overflow-tooltip />
      <el-table-column label="买家" min-width="120" show-overflow-tooltip>
        <template #default="{ row }">{{ row.buyerNick || row.buyerName || '-' }}</template>
      </el-table-column>
      <el-table-column label="商品" min-width="260">
        <template #default="{ row }">
          <div v-if="row.items?.length" class="goods-list">
            <div v-for="(it, idx) in row.items" :key="it.id || idx" class="goods-row">
              <el-image
                v-if="it.picUrl"
                :src="it.picUrl"
                :preview-src-list="(row.items as OrderItem[]).map((x) => x.picUrl).filter(Boolean) as string[]"
                :initial-index="(row.items as OrderItem[]).slice(0, idx).filter((x) => x.picUrl).length"
                fit="cover"
                class="goods-pic"
                preview-teleported
              />
              <div v-else class="goods-pic goods-pic-empty">无图</div>
              <div class="goods-info">
                <div class="goods-title">{{ it.productName || it.skuCode || '商品' }}</div>
                <div v-if="it.skuSpecs || it.skuCode" class="goods-meta">
                  <span v-if="it.skuSpecs">{{ it.skuSpecs }}</span>
                  <span v-if="it.skuCode">SKU {{ it.skuCode }}</span>
                </div>
                <div class="goods-meta">×{{ it.quantity || 1 }}</div>
              </div>
            </div>
          </div>
          <span v-else>-</span>
        </template>
      </el-table-column>
      <el-table-column label="留言备注" min-width="140" show-overflow-tooltip>
        <template #default="{ row }">{{ formatRemark(row) }}</template>
      </el-table-column>
      <el-table-column label="收件信息" min-width="200" show-overflow-tooltip>
        <template #default="{ row }">{{ formatAddress(row.address) }}</template>
      </el-table-column>
      <el-table-column label="金额" width="90">
        <template #default="{ row }">{{ Number(row.payAmount ?? row.totalAmount ?? 0).toFixed(2) }}</template>
      </el-table-column>
      <el-table-column label="下单时间" width="160">
        <template #default="{ row }">{{ formatDateTime(row.orderedAt || row.createdAt) }}</template>
      </el-table-column>
      <el-table-column label="付款时间" width="160">
        <template #default="{ row }">{{ formatDateTime(row.payTime) }}</template>
      </el-table-column>
      <el-table-column label="快递助手状态" width="120">
        <template #default="{ row }">
          <template v-if="row.sourceChannel === 'kdzs'">
            <el-tag size="small">{{ labelKDZSStatus(row) }}</el-tag>
            <div class="kdzs-meta">{{ labelAgentType(row.agentType) }}</div>
          </template>
          <span v-else>-</span>
        </template>
      </el-table-column>
      <el-table-column label="履约状态" width="100">
        <template #default="{ row }">
          <el-tag size="small" type="info">{{ labelStatus(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="分配" width="100">
        <template #default="{ row }">{{ labelAlloc(row.allocType) }}</template>
      </el-table-column>
      <el-table-column label="代发方式" width="140" show-overflow-tooltip>
        <template #default="{ row }">{{ labelDropship(row.dropshipMode) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="120" fixed="right">
        <template #default="{ row }">
          <el-button type="primary" link @click="router.push(`/orders/${row.id}`)">去处理</el-button>
          <div v-if="row.shipEntryLocked" class="lock-tip">已锁发货</div>
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
.goods-list { display: flex; flex-direction: column; gap: 8px; }
.goods-row { display: flex; gap: 8px; align-items: flex-start; }
.goods-pic { width: 48px; height: 48px; border-radius: 4px; flex-shrink: 0; background: #f5f5f5; }
.goods-pic-empty {
  display: flex; align-items: center; justify-content: center;
  font-size: 11px; color: #bbb;
}
.goods-info { min-width: 0; line-height: 1.4; }
.goods-title {
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}
.goods-meta { font-size: 12px; color: #909399; }
.goods-meta span + span::before { content: ' · '; }
.kdzs-meta { margin-top: 4px; font-size: 12px; color: #909399; }
.lock-tip { font-size: 11px; color: #e6a23c; margin-top: 2px; }
</style>
