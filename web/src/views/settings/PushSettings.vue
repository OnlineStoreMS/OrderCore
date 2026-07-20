<script setup lang="ts">
</script>

<template>
  <div class="page">
    <el-empty description="推送设置待开发">
      <template #description>
        <p class="title">推送设置待开发</p>
        <p class="desc">后续将支持飞书 / 企微等渠道，在订单分配后向供应商推送通知。</p>
      </template>
    </el-empty>
  </div>
</template>

<style scoped>
.page {
  background: #fff;
  border-radius: 8px;
  border: 1px solid #f0f0f0;
  min-height: 360px;
  display: flex;
  align-items: center;
  justify-content: center;
}
.title {
  margin: 0 0 8px;
  font-size: 16px;
  color: #262626;
}
.desc {
  margin: 0;
  font-size: 13px;
  color: #8c8c8c;
  line-height: 1.6;
  max-width: 360px;
}
</style>
