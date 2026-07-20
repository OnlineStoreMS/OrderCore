<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  createBinding,
  deleteBinding,
  listBindings,
  listFactories,
  listSuppliers,
  updateBinding,
  type FactoryItem,
  type SupplierBinding,
  type SupplierItem,
} from '../../api/orders'

const loading = ref(false)
const list = ref<SupplierBinding[]>([])
const factories = ref<FactoryItem[]>([])
const suppliers = ref<SupplierItem[]>([])
const dialogVisible = ref(false)
const editingId = ref<number | null>(null)
const form = reactive({
  supplierId: undefined as number | undefined,
  supplierCode: '',
  supplierName: '',
  sourceChannel: 'kdzs',
  externalFactoryId: '',
  externalFactoryName: '',
  platform: 'FXG',
  remark: '',
})

async function load() {
  loading.value = true
  try {
    list.value = await listBindings()
  } catch (e: any) {
    ElMessage.error(e.message || '加载失败')
  } finally {
    loading.value = false
  }
}

async function loadFactories() {
  try {
    const res = await listFactories({ platform: form.platform || 'FXG', pageSize: 100 })
    factories.value = res.items || []
  } catch (e: any) {
    factories.value = []
    ElMessage.warning(e.message || '同步快递助手厂家失败，请确认 StoreSyncAgent 可用')
  }
}

async function loadSuppliers() {
  try {
    const res = await listSuppliers({ page: 1, pageSize: 200 })
    suppliers.value = res.list || []
  } catch (e: any) {
    suppliers.value = []
    ElMessage.warning(e.message || '同步 OSMS 供应商失败，请确认 SupplyCore 可用')
  }
}

function openCreate() {
  editingId.value = null
  Object.assign(form, {
    supplierId: undefined,
    supplierCode: '',
    supplierName: '',
    sourceChannel: 'kdzs',
    externalFactoryId: '',
    externalFactoryName: '',
    platform: 'FXG',
    remark: '',
  })
  dialogVisible.value = true
  loadSuppliers()
  loadFactories()
}

function openEdit(row: SupplierBinding) {
  editingId.value = row.id
  Object.assign(form, {
    supplierId: row.supplierId,
    supplierCode: row.supplierCode || '',
    supplierName: row.supplierName,
    sourceChannel: row.sourceChannel,
    externalFactoryId: row.externalFactoryId,
    externalFactoryName: row.externalFactoryName || '',
    platform: row.platform || 'FXG',
    remark: row.remark || '',
  })
  dialogVisible.value = true
  loadSuppliers()
  loadFactories()
}

function onSupplierChange(sid: number) {
  const s = suppliers.value.find((x) => x.id === sid)
  if (!s) return
  form.supplierName = s.name
  form.supplierCode = s.code || ''
}

function onFactoryChange(fid: string) {
  const f = factories.value.find((x) => x.factoryId === fid)
  if (f) form.externalFactoryName = f.factoryName || f.factoryNick || ''
}

async function submit() {
  if (!form.supplierId || !form.supplierName || !form.externalFactoryId) {
    ElMessage.warning('请选择 OSMS 供应商与快递助手厂家')
    return
  }
  try {
    if (editingId.value) {
      await updateBinding(editingId.value, { ...form })
      ElMessage.success('已更新')
    } else {
      await createBinding({ ...form })
      ElMessage.success('已创建')
    }
    dialogVisible.value = false
    await load()
  } catch (e: any) {
    ElMessage.error(e.message || '保存失败')
  }
}

async function onDelete(row: SupplierBinding) {
  await ElMessageBox.confirm(`确认删除绑定「${row.supplierName} ↔ ${row.externalFactoryName || row.externalFactoryId}」？`, '删除确认')
  try {
    await deleteBinding(row.id)
    ElMessage.success('已删除')
    await load()
  } catch (e: any) {
    ElMessage.error(e.message || '删除失败')
  }
}

onMounted(load)
</script>

<template>
  <div class="page">
    <div class="toolbar">
      <div>
        <h3>供应商 ↔ 快递助手厂家绑定</h3>
        <p>从 SupplyCore 选择供应商，从快递助手同步厂家并绑定。分配代发时按此关系推送对应厂家；未绑定则快递助手侧改自营。</p>
      </div>
      <el-button type="primary" @click="openCreate">新建绑定</el-button>
    </div>

    <el-table v-loading="loading" :data="list" stripe>
      <el-table-column prop="supplierName" label="OSMS供应商" min-width="180">
        <template #default="{ row }">
          <div>{{ row.supplierName }}</div>
          <div class="muted">{{ row.supplierCode || `ID ${row.supplierId}` }}</div>
        </template>
      </el-table-column>
      <el-table-column prop="externalFactoryName" label="快递助手厂家" min-width="180">
        <template #default="{ row }">
          <div>{{ row.externalFactoryName || '-' }}</div>
          <div class="muted">{{ row.externalFactoryId }}</div>
        </template>
      </el-table-column>
      <el-table-column prop="platform" label="平台" width="90" />
      <el-table-column prop="remark" label="备注" min-width="140" show-overflow-tooltip />
      <el-table-column label="操作" width="140" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
          <el-button link type="danger" @click="onDelete(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialogVisible" :title="editingId ? '编辑绑定' : '新建绑定'" width="560px">
      <el-form label-width="120px">
        <el-form-item label="平台">
          <el-select v-model="form.platform" style="width: 100%" @change="loadFactories">
            <el-option label="抖店" value="FXG" />
            <el-option label="淘宝" value="TB" />
            <el-option label="小红书" value="XHS" />
            <el-option label="拼多多" value="PDD" />
            <el-option label="快手" value="KSXD" />
          </el-select>
        </el-form-item>
        <el-form-item label="OSMS供应商" required>
          <el-select
            v-model="form.supplierId"
            filterable
            style="width: 100%"
            placeholder="从 SupplyCore 同步选择"
            @focus="loadSuppliers"
            @change="onSupplierChange"
          >
            <el-option
              v-for="s in suppliers"
              :key="s.id"
              :label="`${s.name}${s.code ? ' (' + s.code + ')' : ''}`"
              :value="s.id"
            />
          </el-select>
          <el-button class="reload" link type="primary" @click="loadSuppliers">重新同步供应商</el-button>
        </el-form-item>
        <el-form-item label="快递助手厂家" required>
          <el-select
            v-model="form.externalFactoryId"
            filterable
            style="width: 100%"
            placeholder="从快递助手同步选择"
            @focus="loadFactories"
            @change="onFactoryChange"
          >
            <el-option
              v-for="f in factories"
              :key="f.factoryId"
              :label="`${f.factoryName || f.factoryNick || f.factoryId} (${f.factoryId})`"
              :value="f.factoryId"
            />
          </el-select>
          <el-button class="reload" link type="primary" @click="loadFactories">重新同步厂家</el-button>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.remark" type="textarea" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="submit">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 12px; }
.toolbar { display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; }
.toolbar h3 { margin: 0 0 6px; }
.toolbar p { margin: 0; color: #64748b; font-size: 13px; max-width: 720px; line-height: 1.5; }
.muted { color: #94a3b8; font-size: 12px; }
.reload { margin-top: 4px; }
</style>
