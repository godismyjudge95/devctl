<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useSitesStore } from '@/stores/sites'
import { getPHPVersions } from '@/lib/api'
import type { Site } from '@/lib/api'
import { toast } from 'vue-sonner'
import { Plus, ExternalLink, Trash2, Shield, Zap, Bot, Loader2 } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import {
  Card, CardContent, CardHeader, CardTitle, CardDescription,
} from '@/components/ui/card'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
  DialogFooter, DialogDescription,
} from '@/components/ui/dialog'

const store = useSitesStore()
onMounted(() => {
  store.load()
  loadVersions()
})

const phpVersions = ref<string[]>([])
const fallbackVersions = ['8.4', '8.3', '8.2', '8.1']

async function loadVersions() {
  try {
    const data = await getPHPVersions()
    phpVersions.value = data.length > 0 ? data.map((v) => v.version) : fallbackVersions
    if (!phpVersions.value.includes(form.value.php_version)) {
      form.value.php_version = phpVersions.value[0] ?? '8.3'
    }
  } catch {
    phpVersions.value = fallbackVersions
  }
}

const dialogOpen = ref(false)
const creating = ref(false)
const removingId = ref<string | null>(null)
const updatingPhpId = ref<string | null>(null)
const form = ref({ domain: '', root_path: '', php_version: '8.3', https: true, aliases: '' })

async function addSite() {
  if (!form.value.domain || !form.value.root_path) return
  creating.value = true
  try {
    await store.create({
      domain: form.value.domain,
      root_path: form.value.root_path,
      php_version: form.value.php_version,
      https: form.value.https ? 1 : 0,
      aliases: form.value.aliases ? form.value.aliases.split(',').map((a: string) => a.trim()) : [],
    })
    toast.success(`Site ${form.value.domain} created`)
    dialogOpen.value = false
    form.value = { domain: '', root_path: '', php_version: phpVersions.value[0] ?? '8.3', https: true, aliases: '' }
  } catch (e: any) {
    toast.error('Failed to create site', { description: e.message })
  } finally {
    creating.value = false
  }
}

async function removeSite(id: string, domain: string) {
  removingId.value = id
  try {
    await store.remove(id)
    toast.success(`Site ${domain} removed`)
  } catch (e: any) {
    toast.error(`Failed to remove ${domain}`, { description: e.message })
  } finally {
    removingId.value = null
  }
}

async function changePhpVersion(site: Site, ver: string) {
  const aliases = (() => { try { return JSON.parse(site.aliases) } catch { return [] } })()
  updatingPhpId.value = site.id
  try {
    await store.update(site.id, {
      domain: site.domain,
      root_path: site.root_path,
      php_version: ver,
      https: site.https,
      spx_enabled: site.spx_enabled,
      aliases,
    })
    toast.success(`${site.domain} switched to PHP ${ver}`)
  } catch (e: any) {
    toast.error(`Failed to change PHP version`, { description: e.message })
  } finally {
    updatingPhpId.value = null
  }
}
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-semibold tracking-tight">Sites</h1>
        <p class="text-sm text-muted-foreground mt-1">Manage local PHP virtual hosts.</p>
      </div>
      <Button @click="dialogOpen = true">
        <Plus class="w-4 h-4" />
        Add Site
      </Button>
    </div>

    <div v-if="store.error" class="rounded-md border border-destructive/50 bg-destructive/10 px-4 py-3 text-sm text-destructive">
      {{ store.error }}
    </div>

    <div v-if="store.loading" class="text-muted-foreground text-sm py-8 text-center">Loading…</div>

    <div v-else class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
      <Card
        v-for="site in store.sites"
        :key="site.id"
        class="hover:border-primary/40 transition-colors"
      >
        <CardHeader class="pb-2">
          <div class="flex items-start justify-between gap-2">
            <CardTitle class="text-base leading-snug">
              <a
                :href="(site.https ? 'https' : 'http') + '://' + site.domain"
                target="_blank"
                class="hover:underline inline-flex items-center gap-1.5"
              >
                {{ site.domain }}
                <ExternalLink class="w-3.5 h-3.5 text-muted-foreground" />
              </a>
            </CardTitle>
            <Select
              :model-value="site.php_version"
              :disabled="updatingPhpId === site.id"
              @update:model-value="(v) => v && changePhpVersion(site, String(v))"
            >
              <SelectTrigger class="h-6 w-28 text-xs font-mono px-2 py-0">
                <Loader2 v-if="updatingPhpId === site.id" class="w-3 h-3 animate-spin mr-1" />
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem v-for="ver in phpVersions" :key="ver" :value="ver" class="text-xs font-mono">
                  PHP {{ ver }}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>
          <CardDescription class="font-mono text-xs truncate">{{ site.root_path }}</CardDescription>
        </CardHeader>
        <CardContent>
          <div class="flex items-center gap-2 flex-wrap">
            <Badge v-if="site.https" variant="success">
              <Shield class="w-3 h-3 mr-1" />HTTPS
            </Badge>
            <Badge v-if="site.spx_enabled" variant="secondary">
              <Zap class="w-3 h-3 mr-1" />SPX
            </Badge>
            <Badge v-if="site.auto_discovered" variant="outline">
              <Bot class="w-3 h-3 mr-1" />Auto
            </Badge>
            <Button
              variant="ghost" size="sm"
              class="ml-auto text-destructive hover:text-destructive hover:bg-destructive/10 h-7 px-2"
              :disabled="removingId === site.id"
              @click="removeSite(site.id, site.domain)"
            >
              <Loader2 v-if="removingId === site.id" class="w-3.5 h-3.5 animate-spin" />
              <Trash2 v-else class="w-3.5 h-3.5" />
            </Button>
          </div>
        </CardContent>
      </Card>

      <div v-if="store.sites.length === 0 && !store.loading"
        class="col-span-full rounded-lg border border-dashed border-border py-16 text-center text-muted-foreground text-sm">
        No sites configured. Click <strong>Add Site</strong> or drop a folder in your watch directory.
      </div>
    </div>

    <!-- Add Site Dialog -->
    <Dialog v-model:open="dialogOpen">
      <DialogContent class="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Add Site</DialogTitle>
          <DialogDescription>Configure a new local PHP virtual host.</DialogDescription>
        </DialogHeader>
        <div class="grid gap-4 py-2">
          <div class="grid gap-1.5">
            <Label for="domain">Domain</Label>
            <Input id="domain" v-model="form.domain" placeholder="myapp.test" />
          </div>
          <div class="grid gap-1.5">
            <Label for="root_path">Root Path</Label>
            <Input id="root_path" v-model="form.root_path" placeholder="/home/user/sites/myapp" class="font-mono" />
          </div>
          <div class="grid grid-cols-2 gap-3">
            <div class="grid gap-1.5">
              <Label for="php_version">PHP Version</Label>
              <Select v-model="form.php_version">
                <SelectTrigger id="php_version">
                  <SelectValue placeholder="Select version" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem v-for="ver in phpVersions" :key="ver" :value="ver">
                    PHP {{ ver }}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div class="grid gap-1.5">
              <Label for="aliases">Aliases</Label>
              <Input id="aliases" v-model="form.aliases" placeholder="www.myapp.test" />
            </div>
          </div>
          <div class="flex items-center gap-2">
            <input type="checkbox" id="https" v-model="form.https" class="rounded" />
            <Label for="https" class="cursor-pointer">Enable HTTPS</Label>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" @click="dialogOpen = false" :disabled="creating">Cancel</Button>
          <Button @click="addSite" :disabled="!form.domain || !form.root_path || creating">
            <Loader2 v-if="creating" class="w-4 h-4 animate-spin mr-1" />
            {{ creating ? 'Creating…' : 'Create' }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
