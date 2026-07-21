<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import type { DateModelType } from 'element-plus'
import * as echarts from 'echarts/core'
import { LineChart, BarChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, LegendComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import { fetchDashboard, labelStatus, orderTypeOptions, syncKDZS, syncStore } from '../api/orders'

echarts.use([LineChart, BarChart, GridComponent, TooltipComponent, LegendComponent, CanvasRenderer])

interface DashCards {
  pendingAlloc?: number
  waitShipEcommerce?: number
  allocated?: number
  purchasing?: number
  shipped?: number
  todayOrders?: number
  todayAmount?: number
  weekOrders?: number
  weekAmount?: number
  monthOrders?: number
  monthAmount?: number
  rangeOrders?: number
  rangeAmount?: number
  rangeSelfAmount?: number
  rangeDropshipAmount?: number
  rangeStart?: string
  rangeEnd?: string
}

interface TrendPoint {
  date: string
  orderCount: number
  amount: number
  selfAmount?: number
  dropshipAmount?: number
}

function pad(n: number) {
  return String(n).padStart(2, '0')
}
function fmtDate(d: Date) {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}
function startOfDay(d = new Date()) {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate())
}
function defaultRange(): [string, string] {
  const end = startOfDay()
  const start = new Date(end)
  start.setDate(start.getDate() - 6)
  return [fmtDate(start), fmtDate(end)]
}

const router = useRouter()
const loading = ref(false)
const syncing = ref('')
const cards = ref<DashCards>({})
const byStatus = ref<Record<string, number>>({})
const bySource = ref<Record<string, number>>({})
const trend = ref<TrendPoint[]>([])
const dateRange = ref<[DateModelType, DateModelType] | null>(defaultRange())

const orderChartEl = ref<HTMLDivElement | null>(null)
const channelChartEl = ref<HTMLDivElement | null>(null)
let orderChart: echarts.ECharts | null = null
let channelChart: echarts.ECharts | null = null

const rangeLabel = computed(() => {
  const s = cards.value.rangeStart
  const e = cards.value.rangeEnd
  if (!s || !e) return ''
  return s === e ? s : `${s} ~ ${e}`
})

const workCards = computed(() => [
  {
    key: 'pendingAlloc',
    label: '待分配',
    tip: '履约待分配',
    value: cards.value.pendingAlloc || 0,
    color: '#e6a23c',
    go: () => router.push({ path: '/allocate', query: { status: 'pending_alloc' } }),
  },
  {
    key: 'waitShip',
    label: '待发货',
    tip: '发货状态待发货',
    value: cards.value.waitShipEcommerce || 0,
    color: '#409eff',
    go: () => router.push({ path: '/orders', query: { shipStatus: 'wait_ship' } }),
  },
  {
    key: 'allocated',
    label: '已分配',
    tip: '含厂家代发锁定',
    value: cards.value.allocated || 0,
    color: '#67c23a',
    go: () => router.push({ path: '/allocate', query: { status: 'allocated' } }),
  },
  {
    key: 'purchasing',
    label: '采购中',
    tip: '采购发货进行中',
    value: cards.value.purchasing || 0,
    color: '#909399',
    go: () => router.push({ path: '/allocate', query: { status: 'purchasing' } }),
  },
  {
    key: 'shipped',
    label: '已发货',
    tip: '发货状态已发货',
    value: cards.value.shipped || 0,
    color: '#0f766e',
    go: () => router.push({ path: '/orders', query: { shipStatus: 'shipped' } }),
  },
])

const typeCards = computed(() =>
  orderTypeOptions.map((t, i) => ({
    key: t.value,
    label: t.label,
    tip: t.tip,
    count: bySource.value[t.value] || 0,
    color: ['#1677ff', '#722ed1', '#13c2c2', '#fa8c16', '#595959'][i] || '#1677ff',
  })),
)

const metricCards = computed(() => [
  { label: '今日订单', value: cards.value.todayOrders || 0, sub: `¥${fmtMoney(cards.value.todayAmount)}` },
  { label: '近7日订单', value: cards.value.weekOrders || 0, sub: `¥${fmtMoney(cards.value.weekAmount)}` },
  { label: '本月订单', value: cards.value.monthOrders || 0, sub: `¥${fmtMoney(cards.value.monthAmount)}` },
])

const rangeSummaryCards = computed(() => [
  {
    label: '区间订单量',
    tip: rangeLabel.value || '当前筛选',
    value: String(cards.value.rangeOrders || 0),
    color: '#1677ff',
  },
  {
    label: '区间销售额',
    tip: '排除关闭/退款',
    value: `¥${fmtMoney(cards.value.rangeAmount)}`,
    color: '#13c2c2',
  },
  {
    label: '自营销售额',
    tip: '自营发货 / 未推厂家',
    value: `¥${fmtMoney(cards.value.rangeSelfAmount)}`,
    color: '#1677ff',
  },
  {
    label: '代发销售额',
    tip: '厂家代发 / OSMS 代发',
    value: `¥${fmtMoney(cards.value.rangeDropshipAmount)}`,
    color: '#722ed1',
  },
])

const pickerShortcuts = [
  {
    text: '今天',
    value: () => {
      const d = startOfDay()
      return [d, d] as [Date, Date]
    },
  },
  {
    text: '昨天',
    value: () => {
      const d = startOfDay()
      d.setDate(d.getDate() - 1)
      return [d, d] as [Date, Date]
    },
  },
  {
    text: '最近7天',
    value: () => {
      const end = startOfDay()
      const start = new Date(end)
      start.setDate(start.getDate() - 6)
      return [start, end] as [Date, Date]
    },
  },
  {
    text: '最近14天',
    value: () => {
      const end = startOfDay()
      const start = new Date(end)
      start.setDate(start.getDate() - 13)
      return [start, end] as [Date, Date]
    },
  },
  {
    text: '最近30天',
    value: () => {
      const end = startOfDay()
      const start = new Date(end)
      start.setDate(start.getDate() - 29)
      return [start, end] as [Date, Date]
    },
  },
  {
    text: '本月',
    value: () => {
      const end = startOfDay()
      const start = new Date(end.getFullYear(), end.getMonth(), 1)
      return [start, end] as [Date, Date]
    },
  },
]

function fmtMoney(v?: number) {
  const n = Number(v || 0)
  return n.toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

function axisDates() {
  return trend.value.map((t) => (t.date.length >= 10 ? t.date.slice(5) : t.date))
}

function moneyAxisLabel(v: number) {
  return v >= 10000 ? `${(v / 10000).toFixed(1)}万` : String(v)
}

async function load() {
  if (!dateRange.value || dateRange.value.length !== 2) {
    ElMessage.warning('请选择时间范围')
    return
  }
  const [startDate, endDate] = dateRange.value.map(String)
  loading.value = true
  try {
    const data = await fetchDashboard({ startDate, endDate }) as {
      cards?: DashCards
      byStatus?: Record<string, number>
      bySource?: Record<string, number>
      trend?: TrendPoint[]
    }
    cards.value = data.cards || {}
    byStatus.value = data.byStatus || {}
    bySource.value = data.bySource || {}
    trend.value = data.trend || []
    await nextTick()
    renderCharts()
  } catch (e: any) {
    ElMessage.error(e.message || '加载失败')
  } finally {
    loading.value = false
  }
}

function renderCharts() {
  const dates = axisDates()
  const counts = trend.value.map((t) => t.orderCount)
  const amounts = trend.value.map((t) => Number(t.amount || 0))
  const selfAmts = trend.value.map((t) => Number(t.selfAmount || 0))
  const dropAmts = trend.value.map((t) => Number(t.dropshipAmount || 0))
  const moneyFmt = (v: number) => `¥${fmtMoney(v)}`

  if (orderChartEl.value) {
    if (!orderChart) orderChart = echarts.init(orderChartEl.value)
    orderChart.setOption({
      color: ['#1677ff', '#13c2c2'],
      legend: { data: ['订单量', '销售额'], top: 0 },
      tooltip: { trigger: 'axis', axisPointer: { type: 'cross' } },
      grid: { left: 48, right: 56, top: 40, bottom: 28 },
      xAxis: { type: 'category', data: dates, boundaryGap: true },
      yAxis: [
        { type: 'value', name: '单', minInterval: 1 },
        {
          type: 'value',
          name: '元',
          splitLine: { show: false },
          axisLabel: { formatter: moneyAxisLabel },
        },
      ],
      series: [
        {
          name: '订单量',
          type: 'line',
          smooth: true,
          yAxisIndex: 0,
          areaStyle: { opacity: 0.08 },
          data: counts,
        },
        {
          name: '销售额',
          type: 'bar',
          yAxisIndex: 1,
          barMaxWidth: 28,
          data: amounts,
          tooltip: { valueFormatter: moneyFmt },
        },
      ],
    }, true)
  }

  if (channelChartEl.value) {
    if (!channelChart) channelChart = echarts.init(channelChartEl.value)
    channelChart.setOption({
      color: ['#1677ff', '#722ed1'],
      legend: { data: ['自营销售额', '代发销售额'], top: 0 },
      tooltip: { trigger: 'axis' },
      grid: { left: 48, right: 24, top: 40, bottom: 28 },
      xAxis: { type: 'category', data: dates },
      yAxis: {
        type: 'value',
        name: '元',
        axisLabel: { formatter: moneyAxisLabel },
      },
      series: [
        {
          name: '自营销售额',
          type: 'bar',
          stack: 'channel',
          barMaxWidth: 28,
          data: selfAmts,
          tooltip: { valueFormatter: moneyFmt },
        },
        {
          name: '代发销售额',
          type: 'bar',
          stack: 'channel',
          barMaxWidth: 28,
          data: dropAmts,
          tooltip: { valueFormatter: moneyFmt },
        },
      ],
    }, true)
  }
}

function onResize() {
  orderChart?.resize()
  channelChart?.resize()
}

async function doSyncKDZS() {
  syncing.value = 'kdzs'
  try {
    const stats = await syncKDZS({ pageSize: 50, tradeStatuses: ['all'] }) as Record<string, number>
    ElMessage.success(`电商同步完成（全平台）：合计 ${stats.total || 0}，拉取 ${stats.fetched || 0}，新增 ${stats.created || 0}，更新 ${stats.updated || 0}`)
    await load()
  } catch (e: any) {
    ElMessage.error(e.message || '同步失败')
  } finally {
    syncing.value = ''
  }
}

async function doSyncStore() {
  syncing.value = 'store'
  try {
    const stats = await syncStore({ pageSize: 50 }) as Record<string, number>
    ElMessage.success(`门店同步完成：新增 ${stats.created || 0}，更新 ${stats.updated || 0}`)
    await load()
  } catch (e: any) {
    ElMessage.error(e.message || '同步失败')
  } finally {
    syncing.value = ''
  }
}

watch(trend, () => nextTick().then(renderCharts))

onMounted(() => {
  load()
  window.addEventListener('resize', onResize)
})
onUnmounted(() => {
  window.removeEventListener('resize', onResize)
  orderChart?.dispose()
  channelChart?.dispose()
})
</script>

<template>
  <div v-loading="loading" class="page">
    <div class="toolbar">
      <el-button type="primary" :loading="syncing === 'kdzs'" @click="doSyncKDZS">同步电商订单</el-button>
      <el-button :loading="syncing === 'store'" @click="doSyncStore">同步门店订单</el-button>
      <el-button @click="router.push('/orders')">订单列表</el-button>
    </div>

    <div class="section-head">订单类型</div>
    <div class="type-cards">
      <button
        v-for="t in typeCards"
        :key="t.key"
        type="button"
        class="type-card"
        :style="{ '--accent': t.color }"
        @click="router.push({ path: '/orders', query: { sourceChannel: t.key } })"
      >
        <div class="type-label">{{ t.label }}</div>
        <div class="type-value">{{ t.count }}</div>
        <div v-if="t.tip" class="type-tip">{{ t.tip }}</div>
      </button>
    </div>

    <div class="section-head">待办</div>
    <div class="work-cards">
      <button
        v-for="c in workCards"
        :key="c.key"
        type="button"
        class="work-card"
        :style="{ '--accent': c.color }"
        @click="c.go()"
      >
        <div class="work-label">{{ c.label }}</div>
        <div class="work-value">{{ c.value }}</div>
        <div class="work-tip">{{ c.tip }}</div>
      </button>
    </div>

    <div class="metric-row">
      <div v-for="m in metricCards" :key="m.label" class="metric-card">
        <div class="metric-label">{{ m.label }}</div>
        <div class="metric-value">{{ m.value }}</div>
        <div class="metric-sub">实际成交额 {{ m.sub }}</div>
      </div>
    </div>

    <div class="section-head row-between">
      <span>趋势分析</span>
      <div class="range-tools">
        <el-date-picker
          v-model="dateRange"
          type="daterange"
          unlink-panels
          range-separator="至"
          start-placeholder="开始日期"
          end-placeholder="结束日期"
          value-format="YYYY-MM-DD"
          :shortcuts="pickerShortcuts"
          :clearable="false"
          @change="load"
        />
      </div>
    </div>

    <div class="channel-sales-row">
      <div
        v-for="m in rangeSummaryCards"
        :key="m.label"
        class="channel-sales-card"
        :style="{ '--accent': m.color }"
      >
        <div class="metric-label">{{ m.label }}</div>
        <div class="channel-sales-value">{{ m.value }}</div>
        <div class="metric-sub">{{ m.tip }}</div>
      </div>
    </div>

    <div class="charts">
      <section>
        <h3>订单量 / 销售额</h3>
        <p class="chart-tip">按下单日；排除关闭与退款完成；销售额优先实付</p>
        <div ref="orderChartEl" class="chart" />
      </section>
      <section>
        <h3>自营 / 代发销售额</h3>
        <p class="chart-tip">代发=厂家代发或 OSMS 供应商代发；其余计自营</p>
        <div ref="channelChartEl" class="chart" />
      </section>
    </div>

    <section class="status-panel">
      <h3>履约状态分布</h3>
      <div class="chips">
        <div
          v-for="(cnt, key) in byStatus"
          :key="key"
          class="chip"
          @click="router.push({ path: '/orders', query: { status: String(key) } })"
        >
          <span>{{ labelStatus(String(key)) }}</span>
          <strong>{{ cnt }}</strong>
        </div>
        <div v-if="!Object.keys(byStatus).length" class="empty">暂无订单</div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: 14px; }
.toolbar { display: flex; gap: 8px; flex-wrap: wrap; }
.section-head { font-size: 13px; font-weight: 600; color: #64748b; margin-top: 2px; }
.row-between { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
.range-tools { display: flex; align-items: center; gap: 8px; }

.type-cards, .work-cards {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 12px;
}
.type-card, .work-card {
  text-align: left;
  border: 1px solid #e8edf3;
  background: #fff;
  border-radius: 10px;
  padding: 14px 16px;
  cursor: pointer;
  border-top: 3px solid var(--accent, #1677ff);
  transition: box-shadow .15s, border-color .15s;
}
.type-card:hover, .work-card:hover { box-shadow: 0 4px 14px rgba(15, 39, 68, 0.08); }
.type-label, .work-label { font-size: 13px; color: #64748b; }
.type-value, .work-value { margin-top: 6px; font-size: 28px; font-weight: 700; color: #0f172a; line-height: 1.1; }
.type-tip, .work-tip { margin-top: 6px; font-size: 12px; color: #94a3b8; }

.metric-row {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}
.metric-card {
  background: #fff;
  border: 1px solid #eef0f3;
  border-radius: 10px;
  padding: 14px 16px;
}
.metric-label { font-size: 13px; color: #64748b; }
.metric-value { margin-top: 4px; font-size: 24px; font-weight: 700; color: #0f172a; }
.metric-sub { margin-top: 4px; font-size: 13px; color: #0f766e; }

.channel-sales-row {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}
.channel-sales-card {
  background: #fff;
  border: 1px solid #eef0f3;
  border-radius: 10px;
  padding: 14px 16px;
  border-top: 3px solid var(--accent, #1677ff);
}
.channel-sales-value {
  margin-top: 6px;
  font-size: 22px;
  font-weight: 700;
  color: #0f172a;
  line-height: 1.15;
}

.charts {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
.charts section, .status-panel {
  background: #fff;
  border-radius: 10px;
  padding: 16px 18px;
  border: 1px solid #eef0f3;
}
.chart-tip { margin: -4px 0 8px; font-size: 12px; color: #94a3b8; }
.chart { width: 100%; height: 300px; }
h3 { margin: 0 0 10px; font-size: 15px; color: #334155; }
.chips { display: flex; flex-wrap: wrap; gap: 10px; }
.chip {
  min-width: 120px; padding: 12px 14px; border-radius: 8px; background: #f8fafc;
  border: 1px solid #e2e8f0; cursor: pointer; display: flex; justify-content: space-between; gap: 12px;
}
.chip:hover { border-color: #93c5fd; background: #eff6ff; }
.chip strong { color: #0f172a; }
.empty { color: #94a3b8; font-size: 13px; }

@media (max-width: 1100px) {
  .type-cards, .work-cards { grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .metric-row, .channel-sales-row, .charts { grid-template-columns: 1fr; }
}
@media (max-width: 700px) {
  .type-cards, .work-cards { grid-template-columns: 1fr 1fr; }
}
</style>
