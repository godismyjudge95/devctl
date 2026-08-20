<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
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
import Surface from '@/components/layout/Surface.vue'
import SectionHeader from '@/components/layout/SectionHeader.vue'
import SettingRow from '@/components/layout/SettingRow.vue'

const store = useSitesStore()
const router = useRouter()

const phpVersions = ref<string[]>([])
const fallbackVersions = ['8.4', '8.3', '8.2', '8.1']
const creating = ref(false)
const form = ref({ domain: '', root_path: '', php_version: '8.3', https: true, aliases: '', public_dir: '' })
const detectedFramework = ref('')

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
  if (!form.value.domain || !form.value.root_path) return
  creating.value = true
  try {
    await store.create({
      domain: form.value.domain,
      root_path: form.value.root_path,
      php_version: form.value.php_version,
      https: form.value.https ? 1 : 0,
      aliases: form.value.aliases ? form.value.aliases.split(',').map((a: string) => a.trim()) : [],
      public_dir: form.value.public_dir,
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
      title="Add site"
      description="Configure a new local PHP virtual host."
      back-to="/sites"
      back-label="Sites"
    >
      <template #actions>
        <Button variant="outline" @click="router.push('/sites')" :disabled="creating">Cancel</Button>
        <Button @click="addSite" :disabled="!form.domain || !form.root_path || creating">
          <Loader2 v-if="creating" class="w-4 h-4 animate-spin" />
          {{ creating ? 'Creating…' : 'Create' }}
        </Button>
      </template>
    </PageHeader>

    <Surface>
      <SectionHeader title="Virtual host" description="Domain, document root, and PHP version." />
      <div class="px-5 pb-2">
        <SettingRow label="Domain" hint="Served at this .test hostname." for="domain">
          <Input id="domain" v-model="form.domain" placeholder="myapp.test" />
        </SettingRow>
        <SettingRow label="Root path" hint="Absolute path to the project on disk." for="root_path">
          <Input id="root_path" v-model="form.root_path" placeholder="/home/user/sites/myapp" class="font-mono" @blur="onRootPathBlur" />
        </SettingRow>
        <SettingRow label="Public directory" hint="Optional web root inside the project, e.g. public." for="public_dir">
          <Input id="public_dir" v-model="form.public_dir" placeholder="public" class="font-mono" />
          <p v-if="detectedFramework" class="text-xs text-muted-foreground mt-1.5">
            Detected: <span class="capitalize">{{ detectedFramework }}</span>
          </p>
        </SettingRow>
        <SettingRow label="PHP version" for="php_version">
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
        </SettingRow>
        <SettingRow label="Aliases" hint="Comma-separated extra hostnames." for="aliases">
          <Input id="aliases" v-model="form.aliases" placeholder="www.myapp.test" />
        </SettingRow>
        <SettingRow label="Force HTTPS" hint="Redirect HTTP to HTTPS for this site." for="https">
          <div class="flex items-center h-9">
            <Checkbox id="https" v-model:checked="form.https" />
          </div>
        </SettingRow>
      </div>
    </Surface>
  </div>
</template>
