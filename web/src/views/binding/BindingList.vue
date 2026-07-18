<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  createBinding, deleteBinding, listBindings, listFactories, updateBinding,
  type FactoryItem, type SupplierBinding,
} from '../../api/orders'

const loading = ref(false)
const list = ref<SupplierBinding[]>([])
const factories = ref<FactoryItem[]>([])
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
  } catch {
    factories.value = []
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
  loadFactories()
}

function onFactoryChange(fid: string) {
  const f = factories.value.find((x) => x.factoryId === fid)
  if (f) form.externalFactoryName = f.factoryName
}

async function submit() {
  if (!form.supplierId || !form.supplierName || !form.externalFactoryId) {
    ElMessage.warning('请填写供应商与厂家')
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
        <p>在 SupplyCore 创建供应商后，在此绑定 StoreSyncAgent 厂家，便于订单中心标准化管理代发。</p>
      </div>
      <el-button type="primary" @click="openCreate">新建绑定</el-button>
    </div>

    <el-table v-loading="loading" :data="list" stripe>
      <el-table-column prop="supplierId" label="供应商ID" width="100" />
      <el-table-column prop="supplierName" label="供应商" min-width="160" />
      <el-table-column prop="supplierCode" label="编码" width="120" />
      <el-table-column prop="externalFactoryId" label="厂家ID" width="140" />
      <el-table-column prop="externalFactoryName" label="厂家名称" min-width="160" />
      <el-table-column prop="platform" label="平台" width="90" />
      <el-table-column prop="remark" label="备注" min-width="140" show-overflow-tooltip />
      <el-table-column label="操作" width="140" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
          <el-button link type="danger" @click="onDelete(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialogVisible" :title="editingId ? '编辑绑定' : '新建绑定'" width="520px">
      <el-form label-width="110px">
        <el-form-item label="供应商ID" required>
          <el-input-number v-model="form.supplierId" :min="1" style="width: 100%" />
        </el-form-item>
        <el-form-item label="供应商名称" required>
          <el-input v-model="form.supplierName" placeholder="与 SupplyCore 供应商名称一致" />
        </el-form-item>
        <el-form-item label="供应商编码">
          <el-input v-model="form.supplierCode" />
        </el-form-item>
        <el-form-item label="平台">
          <el-input v-model="form.platform" placeholder="FXG / TB ..." />
        </el-form-item>
        <el-form-item label="厂家" required>
          <el-select
            v-model="form.externalFactoryId"
            filterable
            allow-create
            style="width: 100%"
            @change="onFactoryChange"
            @focus="loadFactories"
          >
            <el-option
              v-for="f in factories"
              :key="f.factoryId"
              :label="`${f.factoryName} (${f.factoryId})`"
              :value="f.factoryId"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="厂家名称">
          <el-input v-model="form.externalFactoryName" />
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
.toolbar p { margin: 0; color: #64748b; font-size: 13px; max-width: 640px; line-height: 1.5; }
</style>
