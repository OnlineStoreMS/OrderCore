<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import {
  allocateOrder, getOrder, labelAlloc, labelDropship, labelSource, labelStatus, listBindings, listFactories,
  shipOrder, type FactoryItem, type Order, type SupplierBinding,
} from '../../api/orders'

const route = useRoute()
const router = useRouter()
const id = Number(route.params.id)
const loading = ref(false)
const order = ref<Order | null>(null)

const allocVisible = ref(false)
const shipVisible = ref(false)
const factories = ref<FactoryItem[]>([])
const bindings = ref<SupplierBinding[]>([])

const allocForm = reactive({
  allocType: 'self_ship',
  dropshipMode: 'osms_supplier',
  supplierId: undefined as number | undefined,
  supplierName: '',
  factoryId: '',
  factoryName: '',
  purchaseOrderId: '',
  remark: '',
})

const shipForm = reactive({
  expressCompany: '',
  expressNo: '',
  remark: '',
  callback: true,
})

const canAllocate = computed(() => {
  const s = order.value?.status
  return s === 'pending_ship' || s === 'allocated' || s === 'purchasing'
})

const canShip = computed(() => {
  const o = order.value
  if (!o) return false
  if (o.status === 'shipped' || o.status === 'completed' || o.status === 'closed') return false
  if (o.allocType === 'dropship' && o.dropshipMode === 'kdzs_factory') return false
  return !!o.allocType
})

async function load() {
  loading.value = true
  try {
    order.value = await getOrder(id)
  } catch (e: any) {
    ElMessage.error(e.message || '加载失败')
  } finally {
    loading.value = false
  }
}

async function openAllocate() {
  allocVisible.value = true
  try {
    bindings.value = await listBindings()
    const res = await listFactories({ platform: order.value?.platform || 'FXG', pageSize: 100 })
    factories.value = res.items || []
  } catch {
    // 工厂列表依赖 StoreSyncAgent，失败时仍可手工填
  }
}

function onSupplierPick(sid: number) {
  const b = bindings.value.find((x) => x.supplierId === sid)
  if (b) {
    allocForm.supplierName = b.supplierName
    if (allocForm.dropshipMode === 'kdzs_factory') {
      allocForm.factoryId = b.externalFactoryId
      allocForm.factoryName = b.externalFactoryName || ''
    }
  }
}

function onFactoryPick(fid: string) {
  const f = factories.value.find((x) => x.factoryId === fid)
  if (f) allocForm.factoryName = f.factoryName
  const b = bindings.value.find((x) => x.externalFactoryId === fid)
  if (b) {
    allocForm.supplierId = b.supplierId
    allocForm.supplierName = b.supplierName
  }
}

async function submitAllocate() {
  try {
    order.value = await allocateOrder(id, { ...allocForm })
    ElMessage.success('分配成功')
    allocVisible.value = false
  } catch (e: any) {
    ElMessage.error(e.message || '分配失败')
  }
}

async function submitShip() {
  try {
    order.value = await shipOrder(id, { ...shipForm })
    ElMessage.success('发货已记录')
    shipVisible.value = false
  } catch (e: any) {
    ElMessage.error(e.message || '发货失败')
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
        <el-button v-if="canAllocate" type="primary" @click="openAllocate">分配</el-button>
        <el-button v-if="canShip" type="success" @click="shipVisible = true">填写物流</el-button>
      </div>
    </div>

    <template v-if="order">
      <el-descriptions :column="3" border>
        <el-descriptions-item label="来源">{{ labelSource(order.sourceChannel) }}</el-descriptions-item>
        <el-descriptions-item label="平台">{{ order.platform || '-' }} / {{ order.shopName || '-' }}</el-descriptions-item>
        <el-descriptions-item label="平台单号">{{ order.platformOrderId || '-' }}</el-descriptions-item>
        <el-descriptions-item label="状态">{{ labelStatus(order.status) }}</el-descriptions-item>
        <el-descriptions-item label="分配类型">{{ labelAlloc(order.allocType) }}</el-descriptions-item>
        <el-descriptions-item label="代发方式">{{ labelDropship(order.dropshipMode) }}</el-descriptions-item>
        <el-descriptions-item label="供应商">{{ order.supplierName || '-' }}</el-descriptions-item>
        <el-descriptions-item label="厂家">{{ order.factoryName || order.factoryId || '-' }}</el-descriptions-item>
        <el-descriptions-item label="实付">{{ order.payAmount ?? '-' }}</el-descriptions-item>
        <el-descriptions-item label="买家">{{ order.buyerName || order.buyerNick || '-' }} {{ order.buyerPhone || '' }}</el-descriptions-item>
        <el-descriptions-item label="地址" :span="2">{{ order.address?.fullText || order.address?.address || '-' }}</el-descriptions-item>
        <el-descriptions-item label="买家备注">{{ order.remark || '-' }}</el-descriptions-item>
        <el-descriptions-item label="卖家备注">{{ order.sellerRemark || '-' }}</el-descriptions-item>
        <el-descriptions-item label="分配备注">{{ order.allocRemark || '-' }}</el-descriptions-item>
      </el-descriptions>

      <h3>商品明细</h3>
      <el-table :data="order.items || []" size="small">
        <el-table-column label="图片" width="72">
          <template #default="{ row }">
            <el-image
              v-if="row.picUrl"
              :src="row.picUrl"
              :preview-src-list="[row.picUrl]"
              fit="cover"
              style="width: 48px; height: 48px; border-radius: 4px"
              preview-teleported
            />
            <span v-else class="muted">-</span>
          </template>
        </el-table-column>
        <el-table-column prop="productName" label="商品" min-width="200" />
        <el-table-column prop="skuSpecs" label="规格" width="140" />
        <el-table-column prop="skuCode" label="商家编码" width="140" />
        <el-table-column prop="quantity" label="数量" width="80" />
        <el-table-column prop="price" label="单价" width="90" />
        <el-table-column prop="totalAmount" label="小计" width="90" />
      </el-table>

      <h3>发货记录</h3>
      <el-table :data="order.shipments || []" size="small">
        <el-table-column prop="shipmentNo" label="发货单号" width="160" />
        <el-table-column prop="expressCompany" label="快递公司" width="120" />
        <el-table-column prop="expressNo" label="物流单号" min-width="160" />
        <el-table-column prop="callbackStatus" label="回传状态" width="120" />
        <el-table-column prop="callbackMessage" label="回传说明" min-width="200" show-overflow-tooltip />
        <el-table-column prop="shippedAt" label="发货时间" width="170" />
      </el-table>

      <h3>状态流水</h3>
      <el-table :data="order.statusLogs || []" size="small">
        <el-table-column prop="createdAt" label="时间" width="170" />
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
        <template v-if="allocForm.allocType === 'dropship'">
          <el-form-item label="代发方式">
            <el-radio-group v-model="allocForm.dropshipMode">
              <el-radio value="kdzs_factory">快递助手厂家代发</el-radio>
              <el-radio value="osms_supplier">OSMS供应商代发</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item v-if="allocForm.dropshipMode === 'kdzs_factory'" label="厂家">
            <el-select v-model="allocForm.factoryId" filterable allow-create style="width: 100%" @change="onFactoryPick">
              <el-option v-for="f in factories" :key="f.factoryId" :label="f.factoryName" :value="f.factoryId" />
            </el-select>
          </el-form-item>
          <el-form-item label="供应商">
            <el-select v-model="allocForm.supplierId" filterable clearable style="width: 100%" @change="onSupplierPick">
              <el-option v-for="b in bindings" :key="b.id" :label="`${b.supplierName} (#${b.supplierId})`" :value="b.supplierId" />
            </el-select>
          </el-form-item>
        </template>
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

    <el-dialog v-model="shipVisible" title="填写物流单号" width="480px">
      <el-form label-width="100px">
        <el-form-item label="快递公司"><el-input v-model="shipForm.expressCompany" /></el-form-item>
        <el-form-item label="物流单号"><el-input v-model="shipForm.expressNo" /></el-form-item>
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
.actions { display: flex; gap: 8px; }
h3 { margin: 8px 0 0; font-size: 15px; color: #334155; }
.hint { margin-left: 10px; color: #94a3b8; font-size: 12px; }
.muted { color: #94a3b8; }
</style>
