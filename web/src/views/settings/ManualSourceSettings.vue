<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  createManualOrderSource,
  deleteManualOrderSource,
  listManualOrderSources,
  updateManualOrderSource,
  type ManualOrderSource,
} from '../../api/manualSources'

const loading = ref(false)
const list = ref<ManualOrderSource[]>([])
const dialogVisible = ref(false)
const saving = ref(false)
const editingId = ref<number | null>(null)
const form = reactive({
  name: '',
  code: '',
  sort: 100,
  enabled: true,
  remark: '',
})

async function load() {
  loading.value = true
  try {
    list.value = await listManualOrderSources()
  } catch (e: any) {
    ElMessage.error(e.message || '加载失败')
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editingId.value = null
  Object.assign(form, { name: '', code: '', sort: 100, enabled: true, remark: '' })
  dialogVisible.value = true
}

function openEdit(row: ManualOrderSource) {
  editingId.value = row.id
  Object.assign(form, {
    name: row.name,
    code: row.code || '',
    sort: row.sort || 100,
    enabled: row.enabled !== false,
    remark: row.remark || '',
  })
  dialogVisible.value = true
}

async function submit() {
  if (!form.name.trim()) {
    ElMessage.warning('请填写来源名称')
    return
  }
  saving.value = true
  try {
    if (editingId.value) {
      await updateManualOrderSource(editingId.value, {
        name: form.name.trim(),
        code: form.code.trim(),
        sort: form.sort,
        enabled: form.enabled,
        remark: form.remark.trim(),
      })
      ElMessage.success('已保存')
    } else {
      await createManualOrderSource({
        name: form.name.trim(),
        code: form.code.trim(),
        sort: form.sort,
        enabled: form.enabled,
        remark: form.remark.trim(),
      })
      ElMessage.success('已新增')
    }
    dialogVisible.value = false
    await load()
  } catch (e: any) {
    ElMessage.error(e.message || '保存失败')
  } finally {
    saving.value = false
  }
}

async function onToggle(row: ManualOrderSource, enabled: boolean) {
  try {
    await updateManualOrderSource(row.id, { enabled })
    row.enabled = enabled
    ElMessage.success(enabled ? '已启用' : '已停用')
  } catch (e: any) {
    row.enabled = !enabled
    ElMessage.error(e.message || '更新失败')
  }
}

async function onDelete(row: ManualOrderSource) {
  try {
    await ElMessageBox.confirm(`确定删除来源「${row.name}」？不影响已建订单。`, '删除确认', {
      type: 'warning',
    })
  } catch {
    return
  }
  try {
    await deleteManualOrderSource(row.id)
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
    <el-card v-loading="loading">
      <template #header>
        <div class="hdr">
          <div>
            <div class="title">手工订单来源</div>
            <div class="hint">仅用于手工建单；可提前录入，建单时下拉选择</div>
          </div>
          <el-button type="primary" @click="openCreate">新增来源</el-button>
        </div>
      </template>

      <el-table :data="list" border stripe empty-text="暂无来源，请先新增">
        <el-table-column prop="name" label="名称" min-width="160" />
        <el-table-column prop="code" label="编码" width="140">
          <template #default="{ row }">{{ row.code || '—' }}</template>
        </el-table-column>
        <el-table-column prop="sort" label="排序" width="90" align="center" />
        <el-table-column label="状态" width="100" align="center">
          <template #default="{ row }">
            <el-switch :model-value="row.enabled" @change="(v: boolean) => onToggle(row, v)" />
          </template>
        </el-table-column>
        <el-table-column prop="remark" label="备注" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">{{ row.remark || '—' }}</template>
        </el-table-column>
        <el-table-column label="操作" width="140" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button link type="danger" @click="onDelete(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog
      v-model="dialogVisible"
      :title="editingId ? '编辑来源' : '新增来源'"
      width="480px"
      destroy-on-close
    >
      <el-form label-width="80px">
        <el-form-item label="名称" required>
          <el-input v-model="form.name" maxlength="64" placeholder="如：线下门店 / 企微私域" />
        </el-form-item>
        <el-form-item label="编码">
          <el-input v-model="form.code" maxlength="32" placeholder="可选，便于识别" />
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="form.sort" :min="1" :max="9999" />
        </el-form-item>
        <el-form-item label="启用">
          <el-switch v-model="form.enabled" />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="form.remark" type="textarea" :rows="2" maxlength="200" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="submit">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.page { padding: 0; }
.hdr { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.title { font-weight: 600; }
.hint { margin-top: 4px; font-size: 12px; color: #909399; }
</style>
