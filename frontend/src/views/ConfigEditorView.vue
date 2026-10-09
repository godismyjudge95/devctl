<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { toast } from 'vue-sonner'
import { ArrowLeft, Loader2, Save } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import ServiceMark from '@/components/layout/ServiceMark.vue'
import CodeEditor from '@/components/CodeEditor.vue'
import { getServiceConfig, putServiceConfig } from '@/lib/api'

// ---------------------------------------------------------------------------
// Config metadata — which files each service exposes
// ---------------------------------------------------------------------------
interface ServiceConfigMeta {
  label: string
  files: { name: string; label: string }[]
}

const SERVICE_META: Record<string, ServiceConfigMeta> = {
  'mysql':        { label: 'MySQL',       files: [{ name: 'my.cnf',        label: 'my.cnf' }] },
  'redis':        { label: 'Valkey',      files: [{ name: 'valkey.conf',   label: 'valkey.conf' }] },
  'meilisearch':  { label: 'Meilisearch', files: [{ name: 'config.toml',   label: 'config.toml' }] },
  'typesense':    { label: 'Typesense',   files: [{ name: 'typesense.ini', label: 'typesense.ini' }] },
  'mailpit':      { label: 'Mailpit',     files: [{ name: 'config.env',    label: 'config.env' }] },
  'clickhouse':   { label: 'ClickHouse',  files: [
    { name: 'config.xml', label: 'config.xml' },
    { name: 'users.xml',  label: 'users.xml' },
  ]},
}

// PHP-FPM services are dynamic (php-fpm-8.3, php-fpm-8.4, etc.)
function resolveMeta(id: string): ServiceConfigMeta | null {
  if (SERVICE_META[id]) return SERVICE_META[id]
  if (id.startsWith('php-fpm-')) {
    const ver = id.replace('php-fpm-', '')
    return {
      label: `PHP ${ver} FPM`,
      files: [
        { name: 'php.ini',       label: 'php.ini' },
        { name: 'php-fpm.conf',  label: 'php-fpm.conf' },
      ],
    }
  }
  return null
}

// Map a filename to a CodeMirror language key
function fileLanguage(name: string): 'ini' | 'toml' | 'text' {
  if (name.endsWith('.toml')) return 'toml'
  if (name.endsWith('.ini') || name.endsWith('.conf') || name.endsWith('.env') || name.endsWith('.cnf')) return 'ini'
  // config.xml / users.xml — no dedicated XML mode; plain text is fine
  return 'text'
}

// ---------------------------------------------------------------------------
// Route params
// ---------------------------------------------------------------------------
const route  = useRoute()
const router = useRouter()

const serviceId   = computed(() => route.params.id as string)
const initialFile = computed(() => route.params.file as string)
const meta        = computed(() => resolveMeta(serviceId.value))

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------
const activeFile = ref(initialFile.value)
const content    = ref('')
const loading    = ref(false)
const saving     = ref(false)

// ---------------------------------------------------------------------------
// Load file content
// ---------------------------------------------------------------------------
async function loadFile(file: string) {
  loading.value = true
  content.value = ''
  try {
    const res = await getServiceConfig(serviceId.value, file)
    content.value = res.content
  } catch (e: any) {
    toast.error('Failed to load config', { description: e.message })
  } finally {
    loading.value = false
  }
}

// Reload when the route param changes (e.g. switching tabs navigates)
watch(activeFile, (file) => {
  loadFile(file)
})

onMounted(() => {
  loadFile(activeFile.value)
})

// ---------------------------------------------------------------------------
// Save
// ---------------------------------------------------------------------------
async function save() {
  saving.value = true
  try {
    await putServiceConfig(serviceId.value, activeFile.value, content.value)
    toast.success('Config saved', {
      description: `${meta.value?.label ?? serviceId.value} is restarting with the new config.`,
    })
  } catch (e: any) {
    toast.error('Failed to save config', { description: e.message })
  } finally {
    saving.value = false
  }
}

// ---------------------------------------------------------------------------
// Keyboard shortcut: Ctrl+S / Cmd+S
// ---------------------------------------------------------------------------
function onKeydown(e: KeyboardEvent) {
  if ((e.ctrlKey || e.metaKey) && e.key === 's') {
    e.preventDefault()
    if (!saving.value && !loading.value) save()
  }
}
</script>

<template>
  <div class="flex h-full flex-col" tabindex="-1" @keydown="onKeydown">
    <!-- Top bar -->
    <div class="flex h-14 shrink-0 items-center gap-3 border-b border-border bg-card px-4">
      <Button
        variant="ghost"
        size="icon-sm"
        class="-ml-2 shrink-0"
        aria-label="Back to services"
        title="Back to services"
        @click="router.push('/services')"
      >
        <ArrowLeft class="size-4" />
      </Button>

      <ServiceMark :id="serviceId" size="sm" class="hidden sm:inline-flex" />
      <div class="min-w-0 flex-1">
        <h1 class="truncate text-sm font-semibold leading-tight">{{ meta?.label ?? serviceId }}</h1>
        <p class="truncate font-mono text-xs text-muted-foreground">{{ activeFile }}</p>
      </div>

      <div class="flex shrink-0 items-center gap-3">
        <span class="hidden text-xs text-muted-foreground lg:block">
          <kbd class="rounded border border-border bg-muted px-1 font-mono">Ctrl</kbd>
          +
          <kbd class="rounded border border-border bg-muted px-1 font-mono">S</kbd>
          to save &amp; restart
        </span>
        <Button size="sm" :disabled="saving || loading" @click="save">
          <Save class="size-3.5" />
          {{ saving ? 'Saving…' : 'Save & Restart' }}
        </Button>
      </div>
    </div>

    <!-- File tabs (only when the service has several files) -->
    <div v-if="meta && meta.files.length > 1" class="flex shrink-0 items-center border-b border-border bg-card px-4 py-2">
      <Tabs
        :model-value="activeFile"
        @update:model-value="(f) => { activeFile = f as string }"
      >
        <TabsList>
          <TabsTrigger
            v-for="f in meta.files"
            :key="f.name"
            :value="f.name"
            class="font-mono text-xs"
          >
            {{ f.label }}
          </TabsTrigger>
        </TabsList>
      </Tabs>
    </div>

    <div v-if="loading" class="flex flex-1 items-center justify-center gap-2 text-sm text-muted-foreground">
      <Loader2 class="size-4 animate-spin" />
      Loading…
    </div>

    <div v-else class="min-h-0 flex-1 overflow-hidden">
      <CodeEditor
        v-model="content"
        :language="fileLanguage(activeFile)"
        class="h-full"
      />
    </div>
  </div>
</template>
