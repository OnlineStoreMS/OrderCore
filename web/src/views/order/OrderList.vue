<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import {
  createManualOrder,
  formatAddress,
  formatDateTime,
  formatRemark,
  labelAgentType,
  labelEcommerceStatus,
  labelKDZSStatus,
  labelPlatform,
  labelStatus,
  listOrders,
  syncKDZS,
  syncStore,
  type Order,
  type OrderItem,
} from '../../api/orders'
import { dateShortcuts, defaultOrderedRange } from '../../utils/date'

const router = useRouter()
const route = useRoute()
const loading = ref(false)
const list = ref<Order[]>([])
const total = ref(0)
const [defaultStart, defaultEnd] = defaultOrderedRange()
const filters = reactive({
  page: 1,
  pageSize: 20,
  sourceChannel: (route.query.sourceChannel as string) || '',
  status: (route.query.status as string) || '',
  platform: '',
  allocType: '',
  keyword: '',
  orderedRange: [defaultStart, defaultEnd] as [string, string] | null,
  payRange: null as [string, string] | null,
})

const manualVisible = ref(false)
const manualForm = reactive({
  buyerName: '',
  buyerPhone: '',
  remark: '',
  productName: '',
  quantity: 1,
  price: 0,
  address: '',
})

async function load() {
  loading.value = true
  try {
    const params: Record<string, unknown> = {
      page: filters.page,
      pageSize: filters.pageSize,
      sourceChannel: filters.sourceChannel || undefined,
      status: filters.status || undefined,
      platform: filters.platform || undefined,
      allocType: filters.allocType || undefined,
      keyword: filters.keyword || undefined,
    }
    if (filters.orderedRange?.length === 2) {
      params.orderedAtStart = filters.orderedRange[0]
      params.orderedAtEnd = filters.orderedRange[1]
    }
    if (filters.payRange?.length === 2) {
      params.payTimeStart = filters.payRange[0]
      params.payTimeEnd = filters.payRange[1]
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

async function onSyncKDZS() {
  try {
    const stats = await syncKDZS({ pageSize: 50 }) as Record<string, number>
    ElMessage.success(`同步完成（待推单+待发货）：新增 ${stats.created || 0}，更新 ${stats.updated || 0}`)
    await load()
  } catch (e: any) {
    ElMessage.error(e.message || '同步失败')
  }
}

async function onSyncStore() {
  try {
    const stats = await syncStore({ pageSize: 50 }) as Record<string, number>
    ElMessage.success(`同步完成：新增 ${stats.created || 0}，更新 ${stats.updated || 0}`)
    await load()
  } catch (e: any) {
    ElMessage.error(e.message || '同步失败')
  }
}

async function submitManual() {
  if (!manualForm.productName) {
    ElMessage.warning('请填写商品名称')
    return
  }
  try {
    const order = await createManualOrder({
      buyerName: manualForm.buyerName,
      buyerPhone: manualForm.buyerPhone,
      remark: manualForm.remark,
      address: { name: manualForm.buyerName, phone: manualForm.buyerPhone, fullText: manualForm.address },
      items: [{ productName: manualForm.productName, quantity: manualForm.quantity, price: manualForm.price }],
    })
    ElMessage.success('手工订单已创建')
    manualVisible.value = false
    router.push(`/orders/${order.id}`)
  } catch (e: any) {
    ElMessage.error(e.message || '创建失败')
  }
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="toolbar">
      <el-form inline @submit.prevent>
        <el-form-item label="来源">
          <el-select v-model="filters.sourceChannel" clearable style="width: 150px" @change="onFilterChange">
            <el-option label="电商(快递助手)" value="kdzs" />
            <el-option label="门店销售" value="store" />
            <el-option label="手工订单" value="manual" />
            <el-option label="微信小程序" value="wx_mall" />
          </el-select>
        </el-form-item>
        <el-form-item label="平台">
          <el-select v-model="filters.platform" clearable style="width: 120px" @change="onFilterChange">
            <el-option label="抖店" value="FXG" />
            <el-option label="淘宝" value="TB" />
            <el-option label="小红书" value="XHS" />
            <el-option label="拼多多" value="PDD" />
            <el-option label="快手" value="KSXD" />
          </el-select>
        </el-form-item>
        <el-form-item label="履约状态">
          <el-select v-model="filters.status" clearable style="width: 130px" @change="onFilterChange">
            <el-option label="待发货" value="pending_ship" />
            <el-option label="已分配" value="allocated" />
            <el-option label="采购中" value="purchasing" />
            <el-option label="已发货" value="shipped" />
            <el-option label="已完成" value="completed" />
          </el-select>
        </el-form-item>
        <el-form-item label="下单时间">
          <el-date-picker
            v-model="filters.orderedRange"
            type="datetimerange"
            range-separator="至"
            start-placeholder="开始"
            end-placeholder="结束"
            value-format="YYYY-MM-DD HH:mm:ss"
            :shortcuts="dateShortcuts"
            clearable
            style="width: 360px"
            @change="onFilterChange"
          />
        </el-form-item>
        <el-form-item label="付款时间">
          <el-date-picker
            v-model="filters.payRange"
            type="datetimerange"
            range-separator="至"
            start-placeholder="开始"
            end-placeholder="结束"
            value-format="YYYY-MM-DD HH:mm:ss"
            :shortcuts="dateShortcuts"
            clearable
            style="width: 360px"
            @change="onFilterChange"
          />
        </el-form-item>
        <el-form-item>
          <el-input v-model="filters.keyword" clearable placeholder="单号/买家/手机" style="width: 180px" @keyup.enter="onFilterChange" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="onFilterChange">查询</el-button>
        </el-form-item>
      </el-form>
      <div class="right">
        <el-button @click="onSyncKDZS">同步电商</el-button>
        <el-button @click="onSyncStore">同步门店</el-button>
        <el-button type="primary" @click="manualVisible = true">手工建单</el-button>
      </div>
    </div>

    <el-table v-loading="loading" :data="list" stripe @row-click="(row: Order) => router.push(`/orders/${row.id}`)">
      <el-table-column label="平台" width="90">
        <template #default="{ row }">{{ labelPlatform(row.platform) }}</template>
      </el-table-column>
      <el-table-column prop="platformOrderId" label="平台单号" min-width="150" show-overflow-tooltip />
      <el-table-column label="买家" min-width="120" show-overflow-tooltip>
        <template #default="{ row }">{{ row.buyerNick || row.buyerName || '-' }}</template>
      </el-table-column>
      <el-table-column label="商品" min-width="260">
        <template #default="{ row }">
          <div v-if="row.items?.length" class="goods-list" @click.stop>
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
      <el-table-column label="电商订单状态" min-width="140" show-overflow-tooltip>
        <template #default="{ row }">
          <template v-if="row.sourceChannel === 'kdzs'">
            <el-tag
              size="small"
              :type="(row.ecommerceStatusText || row.afterSaleStatusText || '').includes('退') ? 'danger' : 'warning'"
            >{{ labelEcommerceStatus(row) }}</el-tag>
          </template>
          <span v-else>-</span>
        </template>
      </el-table-column>
      <el-table-column label="履约状态" width="100">
        <template #default="{ row }">
          <el-tag size="small" type="info">{{ labelStatus(row.status) }}</el-tag>
        </template>
      </el-table-column>
    </el-table>

    <div class="pager">
      <el-pagination
        v-model:current-page="filters.page"
        v-model:page-size="filters.pageSize"
        :total="total"
        layout="total, prev, pager, next"
        @current-change="load"
      />
    </div>

    <el-dialog v-model="manualVisible" title="手工建单" width="520px">
      <el-form label-width="90px">
        <el-form-item label="买家姓名"><el-input v-model="manualForm.buyerName" /></el-form-item>
        <el-form-item label="手机"><el-input v-model="manualForm.buyerPhone" /></el-form-item>
        <el-form-item label="地址"><el-input v-model="manualForm.address" type="textarea" /></el-form-item>
        <el-form-item label="商品"><el-input v-model="manualForm.productName" /></el-form-item>
        <el-form-item label="数量"><el-input-number v-model="manualForm.quantity" :min="1" /></el-form-item>
        <el-form-item label="单价"><el-input-number v-model="manualForm.price" :min="0" :precision="2" /></el-form-item>
        <el-form-item label="备注"><el-input v-model="manualForm.remark" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="manualVisible = false">取消</el-button>
        <el-button type="primary" @click="submitManual">创建</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 12px; }
.toolbar { display: flex; justify-content: space-between; gap: 12px; flex-wrap: wrap; align-items: flex-start; }
.right { display: flex; gap: 8px; }
.pager { display: flex; justify-content: flex-end; }
:deep(.el-table__row) { cursor: pointer; }
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
</style>
