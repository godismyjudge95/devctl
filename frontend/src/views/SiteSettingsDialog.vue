<script setup lang="ts">
import { ref, watch } from 'vue'
import type { Site } from '@/lib/api'
import { getPHPVersions, detectSite } from '@/lib/api'
import { useSitesStore } from '@/stores/sites'
import { toast } from 'vue-sonner'
import { Loader2, Settings } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { ButtonGroup } from '@/components/ui/button-group'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
  DialogFooter, DialogDescription,
} from '@/components/ui/dialog'

const props = defineProps<{
  site: Site
  phpVersions: string[]
}>()

const emit = defineEmits<{
  (e: 'open-worktree', site: Site): void
}>()

const store = useSitesStore()

const open = ref(false)
const saving = ref(false)

const form = ref({
  domain: '',
  root_path: '',
  public_dir: '',
  php_version: '',
  aliases: '',
  spx_enabled: false,
})
const detectedFramework = ref('')

function openDialog() {
  const aliases = (() => {
    try { return (JSON.parse(props.site.aliases) as string[]).join(', ') }
    catch { return '' }
  })()
  form.value = {
    domain: props.site.domain,
    root_path: props.site.root_path,
    public_dir: props.site.public_dir,
    php_version: props.site.php_version,
    aliases,
    spx_enabled: props.site.spx_enabled === 1,
  }
  detectedFramework.value = props.site.framework ?? ''
  open.value = true
}

async function onRootPathBlur() {
  const path = form.value.root_path.trim()
  if (!path || path === props.site.root_path) return
  try {
    const result = await detectSite(path)
    if (!form.value.public_dir) form.value.public_dir = result.public_dir
    detectedFramework.value = result.framework
  } catch {
    // non-fatal
  }
}

async function save() {
  saving.value = true
  try {
    const aliasList = form.value.aliases
      ? form.value.aliases.split(',').map((a) => a.trim()).filter(Boolean)
      : []
    const spxChanged = form.value.spx_enabled !== (props.site.spx_enabled === 1)
    await store.update(props.site.id, {
      domain: form.value.domain,
      root_path: form.value.root_path,
      public_dir: form.value.public_dir,
      php_version: form.value.php_version,
      aliases: aliasList,
      https: props.site.https,
      spx_enabled: form.value.spx_enabled ? 1 : 0,
    })
    if (spxChanged) {
      await store.toggleSPX(props.site.id, form.value.spx_enabled)
    }
    toast.success(`${form.value.domain} settings saved`)
    open.value = false
  } catch (e: any) {
    toast.error('Failed to save settings', { description: e.message })
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <Button variant="ghost" size="sm" class="text-muted-foreground hover:text-foreground gap-1.5" @click="openDialog">
    <Settings class="w-3.5 h-3.5" />
    Settings
  </Button>

  <Dialog v-model:open="open">
    <DialogContent class="sm:max-w-md">
      <DialogHeader>
        <DialogTitle>Site Settings</DialogTitle>
        <DialogDescription class="font-mono text-xs">{{ site.domain }}</DialogDescription>
      </DialogHeader>

      <div class="grid gap-4 py-2">
        <div class="grid gap-1.5">
          <Label for="sd-domain">Domain</Label>
          <Input id="sd-domain" v-model="form.domain" placeholder="myapp.test" />
        </div>

        <div class="grid gap-1.5">
          <Label for="sd-root">Root Path</Label>
          <Input
            id="sd-root"
            v-model="form.root_path"
            placeholder="/home/user/sites/myapp"
            class="font-mono"
            @blur="onRootPathBlur"
          />
        </div>

        <div class="grid gap-1.5">
          <Label for="sd-public">
            Public Directory
            <span class="text-muted-foreground font-normal">(optional)</span>
          </Label>
          <Input id="sd-public" v-model="form.public_dir" placeholder="public" class="font-mono" />
          <p v-if="detectedFramework" class="text-xs text-muted-foreground">
            Detected: <span class="capitalize">{{ detectedFramework }}</span>
          </p>
        </div>

        <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div class="grid gap-1.5">
            <Label for="sd-php">PHP Version</Label>
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
          </div>

          <div class="grid gap-1.5">
            <Label for="sd-aliases">Aliases</Label>
            <Input id="sd-aliases" v-model="form.aliases" placeholder="www.myapp.test" />
          </div>
        </div>

        <div class="flex items-center gap-2">
          <Checkbox id="sd-spx" v-model:checked="form.spx_enabled" />
          <Label for="sd-spx" class="cursor-pointer">
            Enable SPX Profiler
            <span class="text-muted-foreground font-normal text-xs">(activates via cookie/query param)</span>
          </Label>
        </div>

        <!-- Worktree button — only for non-worktree, git-backed sites -->
        <div v-if="!site.parent_site_id && site.is_git_repo" class="border-t pt-3">
          <Button
            variant="outline"
            size="sm"
            class="w-full"
            @click="emit('open-worktree', site); open = false"
          >
            Add Git Worktree
          </Button>
        </div>
      </div>

      <DialogFooter>
        <ButtonGroup>
          <Button variant="outline" @click="open = false" :disabled="saving">Cancel</Button>
          <Button @click="save" :disabled="!form.domain || !form.root_path || saving">
            <Loader2 v-if="saving" class="w-4 h-4 animate-spin mr-1" />
            {{ saving ? 'Saving…' : 'Save' }}
          </Button>
        </ButtonGroup>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
