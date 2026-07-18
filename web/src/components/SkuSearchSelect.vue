<script setup lang="ts">
import { ref, watch } from 'vue'
import { api } from '../api/wms'

const props = defineProps<{ modelValue?: number | null; placeholder?: string }>()
const emit = defineEmits<{ (e: 'update:modelValue', v: number | null): void; (e: 'select', item: any): void }>()

const options = ref<any[]>([])
const loading = ref(false)
const keyword = ref('')

async function search(q: string) {
  keyword.value = q
  loading.value = true
  try {
    const res = await api.listSkus({ page: 1, pageSize: 30, keyword: q || undefined })
    options.value = res.list || []
  } finally {
    loading.value = false
  }
}

watch(
  () => props.modelValue,
  async (id) => {
    if (!id) return
    if (options.value.some((i) => i.id === id)) return
    try {
      const item = await api.getSku(id)
      if (item) options.value = [item, ...options.value.filter((i) => i.id !== item.id)]
    } catch { /* ignore */ }
  },
  { immediate: true },
)

function onChange(id: number | null) {
  emit('update:modelValue', id)
  const item = options.value.find((i: any) => i.id === id)
  if (item) emit('select', item)
}
</script>

<template>
  <el-select
    :model-value="modelValue ?? undefined"
    filterable
    remote
    clearable
    :remote-method="search"
    :loading="loading"
    :placeholder="placeholder || '搜索库存SKU'"
    style="width: 100%"
    @update:model-value="onChange"
    @focus="() => { if (!options.length) search('') }"
  >
    <el-option
      v-for="item in options"
      :key="item.id"
      :label="`${item.skuCode} ${item.pickName || ''}`"
      :value="item.id"
    />
  </el-select>
</template>
