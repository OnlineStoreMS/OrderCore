import { nextTick, onMounted, onUnmounted, ref, type Ref } from 'vue'

/**
 * 让 el-table 占满页面剩余高度，横/竖滚动条落在表格可视区内，
 * 避免整页拉到最底下才出现横向滚动条。
 */
export function useTableFillHeight(
  pageRef: Ref<HTMLElement | null>,
  reserveRefs: Ref<HTMLElement | null>[],
  options?: { min?: number; gap?: number },
) {
  const tableHeight = ref(Math.max(options?.min ?? 320, window.innerHeight - 280))
  const min = options?.min ?? 320
  const gap = options?.gap ?? 16

  function update() {
    const page = pageRef.value
    if (!page) return
    const pageH = page.clientHeight
    let reserved = 0
    for (const r of reserveRefs) {
      reserved += r.value?.offsetHeight ?? 0
    }
    tableHeight.value = Math.max(min, pageH - reserved - gap)
  }

  let ro: ResizeObserver | null = null

  onMounted(async () => {
    await nextTick()
    update()
    window.addEventListener('resize', update)
    if (typeof ResizeObserver !== 'undefined') {
      ro = new ResizeObserver(() => update())
      if (pageRef.value) ro.observe(pageRef.value)
      for (const r of reserveRefs) {
        if (r.value) ro.observe(r.value)
      }
    }
  })

  onUnmounted(() => {
    window.removeEventListener('resize', update)
    ro?.disconnect()
  })

  return { tableHeight, updateTableHeight: update }
}

/** Shift + 滚轮 → 横向滚动；触控板横向滑动也生效，减少中间区域误点行 */
export function bindTableShiftWheel(tableRoot: HTMLElement | null) {
  if (!tableRoot) return () => {}
  const body =
    (tableRoot.querySelector('.el-table__body-wrapper .el-scrollbar__wrap') as HTMLElement | null) ||
    (tableRoot.querySelector('.el-scrollbar__wrap') as HTMLElement | null) ||
    (tableRoot.querySelector('.el-table__body-wrapper') as HTMLElement | null)
  if (!body) return () => {}

  const onWheel = (e: WheelEvent) => {
    const max = body.scrollWidth - body.clientWidth
    if (max <= 0) return

    // 触控板横向 / Shift+滚轮
    if (Math.abs(e.deltaX) > Math.abs(e.deltaY)) {
      e.preventDefault()
      body.scrollLeft += e.deltaX
      return
    }
    if (e.shiftKey && e.deltaY) {
      e.preventDefault()
      body.scrollLeft += e.deltaY
    }
  }

  body.addEventListener('wheel', onWheel, { passive: false })
  return () => body.removeEventListener('wheel', onWheel)
}
