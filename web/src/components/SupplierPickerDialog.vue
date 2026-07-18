<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import { api } from '../api/wms'

const props = defineProps<{
  modelValue: boolean
  excludeIds?: number[]
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'select', item: any): void
}>()

const visible = computed({
  get: () => props.modelValue,
  set: (v) => emit('update:modelValue', v),
})

const keyword = ref('')
const loading = ref(false)
const list = ref<any[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(10)
const selected = ref<any | null>(null)

const excludeSet = computed(() => new Set(props.excludeIds || []))

async function loadData() {
  loading.value = true
  selected.value = null
  try {
    const data = await api.listSuppliers({
      keyword: keyword.value.trim() || undefined,
      page: page.value,
      pageSize: pageSize.value,
    })
    list.value = data.list || []
    total.value = data.total || 0
  } catch (e) {
    list.value = []
    total.value = 0
    ElMessage.error((e as Error).message || '加载供应商失败')
  } finally {
    loading.value = false
  }
}

watch(
  () => props.modelValue,
  (open) => {
    if (!open) return
    keyword.value = ''
    page.value = 1
    selected.value = null
    loadData()
  },
)

function onSearch() {
  page.value = 1
  loadData()
}

function onPageChange(p: number) {
  page.value = p
  loadData()
}

function onSizeChange(size: number) {
  pageSize.value = size
  page.value = 1
  loadData()
}

function rowClass({ row }: { row: any }) {
  if (excludeSet.value.has(row.id)) return 'row-disabled'
  if (selected.value?.id === row.id) return 'row-selected'
  return ''
}

function onRowClick(row: any) {
  if (excludeSet.value.has(row.id)) {
    ElMessage.warning('该供应商已添加')
    return
  }
  selected.value = row
}

function confirm() {
  if (!selected.value) {
    ElMessage.warning('请选择供应商')
    return
  }
  if (excludeSet.value.has(selected.value.id)) {
    ElMessage.warning('该供应商已添加')
    return
  }
  emit('select', selected.value)
  visible.value = false
}

function onRowDblClick(row: any) {
  onRowClick(row)
  if (!excludeSet.value.has(row.id)) confirm()
}
</script>

<template>
  <el-dialog
    v-model="visible"
    title="选择供应商"
    width="720px"
    append-to-body
    destroy-on-close
    :close-on-click-modal="false"
  >
    <div class="toolbar">
      <el-input
        v-model="keyword"
        clearable
        placeholder="搜索供应商名称 / 编码"
        :prefix-icon="Search"
        style="width: 280px"
        @keyup.enter="onSearch"
        @clear="onSearch"
      />
      <el-button type="primary" @click="onSearch">查询</el-button>
    </div>

    <el-table
      v-loading="loading"
      :data="list"
      border
      size="small"
      highlight-current-row
      :row-class-name="rowClass"
      max-height="360"
      @row-click="onRowClick"
      @row-dblclick="onRowDblClick"
    >
      <el-table-column width="48" align="center">
        <template #default="{ row }">
          <el-radio
            :model-value="selected?.id"
            :label="row.id"
            :disabled="excludeSet.has(row.id)"
            @change="onRowClick(row)"
          >
            &nbsp;
          </el-radio>
        </template>
      </el-table-column>
      <el-table-column prop="code" label="编码" width="120" />
      <el-table-column prop="name" label="供应商名称" min-width="180" />
      <el-table-column prop="shortName" label="简称" width="120" />
      <el-table-column prop="contactName" label="联系人" width="100" />
      <el-table-column prop="phone" label="电话" width="130" />
    </el-table>

    <div class="pager">
      <el-pagination
        background
        layout="total, sizes, prev, pager, next"
        :total="total"
        :page-size="pageSize"
        :current-page="page"
        :page-sizes="[10, 20, 50]"
        @current-change="onPageChange"
        @size-change="onSizeChange"
      />
    </div>

    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="primary" :disabled="!selected" @click="confirm">确定</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.toolbar {
  display: flex;
  gap: 8px;
  margin-bottom: 12px;
}
.pager {
  margin-top: 12px;
  display: flex;
  justify-content: flex-end;
}
:deep(.row-disabled) {
  color: #c0c4cc;
  cursor: not-allowed;
}
:deep(.row-selected) {
  --el-table-tr-bg-color: var(--el-color-primary-light-9);
}
</style>
