<script setup lang="ts">
import { nextTick, onUnmounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import {
  buildOrderCopyText,
  decryptOrders,
  formatAddress,
  formatDateTime,
  formatPlatformShop,
  formatRemarkLines,
  isMaskedReceiver,
  labelAgentType,
  labelEcommerceStatus,
  labelKDZSStatus,
  labelShipStatus,
  labelSource,
  labelStatus,
  listOrders,
  type Order,
  type OrderItem,
} from '../../api/orders'
import { dateShortcuts, dateRangeDefaultTime, formatDateTimeLocal } from '../../utils/date'
import { copyToClipboard } from '../../utils/clipboard'
import { bindTableShiftWheel, useTableFillHeight } from '../../composables/useTableFillHeight'
import SellerFlag from '../../components/SellerFlag.vue'

const router = useRouter()
const route = useRoute()
const loading = ref(false)
const list = ref<Order[]>([])
const total = ref(0)

const pageRef = ref<HTMLElement | null>(null)
const toolbarRef = ref<HTMLElement | null>(null)
const pagerRef = ref<HTMLElement | null>(null)
const tableRef = ref<{ $el?: HTMLElement } | null>(null)
const { tableHeight, updateTableHeight } = useTableFillHeight(pageRef, [toolbarRef, pagerRef])

let unbindWheel: (() => void) | undefined
onUnmounted(() => unbindWheel?.())

async function rebindWheel() {
  await nextTick()
  unbindWheel?.()
  unbindWheel = bindTableShiftWheel(tableRef.value?.$el ?? null)
  updateTableHeight()
}

function last7DaysRange(): [string, string] {
  const end = new Date()
  const start = new Date()
  start.setDate(start.getDate() - 6)
  start.setHours(0, 0, 0, 0)
  end.setHours(23, 59, 59, 0)
  return [formatDateTimeLocal(start), formatDateTimeLocal(end)]
}

function rangeFromQueryDates(startRaw?: unknown, endRaw?: unknown): [string, string] | null {
  if (typeof startRaw !== 'string' || typeof endRaw !== 'string' || !startRaw || !endRaw) return null
  const start = startRaw.length <= 10 ? `${startRaw} 00:00:00` : startRaw
  const end = endRaw.length <= 10 ? `${endRaw} 23:59:59` : endRaw
  return [start, end]
}

function normalizeFulfillmentStatus(s: string) {
  if (s === 'pending_ship') return 'pending_alloc'
  if (s === 'shipped') return '' // 已发货改用发货状态筛选
  return s
}

function isMenuEntry(q = route.query) {
  return q.entry === 'menu'
}

function shipStatusFromQuery(q = route.query) {
  if (typeof q.shipStatus === 'string' && q.shipStatus) return q.shipStatus
  if (q.status === 'shipped') return 'shipped'
  if (q.ecommerceWaitShip === '1' || q.ecommerceWaitShip === 'true') return 'wait_ship'
  return ''
}

const filters = reactive({
  page: 1,
  pageSize: 20,
  sourceChannel: '',
  status: '',
  shipStatus: '',
  platform: '',
  allocType: '',
  salesChannel: '',
  keyword: '',
  orderedRange: null as [string, string] | null,
  shippedRange: null as [string, string] | null,
  payRange: null as [string, string] | null,
})

function applyFiltersFromRoute() {
  const q = route.query
  const menu = isMenuEntry(q)
  const shipInit = shipStatusFromQuery(q)
  filters.page = 1
  filters.sourceChannel = typeof q.sourceChannel === 'string' ? q.sourceChannel : ''
  filters.status =
    shipInit === 'shipped'
      ? ''
      : normalizeFulfillmentStatus(typeof q.status === 'string' ? q.status : '')
  // 仅左侧菜单进入时默认「待发货 + 最近7天」；其它跳转只吃 query，不擅自加默认
  filters.shipStatus = menu ? (shipInit || 'wait_ship') : shipInit
  filters.platform = typeof q.platform === 'string' ? q.platform : ''
  filters.salesChannel = typeof q.salesChannel === 'string' ? q.salesChannel : ''
  // 趋势卡会同时带 salesChannel + allocType；仅有 salesChannel 时也回填分配类型便于看见筛选
  if (typeof q.allocType === 'string' && q.allocType) {
    filters.allocType = q.allocType
  } else if (filters.salesChannel === 'self') {
    filters.allocType = 'self_ship'
  } else if (filters.salesChannel === 'dropship') {
    filters.allocType = 'dropship'
  } else {
    filters.allocType = ''
  }
  filters.keyword = typeof q.keyword === 'string' ? q.keyword : ''
  const orderedFromQuery = rangeFromQueryDates(q.orderedAtStart, q.orderedAtEnd)
  const shippedFromQuery = rangeFromQueryDates(q.shippedAtStart, q.shippedAtEnd)
  filters.orderedRange = orderedFromQuery || (menu ? last7DaysRange() : null)
  filters.shippedRange = shippedFromQuery
  filters.payRange = null
}

async function load() {
  loading.value = true
  try {
    const params: Record<string, unknown> = {
      page: filters.page,
      pageSize: filters.pageSize,
      sourceChannel: filters.sourceChannel || undefined,
      status: filters.status || undefined,
      shipStatus: filters.shipStatus || undefined,
      platform: filters.platform || undefined,
      // 有销售渠道（工作台趋势口径）时优先用它，避免与分配类型 AND 过严
      allocType: filters.salesChannel ? undefined : filters.allocType || undefined,
      salesChannel: filters.salesChannel || undefined,
      keyword: filters.keyword || undefined,
    }
    if (filters.orderedRange?.length === 2) {
      params.orderedAtStart = filters.orderedRange[0]
      params.orderedAtEnd = filters.orderedRange[1]
    }
    if (filters.shippedRange?.length === 2) {
      params.shippedAtStart = filters.shippedRange[0]
      params.shippedAtEnd = filters.shippedRange[1]
    }
    if (filters.payRange?.length === 2) {
      params.payTimeStart = filters.payRange[0]
      params.payTimeEnd = filters.payRange[1]
    }
    const data = await listOrders(params)
    list.value = data.list || []
    total.value = data.total || 0
    await rebindWheel()
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

function onAllocTypeChange() {
  // 用户改分配类型后，不再沿用工作台销售渠道口径
  filters.salesChannel = ''
  onFilterChange()
}

watch(
  () => route.fullPath,
  () => {
    if (route.name !== 'Orders') return
    applyFiltersFromRoute()
    void load()
  },
  { immediate: true },
)

const decrypting = ref(false)
const decryptRow = reactive<Record<number, boolean>>({})

function canDecrypt(order: Order) {
  return order.sourceChannel === 'kdzs' && !!order.platformSysTid
}

function applyDecryptedOrders(items: Order[]) {
  const byId = new Map(items.map((o) => [o.id, o]))
  list.value = list.value.map((o) => byId.get(o.id) || o)
}

async function decryptOne(order: Order, ev?: Event) {
  ev?.stopPropagation()
  if (!canDecrypt(order)) {
    ElMessage.warning('仅电商订单可解密')
    return
  }
  decryptRow[order.id] = true
  try {
    const data = await decryptOrders([order.id])
    applyDecryptedOrders(data.items || [])
    ElMessage.success('解密成功')
  } catch (e: any) {
    ElMessage.error(e.message || '解密失败')
  } finally {
    decryptRow[order.id] = false
  }
}

async function decryptPage() {
  const ids = list.value.filter(canDecrypt).map((o) => o.id)
  if (!ids.length) {
    ElMessage.warning('当前页没有可解密的电商订单')
    return
  }
  decrypting.value = true
  try {
    const data = await decryptOrders(ids)
    applyDecryptedOrders(data.items || [])
    ElMessage.success(`已解密 ${data.success || data.items?.length || 0} 条`)
  } catch (e: any) {
    ElMessage.error(e.message || '批量解密失败')
  } finally {
    decrypting.value = false
  }
}

async function copyOrderText(order: Order, ev?: Event) {
  ev?.stopPropagation()
  const text = buildOrderCopyText(order)
  const addr = formatAddress(order.address)
  if (!addr || addr === '-') {
    ElMessage.warning('暂无收件信息，请先解密')
    return
  }
  const ok = await copyToClipboard(text)
  if (ok) ElMessage.success('已复制')
  else ElMessage.error('复制失败')
}
</script>

<template>
  <div ref="pageRef" class="page">
    <div ref="toolbarRef" class="toolbar">
      <el-form inline @submit.prevent>
        <el-form-item label="订单类型">
          <el-select v-model="filters.sourceChannel" clearable style="width: 140px" @change="onFilterChange">
            <el-option label="电商" value="kdzs" />
            <el-option label="小程序" value="wx_mall" />
            <el-option label="门店" value="store" />
            <el-option label="闲鱼" value="xianyu" />
            <el-option label="手工订单" value="manual" />
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
          <el-select
            v-model="filters.status"
            clearable
            style="width: 130px"
            @change="onFilterChange"
          >
            <el-option label="待分配" value="pending_alloc" />
            <el-option label="已分配" value="allocated" />
            <el-option label="采购中" value="purchasing" />
            <el-option label="已完成" value="completed" />
            <el-option label="已关闭" value="closed" />
          </el-select>
        </el-form-item>
        <el-form-item label="发货状态">
          <el-select
            v-model="filters.shipStatus"
            clearable
            style="width: 120px"
            @change="onFilterChange"
          >
            <el-option label="待发货" value="wait_ship" />
            <el-option label="已发货" value="shipped" />
          </el-select>
        </el-form-item>
        <el-form-item label="分配类型">
          <el-select
            v-model="filters.allocType"
            clearable
            style="width: 130px"
            @change="onAllocTypeChange"
          >
            <el-option label="自营发货" value="self_ship" />
            <el-option label="代发发货" value="dropship" />
            <el-option label="采购发货" value="purchase_then_ship" />
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
            :default-time="dateRangeDefaultTime"
            clearable
            style="width: 360px"
            @change="onFilterChange"
          />
        </el-form-item>
        <el-form-item label="发货时间">
          <el-date-picker
            v-model="filters.shippedRange"
            type="datetimerange"
            range-separator="至"
            start-placeholder="开始"
            end-placeholder="结束"
            value-format="YYYY-MM-DD HH:mm:ss"
            :shortcuts="dateShortcuts"
            :default-time="dateRangeDefaultTime"
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
            :default-time="dateRangeDefaultTime"
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
          <el-button type="warning" plain :loading="decrypting" :disabled="!list.length" @click="decryptPage">本页解密</el-button>
        </el-form-item>
      </el-form>
    </div>

    <el-table
      ref="tableRef"
      v-loading="loading"
      :data="list"
      :height="tableHeight"
      stripe
      @row-click="(row: Order) => router.push(`/orders/${row.id}`)"
    >
      <el-table-column label="订单类型" width="88">
        <template #default="{ row }">{{ labelSource(row.sourceChannel) }}</template>
      </el-table-column>
      <el-table-column label="平台" min-width="180" show-overflow-tooltip>
        <template #default="{ row }">{{ formatPlatformShop(row) }}</template>
      </el-table-column>
      <el-table-column prop="platformOrderId" label="平台单号" min-width="200" width="220">
        <template #default="{ row }">
          <span class="platform-oid">{{ row.platformOrderId || '-' }}</span>
        </template>
      </el-table-column>
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
      <el-table-column label="留言备注" min-width="200">
        <template #default="{ row }">
          <div v-if="formatRemarkLines(row).length" class="remark-lines">
            <div v-for="(line, idx) in formatRemarkLines(row)" :key="idx" class="remark-line">
              <SellerFlag
                v-if="line.kind === 'seller'"
                :model-value="line.sellerFlag ?? 0"
                :size="14"
              />
              <span>{{ line.text }}</span>
            </div>
          </div>
          <span v-else class="muted">-</span>
        </template>
      </el-table-column>
      <el-table-column label="收件信息" min-width="240">
        <template #default="{ row }">
          <div class="addr-cell" @click.stop>
            <div class="addr-text">{{ formatAddress(row.address) }}</div>
            <div v-if="canDecrypt(row)" class="addr-actions">
              <el-button
                v-if="isMaskedReceiver(row)"
                link
                type="warning"
                size="small"
                :loading="decryptRow[row.id]"
                @click="decryptOne(row, $event)"
              >解密</el-button>
              <el-button
                v-else
                link
                type="primary"
                size="small"
                @click="copyOrderText(row, $event)"
              >复制</el-button>
              <el-button
                v-if="!isMaskedReceiver(row)"
                link
                type="warning"
                size="small"
                :loading="decryptRow[row.id]"
                @click="decryptOne(row, $event)"
              >重新解密</el-button>
            </div>
          </div>
        </template>
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
      <el-table-column label="履约状态" width="90">
        <template #default="{ row }">
          <el-tag size="small" type="info">{{ labelStatus(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="发货状态" width="90">
        <template #default="{ row }">
          <el-tag size="small" :type="row.shipStatus === 'shipped' ? 'success' : 'warning'">
            {{ labelShipStatus(row.shipStatus) }}
          </el-tag>
        </template>
      </el-table-column>
    </el-table>

    <div ref="pagerRef" class="pager">
      <el-pagination
        v-model:current-page="filters.page"
        v-model:page-size="filters.pageSize"
        :total="total"
        layout="total, prev, pager, next"
        @current-change="load"
      />
    </div>
  </div>
</template>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  gap: 12px;
  height: calc(100vh - 56px - 32px);
  min-height: 0;
  overflow: hidden;
}
.toolbar {
  display: flex;
  flex-wrap: wrap;
  flex-shrink: 0;
}
.pager { display: flex; justify-content: flex-end; flex-shrink: 0; }
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
.addr-cell { line-height: 1.4; }
.addr-text {
  font-size: 13px;
  white-space: normal;
  word-break: break-all;
}
.addr-actions { margin-top: 4px; display: flex; gap: 4px; flex-wrap: wrap; }
.platform-oid {
  font-variant-numeric: tabular-nums;
  letter-spacing: 0.01em;
  white-space: nowrap;
  word-break: keep-all;
}
.remark-lines { display: flex; flex-direction: column; gap: 2px; line-height: 1.4; }
.remark-line {
  display: flex;
  align-items: flex-start;
  gap: 4px;
  font-size: 12px;
  color: #606266;
  word-break: break-all;
}
.muted { color: #c0c4cc; }
</style>
