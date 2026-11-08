<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { ProjectConfigField, Site } from '@/lib/api'
import { getPHPVersions, detectSite } from '@/lib/api'
import { useSitesStore } from '@/stores/sites'
import { useNodeStore } from '@/stores/node'
import { toast } from 'vue-sonner'
import { GitFork, Loader2 } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import PageHeader from '@/components/layout/PageHeader.vue'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import SettingRow from '@/components/layout/SettingRow.vue'
import EmptyState from '@/components/layout/EmptyState.vue'
import SiteWorkers from '@/components/SiteWorkers.vue'
import SiteProjectConfig from '@/components/SiteProjectConfig.vue'
import ManagedByConfig from '@/components/ManagedByConfig.vue'

const route = useRoute()
const router = useRouter()
const store = useSitesStore()
const nodeStore = useNodeStore()

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
  node_version: '',
  upstream: '',
  preserve_host: false,
})

const siteId = computed(() => route.params.id as string)
const site = computed<Site | undefined>(() => store.sites.find(s => s.id === siteId.value))
const isProxy = computed(() => !!site.value?.proxy)

const configFiles = computed(() => site.value?.project_config?.files ?? [])
const managed = computed(() => new Set<ProjectConfigField>(site.value?.project_config?.managed ?? []))
const isManaged = (f: ProjectConfigField) => managed.value.has(f)

const defaultNode = computed(() => nodeStore.versions.find(v => v.default)?.version ?? '')
const nodeStatus = computed(() => {
  const n = site.value?.node
  if (!n?.version) {
    return defaultNode.value
      ? `No version file. Uses the default, ${defaultNode.value}.`
      : 'No version file and no Node version installed.'
  }
  const from = n.file.split('/').pop()
  return n.resolved
    ? `Runs ${n.resolved} (from ${from}).`
    : `Node ${n.version} (from ${from}) is not installed. Install it on the Helpers page.`
})

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
    node_version: s.node?.version ?? '',
    upstream: s.proxy?.upstream ?? '',
    preserve_host: s.proxy?.preserve_host ?? false,
  })
  detectedFramework.value = s.framework ?? ''
}

onMounted(async () => {
  if (store.sites.length === 0) await store.load()
  nodeStore.fetchAll().catch(() => {})
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
    const spxEnabled = form.spx_enabled
    const spxChanged = spxEnabled !== (site.value.spx_enabled === 1)
    const nodeVersion = form.node_version.trim()
    const nodeChanged = !isManaged('node') && nodeVersion !== '' && nodeVersion !== (site.value.node?.version ?? '')
    const siteId = site.value.id
    const domain = form.domain
    if (isProxy.value) {
      await store.update(siteId, {
        domain: form.domain,
        aliases: aliasList,
        https: form.https ? 1 : 0,
        cors: form.cors ? 1 : 0,
        proxy: { upstream: form.upstream, preserve_host: form.preserve_host },
      })
      toast.success(`${domain} settings saved`)
      router.push('/sites')
      return
    }
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
      await store.toggleSPX(siteId, spxEnabled)
    }
    if (nodeChanged) {
      await nodeStore.setSiteVersion(siteId, nodeVersion)
    }
    toast.success(`${domain} settings saved`)
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
    />

    <div v-if="store.loading && !site" class="flex items-center justify-center gap-2 py-10 text-sm text-muted-foreground">
      <Loader2 class="size-4 animate-spin" />
      Loading…
    </div>
    <EmptyState v-else-if="!site" title="Site not found">
      This site is gone or the id is wrong.
    </EmptyState>

    <template v-else>
      <Card>
        <CardHeader class="border-b">
          <CardTitle>Host</CardTitle>
          <CardDescription>
            {{ isProxy
              ? 'Domain and the URL that receives its requests, WebSockets included.'
              : 'Domain, document root, and PHP and Node versions for this site.' }}
          </CardDescription>
        </CardHeader>
        <CardContent class="pb-0 sm:pb-0">
          <SettingRow label="Domain" for="sd-domain">
            <Input id="sd-domain" v-model="form.domain" placeholder="myapp.test" />
          </SettingRow>
          <template v-if="isProxy">
            <SettingRow label="Upstream URL" hint="For example http://localhost:5173 or 127.0.0.1:3000." for="sd-upstream">
              <Input id="sd-upstream" v-model="form.upstream" placeholder="http://localhost:5173" class="font-mono" />
            </SettingRow>
            <SettingRow
              label="Preserve Host header"
              hint="Send the .test hostname upstream. Off, the upstream sees its own host:port, which Vite accepts without allowedHosts."
              for="sd-preserve-host"
            >
              <div class="flex h-9 items-center">
                <Checkbox id="sd-preserve-host" v-model:checked="form.preserve_host" />
              </div>
            </SettingRow>
          </template>
          <template v-else>
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
              <Input id="sd-public" v-model="form.public_dir" placeholder="public" class="font-mono" :disabled="isManaged('public_dir')" />
              <ManagedByConfig v-if="isManaged('public_dir')" field="public_dir" :files="configFiles" />
              <p v-if="detectedFramework" class="mt-2 text-xs text-muted-foreground">
                Detected: <span class="capitalize">{{ detectedFramework }}</span>
              </p>
            </SettingRow>
            <SettingRow label="PHP version" for="sd-php">
              <Select v-model="form.php_version" :disabled="isManaged('php_version')">
                <SelectTrigger id="sd-php" class="w-full sm:w-48">
                  <SelectValue placeholder="Select version" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem v-for="ver in phpVersions" :key="ver" :value="ver">
                    PHP {{ ver }}
                  </SelectItem>
                </SelectContent>
              </Select>
              <ManagedByConfig v-if="isManaged('php_version')" field="php_version" :files="configFiles" />
            </SettingRow>
            <SettingRow
              label="Node version"
              hint="Saving writes .nvmrc. Use 22, 20.11.1, lts/*, or a range like >=20."
              for="sd-node"
            >
              <Input
                id="sd-node"
                v-model="form.node_version"
                :placeholder="defaultNode ? `Default (${defaultNode})` : 'Default'"
                class="font-mono sm:w-48"
                :disabled="isManaged('node')"
              />
              <p data-testid="site-node-status" class="mt-2 text-xs text-muted-foreground">{{ nodeStatus }}</p>
              <ManagedByConfig v-if="isManaged('node')" field="node" :files="configFiles" />
            </SettingRow>
          </template>
          <SettingRow label="Aliases" hint="Comma-separated extra hostnames." for="sd-aliases">
            <Input id="sd-aliases" v-model="form.aliases" placeholder="www.myapp.test" :disabled="isManaged('aliases')" />
            <ManagedByConfig v-if="isManaged('aliases')" field="aliases" :files="configFiles" />
          </SettingRow>
        </CardContent>
      </Card>

      <Card>
        <CardHeader class="border-b">
          <CardTitle>Options</CardTitle>
          <CardDescription>TLS, profiler, and CORS for this host.</CardDescription>
        </CardHeader>
        <CardContent class="pb-0 sm:pb-0">
          <SettingRow label="Force HTTPS" hint="Redirect HTTP to HTTPS." for="sd-https">
            <div class="flex h-9 items-center">
              <Checkbox id="sd-https" v-model:checked="form.https" :disabled="isManaged('https')" />
            </div>
            <ManagedByConfig v-if="isManaged('https')" field="https" :files="configFiles" />
          </SettingRow>
          <SettingRow v-if="!isProxy" label="SPX profiler" hint="Activates via cookie or query param." for="sd-spx">
            <div class="flex h-9 items-center">
              <Checkbox id="sd-spx" v-model:checked="form.spx_enabled" :disabled="isManaged('spx_enabled')" />
            </div>
            <ManagedByConfig v-if="isManaged('spx_enabled')" field="spx_enabled" :files="configFiles" />
          </SettingRow>
          <SettingRow label="Inject CORS headers" hint="Disable when the app manages its own CORS." for="sd-cors">
            <div class="flex h-9 items-center">
              <Checkbox id="sd-cors" v-model:checked="form.cors" :disabled="isManaged('cors')" />
            </div>
            <ManagedByConfig v-if="isManaged('cors')" field="cors" :files="configFiles" />
          </SettingRow>
        </CardContent>
      </Card>

      <SiteProjectConfig v-if="site.project_config" :config="site.project_config" />

      <SiteWorkers v-if="!isProxy" :site-id="site.id" :managed-by="isManaged('workers') ? configFiles : undefined" />

      <Card v-if="!site.parent_site_id && site.is_git_repo">
        <CardHeader>
          <CardTitle>Worktrees</CardTitle>
          <CardDescription>Check out another branch as its own .test host.</CardDescription>
        </CardHeader>
        <CardContent>
          <Button variant="outline" size="sm" @click="router.push(`/sites/${site.id}/worktree`)">
            <GitFork class="size-3.5" />
            Add git worktree
          </Button>
        </CardContent>
      </Card>

      <div class="flex flex-wrap items-center justify-end gap-2">
        <Button variant="outline" :disabled="saving" @click="router.push('/sites')">Cancel</Button>
        <Button :disabled="!form.domain || !(isProxy ? form.upstream : form.root_path) || saving" @click="save">
          <Loader2 v-if="saving" class="size-4 animate-spin" />
          {{ saving ? 'Saving…' : 'Save' }}
        </Button>
      </div>
    </template>
  </div>
</template>
