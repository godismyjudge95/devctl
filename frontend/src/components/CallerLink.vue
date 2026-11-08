<script setup lang="ts">
import { computed } from 'vue'
import type { RequestCaller } from '@/lib/api'
import { editorUrl } from '@/lib/utils'
import { useSettingsStore } from '@/stores/settings'

const props = defineProps<{ caller: RequestCaller }>()
const settings = useSettingsStore()

const href = computed(() => editorUrl(settings.settings['editor_url'], props.caller.file, props.caller.line))
</script>

<template>
  <a
    :href="href"
    class="break-all font-mono text-xs text-muted-foreground hover:text-foreground hover:underline"
    title="Open in editor"
    data-caller
  >{{ caller.file }}:{{ caller.line }}</a>
</template>
