/** 卖家备注旗帜（对齐快递助手 / 淘宝 star：0灰 1红 2黄 3绿 4蓝 5紫） */
export type SellerFlagOption = { value: number; label: string; color: string }

export const SELLER_FLAG_OPTIONS: SellerFlagOption[] = [
  { value: 0, label: '灰旗', color: '#909399' },
  { value: 1, label: '红旗', color: '#F56C6C' },
  { value: 2, label: '黄旗', color: '#E6A23C' },
  { value: 3, label: '绿旗', color: '#67C23A' },
  { value: 4, label: '蓝旗', color: '#409EFF' },
  { value: 5, label: '紫旗', color: '#A855F7' },
]

export function sellerFlagLabel(v: number | null | undefined) {
  const n = v == null ? 0 : Number(v)
  return SELLER_FLAG_OPTIONS.find((o) => o.value === n)?.label || '灰旗'
}

export function sellerFlagColor(v: number | null | undefined) {
  const n = v == null ? 0 : Number(v)
  return SELLER_FLAG_OPTIONS.find((o) => o.value === n)?.color || '#909399'
}
