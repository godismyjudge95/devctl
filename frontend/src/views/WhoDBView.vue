<script setup lang="ts">
import { ref } from 'vue'
import { Database } from 'lucide-vue-next'

const iframeLoaded = ref(false)
const whodbUrl = 'http://127.0.0.1:8161'
</script>

<template>
  <div class="flex h-full overflow-hidden w-full relative flex-col">
    <div class="flex items-center justify-between px-4 py-2.5 border-b border-border bg-card shrink-0">
      <div>
        <div class="kicker text-[12px]">WhoDB</div>
        <div class="text-[11px] text-muted-foreground mt-0.5">Database explorer</div>
      </div>
    </div>
    <!-- Loading overlay — shown until the iframe fires its load event -->
    <div
      v-if="!iframeLoaded"
      class="absolute inset-0 top-12 flex flex-col items-center justify-center gap-3 bg-background z-10"
    >
      <div class="w-8 h-8 rounded-full border-2 border-foreground/30 border-t-foreground animate-spin" />
      <span class="text-sm text-muted-foreground">Loading WhoDB…</span>
    </div>
    <iframe
      :src="whodbUrl"
      class="w-full flex-1 min-h-0 border-0"
      :class="{ 'opacity-0': !iframeLoaded }"
      allow="clipboard-read; clipboard-write"
      @load="iframeLoaded = true"
    />
  </div>
</template>
