<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { Site } from '@/lib/api'
import { getPHPVersions, detectSite } from '@/lib/api'
import { useSitesStore } from '@/stores/sites'
import { toast } from 'vue-sonner'
import { GitFork, Loader2 } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import PageHeader from '@/components/layout/PageHeader.vue'
import Surface from '@/components/layout/Surface.vue'
import SectionHeader from '@/components/layout/SectionHeader.vue'
import SettingRow from '@/components/layout/SettingRow.vue'
import EmptyState from '@/components/layout/EmptyState.vue'

const route = useRoute()
const router = useRouter()
const store = useSitesStore()

const phpVersions = ref<string[]>([])
const fallbackVersions = ['8.4', '8.3', '8.2', '8.1']
const saving = ref(false)
const detectedFramework = ref('')

const form = reactive({
  domain: '',
  root_path: '',
  public_dir: '',
  php_version: '',
  aliases: '',
  spx_enabled: false,
  https: true,
  cors: false,
})

const siteId = computed(() => route.params.id as string)
const site = computed<Site | undefined>(() => store.sites.find(s => s.id === siteId.value))

function hydrate(s: Site) {
  const aliases = (() => {
    try { return (JSON.parse(s.aliases) as string[]).join(', ') }
    catch { return '' }
  })()
  Object.assign(form, {
    domain: s.domain,
    root_path: s.root_path,
    public_dir: s.public_dir,
    php_version: s.php_version,
    aliases,
    spx_enabled: s.spx_enabled === 1,
    https: s.https === 1,
    cors: s.cors === 1,
  })
  detectedFramework.value = s.framework ?? ''
}

onMounted(async () => {
  if (store.sites.length === 0) await store.load()
  try {
    const data = await getPHPVersions()
    phpVersions.value = data.length > 0 ? data.map((v) => v.version) : fallbackVersions
  } catch {
    phpVersions.value = fallbackVersions
  }
  if (site.value) hydrate(site.value)
})

watch(site, (s) => {
  if (s) hydrate(s)
})

async function onRootPathBlur() {
  if (!site.value) return
  const path = form.root_path.trim()
  if (!path || path === site.value.root_path) return
  try {
    const result = await detectSite(path)
    if (!form.public_dir) form.public_dir = result.public_dir
    detectedFramework.value = result.framework
  } catch {
    // non-fatal
  }
}

async function save() {
  if (!site.value) return
  saving.value = true
  try {
    const aliasList = form.aliases
      ? form.aliases.split(',').map((a) => a.trim()).filter(Boolean)
      : []
    const spxChanged = form.spx_enabled !== (site.value.spx_enabled === 1)
    await store.update(site.value.id, {
      domain: form.domain,
      root_path: form.root_path,
      public_dir: form.public_dir,
      php_version: form.php_version,
      aliases: aliasList,
      https: form.https ? 1 : 0,
      cors: form.cors ? 1 : 0,
      spx_enabled: form.spx_enabled ? 1 : 0,
    })
    if (spxChanged) {
      await store.toggleSPX(site.value.id, form.spx_enabled)
    }
    toast.success(`${form.domain} settings saved`)
    router.push('/sites')
  } catch (e: any) {
    toast.error('Failed to save settings', { description: e.message })
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="space-y-6">
    <PageHeader
      title="Site settings"
      :description="site?.domain ?? 'Loading…'"
      back-to="/sites"
      back-label="Sites"
    >
      <template #actions>
        <Button variant="outline" @click="router.push('/sites')" :disabled="saving">Cancel</Button>
        <Button @click="save" :disabled="!site || !form.domain || !form.root_path || saving">
          <Loader2 v-if="saving" class="w-4 h-4 animate-spin" />
          {{ saving ? 'Saving…' : 'Save' }}
        </Button>
      </template>
    </PageHeader>

    <div v-if="store.loading && !site" class="text-sm text-muted-foreground py-8 text-center">Loading…</div>
    <EmptyState v-else-if="!site" title="Site not found">
      This site is gone or the id is wrong.
    </EmptyState>

    <template v-else>
      <Surface>
        <SectionHeader title="Host" description="Domain, document root, and PHP version for this site." />
        <div class="px-5 pb-2">
          <SettingRow label="Domain" for="sd-domain">
            <Input id="sd-domain" v-model="form.domain" placeholder="myapp.test" />
          </SettingRow>
          <SettingRow label="Root path" hint="Absolute path to the project on disk." for="sd-root">
            <Input
              id="sd-root"
              v-model="form.root_path"
              placeholder="/home/user/sites/myapp"
              class="font-mono"
              @blur="onRootPathBlur"
            />
          </SettingRow>
          <SettingRow label="Public directory" hint="Optional web root inside the project." for="sd-public">
            <Input id="sd-public" v-model="form.public_dir" placeholder="public" class="font-mono" />
            <p v-if="detectedFramework" class="text-xs text-muted-foreground mt-1.5">
              Detected: <span class="capitalize">{{ detectedFramework }}</span>
            </p>
          </SettingRow>
          <SettingRow label="PHP version" for="sd-php">
            <Select v-model="form.php_version">
              <SelectTrigger id="sd-php">
                <SelectValue placeholder="Select version" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="ver in phpVersions" :key="ver" :value="ver">
                  PHP {{ ver }}
                </SelectItem>
              </SelectContent>
            </Select>
          </SettingRow>
          <SettingRow label="Aliases" hint="Comma-separated extra hostnames." for="sd-aliases">
            <Input id="sd-aliases" v-model="form.aliases" placeholder="www.myapp.test" />
          </SettingRow>
        </div>
      </Surface>

      <Surface>
        <SectionHeader title="Options" description="TLS, profiler, and CORS for this host." />
        <div class="px-5 pb-2">
          <SettingRow label="Force HTTPS" hint="Redirect HTTP to HTTPS." for="sd-https">
            <div class="flex items-center h-9">
              <Checkbox id="sd-https" v-model:checked="form.https" />
            </div>
          </SettingRow>
          <SettingRow label="SPX profiler" hint="Activates via cookie or query param." for="sd-spx">
            <div class="flex items-center h-9">
              <Checkbox id="sd-spx" v-model:checked="form.spx_enabled" />
            </div>
          </SettingRow>
          <SettingRow label="Inject CORS headers" hint="Disable when the app manages its own CORS." for="sd-cors">
            <div class="flex items-center h-9">
              <Checkbox id="sd-cors" v-model:checked="form.cors" />
            </div>
          </SettingRow>
        </div>
      </Surface>

      <Surface v-if="!site.parent_site_id && site.is_git_repo">
        <SectionHeader title="Worktrees" description="Check out another branch as its own .test host." />
        <div class="px-5 pb-5">
          <Button variant="outline" @click="router.push(`/sites/${site.id}/worktree`)">
            <GitFork class="w-4 h-4" />
            Add Git Worktree
          </Button>
        </div>
      </Surface>
    </template>
  </div>
</template>
