<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import {
  createManualOrder, labelAlloc, labelSource, labelStatus, listOrders, syncKDZS, syncStore, type Order,
} from '../../api/orders'

const router = useRouter()
const route = useRoute()
const loading = ref(false)
const list = ref<Order[]>([])
const total = ref(0)
const filters = reactive({
  page: 1,
  pageSize: 20,
  sourceChannel: (route.query.sourceChannel as string) || '',
  status: (route.query.status as string) || '',
  allocType: '',
  keyword: '',
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
    const data = await listOrders({ ...filters })
    list.value = data.list || []
    total.value = data.total || 0
  } catch (e: any) {
    ElMessage.error(e.message || '加载失败')
  } finally {
    loading.value = false
  }
}

async function onSyncKDZS() {
  try {
    const stats = await syncKDZS({ pageSize: 50 }) as Record<string, number>
    ElMessage.success(`同步完成：新增 ${stats.created || 0}，更新 ${stats.updated || 0}`)
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
          <el-select v-model="filters.sourceChannel" clearable style="width: 150px" @change="() => { filters.page = 1; load() }">
            <el-option label="电商(快递助手)" value="kdzs" />
            <el-option label="门店销售" value="store" />
            <el-option label="手工订单" value="manual" />
            <el-option label="微信小程序" value="wx_mall" />
          </el-select>
        </el-form-item>
        <el-form-item label="状态">
          <el-select v-model="filters.status" clearable style="width: 130px" @change="() => { filters.page = 1; load() }">
            <el-option label="待发货" value="pending_ship" />
            <el-option label="已分配" value="allocated" />
            <el-option label="采购中" value="purchasing" />
            <el-option label="已发货" value="shipped" />
            <el-option label="已完成" value="completed" />
          </el-select>
        </el-form-item>
        <el-form-item label="分配">
          <el-select v-model="filters.allocType" clearable style="width: 130px" @change="() => { filters.page = 1; load() }">
            <el-option label="自营发货" value="self_ship" />
            <el-option label="代发发货" value="dropship" />
            <el-option label="采购发货" value="purchase_then_ship" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-input v-model="filters.keyword" clearable placeholder="单号/买家/手机" style="width: 180px" @keyup.enter="() => { filters.page = 1; load() }" />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="() => { filters.page = 1; load() }">查询</el-button>
        </el-form-item>
      </el-form>
      <div class="right">
        <el-button @click="onSyncKDZS">同步电商</el-button>
        <el-button @click="onSyncStore">同步门店</el-button>
        <el-button type="primary" @click="manualVisible = true">手工建单</el-button>
      </div>
    </div>

    <el-table v-loading="loading" :data="list" stripe @row-click="(row: Order) => router.push(`/orders/${row.id}`)">
      <el-table-column prop="orderNo" label="内部单号" width="160" />
      <el-table-column label="来源" width="130">
        <template #default="{ row }">{{ labelSource(row.sourceChannel) }}</template>
      </el-table-column>
      <el-table-column prop="platformOrderId" label="平台单号" min-width="150" show-overflow-tooltip />
      <el-table-column label="买家" min-width="140">
        <template #default="{ row }">{{ row.buyerName || row.buyerNick || '-' }}</template>
      </el-table-column>
      <el-table-column prop="payAmount" label="实付" width="90" />
      <el-table-column label="状态" width="100">
        <template #default="{ row }">
          <el-tag size="small">{{ labelStatus(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="分配" width="110">
        <template #default="{ row }">{{ labelAlloc(row.allocType) }}</template>
      </el-table-column>
      <el-table-column prop="createdAt" label="创建时间" width="170" />
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
</style>
