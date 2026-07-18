<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue'
import { ElMessage, type UploadRequestOptions } from 'element-plus'
import { Plus, Iphone } from '@element-plus/icons-vue'
import QRCode from 'qrcode'
import {
  createPhotoUploadSession,
  getPhotoUploadSession,
  uploadImage,
} from '../api/upload'

const props = withDefaults(defineProps<{
  modelValue?: string
  subdir?: string
  tip?: string
  /** 表格内缩略图模式：只显示上传框，不显示 URL 输入 */
  compact?: boolean
  size?: number
  /** 是否显示扫码上传实拍图 */
  scanUpload?: boolean
}>(), {
  compact: false,
  size: 120,
  scanUpload: true,
})
const emit = defineEmits<{ (e: 'update:modelValue', v: string): void }>()

const uploading = ref(false)
const url = computed({
  get: () => props.modelValue || '',
  set: (v) => emit('update:modelValue', v),
})

const boxSize = computed(() => (props.compact ? Math.min(props.size, 56) : props.size))

const scanVisible = ref(false)
const scanLoading = ref(false)
const qrDataUrl = ref('')
const scanToken = ref('')
const scanStatus = ref<'idle' | 'waiting' | 'done' | 'expired'>('idle')
let pollTimer: ReturnType<typeof setInterval> | null = null

async function doUpload(options: UploadRequestOptions) {
  uploading.value = true
  try {
    const file = options.file as File
    const next = await uploadImage(file, props.subdir || 'products')
    url.value = next
    options.onSuccess?.(next as any)
  } catch (e) {
    ElMessage.error((e as Error).message || '上传失败')
    options.onError?.(e as any)
  } finally {
    uploading.value = false
  }
}

function clear(e?: Event) {
  e?.stopPropagation()
  e?.preventDefault()
  url.value = ''
}

function stopPoll() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

async function openScan() {
  scanVisible.value = true
  scanLoading.value = true
  scanStatus.value = 'idle'
  qrDataUrl.value = ''
  scanToken.value = ''
  stopPoll()
  try {
    const session = await createPhotoUploadSession(props.subdir || 'products')
    scanToken.value = session.token
    const pageUrl = `${window.location.origin}/m/photo-upload?token=${encodeURIComponent(session.token)}`
    qrDataUrl.value = await QRCode.toDataURL(pageUrl, {
      width: 220,
      margin: 2,
      errorCorrectionLevel: 'M',
    })
    scanStatus.value = 'waiting'
    pollTimer = setInterval(async () => {
      try {
        const s = await getPhotoUploadSession(scanToken.value)
        if (s.status === 'done' && s.url) {
          url.value = s.url
          scanStatus.value = 'done'
          stopPoll()
          ElMessage.success('实拍图已上传')
          setTimeout(() => { scanVisible.value = false }, 600)
        }
      } catch {
        scanStatus.value = 'expired'
        stopPoll()
      }
    }, 2000)
  } catch (e) {
    ElMessage.error((e as Error).message || '创建扫码会话失败')
    scanVisible.value = false
  } finally {
    scanLoading.value = false
  }
}

function closeScan() {
  scanVisible.value = false
  stopPoll()
}

onUnmounted(stopPoll)
</script>

<template>
  <div class="img-field" :class="{ compact }" :style="{ '--img-size': boxSize + 'px' }">
    <div v-if="url" class="preview-wrap">
      <el-image
        :src="url"
        fit="cover"
        class="preview-img"
        :preview-src-list="[url]"
        preview-teleported
        hide-on-click-modal
      />
      <button v-if="compact" type="button" class="clear-btn" title="清除" @click="clear">×</button>
      <button
        v-if="compact && scanUpload"
        type="button"
        class="scan-btn"
        title="扫码上传实拍图"
        @click.stop="openScan"
      >
        <el-icon :size="12"><Iphone /></el-icon>
      </button>
    </div>
    <el-upload
      v-else
      class="uploader"
      :show-file-list="false"
      accept="image/*"
      :http-request="doUpload"
      :disabled="uploading"
    >
      <div class="placeholder" v-loading="uploading">
        <el-icon :size="compact ? 16 : 20"><Plus /></el-icon>
        <span v-if="!compact">{{ tip || '上传图片' }}</span>
        <button
          v-if="compact && scanUpload"
          type="button"
          class="scan-btn empty"
          title="扫码上传实拍图"
          @click.stop.prevent="openScan"
        >
          <el-icon :size="12"><Iphone /></el-icon>
        </button>
      </div>
    </el-upload>

    <template v-if="!compact">
      <div class="actions">
        <div class="action-row">
          <el-button v-if="url" link type="danger" size="small" @click="clear">清除</el-button>
          <el-upload
            v-if="url"
            :show-file-list="false"
            accept="image/*"
            :http-request="doUpload"
            :disabled="uploading"
          >
            <el-button link type="primary" size="small">本地上传</el-button>
          </el-upload>
          <el-button
            v-if="scanUpload"
            link
            type="primary"
            size="small"
            :icon="Iphone"
            @click="openScan"
          >
            扫码上传
          </el-button>
        </div>
        <el-input v-model="url" size="small" placeholder="或填入图片 URL" />
      </div>
    </template>

    <el-dialog
      v-model="scanVisible"
      title="手机扫码上传实拍图"
      width="360px"
      append-to-body
      destroy-on-close
      @closed="closeScan"
    >
      <div class="scan-body" v-loading="scanLoading">
        <img v-if="qrDataUrl" :src="qrDataUrl" alt="扫码上传" class="qr" />
        <p v-if="scanStatus === 'waiting'" class="hint">请用手机扫描二维码，拍照后自动回填到此处</p>
        <p v-else-if="scanStatus === 'done'" class="hint ok">上传成功</p>
        <p v-else-if="scanStatus === 'expired'" class="hint err">会话已过期，请关闭后重试</p>
        <p v-else class="hint">正在生成二维码…</p>
      </div>
      <template #footer>
        <el-button @click="closeScan">关闭</el-button>
        <el-button type="primary" :disabled="scanLoading" @click="openScan">刷新二维码</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.img-field {
  width: calc(var(--img-size) + 20px);
  display: inline-flex;
  flex-direction: column;
  align-items: flex-start;
}
.img-field.compact {
  width: var(--img-size);
  vertical-align: middle;
}
.preview-wrap {
  position: relative;
  width: var(--img-size);
  height: var(--img-size);
  border: 1px dashed var(--el-border-color);
  border-radius: 6px;
  overflow: hidden;
  background: #fafafa;
}
.preview-img {
  width: 100%;
  height: 100%;
  display: block;
  cursor: zoom-in;
}
.preview-img :deep(img) {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.uploader :deep(.el-upload) {
  border: 1px dashed var(--el-border-color);
  border-radius: 6px;
  cursor: pointer;
  overflow: hidden;
  width: var(--img-size);
  height: var(--img-size);
  display: flex;
  align-items: center;
  justify-content: center;
  background: #fafafa;
}
.placeholder {
  position: relative;
  width: var(--img-size);
  height: var(--img-size);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 4px;
  color: #909399;
  font-size: 12px;
}
.clear-btn {
  position: absolute;
  top: 0;
  right: 0;
  z-index: 2;
  width: 18px;
  height: 18px;
  border: none;
  border-radius: 0 0 0 6px;
  background: rgba(0, 0, 0, 0.55);
  color: #fff;
  font-size: 14px;
  line-height: 16px;
  cursor: pointer;
  padding: 0;
}
.scan-btn {
  position: absolute;
  left: 0;
  bottom: 0;
  z-index: 2;
  width: 20px;
  height: 20px;
  border: none;
  border-radius: 0 6px 0 0;
  background: rgba(64, 158, 255, 0.9);
  color: #fff;
  cursor: pointer;
  padding: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}
.scan-btn.empty {
  border-radius: 0 6px 0 6px;
}
.actions {
  margin-top: 6px;
  display: flex;
  flex-direction: column;
  gap: 4px;
  width: 100%;
  min-width: 140px;
}
.action-row {
  display: flex;
  flex-wrap: wrap;
  gap: 2px 8px;
  align-items: center;
}
.scan-body {
  display: flex;
  flex-direction: column;
  align-items: center;
  min-height: 260px;
  justify-content: center;
}
.qr {
  width: 220px;
  height: 220px;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 8px;
}
.hint {
  margin: 12px 0 0;
  font-size: 13px;
  color: #606266;
  text-align: center;
  line-height: 1.5;
}
.hint.ok { color: var(--el-color-success); }
.hint.err { color: var(--el-color-danger); }
</style>
