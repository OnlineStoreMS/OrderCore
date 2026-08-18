<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  allocateOrder,
  buildOrderCopyText,
  decryptOrders,
  formatAddress,
  revokeAllocateOrder,
  formatDateTime,
  getOrder,
  isMaskedReceiver,
  labelAgentType,
  labelAlloc,
  labelDropship,
  labelEcommerceStatus,
  labelKDZSStatus,
  labelShipStatus,
  labelSource,
  labelStatus,
  listBindings,
  listSuppliers,
  shipOrder,
  updateOrderRemarks,
  type Order,
  type SupplierBinding,
  type SupplierItem,
} from '../../api/orders'
import { copyToClipboard } from '../../utils/clipboard'
import { pushOrder } from '../../api/settings'
import { EXPRESS_COMPANIES, findExpressCompany } from '../../constants/expressCompanies'
import SellerFlag from '../../components/SellerFlag.vue'

const route = useRoute()
const router = useRouter()
const id = Number(route.params.id)
const loading = ref(false)
const order = ref<Order | null>(null)
const decrypting = ref(false)

const canDecrypt = computed(() => {
  const o = order.value
  return !!o && o.sourceChannel === 'kdzs' && !!o.platformSysTid
})

async function onDecrypt() {
  if (!order.value || !canDecrypt.value) return
  decrypting.value = true
  try {
    const data = await decryptOrders([order.value.id])
    if (data.items?.[0]) order.value = data.items[0]
    ElMessage.success('解密成功')
  } catch (e: any) {
    ElMessage.error(e.message || '解密失败')
  } finally {
    decrypting.value = false
  }
}

async function onCopyReceiver() {
  if (!order.value) return
  const addr = formatAddress(order.value.address)
  if (!addr || addr === '-') {
    ElMessage.warning('暂无收件信息，请先解密')
    return
  }
  const ok = await copyToClipboard(buildOrderCopyText(order.value))
  if (ok) ElMessage.success('已复制')
  else ElMessage.error('复制失败')
}

const allocVisible = ref(false)
const shipVisible = ref(false)
const bindings = ref<SupplierBinding[]>([])
const suppliers = ref<SupplierItem[]>([])
const savingRemarks = ref(false)
const remarkForm = reactive({
  sellerRemark: '',
  sellerFlag: 0,
  fenFaRemark: '',
  printerRemark: '',
  allocRemark: '',
})

function syncRemarkForm(o: Order | null) {
  remarkForm.sellerRemark = o?.sellerRemark || ''
  remarkForm.sellerFlag = o?.sellerFlag ?? 0
  remarkForm.fenFaRemark = o?.fenFaRemark || ''
  remarkForm.printerRemark = o?.printerRemark || ''
  remarkForm.allocRemark = o?.allocRemark || ''
}

const allocForm = reactive({
  allocType: 'self_ship',
  supplierId: undefined as number | undefined,
  supplierName: '',
  purchaseOrderId: '',
  remark: '',
})

const shipForm = reactive({
  expressCompany: '',
  expressCode: '',
  expressNo: '',
  remark: '',
  callback: true,
})

function onShipCompanyChange(code: string) {
  const hit = findExpressCompany(code)
  shipForm.expressCode = hit?.code || code || ''
  shipForm.expressCompany = hit?.name || code || ''
}

function hasBlockingEcommerce(o: Order) {
  const text = `${o.ecommerceStatusText || ''} ${o.afterSaleStatusText || ''} ${o.ecommerceStatus || ''} ${o.afterSaleStatus || ''}`
  return /退款|售后|关闭|WAIT_SELLER_AGREE|REFUNDING|REFUND_SUCCESS|TRADE_CLOSED/i.test(text)
}

const canAllocate = computed(() => {
  const o = order.value
  if (!o) return false
  if (o.status === 'closed' || o.status === 'completed') return false
  if (o.shipStatus === 'shipped') return false
  // 快递助手已推厂家代发：只跟踪，不再二次分配
  if (o.sourceChannel === 'kdzs' && o.agentType === 2) return false
  if (o.sourceChannel === 'kdzs' && hasBlockingEcommerce(o)) return false
  const s = o.status
  return s === 'pending_alloc' || s === 'pending_ship' || s === 'allocated' || s === 'purchasing'
})

const canRevokeAllocate = computed(() => {
  const o = order.value
  if (!o) return false
  if (o.status === 'completed' || o.status === 'closed') return false
  if (o.shipStatus === 'shipped') return false
  if (!o.allocType) return false
  // 厂家代发请在快递助手撤单后由同步回退
  if (o.sourceChannel === 'kdzs' && o.agentType === 2 && o.dropshipMode === 'kdzs_factory') return false
  return o.status === 'allocated' || o.status === 'purchasing'
})

const canShip = computed(() => {
  const o = order.value
  if (!o) return false
  if (o.shipEntryLocked) return false
  if (o.shipStatus === 'shipped') return false
  if (o.status === 'completed' || o.status === 'closed') return false
  if (o.allocType === 'dropship' && o.dropshipMode === 'kdzs_factory') return false
  if (o.sourceChannel === 'kdzs' && hasBlockingEcommerce(o)) return false
  return !!o.allocType
})

function shippedQtyMap(o: Order): Record<number, number> {
  const map: Record<number, number> = {}
  let hasItemRows = false
  for (const sh of o.shipments || []) {
    if (sh.items?.length) {
      hasItemRows = true
      for (const it of sh.items) {
        if (!it.orderItemId || it.qty <= 0) continue
        map[it.orderItemId] = (map[it.orderItemId] || 0) + it.qty
      }
    }
  }
  // 与后端 shippedQtyByItem 一致：无运单时不可用「已全部发货」兜底
  if (!hasItemRows && (o.shipments || []).length > 0 && o.shipStatus === 'shipped') {
    for (const it of o.items || []) {
      if (it.id) map[it.id] = it.quantity
    }
  }
  return map
}

const remainingShipItems = computed(() => {
  const o = order.value
  if (!o?.items?.length) return []
  const shipped = shippedQtyMap(o)
  const hasFull = (o.items || []).some((it) => it.splitKind === 'full')
  const parentsWithPartial = new Set(
    (o.items || [])
      .filter((it) => it.splitKind === 'partial' && it.parentOrderItemId)
      .map((it) => it.parentOrderItemId!),
  )
  return o.items
    .filter((it) => it.id)
    .filter((it) => {
      if (it.splitKind === 'partial' || it.splitKind === 'full') return true
      if (hasFull) return false
      if (parentsWithPartial.has(it.id!)) return false
      return true
    })
    .map((it) => {
      const left = Math.max(0, (it.quantity || 0) - (shipped[it.id!] || 0))
      return { ...it, remaining: left }
    })
    .filter((it) => it.remaining > 0)
})

/** 商品明细树形行：根行 + └ 拆分子行 */
type ItemTreeRow = {
  key: string
  item: NonNullable<Order['items']>[number]
  depth: number
  isSplitChild: boolean
  isSplitParent: boolean
  fullGroupHeader?: boolean
}

const itemTreeRows = computed((): ItemTreeRow[] => {
  const items = order.value?.items || []
  if (!items.length) return []
  const childrenByParent = new Map<number, typeof items>()
  const fullChildren: typeof items = []
  const roots: typeof items = []
  for (const it of items) {
    if (it.splitKind === 'full') {
      fullChildren.push(it)
      continue
    }
    if (it.splitKind === 'partial' && it.parentOrderItemId) {
      const list = childrenByParent.get(it.parentOrderItemId) || []
      list.push(it)
      childrenByParent.set(it.parentOrderItemId, list)
      continue
    }
    roots.push(it)
  }
  const out: ItemTreeRow[] = []
  for (const root of roots) {
    const kids = childrenByParent.get(root.id || 0) || []
    out.push({
      key: `root-${root.id}`,
      item: root,
      depth: 0,
      isSplitChild: false,
      isSplitParent: kids.length > 0,
    })
    for (const ch of kids) {
      out.push({
        key: `child-${ch.id}`,
        item: ch,
        depth: 1,
        isSplitChild: true,
        isSplitParent: false,
      })
    }
  }
  if (fullChildren.length) {
    out.push({
      key: 'full-header',
      item: { quantity: 0, price: 0, productName: '整单拆分' },
      depth: 0,
      isSplitChild: false,
      isSplitParent: false,
      fullGroupHeader: true,
    })
    for (const ch of fullChildren) {
      out.push({
        key: `full-${ch.id}`,
        item: ch,
        depth: 1,
        isSplitChild: true,
        isSplitParent: false,
      })
    }
  }
  return out
})

const shipItemIds = ref<number[]>([])

watch(shipVisible, (v) => {
  if (v) {
    shipItemIds.value = remainingShipItems.value.map((it) => it.id!).filter(Boolean)
  }
})

const bindingHint = computed(() => {
  if (allocForm.allocType !== 'dropship' || !allocForm.supplierId) return ''
  const b = bindings.value.find((x) => x.supplierId === allocForm.supplierId)
  if (b?.externalFactoryId) {
    return `已绑定快递助手厂家：${b.externalFactoryName || b.externalFactoryId}，将推送厂家代发`
  }
  return '未绑定快递助手厂家：快递助手侧改为自营，由该供应商线下代发'
})

async function load() {
  loading.value = true
  try {
    order.value = await getOrder(id)
    syncRemarkForm(order.value)
  } catch (e: any) {
    ElMessage.error(e.message || '加载失败')
  } finally {
    loading.value = false
  }
}

function onSellerFlagChange(v: number | null) {
  remarkForm.sellerFlag = v ?? 0
  void saveRemarks()
}

async function saveRemarks() {
  if (!order.value || savingRemarks.value) return
  const flag = remarkForm.sellerFlag ?? 0
  const same =
    (remarkForm.sellerRemark || '') === (order.value.sellerRemark || '') &&
    flag === (order.value.sellerFlag ?? 0) &&
    (remarkForm.fenFaRemark || '') === (order.value.fenFaRemark || '') &&
    (remarkForm.printerRemark || '') === (order.value.printerRemark || '') &&
    (remarkForm.allocRemark || '') === (order.value.allocRemark || '')
  if (same) return
  const writeBackKDZS =
    order.value.sourceChannel === 'kdzs' &&
    ((remarkForm.sellerRemark || '') !== (order.value.sellerRemark || '') ||
      flag !== (order.value.sellerFlag ?? 0) ||
      (remarkForm.printerRemark || '') !== (order.value.printerRemark || ''))
  savingRemarks.value = true
  try {
    order.value = await updateOrderRemarks(id, {
      sellerRemark: remarkForm.sellerRemark,
      sellerFlag: flag,
      fenFaRemark: remarkForm.fenFaRemark,
      printerRemark: remarkForm.printerRemark,
      allocRemark: remarkForm.allocRemark,
    })
    syncRemarkForm(order.value)
    ElMessage.success(writeBackKDZS ? '备注已保存并写回快递助手' : '备注已保存')
  } catch (e: any) {
    ElMessage.error(e.message || '保存失败')
    syncRemarkForm(order.value)
  } finally {
    savingRemarks.value = false
  }
}

async function openAllocate() {
  allocVisible.value = true
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
    allocForm.supplierName = s.name
    return
  }
  const b = bindings.value.find((x) => x.supplierId === sid)
  if (b) allocForm.supplierName = b.supplierName
}

async function submitAllocate() {
  try {
    order.value = await allocateOrder(id, {
      allocType: allocForm.allocType,
      supplierId: allocForm.supplierId,
      supplierName: allocForm.supplierName,
      purchaseOrderId: allocForm.purchaseOrderId,
      remark: allocForm.remark,
    })
    syncRemarkForm(order.value)
    const poTip = order.value?.purchaseOrderId ? `，已生成供应商代发单 ${order.value.purchaseOrderId}` : ''
    ElMessage.success(`分配成功${poTip}`)
    allocVisible.value = false
  } catch (e: any) {
    ElMessage.error(e.message || '分配失败')
  }
}

async function onRevokeAllocate() {
  try {
    await ElMessageBox.confirm(
      '确认撤回分配？将同步在快递助手撤单（回到待推单），订单中心恢复为待分配。',
      '撤回分配',
      {
        type: 'warning',
        confirmButtonText: '撤回',
        cancelButtonText: '取消',
      },
    )
    order.value = await revokeAllocateOrder(id)
    syncRemarkForm(order.value)
    ElMessage.success('已撤回分配（快递助手已同步）')
  } catch (e: any) {
    if (e === 'cancel' || e === 'close') return
    ElMessage.error(e.message || '撤回失败')
  }
}

async function submitShip() {
  if (!shipForm.expressCompany?.trim() && shipForm.expressCode) {
    onShipCompanyChange(shipForm.expressCode)
  }
  if (!shipForm.expressCompany?.trim()) {
    ElMessage.warning('请选择快递公司')
    return
  }
  if (!shipForm.expressNo?.trim()) {
    ElMessage.warning('请填写物流单号')
    return
  }
  const remaining = remainingShipItems.value
  let items: { orderItemId: number; qty: number }[] | undefined
  if (remaining.length) {
    if (!shipItemIds.value.length) {
      ElMessage.warning('请选择本单要发货的商品')
      return
    }
    items = remaining
      .filter((it) => shipItemIds.value.includes(it.id!))
      .map((it) => ({ orderItemId: it.id!, qty: it.remaining }))
    if (!items.length) {
      ElMessage.warning('请选择本单要发货的商品')
      return
    }
  }
  try {
    order.value = await shipOrder(id, {
      expressCompany: shipForm.expressCompany,
      expressNo: shipForm.expressNo,
      remark: shipForm.remark,
      callback: shipForm.callback,
      items,
    })
    const expressNo = String(shipForm.expressNo || '').trim()
    const list = order.value.shipments || []
    const sh = list.find((s) => (s.expressNo || '').trim() === expressNo)
      || [...list].sort((a, b) => b.id - a.id)[0]
    const cbStatus = (sh?.callbackStatus || '').toLowerCase()
    const cbMsg = (sh?.callbackMessage || '').trim()
    if (shipForm.callback && cbStatus === 'failed') {
      await ElMessageBox.alert(cbMsg || '平台回传失败（无详细报错）', '回传失败（快递助手/平台原始报错）', {
        type: 'error',
        confirmButtonText: '知道了',
      })
    } else {
      const base = order.value.shipStatus === 'partial_shipped' ? '已记录部分发货' : '发货已记录'
      ElMessage.success(cbMsg && cbStatus === 'succeeded' ? `${base}：${cbMsg}` : base)
    }
    shipVisible.value = false
  } catch (e: any) {
    ElMessage.error(e.message || '发货失败')
  }
}

async function onPushSupplier() {
  try {
    await pushOrder(id)
    ElMessage.success('已推送给供应商渠道')
  } catch (e: any) {
    ElMessage.error(e.message || '推送失败')
  }
}

onMounted(load)
</script>

<template>
  <div v-loading="loading" class="page">
    <div class="head">
      <div>
        <el-button text @click="router.back()">← 返回</el-button>
        <h2>{{ order?.orderNo || '订单详情' }}</h2>
      </div>
      <div class="actions">
        <el-button v-if="canDecrypt" type="warning" plain :loading="decrypting" @click="onDecrypt">
          {{ order && isMaskedReceiver(order) ? '解密地址' : '重新解密' }}
        </el-button>
        <el-button v-if="order && formatAddress(order.address) !== '-'" @click="onCopyReceiver">复制地址规格</el-button>
        <el-button v-if="canAllocate" type="primary" @click="openAllocate">分配</el-button>
        <el-button v-if="canRevokeAllocate" @click="onRevokeAllocate">撤回分配</el-button>
        <el-tooltip v-if="order?.shipEntryLocked" :content="order.shipLockReason || '已锁定填单号发货'" placement="top">
          <el-button type="success" disabled>填写物流</el-button>
        </el-tooltip>
        <el-button v-else-if="canShip" type="success" @click="shipVisible = true">填写物流</el-button>
        <el-button v-if="order" @click="onPushSupplier">推送供应商</el-button>
      </div>
    </div>

    <template v-if="order">
      <el-descriptions :column="3" border>
        <el-descriptions-item label="订单类型">{{ labelSource(order.sourceChannel) }}</el-descriptions-item>
        <el-descriptions-item v-if="order.sourceChannel === 'manual'" label="订单来源">
          {{ order.manualSourceName || '—' }}
        </el-descriptions-item>
        <el-descriptions-item label="平台">{{ order.platform || '-' }} / {{ order.shopName || '-' }}</el-descriptions-item>
        <el-descriptions-item label="平台单号">{{ order.platformOrderId || '-' }}</el-descriptions-item>
        <el-descriptions-item label="履约状态">{{ labelStatus(order.status) }}</el-descriptions-item>
        <el-descriptions-item label="发货状态">{{ labelShipStatus(order.shipStatus) }}</el-descriptions-item>
        <el-descriptions-item label="快递助手状态">
          {{ labelKDZSStatus(order) }}
          <span v-if="order.sourceChannel === 'kdzs'" class="muted"> · {{ labelAgentType(order.agentType) }}</span>
        </el-descriptions-item>
        <el-descriptions-item label="电商订单状态">
          {{ labelEcommerceStatus(order) }}
          <div v-if="order.afterSaleStatusText" class="muted">售后：{{ order.afterSaleStatusText }}</div>
        </el-descriptions-item>
        <el-descriptions-item label="发货入口">
          <el-tag v-if="order.shipEntryLocked" type="warning" size="small">已锁定</el-tag>
          <el-tag v-else type="success" size="small">可填单号</el-tag>
          <div v-if="order.shipLockReason" class="muted">{{ order.shipLockReason }}</div>
        </el-descriptions-item>
        <el-descriptions-item label="分配类型">{{ labelAlloc(order.allocType) }}</el-descriptions-item>
        <el-descriptions-item label="代发方式">{{ labelDropship(order.dropshipMode) }}</el-descriptions-item>
        <el-descriptions-item label="供应商">{{ order.supplierName || '-' }}</el-descriptions-item>
        <el-descriptions-item label="厂家">{{ order.factoryName || order.factoryId || '-' }}</el-descriptions-item>
        <el-descriptions-item label="实付">{{ order.payAmount ?? '-' }}</el-descriptions-item>
        <el-descriptions-item label="邮费">{{ order.freightAmount ?? 0 }}</el-descriptions-item>
        <el-descriptions-item label="买家">{{ order.buyerName || order.buyerNick || '-' }} {{ order.buyerPhone || '' }}</el-descriptions-item>
        <el-descriptions-item label="地址" :span="2">
          <div>{{ order.address?.fullText || order.address?.address || '-' }}</div>
          <div v-if="canDecrypt || (order.address?.fullText || order.address?.address)" class="addr-actions">
            <el-button v-if="canDecrypt && isMaskedReceiver(order)" link type="warning" size="small" :loading="decrypting" @click="onDecrypt">解密</el-button>
            <el-button v-if="formatAddress(order.address) !== '-'" link type="primary" size="small" @click="onCopyReceiver">复制</el-button>
          </div>
        </el-descriptions-item>
        <el-descriptions-item label="买家留言">{{ order.remark || '-' }}</el-descriptions-item>
        <el-descriptions-item label="发货内容" :span="2">{{ order.shipContent || '-' }}</el-descriptions-item>
        <el-descriptions-item label="卖家备注">
          <div class="seller-remark-row">
            <SellerFlag
              :model-value="remarkForm.sellerFlag"
              mode="edit"
              :size="16"
              @update:model-value="onSellerFlagChange"
            />
            <el-input
              v-model="remarkForm.sellerRemark"
              class="remark-input"
              size="small"
              :placeholder="order.sourceChannel === 'kdzs' ? '保存后写回快递助手' : '点击填写'"
              clearable
              @change="saveRemarks"
            />
          </div>
        </el-descriptions-item>
        <el-descriptions-item label="分发备注">
          <el-input
            v-model="remarkForm.fenFaRemark"
            class="remark-input"
            size="small"
            placeholder="点击填写"
            clearable
            @change="saveRemarks"
          />
        </el-descriptions-item>
        <el-descriptions-item label="打单备注">
          <el-input
            v-model="remarkForm.printerRemark"
            class="remark-input"
            size="small"
            :placeholder="order.sourceChannel === 'kdzs' ? '保存后写回快递助手' : '点击填写'"
            clearable
            @change="saveRemarks"
          />
        </el-descriptions-item>
        <el-descriptions-item label="分配备注" :span="1">
          <el-input
            v-model="remarkForm.allocRemark"
            class="remark-input"
            size="small"
            placeholder="点击填写"
            clearable
            @change="saveRemarks"
          />
        </el-descriptions-item>
      </el-descriptions>

      <h3>商品明细</h3>
      <el-table :data="itemTreeRows" size="small" row-key="key">
        <el-table-column label="图片" width="72">
          <template #default="{ row }">
            <template v-if="row.fullGroupHeader">
              <span class="muted">—</span>
            </template>
            <template v-else>
              <el-image
                v-if="row.item.picUrl"
                :src="row.item.picUrl"
                :preview-src-list="[row.item.picUrl]"
                fit="cover"
                style="width: 48px; height: 48px; border-radius: 4px"
                preview-teleported
              />
              <span v-else class="muted">-</span>
            </template>
          </template>
        </el-table-column>
        <el-table-column label="商品" min-width="220">
          <template #default="{ row }">
            <div :class="{ 'split-child': row.isSplitChild || row.fullGroupHeader }">
              <span v-if="row.isSplitChild" class="split-prefix">└ </span>
              <span v-if="row.fullGroupHeader" class="split-group">整单拆分</span>
              <template v-else>
                {{
                  row.isSplitChild
                    ? (row.item.skuSpecs || row.item.productName || row.item.skuCode || '规格')
                    : (row.item.productName || row.item.skuCode || '商品')
                }}
                <el-tag v-if="row.isSplitChild" size="small" type="warning" class="split-tag">拆分</el-tag>
                <el-tag v-else-if="row.isSplitParent" size="small" type="info" class="split-tag">已拆分</el-tag>
              </template>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="规格" width="140">
          <template #default="{ row }">
            <span v-if="row.fullGroupHeader || row.isSplitChild">—</span>
            <span v-else-if="row.item.skuSpecs && row.item.skuSpecs !== row.item.productName">{{ row.item.skuSpecs }}</span>
            <span v-else>—</span>
          </template>
        </el-table-column>
        <el-table-column prop="platformSkuId" label="平台SKU" width="130" show-overflow-tooltip>
          <template #default="{ row }">{{ row.fullGroupHeader ? '—' : (row.item.platformSkuId || '—') }}</template>
        </el-table-column>
        <el-table-column label="商家编码" width="140">
          <template #default="{ row }">{{ row.fullGroupHeader ? '—' : (row.item.skuCode || '—') }}</template>
        </el-table-column>
        <el-table-column label="数量" width="80">
          <template #default="{ row }">{{ row.fullGroupHeader ? '—' : row.item.quantity }}</template>
        </el-table-column>
        <el-table-column label="单价" width="90">
          <template #default="{ row }">{{ row.fullGroupHeader || row.isSplitChild ? '—' : row.item.price }}</template>
        </el-table-column>
        <el-table-column label="小计" width="90">
          <template #default="{ row }">{{ row.fullGroupHeader || row.isSplitChild ? '—' : row.item.totalAmount }}</template>
        </el-table-column>
      </el-table>

      <h3>发货记录</h3>
      <el-table :data="order.shipments || []" size="small">
        <el-table-column prop="shipmentNo" label="发货单号" width="160" />
        <el-table-column prop="expressCompany" label="快递公司" width="120" />
        <el-table-column prop="expressNo" label="物流单号" min-width="160" />
        <el-table-column label="发货商品" min-width="220">
          <template #default="{ row }">
            <template v-if="row.items?.length">
              <div v-for="(it, idx) in row.items" :key="it.id || idx" class="ship-item-line">
                {{ it.skuSpecs || it.productName || it.skuCode || `行#${it.orderItemId}` }} ×{{ it.qty }}
              </div>
            </template>
            <span v-else class="muted">整单/未关联明细</span>
          </template>
        </el-table-column>
        <el-table-column prop="callbackStatus" label="回传状态" width="120" />
        <el-table-column prop="callbackMessage" label="回传说明" min-width="200" show-overflow-tooltip />
        <el-table-column label="发货时间" width="170">
          <template #default="{ row }">{{ formatDateTime(row.shippedAt) }}</template>
        </el-table-column>
      </el-table>

      <h3>状态流水</h3>
      <el-table :data="order.statusLogs || []" size="small">
        <el-table-column label="时间" width="170">
          <template #default="{ row }">{{ formatDateTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column prop="action" label="动作" width="140" />
        <el-table-column label="状态" min-width="180">
          <template #default="{ row }">
            {{ labelStatus(row.fromStatus) }} → {{ labelStatus(row.toStatus) }}
          </template>
        </el-table-column>
        <el-table-column prop="remark" label="备注" min-width="200" show-overflow-tooltip />
      </el-table>
    </template>

    <el-dialog v-model="allocVisible" title="订单分配" width="560px">
      <el-form label-width="110px">
        <el-form-item label="分配类型">
          <el-radio-group v-model="allocForm.allocType">
            <el-radio value="self_ship">自营发货</el-radio>
            <el-radio value="dropship">代发发货</el-radio>
            <el-radio value="purchase_then_ship">采购发货</el-radio>
          </el-radio-group>
        </el-form-item>
        <p v-if="order?.sourceChannel === 'kdzs'" class="alloc-tip">
          自营/采购：快递助手改为自营。代发：按厂家绑定自动推厂家，无绑定则快递助手改自营。
        </p>
        <el-form-item v-if="allocForm.allocType === 'dropship'" label="OSMS供应商" required>
          <el-select
            v-model="allocForm.supplierId"
            filterable
            style="width: 100%"
            placeholder="从 SupplyCore 选择供应商"
            @change="onSupplierPick"
          >
            <el-option
              v-for="s in suppliers"
              :key="s.id"
              :label="`${s.name}${s.code ? ' (' + s.code + ')' : ''}`"
              :value="s.id"
            />
          </el-select>
          <div v-if="bindingHint" class="alloc-tip">{{ bindingHint }}</div>
        </el-form-item>
        <el-form-item v-if="allocForm.allocType === 'purchase_then_ship'" label="采购单号">
          <el-input v-model="allocForm.purchaseOrderId" placeholder="可选，关联 SupplyCore 采购单" />
        </el-form-item>
        <el-form-item label="备注"><el-input v-model="allocForm.remark" type="textarea" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="allocVisible = false">取消</el-button>
        <el-button type="primary" @click="submitAllocate">确认分配</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="shipVisible" title="填写物流单号" width="560px">
      <el-form label-width="100px">
        <el-form-item v-if="remainingShipItems.length" label="发货商品" required>
          <el-checkbox-group v-model="shipItemIds">
            <div v-for="it in remainingShipItems" :key="it.id" class="ship-pick-row">
              <el-checkbox :value="it.id">
                {{ it.skuSpecs || it.productName || it.skuCode || `商品#${it.id}` }}
                <span class="muted">（可发 {{ it.remaining }}）</span>
              </el-checkbox>
            </div>
          </el-checkbox-group>
          <div class="hint">勾选本运单实际发出的商品；未勾选的仍为待发货。</div>
        </el-form-item>
        <el-form-item label="快递公司" required>
          <el-select
            v-model="shipForm.expressCode"
            filterable
            allow-create
            default-first-option
            clearable
            placeholder="选择或搜索快递公司"
            style="width: 100%"
            @change="onShipCompanyChange"
          >
            <el-option
              v-for="c in EXPRESS_COMPANIES"
              :key="c.code"
              :label="c.name"
              :value="c.code"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="物流单号" required><el-input v-model="shipForm.expressNo" placeholder="运单号" /></el-form-item>
        <el-form-item label="回传来源">
          <el-switch v-model="shipForm.callback" />
          <span class="hint">电商订单将回传 StoreSyncAgent</span>
        </el-form-item>
        <el-form-item label="备注"><el-input v-model="shipForm.remark" /></el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="shipVisible = false">取消</el-button>
        <el-button type="primary" @click="submitShip">确认发货</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 16px; }
.head { display: flex; justify-content: space-between; align-items: flex-start; }
.head h2 { margin: 4px 0 0; }
.actions { display: flex; gap: 8px; flex-wrap: wrap; }
h3 { margin: 8px 0 0; font-size: 15px; color: #334155; }
.hint { margin-left: 10px; color: #94a3b8; font-size: 12px; }
.muted { color: #94a3b8; font-size: 12px; }
.split-child { color: #475569; }
.split-prefix { color: #8f959e; }
.split-tag { margin-left: 6px; vertical-align: middle; }
.split-group { font-weight: 600; color: #64748b; }
.ship-pick-row { margin-bottom: 6px; }
.ship-item-line { font-size: 12px; line-height: 1.5; }
.addr-actions { margin-top: 6px; display: flex; gap: 8px; }
.alloc-tip { margin: 0 0 12px 110px; color: #64748b; font-size: 12px; line-height: 1.5; }
.remark-input { width: 100%; max-width: 220px; }
.seller-remark-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
}
</style>
