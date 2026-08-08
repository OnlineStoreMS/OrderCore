<script setup lang="ts">
import { SELLER_FLAG_OPTIONS, sellerFlagColor, sellerFlagLabel } from '../utils/sellerFlag'

const props = withDefaults(
  defineProps<{
    modelValue?: number | null
    /** view=仅展示；edit=可点选 */
    mode?: 'view' | 'edit'
    size?: number
  }>(),
  {
    modelValue: 0,
    mode: 'view',
    size: 16,
  },
)

const emit = defineEmits<{
  'update:modelValue': [number | null]
}>()

function onPick(v: number) {
  if (props.mode !== 'edit') return
  emit('update:modelValue', v)
}
</script>

<template>
  <span v-if="mode === 'view'" class="flag-view" :title="sellerFlagLabel(modelValue)">
    <svg class="flag-icon" :width="size" :height="size" viewBox="0 0 16 16" aria-hidden="true">
      <path
        d="M3 2.2h8.2c.35 0 .55.4.34.68L9.8 5.8l1.74 2.92c.2.34 0 .78-.4.78H3V2.2z"
        :fill="sellerFlagColor(modelValue)"
      />
      <path d="M3 1.5v13" stroke="#606266" stroke-width="1.4" stroke-linecap="round" fill="none" />
    </svg>
  </span>
  <span v-else class="flag-edit">
    <button
      v-for="opt in SELLER_FLAG_OPTIONS"
      :key="opt.value"
      type="button"
      class="flag-btn"
      :class="{ active: (modelValue ?? 0) === opt.value }"
      :title="opt.label"
      @click="onPick(opt.value)"
    >
      <svg class="flag-icon" :width="size" :height="size" viewBox="0 0 16 16" aria-hidden="true">
        <path
          d="M3 2.2h8.2c.35 0 .55.4.34.68L9.8 5.8l1.74 2.92c.2.34 0 .78-.4.78H3V2.2z"
          :fill="opt.color"
        />
        <path d="M3 1.5v13" stroke="#606266" stroke-width="1.4" stroke-linecap="round" fill="none" />
      </svg>
    </button>
  </span>
</template>

<style scoped>
.flag-view {
  display: inline-flex;
  align-items: center;
  vertical-align: middle;
  line-height: 1;
}
.flag-edit {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.flag-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 2px 3px;
  border: 1px solid transparent;
  border-radius: 4px;
  background: transparent;
  cursor: pointer;
  line-height: 1;
}
.flag-btn:hover { background: #f5f7fa; }
.flag-btn.active {
  border-color: #c0c4cc;
  background: #f0f2f5;
}
.flag-icon { display: block; flex-shrink: 0; }
</style>
