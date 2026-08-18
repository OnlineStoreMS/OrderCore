import type { OrderItem } from '../api/orders'

export type ItemTreeRow = {
  key: string
  item: OrderItem
  isSplitChild: boolean
  isSplitParent: boolean
  fullGroupHeader?: boolean
}

function isSplitChildItem(it: OrderItem) {
  return !!(it.splitKind || (it.parentOrderItemId && it.parentOrderItemId > 0))
}

/** 列表用：只展示原商品根行，不展示拆分关系/子行 */
export function listOrderRootItems(items: OrderItem[] | undefined): OrderItem[] {
  return (items || []).filter((it) => !isSplitChildItem(it))
}

/** 详情用：根行 + └ 拆分子行（整单拆分单独分组） */
export function buildItemTreeRows(items: OrderItem[] | undefined): ItemTreeRow[] {
  if (!items?.length) return []
  const childrenByParent = new Map<number, OrderItem[]>()
  const fullChildren: OrderItem[] = []
  const roots: OrderItem[] = []
  for (const it of items) {
    if (it.splitKind === 'full') {
      fullChildren.push(it)
      continue
    }
    if (it.splitKind === 'partial' && it.parentOrderItemId) {
      const list = childrenByParent.get(it.parentOrderItemId) || []
      list.push(it)
      childrenByParent.set(it.parentOrderItemId, list)
      continue
    }
    roots.push(it)
  }
  const out: ItemTreeRow[] = []
  for (const root of roots) {
    const kids = childrenByParent.get(root.id || 0) || []
    out.push({
      key: `root-${root.id}`,
      item: root,
      isSplitChild: false,
      isSplitParent: kids.length > 0,
    })
    for (const ch of kids) {
      out.push({
        key: `child-${ch.id}`,
        item: ch,
        isSplitChild: true,
        isSplitParent: false,
      })
    }
  }
  if (fullChildren.length) {
    out.push({
      key: 'full-header',
      item: { quantity: 0, price: 0, productName: '整单拆分' },
      isSplitChild: false,
      isSplitParent: false,
      fullGroupHeader: true,
    })
    for (const ch of fullChildren) {
      out.push({
        key: `full-${ch.id}`,
        item: ch,
        isSplitChild: true,
        isSplitParent: false,
      })
    }
  }
  return out
}

/** 列表商品标题 */
export function listItemTitle(it: OrderItem): string {
  return (it.productName || it.skuCode || '商品').trim() || '商品'
}

/** 列表规格/SKU 副行：与标题相同则隐藏 */
export function listItemMeta(it: OrderItem): { spec?: string; sku?: string } {
  const title = listItemTitle(it)
  const spec = (it.skuSpecs || '').trim()
  const sku = (it.skuCode || '').trim()
  return {
    spec: spec && spec !== title ? spec : undefined,
    sku: sku || undefined,
  }
}

/** 详情树主标题：拆分子行优先规格名 */
export function itemTreeTitle(node: ItemTreeRow): string {
  const it = node.item
  if (node.isSplitChild) {
    return (it.skuSpecs || it.productName || it.skuCode || '规格').trim() || '规格'
  }
  return (it.productName || it.skuCode || '商品').trim() || '商品'
}

/** 详情树规格/SKU 副行 */
export function itemTreeMeta(node: ItemTreeRow): { spec?: string; sku?: string } {
  const it = node.item
  const title = itemTreeTitle(node)
  const spec = (it.skuSpecs || '').trim()
  const sku = (it.skuCode || '').trim()
  return {
    spec: spec && spec !== title ? spec : undefined,
    sku: !node.isSplitChild && sku ? sku : undefined,
  }
}
