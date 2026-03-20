<script setup lang="ts">
import { ref } from 'vue'
import { Database } from 'lucide-vue-next'

const iframeLoaded = ref(false)
const whodbUrl = 'http://127.0.0.1:8161'
</script>

<template>
  <div class="flex h-full overflow-hidden w-full relative">
    <!-- Loading overlay — shown until the iframe fires its load event -->
    <div
      v-if="!iframeLoaded"
      class="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-background z-10"
    >
      <div class="w-8 h-8 rounded-full border-2 border-primary border-t-transparent animate-spin" />
      <span class="text-sm text-muted-foreground">Loading WhoDB…</span>
    </div>
    <iframe
      :src="whodbUrl"
      class="w-full h-full border-0"
      :class="{ 'opacity-0': !iframeLoaded }"
      allow="clipboard-read; clipboard-write"
      @load="iframeLoaded = true"
    />
  </div>
</template>
