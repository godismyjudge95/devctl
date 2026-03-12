<script setup lang="ts">
import { ref, onMounted } from 'vue'
import {
  getPHPVersions, installPHP, uninstallPHP,
  startPHPVersion, stopPHPVersion, restartPHPVersion,
  getPHPSettings, setPHPSettings,
  type PHPVersion, type PHPSettings,
} from '@/lib/api'
import { Plus, Trash2, Play, Square, RotateCcw, Loader2 } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
  DialogFooter, DialogDescription,
} from '@/components/ui/dialog'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import { toast } from 'vue-sonner'

const versions = ref<PHPVersion[]>([])
const settings = ref<PHPSettings>({ upload_max_filesize: '128M', memory_limit: '256M', max_execution_time: '120', post_max_size: '128M' })
const loading = ref(true)
const settingsSaving = ref(false)
const settingsSaved = ref(false)
const error = ref<string | null>(null)

const installOpen = ref(false)
const installVer = ref('')
const installing = ref(false)
const installError = ref<string | null>(null)

const uninstallTarget = ref<string | null>(null)
const uninstalling = ref(false)

// Per-version pending action: 'start' | 'stop' | 'restart' | null
const pending = ref<Record<string, string>>({})

const KNOWN_VERSIONS = ['8.4', '8.3', '8.2', '8.1', '8.0', '7.4']

async function load() {
  loading.value = true
  error.value = null
  try {
    const [v, s] = await Promise.all([getPHPVersions(), getPHPSettings()])
    versions.value = v
    settings.value = s
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

function availableToInstall() {
  const installed = new Set(versions.value.map(v => v.version))
  return KNOWN_VERSIONS.filter(v => !installed.has(v))
}

function openInstall() {
  installVer.value = availableToInstall()[0] ?? ''
  installError.value = null
  installOpen.value = true
}

async function doInstall() {
  if (!installVer.value) return
  installing.value = true
  installError.value = null
  try {
    versions.value = await installPHP(installVer.value)
    installOpen.value = false
    toast.success(`PHP ${installVer.value} installed`)
  } catch (e: unknown) {
    installError.value = e instanceof Error ? e.message : String(e)
  } finally {
    installing.value = false
  }
}

async function doUninstall() {
  if (!uninstallTarget.value) return
  const ver = uninstallTarget.value
  uninstalling.value = true
  try {
    await uninstallPHP(ver)
    versions.value = versions.value.filter(v => v.version !== ver)
    uninstallTarget.value = null
    toast.success(`PHP ${ver} uninstalled`)
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    uninstalling.value = false
  }
}

async function doStart(ver: string) {
  pending.value[ver] = 'start'
  try {
    await startPHPVersion(ver)
    const item = versions.value.find(v => v.version === ver)
    if (item) item.status = 'running'
    toast.success(`PHP ${ver} FPM started`)
  } catch (e: any) {
    toast.error(`Failed to start PHP ${ver}`, { description: e.message })
  } finally {
    delete pending.value[ver]
  }
}

async function doStop(ver: string) {
  pending.value[ver] = 'stop'
  try {
    await stopPHPVersion(ver)
    const item = versions.value.find(v => v.version === ver)
    if (item) item.status = 'stopped'
    toast.success(`PHP ${ver} FPM stopped`)
  } catch (e: any) {
    toast.error(`Failed to stop PHP ${ver}`, { description: e.message })
  } finally {
    delete pending.value[ver]
  }
}

async function doRestart(ver: string) {
  pending.value[ver] = 'restart'
  try {
    await restartPHPVersion(ver)
    const item = versions.value.find(v => v.version === ver)
    if (item) item.status = 'running'
    toast.success(`PHP ${ver} FPM restarted`)
  } catch (e: any) {
    toast.error(`Failed to restart PHP ${ver}`, { description: e.message })
  } finally {
    delete pending.value[ver]
  }
}

function statusVariant(status: string): 'success' | 'destructive' | 'secondary' {
  if (status === 'running') return 'success'
  if (status === 'stopped') return 'destructive'
  return 'secondary'
}

async function saveSettings() {
  settingsSaving.value = true
  settingsSaved.value = false
  error.value = null
  try {
    settings.value = await setPHPSettings(settings.value)
    settingsSaved.value = true
    setTimeout(() => { settingsSaved.value = false }, 2500)
    toast.success('PHP settings saved')
  } catch (e: unknown) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    settingsSaving.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="space-y-6">
    <div class="flex items-center justify-between">
      <div>
        <h1 class="text-2xl font-semibold tracking-tight">PHP</h1>
        <p class="text-sm text-muted-foreground mt-1">Manage installed PHP-FPM versions and global settings.</p>
      </div>
      <Button @click="openInstall" :disabled="availableToInstall().length === 0">
        <Plus class="w-4 h-4" />
        Install version
      </Button>
    </div>

    <div v-if="error" class="rounded-md border border-destructive/50 bg-destructive/10 px-4 py-3 text-sm text-destructive">
      {{ error }}
    </div>

    <div v-if="loading" class="text-muted-foreground text-sm py-8 text-center">Loading…</div>

    <div v-else-if="versions.length > 0" class="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
      <Card v-for="v in versions" :key="v.version">
        <CardHeader class="pb-3">
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-2">
              <CardTitle class="font-mono text-xl">PHP {{ v.version }}</CardTitle>
              <Badge :variant="statusVariant(v.status ?? 'unknown')" class="text-xs">
                <span class="flex items-center gap-1">
                  <Loader2 v-if="pending[v.version]" class="w-2.5 h-2.5 animate-spin" />
                  <span v-else class="inline-block w-1.5 h-1.5 rounded-full"
                    :class="v.status === 'running' ? 'bg-green-600' : v.status === 'stopped' ? 'bg-red-400' : 'bg-amber-400'"
                  />
                  {{ pending[v.version] ? pending[v.version] + 'ing…' : (v.status ?? 'unknown') }}
                </span>
              </Badge>
            </div>
            <Button
              variant="ghost" size="sm"
              class="text-destructive hover:text-destructive hover:bg-destructive/10 h-8 w-8 p-0"
              @click="uninstallTarget = v.version"
              :disabled="!!pending[v.version]"
            >
              <Trash2 class="w-4 h-4" />
            </Button>
          </div>
          <CardDescription class="font-mono text-xs">{{ v.fpm_socket }}</CardDescription>
        </CardHeader>
        <CardContent>
          <div class="flex items-center gap-1 flex-wrap">
            <Button
              v-if="v.status !== 'running'"
              variant="outline" size="sm"
              :disabled="!!pending[v.version]"
              @click="doStart(v.version)"
            >
              <Loader2 v-if="pending[v.version] === 'start'" class="w-3.5 h-3.5 animate-spin" />
              <Play v-else class="w-3.5 h-3.5" />
              Start
            </Button>
            <Button
              v-if="v.status === 'running'"
              variant="outline" size="sm"
              :disabled="!!pending[v.version]"
              @click="doStop(v.version)"
            >
              <Loader2 v-if="pending[v.version] === 'stop'" class="w-3.5 h-3.5 animate-spin" />
              <Square v-else class="w-3.5 h-3.5" />
              Stop
            </Button>
            <Button
              variant="ghost" size="sm"
              :disabled="!!pending[v.version]"
              @click="doRestart(v.version)"
            >
              <Loader2 v-if="pending[v.version] === 'restart'" class="w-3.5 h-3.5 animate-spin" />
              <RotateCcw v-else class="w-3.5 h-3.5" />
              Restart
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>

    <div v-else class="rounded-lg border border-dashed border-border py-16 text-center space-y-3">
      <p class="text-muted-foreground text-sm">No PHP versions installed.</p>
      <Button variant="outline" @click="openInstall">Install PHP</Button>
    </div>

    <!-- Global settings card -->
    <Card>
      <CardHeader>
        <CardTitle>Global php.ini Settings</CardTitle>
        <CardDescription>Applied to all installed PHP-FPM versions on save. Restart each version to apply.</CardDescription>
      </CardHeader>
      <CardContent class="space-y-4">
        <div class="grid gap-4 sm:grid-cols-2">
          <div class="grid gap-1.5">
            <Label for="memory_limit">Memory limit</Label>
            <Input id="memory_limit" v-model="settings.memory_limit" placeholder="256M" class="font-mono" />
          </div>
          <div class="grid gap-1.5">
            <Label for="upload_max_filesize">Upload max filesize</Label>
            <Input id="upload_max_filesize" v-model="settings.upload_max_filesize" placeholder="128M" class="font-mono" />
          </div>
          <div class="grid gap-1.5">
            <Label for="post_max_size">Post max size</Label>
            <Input id="post_max_size" v-model="settings.post_max_size" placeholder="128M" class="font-mono" />
          </div>
          <div class="grid gap-1.5">
            <Label for="max_execution_time">Max execution time (s)</Label>
            <Input id="max_execution_time" v-model="settings.max_execution_time" placeholder="120" class="font-mono" />
          </div>
        </div>
        <div class="flex items-center gap-3 pt-1">
          <Button @click="saveSettings" :disabled="settingsSaving">
            {{ settingsSaving ? 'Saving…' : 'Save settings' }}
          </Button>
          <span v-if="settingsSaved" class="text-sm text-green-600">Saved.</span>
        </div>
      </CardContent>
    </Card>

    <!-- Install Dialog -->
    <Dialog v-model:open="installOpen">
      <DialogContent class="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>Install PHP</DialogTitle>
          <DialogDescription>
            Installs php{{ installVer }}-fpm plus common extensions via apt-get. Requires root (devctl runs as root).
          </DialogDescription>
        </DialogHeader>
        <div class="grid gap-1.5 py-2">
          <Label>Version</Label>
          <Select v-model="installVer">
            <SelectTrigger>
              <SelectValue placeholder="Select version" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="ver in availableToInstall()" :key="ver" :value="ver">
                PHP {{ ver }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div v-if="installError" class="text-xs text-destructive">{{ installError }}</div>
        <DialogFooter>
          <Button variant="outline" @click="installOpen = false">Cancel</Button>
          <Button @click="doInstall" :disabled="installing || !installVer">
            {{ installing ? 'Installing…' : 'Install' }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>

    <!-- Uninstall Confirm Dialog -->
    <Dialog :open="!!uninstallTarget" @update:open="(v) => { if (!v) uninstallTarget = null }">
      <DialogContent class="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>Uninstall PHP {{ uninstallTarget }}</DialogTitle>
          <DialogDescription>
            This will stop the FPM process and purge <code class="font-mono">php{{ uninstallTarget }}-*</code> packages.
            Sites using this version will stop working.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" @click="uninstallTarget = null">Cancel</Button>
          <Button variant="destructive" @click="doUninstall" :disabled="uninstalling">
            {{ uninstalling ? 'Uninstalling…' : 'Uninstall' }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
