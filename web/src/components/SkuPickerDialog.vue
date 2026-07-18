<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import { api } from '../api/wms'

const props = defineProps<{
  modelValue: boolean
  /** 已在单据中的 invSkuId，用于禁用/提示 */
  excludeIds?: number[]
  multiple?: boolean
  title?: string
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', v: boolean): void
  (e: 'confirm', items: any[]): void
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
const pageSize = ref(20)
const selectedRows = ref<any[]>([])
const tableRef = ref<any>()

const excludeSet = computed(() => new Set(props.excludeIds || []))
const multi = computed(() => props.multiple !== false)

function statusLabel(s?: string) {
  return ({ active: '在售', inactive: '停用', clearance: '清仓' } as Record<string, string>)[s || ''] || s || '-'
}

async function loadData() {
  loading.value = true
  try {
    const data = await api.listSkus({
      keyword: keyword.value.trim() || undefined,
      page: page.value,
      pageSize: pageSize.value,
    })
    list.value = data.list || []
    total.value = data.total || 0
  } catch (e) {
    list.value = []
    total.value = 0
    ElMessage.error((e as Error).message || '加载库存SKU失败')
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
    selectedRows.value = []
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

function selectable(row: any) {
  return !excludeSet.value.has(row.id)
}

function onSelectionChange(rows: any[]) {
  selectedRows.value = rows.filter((r) => selectable(r))
}

function rowClass({ row }: { row: any }) {
  return excludeSet.value.has(row.id) ? 'row-disabled' : ''
}

function confirm() {
  const rows = selectedRows.value.filter((r) => selectable(r))
  if (!rows.length) {
    ElMessage.warning('请选择库存SKU')
    return
  }
  const already = rows.filter((r) => excludeSet.value.has(r.id))
  if (already.length) {
    ElMessage.warning('部分SKU已在单据中，已自动跳过')
  }
  emit('confirm', rows.filter((r) => !excludeSet.value.has(r.id)))
  visible.value = false
}
</script>

<template>
  <el-dialog
    v-model="visible"
    :title="title || '添加商品'"
    width="960px"
    append-to-body
    destroy-on-close
    :close-on-click-modal="false"
    class="sku-picker-dialog"
  >
    <div class="toolbar">
      <el-input
        v-model="keyword"
        clearable
        placeholder="搜索库存SKU / 配货名称"
        :prefix-icon="Search"
        style="width: 320px"
        @keyup.enter="onSearch"
        @clear="onSearch"
      />
      <el-button type="primary" @click="onSearch">查询</el-button>
      <span v-if="multi && selectedRows.length" class="sel-tip">已选 {{ selectedRows.length }} 个</span>
    </div>

    <el-table
      ref="tableRef"
      v-loading="loading"
      :data="list"
      border
      stripe
      size="small"
      max-height="420"
      row-key="id"
      :row-class-name="rowClass"
      @selection-change="onSelectionChange"
    >
      <el-table-column v-if="multi" type="selection" width="48" :selectable="selectable" reserve-selection />
      <el-table-column label="图片" width="64" align="center">
        <template #default="{ row }">
          <el-image
            v-if="row.pic"
            :src="row.pic"
            fit="cover"
            style="width: 36px; height: 36px"
            :preview-src-list="[row.pic]"
            preview-teleported
          />
          <span v-else class="muted">-</span>
        </template>
      </el-table-column>
      <el-table-column prop="skuCode" label="库存SKU" width="150" show-overflow-tooltip />
      <el-table-column prop="pickName" label="配货名称" min-width="160" show-overflow-tooltip />
      <el-table-column label="商品状态" width="90">
        <template #default="{ row }">{{ statusLabel(row.status) }}</template>
      </el-table-column>
      <el-table-column prop="style1" label="款式1" width="100" show-overflow-tooltip />
      <el-table-column prop="style2" label="款式2" width="100" show-overflow-tooltip />
      <el-table-column prop="weightG" label="重量(g)" width="90" align="right" />
      <el-table-column label="上次采购价" width="110" align="right">
        <template #default="{ row }">{{ row.lastPurchasePrice != null ? Number(row.lastPurchasePrice).toFixed(2) : '-' }}</template>
      </el-table-column>
    </el-table>

    <div class="pager">
      <el-pagination
        background
        layout="total, sizes, prev, pager, next"
        :total="total"
        :page-size="pageSize"
        :current-page="page"
        :page-sizes="[20, 50, 100]"
        @current-change="onPageChange"
        @size-change="onSizeChange"
      />
    </div>

    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="primary" :disabled="!selectedRows.length" @click="confirm">确定添加</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.toolbar {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 12px;
}
.sel-tip { color: #606266; font-size: 13px; }
.pager {
  margin-top: 12px;
  display: flex;
  justify-content: flex-end;
}
.muted { color: #bbb; }
:deep(.row-disabled) {
  color: #c0c4cc;
}
</style>
