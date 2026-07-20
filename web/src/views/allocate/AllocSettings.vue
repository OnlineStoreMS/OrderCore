<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { listSuppliers, type SupplierItem } from '../../api/orders'
import {
  createSkuSupplierRule,
  deleteSkuSupplierRule,
  getAllocSettings,
  listSkuSupplierRules,
  updateAllocSettings,
  updateSkuSupplierRule,
  type SkuSupplierRule,
} from '../../api/allocateSettings'

const loading = ref(false)
const savingSettings = ref(false)
const list = ref<SkuSupplierRule[]>([])
const suppliers = ref<SupplierItem[]>([])
const enabled = ref(false)
const keyword = ref('')
const dialogVisible = ref(false)
const editingId = ref<number | null>(null)
const form = reactive({
  skuCode: '',
  supplierId: undefined as number | undefined,
  supplierCode: '',
  supplierName: '',
  priority: 100,
  status: 1 as number,
  remark: '',
})

async function loadSettings() {
  try {
    const cfg = await getAllocSettings()
    enabled.value = !!cfg.enabled
  } catch (e: any) {
    ElMessage.error(e.message || '加载分配设置失败')
  }
}

async function loadRules() {
  loading.value = true
  try {
    list.value = await listSkuSupplierRules(keyword.value.trim() || undefined)
  } catch (e: any) {
    ElMessage.error(e.message || '加载 SKU 绑定失败')
  } finally {
    loading.value = false
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

async function onToggleEnabled(val: boolean) {
  savingSettings.value = true
  try {
    await updateAllocSettings({ enabled: val, strategy: 'bind_dropship_only' })
    enabled.value = val
    ElMessage.success(val ? '已开启自动分配' : '已关闭自动分配')
  } catch (e: any) {
    enabled.value = !val
    ElMessage.error(e.message || '保存失败')
  } finally {
    savingSettings.value = false
  }
}

function openCreate() {
  editingId.value = null
  Object.assign(form, {
    skuCode: '',
    supplierId: undefined,
    supplierCode: '',
    supplierName: '',
    priority: 100,
    status: 1,
    remark: '',
  })
  dialogVisible.value = true
  loadSuppliers()
}

function openEdit(row: SkuSupplierRule) {
  editingId.value = row.id
  Object.assign(form, {
    skuCode: row.skuCode,
    supplierId: row.supplierId,
    supplierCode: row.supplierCode || '',
    supplierName: row.supplierName,
    priority: row.priority || 100,
    status: row.status,
    remark: row.remark || '',
  })
  dialogVisible.value = true
  loadSuppliers()
}

function onSupplierChange(sid: number) {
  const s = suppliers.value.find((x) => x.id === sid)
  if (!s) return
  form.supplierName = s.name
  form.supplierCode = s.code || ''
}

async function submit() {
  if (!form.skuCode.trim() || !form.supplierId || !form.supplierName) {
    ElMessage.warning('请填写 SKU 编码并选择供应商')
    return
  }
  const payload = {
    skuCode: form.skuCode.trim(),
    supplierId: form.supplierId,
    supplierCode: form.supplierCode,
    supplierName: form.supplierName,
    priority: form.priority,
    status: form.status,
    remark: form.remark,
  }
  try {
    if (editingId.value) {
      await updateSkuSupplierRule(editingId.value, payload)
      ElMessage.success('已更新')
    } else {
      await createSkuSupplierRule(payload)
      ElMessage.success('已创建')
    }
    dialogVisible.value = false
    await loadRules()
  } catch (e: any) {
    ElMessage.error(e.message || '保存失败')
  }
}

async function onDelete(row: SkuSupplierRule) {
  await ElMessageBox.confirm(`确认删除 SKU「${row.skuCode}」与供应商「${row.supplierName}」的绑定？`, '删除确认')
  try {
    await deleteSkuSupplierRule(row.id)
    ElMessage.success('已删除')
    await loadRules()
  } catch (e: any) {
    ElMessage.error(e.message || '删除失败')
  }
}

onMounted(async () => {
  await Promise.all([loadSettings(), loadRules()])
})
</script>

<template>
  <div class="page">
    <div class="toolbar">
      <div>
        <h3>分配设置</h3>
        <p>
          <strong>记忆模式</strong>：在订单上手动代发并选择供应商后，系统会自动记住该订单各 SKU → 供应商；
          可在下方查找并修改。开启自动分配后，后续相同 SKU 的待分配订单会按记忆自动代发；
          多 SKU 对应不同供应商时不自动分配。
        </p>
      </div>
    </div>

    <el-card shadow="never" class="settings-card">
      <div class="settings-row">
        <div>
          <div class="settings-title">自动分配</div>
          <div class="muted">按已记忆/已维护的 SKU 绑定自动代发；无绑定保持待分配</div>
        </div>
        <el-switch
          :model-value="enabled"
          :loading="savingSettings"
          inline-prompt
          active-text="开"
          inactive-text="关"
          @change="onToggleEnabled"
        />
      </div>
    </el-card>

    <div class="section-head">
      <h4>SKU → 供应商绑定</h4>
      <div class="section-actions">
        <el-input
          v-model="keyword"
          clearable
          placeholder="搜索 SKU / 供应商 / 备注"
          style="width: 240px"
          @keyup.enter="loadRules"
          @clear="loadRules"
        />
        <el-button @click="loadRules">查询</el-button>
        <el-button type="primary" @click="openCreate">新建绑定</el-button>
      </div>
    </div>

    <el-table v-loading="loading" :data="list" stripe>
      <el-table-column prop="skuCode" label="SKU 编码" min-width="160" />
      <el-table-column prop="supplierName" label="OSMS 供应商" min-width="180">
        <template #default="{ row }">
          <div>{{ row.supplierName }}</div>
          <div class="muted">{{ row.supplierCode || `ID ${row.supplierId}` }}</div>
        </template>
      </el-table-column>
      <el-table-column prop="priority" label="优先级" width="90" />
      <el-table-column prop="status" label="状态" width="90">
        <template #default="{ row }">
          <el-tag :type="row.status === 1 ? 'success' : 'info'" size="small">
            {{ row.status === 1 ? '启用' : '停用' }}
          </el-tag>
        </template>
      </el-table-column>
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
        <el-form-item label="SKU 编码" required>
          <el-input v-model="form.skuCode" placeholder="与订单行 skuCode / 快递助手 outerId 一致" />
        </el-form-item>
        <el-form-item label="OSMS 供应商" required>
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
        </el-form-item>
        <el-form-item label="优先级">
          <el-input-number v-model="form.priority" :min="1" :max="9999" />
        </el-form-item>
        <el-form-item label="状态">
          <el-radio-group v-model="form.status">
            <el-radio :value="1">启用</el-radio>
            <el-radio :value="0">停用</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.remark" type="textarea" :rows="2" />
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
.page { padding: 4px 0 24px; }
.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
  margin-bottom: 16px;
}
.toolbar h3 { margin: 0 0 6px; font-size: 18px; }
.toolbar p { margin: 0; color: #666; font-size: 13px; line-height: 1.5; max-width: 720px; }
.settings-card { margin-bottom: 20px; }
.settings-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
}
.settings-title { font-weight: 600; margin-bottom: 4px; }
.section-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
  gap: 12px;
  flex-wrap: wrap;
}
.section-head h4 { margin: 0; font-size: 15px; }
.section-actions { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.muted { color: #999; font-size: 12px; }
</style>
