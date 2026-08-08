<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  allocateOrder,
  batchDropshipOrders,
  buildOrderCopyText,
  decryptOrders,
  formatAddress,
  formatDateTime,
  formatRemarkLines,
  isMaskedReceiver,
  labelAgentType,
  labelAlloc,
  labelDropship,
  labelEcommerceStatus,
  labelKDZSStatus,
  labelPlatform,
  labelShipStatus,
  labelStatus,
  listBindings,
  listOrders,
  listSuppliers,
  revokeAllocateOrder,
  type Order,
  type OrderItem,
  type SupplierBinding,
  type SupplierItem,
} from '../../api/orders'
import SellerFlag from '../../components/SellerFlag.vue'
import { dateShortcuts, dateRangeDefaultTime, formatDateTimeLocal } from '../../utils/date'
import { copyToClipboard } from '../../utils/clipboard'
import { bindTableShiftWheel, useTableFillHeight } from '../../composables/useTableFillHeight'

const router = useRouter()
const route = useRoute()
const loading = ref(false)
const batching = ref(false)
const list = ref<Order[]>([])
const total = ref(0)
const selected = ref<Order[]>([])

const pageRef = ref<HTMLElement | null>(null)
const toolbarRef = ref<HTMLElement | null>(null)
const alertRef = ref<HTMLElement | null>(null)
const pagerRef = ref<HTMLElement | null>(null)
const tableRef = ref<{ $el?: HTMLElement } | null>(null)
const { tableHeight, updateTableHeight } = useTableFillHeight(pageRef, [toolbarRef, alertRef, pagerRef], {
  min: 280,
})

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

function normalizeStatus(raw: string) {
  if (raw === 'pending_ship') return 'pending_alloc'
  return raw
}

function applyAllocatedDefaults(forceDate = true) {
  filters.shipStatus = 'wait_ship'
  if (forceDate) {
    filters.orderedRange = last7DaysRange()
  }
}

const [defaultStart, defaultEnd] = last7DaysRange()
const statusFromQuery = typeof route.query.status === 'string' ? route.query.status : ''
const shipStatusFromQuery = typeof route.query.shipStatus === 'string' ? route.query.shipStatus : ''
const normalizedStatus = normalizeStatus(statusFromQuery) || 'pending_alloc'

const filters = reactive({
  page: 1,
  pageSize: 20,
  status: normalizedStatus,
  // 已分配默认：待发货 + 最近7天
  shipStatus:
    shipStatusFromQuery ||
    (normalizedStatus === 'allocated' ? 'wait_ship' : ''),
  allocType: '',
  keyword: '',
  orderedRange: [defaultStart, defaultEnd] as [string, string] | null,
})

const dropshipVisible = ref(false)
const suppliers = ref<SupplierItem[]>([])
const bindings = ref<SupplierBinding[]>([])
const dropshipForm = reactive({
  supplierId: undefined as number | undefined,
  supplierName: '',
})

const selectedCount = computed(() => selected.value.length)
const canBatchAllocate = computed(() =>
  selected.value.some((o) => o.status === 'pending_alloc' || o.status === 'pending_ship'),
)
const canBatchRevoke = computed(() =>
  selected.value.some((o) => o.status === 'allocated' || o.status === 'purchasing'),
)

async function load() {
  loading.value = true
  try {
    const params: Record<string, unknown> = {
      page: filters.page,
      pageSize: filters.pageSize,
      status: filters.status || undefined,
      shipStatus: filters.shipStatus || undefined,
      allocType: filters.allocType || undefined,
      keyword: filters.keyword || undefined,
    }
    if (filters.orderedRange?.length === 2) {
      params.orderedAtStart = filters.orderedRange[0]
      params.orderedAtEnd = filters.orderedRange[1]
    }
    const data = await listOrders(params)
    list.value = data.list || []
    total.value = data.total || 0
    selected.value = []
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

const decryptRow = reactive<Record<number, boolean>>({})

function canDecrypt(order: Order) {
  return order.sourceChannel === 'kdzs' && !!order.platformSysTid
}

function applyDecryptedOrders(items: Order[]) {
  const byId = new Map(items.map((o) => [o.id, o]))
  list.value = list.value.map((o) => byId.get(o.id) || o)
}

async function decryptOne(order: Order) {
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

async function copyOrderText(order: Order) {
  const addr = formatAddress(order.address)
  if (!addr || addr === '-') {
    ElMessage.warning('暂无收件信息，请先解密')
    return
  }
  const ok = await copyToClipboard(buildOrderCopyText(order))
  if (ok) ElMessage.success('已复制')
  else ElMessage.error('复制失败')
}

function onStatusTabChange() {
  // 切到「已分配」时默认：待发货 + 最近7天
  if (filters.status === 'allocated') {
    applyAllocatedDefaults(true)
  }
  onFilterChange()
}

watch(
  () => route.query.status,
  (raw) => {
    const next = normalizeStatus(typeof raw === 'string' ? raw : '') || 'pending_alloc'
    if (filters.status === next) return
    filters.status = next
    if (next === 'allocated') {
      applyAllocatedDefaults(true)
    }
    filters.page = 1
    void load()
  },
)

function onSelectionChange(rows: Order[]) {
  selected.value = rows
}

async function ensureSuppliers() {
  if (suppliers.value.length) return
  try {
    const [b, s] = await Promise.all([
      listBindings(),
      listSuppliers({ page: 1, pageSize: 200 }),
    ])
    bindings.value = b || []
    suppliers.value = s.list || []
  } catch {
    bindings.value = []
    suppliers.value = []
  }
}

function onSupplierPick(sid: number) {
  const s = suppliers.value.find((x) => x.id === sid)
  if (s) {
    dropshipForm.supplierName = s.name
    return
  }
  const b = bindings.value.find((x) => x.supplierId === sid)
  if (b) dropshipForm.supplierName = b.supplierName
}

async function runBatch(
  rows: Order[],
  label: string,
  fn: (o: Order) => Promise<void>,
) {
  if (!rows.length) {
    ElMessage.warning('请先勾选订单')
    return
  }
  try {
    await ElMessageBox.confirm(`确认对选中的 ${rows.length} 笔订单执行「${label}」？`, '批量操作', {
      type: 'warning',
      confirmButtonText: '执行',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  batching.value = true
  let ok = 0
  const errors: string[] = []
  try {
    for (const o of rows) {
      try {
        await fn(o)
        ok++
      } catch (e: any) {
        errors.push(`${o.orderNo || o.id}: ${e.message || '失败'}`)
      }
    }
  } finally {
    batching.value = false
  }
  if (errors.length) {
    ElMessage.warning(`${label}完成：成功 ${ok}，失败 ${errors.length}。${errors[0]}`)
  } else {
    ElMessage.success(`${label}成功 ${ok} 笔`)
  }
  await load()
}

async function batchSelfShip() {
  const rows = selected.value.filter((o) => o.status === 'pending_alloc' || o.status === 'pending_ship')
  await runBatch(rows, '批量自营发货', async (o) => {
    await allocateOrder(o.id, { allocType: 'self_ship' })
  })
}

async function openBatchDropship() {
  const rows = selected.value.filter((o) => o.status === 'pending_alloc' || o.status === 'pending_ship')
  if (!rows.length) {
    ElMessage.warning('请勾选待分配订单')
    return
  }
  dropshipForm.supplierId = undefined
  dropshipForm.supplierName = ''
  await ensureSuppliers()
  dropshipVisible.value = true
}

async function submitBatchDropship() {
  if (!dropshipForm.supplierId) {
    ElMessage.warning('请选择供应商')
    return
  }
  const rows = selected.value.filter((o) => o.status === 'pending_alloc' || o.status === 'pending_ship')
  if (!rows.length) {
    ElMessage.warning('请勾选待分配订单')
    return
  }
  try {
    await ElMessageBox.confirm(
      `确认将 ${rows.length} 笔订单合并代发给「${dropshipForm.supplierName || '供应商'}」？将生成一张代发采购单。`,
      '批量代发',
      { type: 'warning', confirmButtonText: '执行', cancelButtonText: '取消' },
    )
  } catch {
    return
  }
  dropshipVisible.value = false
  batching.value = true
  try {
    const res = await batchDropshipOrders({
      orderIds: rows.map((o) => o.id),
      supplierId: dropshipForm.supplierId,
      supplierName: dropshipForm.supplierName,
    })
    const failHint = res.failed ? `，失败 ${res.failed}` : ''
    ElMessage.success(
      `已生成代发单 ${res.poNo}：${res.success} 笔销售单 / ${res.lineCount} 行明细，原始订单金额合计 ¥${Number(res.saleAmount ?? res.totalAmount ?? 0).toFixed(2)}${failHint}`,
    )
    if (res.errors?.length) {
      ElMessage.warning(res.errors[0])
    }
    await load()
  } catch (e: any) {
    ElMessage.error(e.message || '批量代发失败')
  } finally {
    batching.value = false
  }
}

async function batchRevoke() {
  const rows = selected.value.filter((o) => o.status === 'allocated' || o.status === 'purchasing')
  await runBatch(rows, '批量撤回分配', async (o) => {
    await revokeAllocateOrder(o.id)
  })
}

onMounted(load)
</script>

<template>
  <div ref="pageRef" class="page">
    <div ref="toolbarRef" class="toolbar">
      <div class="filters">
        <el-radio-group v-model="filters.status" @change="onStatusTabChange">
          <el-radio-button value="pending_alloc">待分配</el-radio-button>
          <el-radio-button value="allocated">已分配</el-radio-button>
          <el-radio-button value="purchasing">采购中</el-radio-button>
          <el-radio-button value="">全部</el-radio-button>
        </el-radio-group>
        <el-select
          v-model="filters.shipStatus"
          clearable
          placeholder="发货状态"
          style="width: 120px"
          @change="onFilterChange"
        >
          <el-option label="待发货" value="wait_ship" />
          <el-option label="已发货" value="shipped" />
        </el-select>
        <el-select
          v-model="filters.allocType"
          clearable
          placeholder="分配类型"
          style="width: 130px"
          @change="onFilterChange"
        >
          <el-option label="自营发货" value="self_ship" />
          <el-option label="代发发货" value="dropship" />
          <el-option label="采购发货" value="purchase_then_ship" />
        </el-select>
        <div class="date-field">
          <span class="date-label">下单时间</span>
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
        </div>
        <el-input
          v-model="filters.keyword"
          clearable
          placeholder="搜索单号/买家"
          style="width: 200px"
          @keyup.enter="onFilterChange"
        />
      </div>
      <div class="batch-actions">
        <span v-if="selectedCount" class="muted">已选 {{ selectedCount }}</span>
        <el-button :disabled="!canBatchAllocate" :loading="batching" @click="batchSelfShip">批量自营</el-button>
        <el-button type="primary" :disabled="!canBatchAllocate" :loading="batching" @click="openBatchDropship">批量代发</el-button>
        <el-button type="warning" plain :disabled="!canBatchRevoke" :loading="batching" @click="batchRevoke">批量撤回</el-button>
      </div>
    </div>

    <div ref="alertRef">
      <el-alert
        type="info"
        :closable="false"
        title="分配说明"
        description="自营发货：本仓发货后填单号回传；代发发货：快递助手厂家代发（推送即可）或 OSMS 供应商代发（线下沟通后填单号）；采购发货：先采购到货再自营发出。可勾选多单批量操作。"
        show-icon
      />
    </div>

    <el-table
      ref="tableRef"
      v-loading="loading"
      :data="list"
      :height="tableHeight"
      stripe
      row-key="id"
      @selection-change="onSelectionChange"
    >
      <el-table-column type="selection" width="48" fixed="left" />
      <el-table-column label="供应商" min-width="120" show-overflow-tooltip>
        <template #default="{ row }">{{ row.supplierName || '-' }}</template>
      </el-table-column>
      <el-table-column label="平台" width="90">
        <template #default="{ row }">{{ labelPlatform(row.platform) }}</template>
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
          <div class="addr-cell">
            <div class="addr-text">{{ formatAddress(row.address) }}</div>
            <div v-if="canDecrypt(row)" class="addr-actions">
              <el-button
                v-if="isMaskedReceiver(row)"
                link
                type="warning"
                size="small"
                :loading="decryptRow[row.id]"
                @click="decryptOne(row)"
              >解密</el-button>
              <el-button
                v-else
                link
                type="primary"
                size="small"
                @click="copyOrderText(row)"
              >复制</el-button>
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

    <div ref="pagerRef" class="pager">
      <el-pagination
        v-model:current-page="filters.page"
        :page-size="filters.pageSize"
        :total="total"
        layout="total, prev, pager, next"
        @current-change="load"
      />
    </div>

    <el-dialog v-model="dropshipVisible" title="批量代发" width="480px">
      <el-form label-width="110px">
        <el-form-item label="OSMS供应商" required>
          <el-select
            v-model="dropshipForm.supplierId"
            filterable
            style="width: 100%"
            placeholder="选择供应商"
            @change="onSupplierPick"
          >
            <el-option-group v-if="bindings.length" label="已绑定厂家">
              <el-option
                v-for="b in bindings"
                :key="`b-${b.supplierId}`"
                :label="`${b.supplierName} → ${b.externalFactoryName || b.externalFactoryId}`"
                :value="b.supplierId"
              />
            </el-option-group>
            <el-option-group label="全部供应商">
              <el-option
                v-for="s in suppliers"
                :key="s.id"
                :label="`${s.name}${s.code ? ` (${s.code})` : ''}`"
                :value="s.id"
              />
            </el-option-group>
          </el-select>
        </el-form-item>
        <p class="alloc-tip">有厂家绑定会自动推快递助手厂家；无绑定则线下代发。</p>
      </el-form>
      <template #footer>
        <el-button @click="dropshipVisible = false">取消</el-button>
        <el-button type="primary" :loading="batching" @click="submitBatchDropship">确认代发</el-button>
      </template>
    </el-dialog>
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
  justify-content: space-between;
  gap: 12px;
  align-items: flex-start;
  flex-wrap: wrap;
  flex-shrink: 0;
}
.filters { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; }
.batch-actions { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.date-field { display: inline-flex; align-items: center; gap: 8px; }
.date-label { color: #606266; font-size: 14px; white-space: nowrap; }
.muted { color: #909399; font-size: 13px; margin-right: 4px; }
.pager { display: flex; justify-content: flex-end; flex-shrink: 0; }
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
.addr-cell { line-height: 1.4; }
.addr-text { font-size: 13px; white-space: normal; word-break: break-all; }
.addr-actions { margin-top: 4px; display: flex; gap: 4px; flex-wrap: wrap; }
.kdzs-meta { font-size: 12px; color: #909399; margin-top: 2px; }
.lock-tip { font-size: 12px; color: #e6a23c; }
.platform-oid { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; font-size: 12px; }
.alloc-tip { margin: 0; color: #909399; font-size: 13px; line-height: 1.5; }
.remark-lines { display: flex; flex-direction: column; gap: 2px; line-height: 1.4; }
.remark-line {
  display: flex;
  align-items: flex-start;
  gap: 4px;
  font-size: 12px;
  color: #606266;
  word-break: break-all;
}
</style>
