<script setup lang="ts">
import type { Share } from '@/lib/api'
import { Copy, Loader2, Radio, RotateCw, Square, TriangleAlert } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { useSiteShare } from '@/composables/useSiteShare'

const props = defineProps<{ share: Share }>()
const { start, stop, copy } = useSiteShare()
</script>

<template>
  <div
    data-testid="site-share"
    :data-status="props.share.status"
    class="flex min-w-0 items-center gap-1 text-xs"
  >
    <template v-if="props.share.status === 'starting'">
      <Loader2 class="size-3.5 shrink-0 animate-spin text-muted-foreground" />
      <span class="text-muted-foreground">Starting public tunnel…</span>
    </template>

    <template v-else-if="props.share.status === 'active' && props.share.url">
      <Radio class="size-3.5 shrink-0 text-success-soft-foreground" />
      <a
        :href="props.share.url"
        target="_blank"
        rel="noopener"
        class="min-w-0 truncate font-mono hover:underline"
        :title="props.share.url"
      >{{ props.share.url }}</a>
      <Button
        variant="ghost"
        size="icon-sm"
        class="size-6"
        aria-label="Copy public URL"
        title="Copy public URL"
        @click="copy(props.share.url)"
      >
        <Copy class="size-3.5" />
      </Button>
    </template>

    <template v-else-if="props.share.status === 'failed'">
      <TriangleAlert class="size-3.5 shrink-0 text-destructive" />
      <span class="min-w-0 truncate text-destructive" :title="props.share.error">
        Tunnel failed: {{ props.share.error }}
      </span>
      <Button
        variant="ghost"
        size="icon-sm"
        class="size-6"
        aria-label="Retry sharing"
        title="Retry"
        @click="start(props.share.site_id, props.share.domain)"
      >
        <RotateCw class="size-3.5" />
      </Button>
    </template>

    <Button
      variant="ghost"
      size="icon-sm"
      class="size-6 text-muted-foreground hover:text-foreground"
      aria-label="Stop sharing"
      title="Stop sharing"
      @click="stop(props.share.site_id, props.share.domain)"
    >
      <Square class="size-3.5" />
    </Button>
  </div>
</template>
