<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { toast } from 'vue-sonner'
import { ArrowLeft, Save } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
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
  <div class="flex flex-col h-full" @keydown="onKeydown" tabindex="-1">
    <!-- Top bar -->
    <div class="flex items-center gap-3 px-4 py-2 border-b border-border shrink-0">
      <Button variant="ghost" size="icon" class="shrink-0" @click="router.push('/services')">
        <ArrowLeft class="w-4 h-4" />
      </Button>

      <div class="flex items-center gap-2 min-w-0">
        <span class="font-medium text-sm truncate">{{ meta?.label ?? serviceId }}</span>
        <span class="text-muted-foreground text-sm">/</span>
        <span class="text-sm text-muted-foreground font-mono truncate">{{ activeFile }}</span>
      </div>

      <!-- File tabs (only shown when the service has multiple files) -->
      <Tabs
        v-if="meta && meta.files.length > 1"
        :model-value="activeFile"
        @update:model-value="(f) => { activeFile = f as string }"
        class="ml-2"
      >
        <TabsList>
          <TabsTrigger
            v-for="f in meta.files"
            :key="f.name"
            :value="f.name"
          >
            {{ f.label }}
          </TabsTrigger>
        </TabsList>
      </Tabs>

      <div class="ml-auto flex items-center gap-2 shrink-0">
        <span class="text-xs text-muted-foreground hidden sm:block">Ctrl+S to save &amp; restart</span>
        <Button size="sm" :disabled="saving || loading" @click="save">
          <Save class="w-3.5 h-3.5 mr-2" />
          {{ saving ? 'Saving…' : 'Save & Restart' }}
        </Button>
      </div>
    </div>

    <!-- Loading state -->
    <div v-if="loading" class="flex-1 flex items-center justify-center text-muted-foreground text-sm">
      Loading…
    </div>

    <!-- Editor -->
    <div v-else class="flex-1 min-h-0 overflow-hidden">
      <CodeEditor
        v-model="content"
        :language="fileLanguage(activeFile)"
        class="h-full"
      />
    </div>
  </div>
</template>
