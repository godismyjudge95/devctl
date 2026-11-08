<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useSitesStore } from '@/stores/sites'
import { getPHPVersions, detectSite } from '@/lib/api'
import { toast } from 'vue-sonner'
import { Loader2 } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import PageHeader from '@/components/layout/PageHeader.vue'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import SettingRow from '@/components/layout/SettingRow.vue'

const store = useSitesStore()
const route = useRoute()
const router = useRouter()

// A proxy site forwards to a dev server on a port instead of serving PHP.
const isProxy = ref(route.query.type === 'proxy')

const phpVersions = ref<string[]>([])
const fallbackVersions = ['8.4', '8.3', '8.2', '8.1']
const creating = ref(false)
const form = ref({
  domain: '', root_path: '', php_version: '8.3', https: true, aliases: '', public_dir: '',
  upstream: '', preserve_host: false,
})
const detectedFramework = ref('')
const canCreate = computed(() =>
  !!form.value.domain && !!(isProxy.value ? form.value.upstream : form.value.root_path),
)

function setType(proxy: boolean) {
  isProxy.value = proxy
  router.replace({ query: proxy ? { type: 'proxy' } : {} })
}

onMounted(async () => {
  try {
    const data = await getPHPVersions()
    phpVersions.value = data.length > 0 ? data.map((v) => v.version) : fallbackVersions
  } catch {
    phpVersions.value = fallbackVersions
  }
  if (!phpVersions.value.includes(form.value.php_version)) {
    form.value.php_version = phpVersions.value[0] ?? '8.3'
  }
})

async function onRootPathBlur() {
  const path = form.value.root_path.trim()
  if (!path) return
  try {
    const result = await detectSite(path)
    form.value.public_dir = result.public_dir
    detectedFramework.value = result.framework
  } catch {
    // Detection failure is non-fatal.
  }
}

async function addSite() {
  if (!canCreate.value) return
  creating.value = true
  const f = form.value
  const aliases = f.aliases ? f.aliases.split(',').map((a: string) => a.trim()).filter(Boolean) : []
  try {
    await store.create(isProxy.value
      ? {
          domain: f.domain,
          https: f.https ? 1 : 0,
          aliases,
          proxy: { upstream: f.upstream, preserve_host: f.preserve_host },
        }
      : {
          domain: f.domain,
          root_path: f.root_path,
          php_version: f.php_version,
          https: f.https ? 1 : 0,
          aliases,
          public_dir: f.public_dir,
        })
    toast.success(`Site ${form.value.domain} created`)
    router.push('/sites')
  } catch (e: any) {
    toast.error('Failed to create site', { description: e.message })
  } finally {
    creating.value = false
  }
}
</script>

<template>
  <div class="space-y-6">
    <PageHeader
      :title="isProxy ? 'Add proxy site' : 'Add site'"
      :description="isProxy
        ? 'Serve a dev server on a local port at a .test hostname with HTTPS.'
        : 'Configure a new local PHP virtual host.'"
      back-to="/sites"
      back-label="Sites"
    />

    <Card>
      <CardHeader class="border-b">
        <CardTitle>Virtual host</CardTitle>
        <CardDescription>
          {{ isProxy ? 'Domain and the URL that receives its requests.' : 'Domain, document root, and PHP version.' }}
        </CardDescription>
      </CardHeader>
      <CardContent class="pb-0 sm:pb-0">
        <SettingRow label="Type" hint="A proxy sends every request, WebSockets included, to a local port or URL.">
          <div class="flex gap-2" role="group" aria-label="Site type">
            <Button size="sm" :variant="isProxy ? 'outline' : 'default'" :aria-pressed="!isProxy" @click="setType(false)">
              PHP site
            </Button>
            <Button size="sm" :variant="isProxy ? 'default' : 'outline'" :aria-pressed="isProxy" @click="setType(true)">
              Proxy
            </Button>
          </div>
        </SettingRow>
        <SettingRow label="Domain" hint="Served at this .test hostname." for="domain">
          <Input id="domain" v-model="form.domain" :placeholder="isProxy ? 'vite.test' : 'myapp.test'" />
        </SettingRow>
        <template v-if="isProxy">
          <SettingRow label="Upstream URL" hint="For example http://localhost:5173 or 127.0.0.1:3000." for="upstream">
            <Input id="upstream" v-model="form.upstream" placeholder="http://localhost:5173" class="font-mono" />
          </SettingRow>
          <SettingRow
            label="Preserve Host header"
            hint="Send the .test hostname upstream. Off, the upstream sees its own host:port, which Vite accepts without allowedHosts."
            for="preserve_host"
          >
            <div class="flex h-9 items-center">
              <Checkbox id="preserve_host" v-model:checked="form.preserve_host" />
            </div>
          </SettingRow>
        </template>
        <template v-else>
          <SettingRow label="Root path" hint="Absolute path to the project on disk." for="root_path">
            <Input id="root_path" v-model="form.root_path" placeholder="/home/user/sites/myapp" class="font-mono" @blur="onRootPathBlur" />
          </SettingRow>
          <SettingRow label="Public directory" hint="Optional web root inside the project, e.g. public." for="public_dir">
            <Input id="public_dir" v-model="form.public_dir" placeholder="public" class="font-mono" />
            <p v-if="detectedFramework" class="mt-2 text-xs text-muted-foreground">
              Detected: <span class="capitalize">{{ detectedFramework }}</span>
            </p>
          </SettingRow>
          <SettingRow label="PHP version" for="php_version">
            <Select v-model="form.php_version">
              <SelectTrigger id="php_version" class="w-full sm:w-48">
                <SelectValue placeholder="Select version" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="ver in phpVersions" :key="ver" :value="ver">
                  PHP {{ ver }}
                </SelectItem>
              </SelectContent>
            </Select>
          </SettingRow>
        </template>
        <SettingRow label="Aliases" hint="Comma-separated extra hostnames." for="aliases">
          <Input id="aliases" v-model="form.aliases" placeholder="www.myapp.test" />
        </SettingRow>
        <SettingRow label="Force HTTPS" hint="Redirect HTTP to HTTPS for this site." for="https">
          <div class="flex h-9 items-center">
            <Checkbox id="https" v-model:checked="form.https" />
          </div>
        </SettingRow>
      </CardContent>
    </Card>

    <div class="flex flex-wrap items-center justify-end gap-2">
      <Button variant="outline" :disabled="creating" @click="router.push('/sites')">Cancel</Button>
      <Button :disabled="!canCreate || creating" @click="addSite">
        <Loader2 v-if="creating" class="size-4 animate-spin" />
        {{ creating ? 'Creating…' : 'Create site' }}
      </Button>
    </div>
  </div>
</template>
