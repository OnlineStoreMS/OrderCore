<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { pcaTextArr } from 'element-china-area-data'
import {
  createManualOrder,
  createManualOrdersBatch,
  parseManualAddress,
  searchManualRecipients,
  searchPIMProducts,
  searchShopProducts,
  type ParsedAddress,
  type RecipientSearchItem,
} from '../../api/manualOrder'
import { getToken, getRefreshToken } from '../../utils/auth'
import { getShippingCoreUrl } from '../../utils/runtimeConfig'
import SellerFlag from '../../components/SellerFlag.vue'

type AreaNode = { label: string; value: string; children?: AreaNode[] }
const areaOptions = pcaTextArr as AreaNode[]

type CreateAction = 'create_only' | 'create_and_push' | 'create_and_print'
type PrintMode = 'kdzs' | 'carrier'

const props = defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{
  'update:modelValue': [boolean]
  /** skipNavigate：创建并打印已跳转发货中心，父级勿再进订单详情 */
  created: [payload?: { orderId?: number; action?: CreateAction; skipNavigate?: boolean }]
}>()

const visible = computed({
  get: () => props.modelValue,
  set: (v: boolean) => emit('update:modelValue', v),
})

type Mode = 'single' | 'batch'
const mode = ref<Mode>('single')
const submitting = ref(false)
const filling = ref(false)

const rawAddress = ref('')
/** 输=整段手输；选=省市区下拉 + 详细地址（对齐快递助手） */
const addrMode = ref<'type' | 'select'>('type')
const typedAddress = ref('')
const form = reactive({
  buyerName: '',
  buyerPhone: '',
  buyerTel: '',
  province: '',
  city: '',
  district: '',
  detail: '',
  remark: '',
  shipContent: '',
  sellerFlag: 0 as number,
  saveCustomer: true,
  syncKdzs: true,
  platformOrderNo: '',
})

const provinceOptions = computed(() => areaOptions)
const cityOptions = computed(() => {
  const p = areaOptions.find((x) => x.value === form.province || x.label === form.province)
  return p?.children || []
})
const districtOptions = computed(() => {
  const c = cityOptions.value.find((x) => x.value === form.city || x.label === form.city)
  return c?.children || []
})

type ItemRow = {
  productName: string
  /** 商家编码 */
  outerId: string
  /** 规格编码 */
  skuCode: string
  /** 规格名称 */
  skuSpecs: string
  skuId?: number
  platformItemId?: string
  platformSkuId?: string
  picUrl?: string
  quantity: number
  /** 单价 */
  price: number
  source?: 'pim' | 'shop' | 'manual'
}
const emptyItem = (): ItemRow => ({
  productName: '',
  outerId: '',
  skuCode: '',
  skuSpecs: '',
  quantity: 1,
  price: 0,
  source: 'manual',
})
const items = ref<ItemRow[]>([])

type BatchReceiver = {
  buyerName: string
  buyerPhone: string
  buyerTel: string
  province: string
  city: string
  district: string
  detail: string
}
const batchReceivers = ref<BatchReceiver[]>([])

const productDialog = ref(false)
const productSource = ref<'pim' | 'shop'>('pim')
const productKeyword = ref('')
const productLoading = ref(false)
const pimList = ref<any[]>([])
const shopList = ref<any[]>([])
const shopPlatform = ref('FXG')

const recipientDialog = ref(false)
const recipientKeyword = ref('')
const recipientLoading = ref(false)
const recipientList = ref<RecipientSearchItem[]>([])
const recipientTotal = ref(0)
const recipientPage = ref(1)
const selectedRecipientKey = ref('')

watch(visible, (v) => {
  if (v) reset()
})

function reset() {
  mode.value = 'single'
  rawAddress.value = ''
  addrMode.value = 'type'
  typedAddress.value = ''
  Object.assign(form, {
    buyerName: '',
    buyerPhone: '',
    buyerTel: '',
    province: '',
    city: '',
    district: '',
    detail: '',
    remark: '',
    shipContent: '',
    sellerFlag: 0,
    saveCustomer: true,
    syncKdzs: true,
    platformOrderNo: '',
  })
  items.value = []
  batchReceivers.value = []
  selectedRecipientKey.value = ''
}

function composeTypedAddress() {
  return `${form.province || ''}${form.city || ''}${form.district || ''}${form.detail || ''}`
}

function syncTypedFromForm() {
  typedAddress.value = composeTypedAddress()
}

/** 将手输整段地址尽量拆成省市区+详细（本地匹配行政区划） */
function splitTypedAddress(text: string) {
  const raw = (text || '').trim()
  if (!raw) {
    form.province = ''
    form.city = ''
    form.district = ''
    form.detail = ''
    return
  }
  for (const p of areaOptions) {
    const pNames = uniqueNames(p.label)
    for (const pn of pNames) {
      if (!raw.startsWith(pn)) continue
      let rest = raw.slice(pn.length)
      const cities = p.children || []
      for (const c of cities) {
        const cNames = uniqueNames(c.label)
        for (const cn of cNames) {
          if (!rest.startsWith(cn)) continue
          rest = rest.slice(cn.length)
          const districts = c.children || []
          for (const d of districts) {
            const dNames = uniqueNames(d.label)
            for (const dn of dNames) {
              if (!rest.startsWith(dn)) continue
              form.province = p.label
              form.city = c.label
              form.district = d.label
              form.detail = rest.slice(dn.length).trim()
              return
            }
          }
          form.province = p.label
          form.city = c.label
          form.district = ''
          form.detail = rest.trim()
          return
        }
      }
      form.province = p.label
      form.city = ''
      form.district = ''
      form.detail = rest.trim()
      return
    }
  }
  form.province = ''
  form.city = ''
  form.district = ''
  form.detail = raw
}

function uniqueNames(label: string) {
  const names = [label]
  // 兼容「北京」/「北京市」等简写前缀匹配（长的优先）
  if (label.endsWith('省') || label.endsWith('市') || label.endsWith('区') || label.endsWith('县')) {
    names.push(label.slice(0, -1))
  }
  if (label.endsWith('壮族自治区') || label.endsWith('回族自治区') || label.endsWith('维吾尔自治区')) {
    names.push(label.replace(/壮族自治区|回族自治区|维吾尔自治区$/, ''))
  } else if (label.endsWith('自治区')) {
    names.push(label.slice(0, -3))
  } else if (label.endsWith('特别行政区')) {
    names.push(label.slice(0, -5))
  }
  return [...new Set(names)].sort((a, b) => b.length - a.length)
}

/** 填充后的省市区名对齐到下拉选项（如 北京市/北京市 → 北京市/市辖区） */
function normalizeRegionFields() {
  const p = areaOptions.find(
    (x) => x.label === form.province || x.value === form.province || uniqueNames(x.label).includes(form.province),
  )
  if (!p) return
  form.province = p.label
  const cities = p.children || []
  let c = cities.find(
    (x) => x.label === form.city || x.value === form.city || uniqueNames(x.label).includes(form.city),
  )
  if (!c && form.city && (form.city === form.province || form.city === p.label)) {
    c = cities.find((x) => x.label === '市辖区') || cities[0]
  }
  if (!c) {
    // 直辖市解析常把区放到 city
    for (const city of cities) {
      const d = (city.children || []).find(
        (x) => x.label === form.city || x.label === form.district || uniqueNames(x.label).includes(form.district),
      )
      if (d) {
        form.city = city.label
        form.district = d.label
        return
      }
    }
    return
  }
  form.city = c.label
  const d = (c.children || []).find(
    (x) => x.label === form.district || x.value === form.district || uniqueNames(x.label).includes(form.district),
  )
  if (d) form.district = d.label
}

function onProvinceChange() {
  form.city = ''
  form.district = ''
}

function onCityChange() {
  form.district = ''
}

function switchAddrMode(next: 'type' | 'select') {
  if (next === addrMode.value) return
  if (next === 'type') {
    syncTypedFromForm()
    addrMode.value = 'type'
    return
  }
  // 输 → 选：把手输内容拆进省市区
  splitTypedAddress(typedAddress.value || composeTypedAddress())
  normalizeRegionFields()
  addrMode.value = 'select'
}

async function ensureStructuredAddress(): Promise<boolean> {
  if (addrMode.value === 'type') {
    splitTypedAddress(typedAddress.value)
    normalizeRegionFields()
  }
  if (form.province && form.city && form.district && form.detail) return true
  // 手输拆不出时，走一键填充同款解析
  const text = (addrMode.value === 'type' ? typedAddress.value : composeTypedAddress()).trim()
  if (!text) {
    ElMessage.warning('请填写收件地址')
    return false
  }
  try {
    const p = (await parseManualAddress(text, false)) as ParsedAddress
    if (p?.address) {
      form.province = p.address.province || form.province
      form.city = p.address.city || form.city
      form.district = p.address.district || form.district
      form.detail = p.address.detail || form.detail
      normalizeRegionFields()
    }
  } catch {
    // ignore，下面统一校验
  }
  if (!form.province || !form.city || !form.district || !form.detail) {
    ElMessage.warning('请填写完整省市区与详细地址（可点「选」选择省市区）')
    return false
  }
  return true
}

function lineAmount(it: ItemRow) {
  const qty = Number(it.quantity) || 0
  const unit = Number(it.price) || 0
  return Math.round(unit * qty * 100) / 100
}

const orderAmount = computed(() =>
  items.value.reduce((sum, it) => sum + (it.productName.trim() ? lineAmount(it) : 0), 0),
)

function applyParsed(p: ParsedAddress) {
  form.buyerName = p.name || form.buyerName
  form.buyerPhone = p.phone || form.buyerPhone
  form.buyerTel = p.tel || form.buyerTel
  form.province = p.address?.province || ''
  form.city = p.address?.city || ''
  form.district = p.address?.district || ''
  form.detail = p.address?.detail || ''
  normalizeRegionFields()
  syncTypedFromForm()
  // 识别出省市区后切到「选」，便于核对；仍可再改或切回「输」
  if (form.province || form.city || form.district) addrMode.value = 'select'
}

async function onOneClickFill() {
  if (!rawAddress.value.trim()) {
    ElMessage.warning('请先粘贴收件人信息')
    return
  }
  filling.value = true
  try {
    if (mode.value === 'batch') {
      const list = (await parseManualAddress(rawAddress.value, true)) as ParsedAddress[]
      batchReceivers.value = (list || []).map((p) => ({
        buyerName: p.name || '',
        buyerPhone: p.phone || '',
        buyerTel: p.tel || '',
        province: p.address?.province || '',
        city: p.address?.city || '',
        district: p.address?.district || '',
        detail: p.address?.detail || '',
      }))
      if (!batchReceivers.value.length) ElMessage.warning('未识别到有效地址')
      else ElMessage.success(`已识别 ${batchReceivers.value.length} 条`)
    } else {
      const p = (await parseManualAddress(rawAddress.value, false)) as ParsedAddress
      applyParsed(p)
      ElMessage.success('已填充，可继续编辑')
    }
  } catch (e: any) {
    ElMessage.error(e.message || '识别失败')
  } finally {
    filling.value = false
  }
}

function openRecipientPicker() {
  recipientDialog.value = true
  recipientKeyword.value = form.buyerPhone || form.buyerName || ''
  recipientPage.value = 1
  selectedRecipientKey.value = ''
  void searchRecipients()
}

async function searchRecipients() {
  recipientLoading.value = true
  try {
    const data = await searchManualRecipients(recipientKeyword.value.trim(), recipientPage.value, 20)
    recipientList.value = data?.list || []
    recipientTotal.value = data?.total || 0
  } catch (e: any) {
    ElMessage.error(e.message || '搜索失败')
  } finally {
    recipientLoading.value = false
  }
}

function recipientRowKey(row: RecipientSearchItem) {
  return `${row.customerId}-${row.addressId}`
}

function formatRecipientAddress(row: RecipientSearchItem) {
  return `${row.province || ''}${row.city || ''}${row.district || ''}${row.detail || ''}`
}

function applyRecipient(row: RecipientSearchItem) {
  form.buyerName = row.contactName || row.displayName || ''
  form.buyerPhone = row.phone || row.primaryPhone || ''
  form.province = row.province || ''
  form.city = row.city || ''
  form.district = row.district || ''
  form.detail = row.detail || ''
  normalizeRegionFields()
  syncTypedFromForm()
  if (form.province || form.city || form.district) addrMode.value = 'select'
  selectedRecipientKey.value = recipientRowKey(row)
  recipientDialog.value = false
  ElMessage.success('已填入收件人，可继续编辑')
}

function addItem() {
  items.value.push(emptyItem())
}

function removeItem(idx: number) {
  items.value.splice(idx, 1)
}

async function openProductPicker() {
  productDialog.value = true
  productKeyword.value = ''
  pimList.value = []
  shopList.value = []
}

async function searchProducts() {
  const kw = productKeyword.value.trim()
  if (!kw) {
    ElMessage.warning('请输入 SKU 规格名称或规格编码')
    return
  }
  productLoading.value = true
  try {
    if (productSource.value === 'pim') {
      const data = await searchPIMProducts(kw)
      pimList.value = data?.list || []
    } else {
      const data = await searchShopProducts({
        platform: shopPlatform.value,
        keyword: kw,
        pageNo: 1,
        pageSize: 30,
      })
      // 店铺接口按 SPU 返回，前端再按规格名/编码收窄 SKU
      const lower = kw.toLowerCase()
      shopList.value = (data?.items || [])
        .map((item) => {
          const skus = (item.skus || []).filter((sku) => {
            const name = (sku.propertiesName || '').toLowerCase()
            const code = (sku.outerId || '').toLowerCase()
            return name.includes(lower) || code.includes(lower)
          })
          if (!skus.length && (item.skus || []).length) return null
          return { ...item, skus: skus.length ? skus : item.skus }
        })
        .filter(Boolean) as typeof shopList.value
    }
  } catch (e: any) {
    ElMessage.error(e.message || '搜索失败')
  } finally {
    productLoading.value = false
  }
}

/** 只要规格值：颜色分类: R7101-12速… → R7101-12速… */
function formatSpecValuesOnly(input: unknown): string {
  if (input && typeof input === 'object' && !Array.isArray(input)) {
    const map = input as Record<string, unknown>
    const vals = Object.keys(map)
      .sort()
      .map((k) => String(map[k] ?? '').trim())
      .filter(Boolean)
    if (vals.length) return vals.join(' / ')
  }
  const raw = String(input ?? '').trim()
  if (!raw || raw === '-') return ''
  return raw
    .split(/\s*\/\s*/)
    .map((part) => {
      const m = part.match(/^[^:：]+[:：]\s*(.+)$/)
      return (m ? m[1] : part).trim()
    })
    .filter(Boolean)
    .join(' / ')
}

function pickPIM(row: any) {
  items.value.push({
    productName: row.productName || '',
    outerId: row.productCode || row.outerId || '',
    skuCode: row.skuCode || '',
    skuSpecs: formatSpecValuesOnly(row.specs) || formatSpecValuesOnly(row.specLabel),
    skuId: row.skuId,
    picUrl: row.pic,
    quantity: 1,
    price: Number(row.price || 0),
    source: 'pim',
  })
  productDialog.value = false
}

function pickShopSku(item: any, sku: any) {
  items.value.push({
    productName: item.title || '',
    outerId: item.outerId || '',
    skuCode: sku.outerId || '',
    skuSpecs: formatSpecValuesOnly(sku.propertiesName),
    platformItemId: item.itemId,
    platformSkuId: sku.skuId,
    picUrl: sku.picUrl || item.picUrl,
    quantity: 1,
    price: Number(sku.price || 0),
    source: 'shop',
  })
  productDialog.value = false
}

function buildItemsPayload() {
  return items.value
    .filter((i) => i.productName.trim())
    .map((i) => ({
      productName: i.productName,
      skuCode: i.skuCode,
      skuSpecs: i.skuSpecs,
      skuId: i.skuId,
      platformItemId: i.platformItemId,
      platformSkuId: i.platformSkuId,
      picUrl: i.picUrl,
      quantity: i.quantity || 1,
      price: i.price || 0, // 单价；后端按 单价×数量 计总价
    }))
}

async function pickPrintMode(): Promise<PrintMode | null> {
  try {
    await ElMessageBox.confirm(
      '请选择打印方式：快递助手打印将使用发货中心默认快递助手账号并同步；自建物流打印不同步快递助手。创建成功后将跳转发货中心继续打单发货。',
      '创建并打印',
      {
        distinguishCancelAndClose: true,
        confirmButtonText: '快递助手打印',
        cancelButtonText: '自建物流打印',
        type: 'info',
      },
    )
    return 'kdzs'
  } catch (action) {
    if (action === 'cancel') return 'carrier'
    return null
  }
}

/** 带登录态跳转发货中心待发货，并自动打开打单弹窗 */
function goShippingPrint(order: { id: number; orderNo?: string }, printMode: PrintMode) {
  const base = getShippingCoreUrl().replace(/\/$/, '')
  if (!base) {
    throw new Error('发货中心地址未配置')
  }
  const scMode = printMode === 'carrier' ? 'sf' : 'kdzs'
  const params = new URLSearchParams({
    orderId: String(order.id),
    printMode: scMode,
    autoShip: '1',
    sourceChannel: 'manual',
  })
  if (order.orderNo) params.set('keyword', order.orderNo)
  const pending = `/pending?${params.toString()}`
  const token = getToken()
  let target = `${base}${pending}`
  if (token) {
    const url = new URL(`${base}/auth/callback`)
    url.searchParams.set('token', token)
    const refresh = getRefreshToken()
    if (refresh) url.searchParams.set('refresh', refresh)
    url.searchParams.set('redirect', pending)
    target = url.toString()
  }
  // replace：避免返回键回到半关闭的建单弹窗；先跳转再关弹窗
  window.location.replace(target)
}

async function submit(action: CreateAction) {
  let printMode: PrintMode | undefined
  if (action === 'create_and_print') {
    const picked = await pickPrintMode()
    if (!picked) return
    printMode = picked
  }
  const payloadItems = buildItemsPayload()
  // 对齐快递助手：有商品时建单不传发货内容；无商品时才写入
  const shipContent = payloadItems.length ? '' : (form.shipContent || '').trim()
  // 自建物流打印：即使开关打开也不同步
  const syncKdzs = printMode === 'carrier' ? false : form.syncKdzs
  submitting.value = true
  try {
    if (mode.value === 'batch') {
      if (!batchReceivers.value.length) {
        ElMessage.warning('请先批量识别收件人')
        return
      }
      const res = await createManualOrdersBatch({
        receivers: batchReceivers.value.map((r) => ({
          buyerName: r.buyerName,
          buyerPhone: r.buyerPhone,
          buyerTel: r.buyerTel,
          address: {
            name: r.buyerName,
            phone: r.buyerPhone,
            province: r.province,
            city: r.city,
            district: r.district,
            address: r.detail,
          },
        })),
        items: payloadItems,
        remark: form.remark,
        shipContent,
        sellerFlag: form.sellerFlag,
        saveCustomer: form.saveCustomer,
        syncKdzs,
        createAction: action,
        printMode,
      })
      ElMessage.success(successMsg(action, res.total, printMode, syncKdzs))
      const first = res.orders?.[0]
      if (action === 'create_and_print' && printMode) {
        if (!first?.id) {
          ElMessage.error('已创建但缺少订单 ID，无法跳转发货中心')
          visible.value = false
          emit('created', { orderId: first?.id, action, skipNavigate: true })
          return
        }
        goShippingPrint(first, printMode)
        return
      }
      visible.value = false
      emit('created', { orderId: first?.id, action })
    } else {
      if (!form.buyerName || (!form.buyerPhone && !form.buyerTel)) {
        ElMessage.warning('请填写收件人与手机/固话')
        return
      }
      if (!(await ensureStructuredAddress())) return
      const order = await createManualOrder({
        buyerName: form.buyerName,
        buyerPhone: form.buyerPhone,
        buyerTel: form.buyerTel,
        remark: form.remark,
        shipContent,
        sellerFlag: form.sellerFlag,
        saveCustomer: form.saveCustomer,
        syncKdzs,
        createAction: action,
        printMode,
        platformOrderNo: form.platformOrderNo || undefined,
        address: {
          name: form.buyerName,
          phone: form.buyerPhone,
          province: form.province,
          city: form.city,
          district: form.district,
          address: form.detail,
        },
        items: payloadItems,
      })
      ElMessage.success(successMsg(action, 1, printMode, syncKdzs))
      if (action === 'create_and_print' && printMode) {
        if (!order?.id) {
          ElMessage.error('已创建但缺少订单 ID，无法跳转发货中心')
          visible.value = false
          emit('created', { orderId: order?.id, action, skipNavigate: true })
          return
        }
        goShippingPrint(order, printMode)
        return
      }
      visible.value = false
      emit('created', { orderId: order.id, action })
    }
  } catch (e: any) {
    ElMessage.error(e.message || '创建失败')
  } finally {
    submitting.value = false
  }
}

function successMsg(action: CreateAction, total: number, printMode?: PrintMode, syncKdzs = true) {
  const n = total > 1 ? `${total} 个订单` : '订单'
  if (action === 'create_and_push') {
    return syncKdzs ? `已创建并推送（自营/发货中心默认账号）${n}` : `已创建并分配自营（未同步快递助手）${n}`
  }
  if (action === 'create_and_print') {
    if (printMode === 'carrier') return `已创建并分配自营${n}，正在跳转发货中心打单…`
    return `已创建并推送（自营）${n}，正在跳转发货中心打单…`
  }
  return syncKdzs ? `已创建${n}到待推单（已同步发货中心默认账号）` : `已创建本地${n}（未同步快递助手）`
}

function clearContent() {
  reset()
  ElMessage.success('已清空内容')
}
</script>

<template>
  <el-dialog v-model="visible" title="手工建单" width="1080px" destroy-on-close class="manual-dialog">
    <div class="form">
      <!-- 识别类型与选项同一行，整体左对齐 -->
      <div class="field-row">
        <span class="field-label">识别类型：</span>
        <el-radio-group v-model="mode">
          <el-radio value="single">单个识别</el-radio>
          <el-radio value="batch">批量识别</el-radio>
        </el-radio-group>
      </div>

      <!-- 单个：左粘贴 + 右收件字段（对齐快递助手） -->
      <div v-if="mode === 'single'" class="recv-panel">
        <div class="recv-paste">
          <el-input
            v-model="rawAddress"
            type="textarea"
            :rows="7"
            resize="none"
            placeholder="将从其他处（如淘宝、微信）复制的收件人信息粘贴至此处，点击一键填充"
          />
          <el-button class="fill-btn" link type="primary" :loading="filling" @click="onOneClickFill">
            一键填充
          </el-button>
        </div>
        <div class="recv-fields">
          <div class="field-row">
            <span class="field-label">收件人</span>
            <el-input v-model="form.buyerName" placeholder="收件人姓名" class="w-name" />
            <el-button link type="warning" @click="openRecipientPicker">已有收件人</el-button>
            <span class="inline-label">手机</span>
            <el-input v-model="form.buyerPhone" placeholder="手机" class="w-phone" />
            <span class="inline-label">固话</span>
            <el-input v-model="form.buyerTel" placeholder="固话" class="w-tel" />
          </div>
          <div class="field-row addr-field-row">
            <span class="field-label">收件地址</span>
            <div v-if="addrMode === 'type'" class="addr-row grow">
              <el-input v-model="typedAddress" placeholder="省市区详细地址，可整段手输" />
              <el-button class="addr-toggle" type="warning" @click="switchAddrMode('select')">选</el-button>
            </div>
            <div v-else class="addr-row grow">
              <el-select
                v-model="form.province"
                placeholder="省份"
                filterable
                clearable
                class="w-pca"
                @change="onProvinceChange"
              >
                <el-option v-for="p in provinceOptions" :key="p.value" :label="p.label" :value="p.label" />
              </el-select>
              <el-select
                v-model="form.city"
                placeholder="市"
                filterable
                clearable
                class="w-pca"
                :disabled="!form.province"
                @change="onCityChange"
              >
                <el-option v-for="c in cityOptions" :key="c.value" :label="c.label" :value="c.label" />
              </el-select>
              <el-select
                v-model="form.district"
                placeholder="区"
                filterable
                clearable
                class="w-pca"
                :disabled="!form.city"
              >
                <el-option v-for="d in districtOptions" :key="d.value" :label="d.label" :value="d.label" />
              </el-select>
              <el-input v-model="form.detail" placeholder="详细地址" class="grow-input" />
              <el-button class="addr-toggle" type="warning" @click="switchAddrMode('type')">输</el-button>
            </div>
          </div>
          <div class="field-row save-row">
            <span class="field-label" />
            <el-checkbox v-model="form.saveCustomer">同时保存收件人到客户中心</el-checkbox>
          </div>
        </div>
      </div>

      <!-- 批量：整行粘贴 + 识别结果 -->
      <template v-else>
        <div class="batch-paste">
          <el-input
            v-model="rawAddress"
            type="textarea"
            :rows="4"
            placeholder="每行一条：姓名 手机 省市区详细地址"
          />
          <div class="mt8">
            <el-button type="primary" plain :loading="filling" @click="onOneClickFill">一键填充</el-button>
          </div>
        </div>
        <div class="field-row top-gap">
          <span class="field-label">识别结果</span>
          <div class="grow">
            <el-table :data="batchReceivers" size="small" max-height="200" empty-text="请先粘贴并一键填充">
              <el-table-column prop="buyerName" label="收件人" width="100" />
              <el-table-column prop="buyerPhone" label="手机" width="120" />
              <el-table-column label="地址" min-width="220">
                <template #default="{ row }">{{ row.province }}{{ row.city }}{{ row.district }}{{ row.detail }}</template>
              </el-table-column>
            </el-table>
            <el-checkbox v-model="form.saveCustomer" class="mt8">同时保存收件人到客户中心</el-checkbox>
          </div>
        </div>
      </template>

      <div class="goods-block">
        <div class="goods-toolbar">
          <span class="field-label goods-label">商品信息：</span>
          <el-button type="warning" @click="openProductPicker">从商品库选择</el-button>
          <span class="goods-total">订单合计 ¥{{ orderAmount.toFixed(2) }}</span>
        </div>
        <el-table :data="items" border size="small" class="goods-table" empty-text="暂无数据">
          <el-table-column label="商品名称" min-width="150">
            <template #default="{ row }">
              <el-input v-model="row.productName" placeholder="商品名称" />
            </template>
          </el-table-column>
          <el-table-column label="商家编码" width="110">
            <template #default="{ row }">
              <el-input v-model="row.outerId" placeholder="商家编码" />
            </template>
          </el-table-column>
          <el-table-column label="规格名称" width="110">
            <template #default="{ row }">
              <el-input v-model="row.skuSpecs" placeholder="规格名称" />
            </template>
          </el-table-column>
          <el-table-column label="规格编码" width="110">
            <template #default="{ row }">
              <el-input v-model="row.skuCode" placeholder="规格编码" />
            </template>
          </el-table-column>
          <el-table-column label="商品数量" width="130">
            <template #default="{ row }">
              <el-input-number
                v-model="row.quantity"
                :min="1"
                :precision="0"
                :step="1"
                controls-position="right"
                class="goods-qty"
              />
            </template>
          </el-table-column>
          <el-table-column label="单价" width="96">
            <template #default="{ row }">
              <el-input v-model.number="row.price" placeholder="单价" />
            </template>
          </el-table-column>
          <el-table-column label="小计" width="88">
            <template #default="{ row }">
              <span class="amt">¥{{ lineAmount(row).toFixed(2) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="70" fixed="right">
            <template #default="{ $index }">
              <el-button link type="danger" @click="removeItem($index)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
        <div class="goods-footer">
          <el-button link type="primary" @click="addItem">+ 添加商品</el-button>
          <span class="hint">价格为单价；改数量时小计自动按 单价×数量 计算</span>
        </div>
      </div>

      <div class="field-row top-gap">
        <span class="field-label">订单编号：</span>
        <el-input v-model="form.platformOrderNo" placeholder="可选，不填则用系统单号" class="w-orderno" />
      </div>
      <div class="field-row remark-flag-row">
        <span class="field-label">订单备注：</span>
        <el-input
          v-model="form.remark"
          type="textarea"
          :rows="2"
          maxlength="500"
          show-word-limit
          placeholder="最多500字"
          class="grow-input"
        />
        <div class="flag-aside">
          <SellerFlag v-model="form.sellerFlag" mode="edit" :size="18" />
        </div>
      </div>
      <div class="field-row ship-row">
        <span class="field-label">发货内容：</span>
        <div class="ship-box">
          <el-input
            v-model="form.shipContent"
            type="textarea"
            :rows="2"
            placeholder="填写本地商品后将自动填充发货内容"
            class="grow-input"
          />
          <div class="ship-tip">仅填写发货内容的手工订单将无法对账</div>
        </div>
      </div>
      <div class="field-row">
        <span class="field-label field-label-wide">同步快递助手：</span>
        <el-switch v-model="form.syncKdzs" />
        <span class="hint">账号取发货中心「默认」快递助手账号；创建并推送=自营自己打单。自建物流打印时忽略此开关</span>
      </div>
    </div>

    <template #footer>
      <div class="manual-footer">
        <el-button class="btn-create" type="warning" :loading="submitting" @click="submit('create_only')">
          仅创建
        </el-button>
        <el-button class="btn-create" type="warning" :loading="submitting" @click="submit('create_and_push')">
          创建并推送
        </el-button>
        <el-button class="btn-create" type="warning" :loading="submitting" @click="submit('create_and_print')">
          创建并打印
        </el-button>
        <el-button :disabled="submitting" @click="clearContent">清空内容</el-button>
      </div>
    </template>

    <!-- 选择已有收件人 -->
    <el-dialog v-model="recipientDialog" title="选择已有收件人" width="760px" append-to-body>
      <div class="search-row">
        <el-input
          v-model="recipientKeyword"
          placeholder="姓名 / 手机 / 地址"
          clearable
          @keyup.enter="() => { recipientPage = 1; searchRecipients() }"
        />
        <el-button
          type="primary"
          :loading="recipientLoading"
          @click="() => { recipientPage = 1; searchRecipients() }"
        >
          搜索
        </el-button>
      </div>
      <el-table
        :data="recipientList"
        size="small"
        max-height="360"
        highlight-current-row
        v-loading="recipientLoading"
        @row-dblclick="applyRecipient"
      >
        <el-table-column label="收件人" width="120">
          <template #default="{ row }">{{ row.contactName || row.displayName || '-' }}</template>
        </el-table-column>
        <el-table-column label="手机" width="120">
          <template #default="{ row }">{{ row.phone || row.primaryPhone || '-' }}</template>
        </el-table-column>
        <el-table-column label="地址" min-width="260">
          <template #default="{ row }">
            {{ formatRecipientAddress(row) }}
            <el-tag v-if="row.isDefault" size="small" type="success" class="default-tag">默认</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="90" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="applyRecipient(row)">使用</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="pager-row">
        <el-pagination
          v-model:current-page="recipientPage"
          :page-size="20"
          :total="recipientTotal"
          layout="total, prev, pager, next"
          @current-change="searchRecipients"
        />
      </div>
      <div class="hint-block">同一客户的多个地址会全部列出；选中后仍可在表单中修改。</div>
    </el-dialog>

    <el-dialog v-model="productDialog" title="选择商品" width="720px" append-to-body>
      <el-radio-group v-model="productSource" class="mb8">
        <el-radio-button value="pim">商品库 (PIM)</el-radio-button>
        <el-radio-button value="shop">电商店铺商品</el-radio-button>
      </el-radio-group>
      <div class="search-row">
        <el-select v-if="productSource === 'shop'" v-model="shopPlatform" style="width: 120px">
          <el-option label="抖店" value="FXG" />
          <el-option label="淘宝" value="TB" />
          <el-option label="拼多多" value="PDD" />
          <el-option label="快手" value="KSXD" />
          <el-option label="小红书" value="XHS" />
        </el-select>
        <el-input
          v-model="productKeyword"
          placeholder="SKU规格名称 / 规格编码"
          clearable
          @keyup.enter="searchProducts"
        />
        <el-button type="primary" :loading="productLoading" @click="searchProducts">搜索</el-button>
      </div>
      <el-table v-if="productSource === 'pim'" :data="pimList" size="small" max-height="360" @row-click="pickPIM">
        <el-table-column prop="productName" label="商品" min-width="160" />
        <el-table-column label="规格名称" width="180">
          <template #default="{ row }">
            {{ formatSpecValuesOnly(row.specs) || formatSpecValuesOnly(row.specLabel) || '-' }}
          </template>
        </el-table-column>
        <el-table-column prop="skuCode" label="规格编码" width="130" />
        <el-table-column prop="price" label="单价" width="90" />
      </el-table>
      <div v-else class="shop-list">
        <div v-for="item in shopList" :key="item.itemId" class="shop-item">
          <div class="shop-title">{{ item.title }}</div>
          <div class="shop-skus">
            <el-button
              v-for="sku in item.skus || [{ skuId: item.itemId, propertiesName: '默认', outerId: item.outerId, price: '0', picUrl: item.picUrl }]"
              :key="sku.skuId"
              size="small"
              @click="pickShopSku(item, sku)"
            >
              {{ sku.propertiesName || '规格' }}
              <template v-if="sku.outerId"> / {{ sku.outerId }}</template>
              ¥{{ sku.price || 0 }}
            </el-button>
          </div>
        </div>
        <el-empty v-if="!shopList.length" description="暂无数据，请搜索" />
      </div>
    </el-dialog>
  </el-dialog>
</template>

<style scoped>
.form { max-height: 68vh; overflow: auto; padding-right: 4px; }
.field-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 12px;
  width: 100%;
}
.field-label {
  flex: 0 0 84px;
  width: 84px;
  color: #606266;
  font-size: 14px;
  line-height: 32px;
  text-align: left;
  white-space: nowrap;
}
.field-label-wide { flex-basis: 110px; width: 110px; }
.inline-label { color: #606266; font-size: 14px; white-space: nowrap; }
.recv-panel {
  display: grid;
  grid-template-columns: 280px 1fr;
  gap: 16px;
  margin-bottom: 16px;
  align-items: stretch;
}
.recv-paste {
  position: relative;
  border: 1px solid #dcdfe6;
  border-radius: 4px;
  padding: 8px;
  background: #fafafa;
}
.recv-paste :deep(.el-textarea__inner) {
  box-shadow: none;
  background: transparent;
  padding: 0 0 28px;
}
.fill-btn { position: absolute; right: 10px; bottom: 8px; }
.recv-fields { min-width: 0; }
.addr-field-row { align-items: flex-start; }
.addr-row { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; }
.addr-row.grow, .grow { flex: 1; min-width: 0; }
.grow-input { flex: 1; min-width: 140px; }
.addr-toggle { flex-shrink: 0; min-width: 36px; padding: 8px 10px; }
.save-row { margin-top: -4px; }
.batch-paste { margin-bottom: 8px; }
.top-gap { margin-top: 4px; }
.w-name { width: 140px; }
.w-phone { width: 130px; }
.w-tel { width: 120px; }
.w-pca { width: 110px; }
.w-orderno { width: 320px; }
.remark-flag-row { align-items: flex-start; }
.flag-aside {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  padding-top: 6px;
}
.mt8 { margin-top: 8px; }
.mb8 { margin-bottom: 8px; }
.hint-block { margin-top: 6px; color: #909399; font-size: 12px; line-height: 1.4; }
.goods-block { width: 100%; margin: 4px 0 12px; }
.goods-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 10px;
}
.goods-label { flex: none; width: auto; }
.goods-total { margin-left: auto; color: #303133; font-size: 13px; font-variant-numeric: tabular-nums; }
.goods-table { width: 100%; }
.goods-table :deep(.goods-qty) { width: 110px; }
.goods-footer {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 8px;
}
.amt { color: #303133; font-variant-numeric: tabular-nums; }
.hint { margin-left: 4px; color: #909399; font-size: 12px; }
.ship-row { align-items: flex-start; }
.ship-box { flex: 1; min-width: 0; }
.ship-tip { margin-top: 6px; color: #909399; font-size: 12px; line-height: 1.4; }
.manual-footer {
  display: flex;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 10px;
  width: 100%;
}
.manual-footer .btn-create {
  min-width: 108px;
}
.search-row { display: flex; gap: 8px; margin-bottom: 10px; }
.pager-row { display: flex; justify-content: flex-end; margin-top: 10px; }
.default-tag { margin-left: 6px; }
.shop-list { max-height: 360px; overflow: auto; }
.shop-item { padding: 8px 0; border-bottom: 1px solid #ebeef5; }
.shop-title { font-weight: 500; margin-bottom: 6px; }
.shop-skus { display: flex; flex-wrap: wrap; gap: 6px; }
@media (max-width: 960px) {
  .recv-panel { grid-template-columns: 1fr; }
}
</style>
