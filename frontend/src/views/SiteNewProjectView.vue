<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { CheckCircle2, ExternalLink, Loader2, XCircle } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Progress } from '@/components/ui/progress'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import PageHeader from '@/components/layout/PageHeader.vue'
import SettingRow from '@/components/layout/SettingRow.vue'
import { useSitesStore } from '@/stores/sites'
import {
  getPHPVersions, getSiteTemplates, newSiteStream,
  type NewSiteDone, type NewSiteStep, type SiteTemplate,
} from '@/lib/api'

const router = useRouter()
const sitesStore = useSitesStore()

const DEFAULT_PHP = 'default'
const NAME_PATTERN = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/

const templates = ref<SiteTemplate[]>([])
const loadError = ref('')
const phpVersions = ref<string[]>([])

const templateId = ref('')
const name = ref('')
const phpVersion = ref(DEFAULT_PHP)
const options = ref<Record<string, string>>({})

type Phase = 'form' | 'running' | 'done' | 'failed'
const phase = ref<Phase>('form')
const step = ref<NewSiteStep | null>(null)
const output = ref('')
const errorMessage = ref('')
const result = ref<NewSiteDone | null>(null)
const logEl = ref<HTMLElement | null>(null)
let stream: AbortController | null = null

const template = computed(() => templates.value.find(t => t.id === templateId.value))
const nameValid = computed(() => NAME_PATTERN.test(name.value))
const nameError = computed(() =>
  name.value && !nameValid.value ? 'Use lowercase letters, digits, and hyphens.' : '',
)
const progress = computed(() => (step.value ? Math.round(((step.value.index - 1) / step.value.total) * 100) : 0))

onMounted(async () => {
  try {
    templates.value = await getSiteTemplates()
    if (templates.value[0]) templateId.value = templates.value[0].id
  } catch (e: any) {
    loadError.value = e.message
  }
  try {
    phpVersions.value = (await getPHPVersions()).map(v => v.version)
  } catch {
    // The default PHP version still works without the list.
  }
})

onBeforeUnmount(() => stream?.abort())

// Each template has its own options; reset them to its defaults on switch.
watch(template, (t) => {
  options.value = Object.fromEntries((t?.options ?? []).map(o => [o.id, o.default]))
})

watch(output, async () => {
  await nextTick()
  if (logEl.value) logEl.value.scrollTop = logEl.value.scrollHeight
})

function create() {
  if (!template.value || !nameValid.value) return
  phase.value = 'running'
  step.value = null
  output.value = ''
  errorMessage.value = ''
  stream = newSiteStream(
    {
      name: name.value,
      template: template.value.id,
      php_version: phpVersion.value === DEFAULT_PHP ? undefined : phpVersion.value,
      options: options.value,
    },
    {
      onOutput: (chunk) => { output.value += chunk },
      onStep: (s) => { step.value = s },
      onDone: (res) => {
        result.value = res
        phase.value = 'done'
        sitesStore.load()
      },
      onError: (message) => {
        errorMessage.value = message
        phase.value = 'failed'
      },
    },
  )
}
</script>

<template>
  <div class="space-y-6">
    <PageHeader
      title="New site"
      description="Create a project from a template in your sites directory and serve it at https://name.test."
      back-to="/sites"
      back-label="Sites"
    />

    <p v-if="loadError" class="text-sm text-destructive">Could not load templates: {{ loadError }}</p>

    <template v-if="phase === 'form'">
      <Card>
        <CardHeader class="border-b">
          <CardTitle>Template</CardTitle>
          <CardDescription>What the project starts from.</CardDescription>
        </CardHeader>
        <CardContent>
          <div role="radiogroup" aria-label="Template" class="grid gap-3 sm:grid-cols-2">
            <button
              v-for="t in templates"
              :key="t.id"
              type="button"
              role="radio"
              :aria-checked="t.id === templateId"
              :class="[
                'rounded-lg border p-4 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
                t.id === templateId ? 'border-primary bg-primary/5' : 'border-border hover:bg-muted/50',
              ]"
              @click="templateId = t.id"
            >
              <p class="text-sm font-medium">{{ t.label }}</p>
              <p class="mt-1 text-xs text-muted-foreground">{{ t.description }}</p>
            </button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader class="border-b">
          <CardTitle>Project</CardTitle>
          <CardDescription>The directory name is also the domain.</CardDescription>
        </CardHeader>
        <CardContent class="pb-0 sm:pb-0">
          <SettingRow label="Name" :hint="nameValid ? `Served at https://${name}.test` : 'Lowercase letters, digits, and hyphens.'" for="name">
            <Input id="name" v-model.trim="name" placeholder="myapp" class="font-mono" autocomplete="off" />
            <p v-if="nameError" class="mt-2 text-xs text-destructive">{{ nameError }}</p>
          </SettingRow>
          <SettingRow label="PHP version" for="php_version">
            <Select v-model="phpVersion">
              <SelectTrigger id="php_version" class="w-full sm:w-56">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem :value="DEFAULT_PHP">Newest installed</SelectItem>
                <SelectItem v-for="v in phpVersions" :key="v" :value="v">PHP {{ v }}</SelectItem>
              </SelectContent>
            </Select>
          </SettingRow>
          <SettingRow v-for="o in template?.options ?? []" :key="`${templateId}-${o.id}`" :label="o.label" :for="`opt-${o.id}`">
            <Select v-if="o.choices?.length" v-model="options[o.id]">
              <SelectTrigger :id="`opt-${o.id}`" class="w-full sm:w-56">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="c in o.choices" :key="c.value" :value="c.value">{{ c.label }}</SelectItem>
              </SelectContent>
            </Select>
            <Input v-else :id="`opt-${o.id}`" v-model="options[o.id]" autocomplete="off" />
          </SettingRow>
        </CardContent>
      </Card>

      <div class="flex flex-wrap items-center justify-end gap-2">
        <Button variant="outline" @click="router.push('/sites')">Cancel</Button>
        <Button :disabled="!template || !nameValid" @click="create">Create site</Button>
      </div>
    </template>

    <Card v-else>
      <CardHeader class="border-b">
        <CardTitle class="flex items-center gap-2">
          <Loader2 v-if="phase === 'running'" class="size-4 animate-spin" />
          <CheckCircle2 v-else-if="phase === 'done'" class="size-4 text-emerald-600" />
          <XCircle v-else class="size-4 text-destructive" />
          <span v-if="phase === 'running'">Creating {{ name }}.test</span>
          <span v-else-if="phase === 'done'">{{ result?.site.domain }} is ready</span>
          <span v-else>Could not create {{ name }}.test</span>
        </CardTitle>
        <CardDescription v-if="phase === 'running'" data-testid="new-site-step">
          <template v-if="step">Step {{ step.index }} of {{ step.total }}: {{ step.label }}</template>
          <template v-else>Starting…</template>
        </CardDescription>
        <CardDescription v-else-if="phase === 'failed'" class="text-destructive" data-testid="new-site-error">
          {{ errorMessage }}
        </CardDescription>
        <CardDescription v-else>{{ result?.site.root_path }}</CardDescription>
      </CardHeader>
      <CardContent class="space-y-4">
        <Progress v-if="phase === 'running'" :model-value="progress" />
        <pre
          v-if="output"
          ref="logEl"
          aria-label="Output"
          class="max-h-80 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-muted p-3 font-mono text-xs"
        >{{ output }}</pre>
        <div class="flex flex-wrap items-center justify-end gap-2">
          <template v-if="phase === 'done' && result">
            <Button variant="outline" @click="router.push(`/sites/${result.site.id}`)">Site settings</Button>
            <Button as="a" :href="result.url" target="_blank" rel="noopener">
              <ExternalLink class="size-3.5" />
              Open {{ result.url }}
            </Button>
          </template>
          <template v-else-if="phase === 'failed'">
            <Button variant="outline" @click="router.push('/sites')">Back to sites</Button>
            <Button @click="phase = 'form'">Edit and retry</Button>
          </template>
        </div>
      </CardContent>
    </Card>
  </div>
</template>
