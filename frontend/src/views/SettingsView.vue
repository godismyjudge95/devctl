<script setup lang="ts">
import { onMounted, ref, computed, watch } from 'vue'
import { useSettingsStore } from '@/stores/settings'
import { useServicesStore } from '@/stores/services'
import { Download, ShieldCheck, RotateCw, Plus, Pencil, Trash2, Database } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { ButtonGroup } from '@/components/ui/button-group'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Badge } from '@/components/ui/badge'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter,
} from '@/components/ui/dialog'
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select'
import { restartDevctl, trustTLS, getWhoDBSettings, putWhoDBSettings } from '@/lib/api'
import { toast } from 'vue-sonner'
import type { WhoDBManualConnection, WhoDBAutoConnection } from '@/lib/api'

const store = useSettingsStore()
const servicesStore = useServicesStore()
onMounted(() => {
  store.load()
  if (servicesStore.whodbInstalled) loadWhoDB()
})

// Load WhoDB settings once the service state arrives via SSE (may not be ready at mount time).
watch(() => servicesStore.whodbInstalled, (installed) => {
  if (installed && whodbAutoConns.value.length === 0 && !whodbLoading.value) {
    loadWhoDB()
  }
})

async function save(key: string, value: string) {
  try {
    await store.save({ [key]: value })
    toast.success('Setting saved')
  } catch (e: any) {
    toast.error('Failed to save setting', { description: e.message })
  }
}

const restarting = ref(false)
const restartStatus = ref<'idle' | 'restarting' | 'reconnecting' | 'done' | 'error'>('idle')

async function saveAndRestart() {
  restarting.value = true
  restartStatus.value = 'restarting'
  try {
    await restartDevctl()
  } catch {
    // The process may die before it can send a response — that's fine.
  }
  restartStatus.value = 'reconnecting'
  // Poll /api/settings until the server comes back up.
  const deadline = Date.now() + 15_000
  while (Date.now() < deadline) {
    await new Promise(r => setTimeout(r, 800))
    try {
      const res = await fetch('/api/settings')
      if (res.ok) {
        await store.load()
        restartStatus.value = 'done'
        restarting.value = false
        setTimeout(() => { restartStatus.value = 'idle' }, 3000)
        return
      }
    } catch {
      // server not up yet — keep polling
    }
  }
  restartStatus.value = 'error'
  restarting.value = false
}

const trusting = ref(false)
const trustStatus = ref<'idle' | 'working' | 'done' | 'error'>('idle')
const trustMessage = ref('')

async function downloadCert() {
  const res = await fetch('/api/tls/cert')
  if (!res.ok) { alert('Failed to fetch certificate'); return }
  const blob = await res.blob()
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = 'devctl-root.crt'
  a.click()
}

async function trustCert() {
  trusting.value = true
  trustStatus.value = 'working'
  trustMessage.value = ''
  try {
    const result = await trustTLS()
    trustStatus.value = 'done'
    trustMessage.value = result.output || 'Certificate trusted successfully.'
  } catch (e: unknown) {
    trustStatus.value = 'error'
    trustMessage.value = e instanceof Error ? e.message : 'Failed to trust certificate.'
  } finally {
    trusting.value = false
    setTimeout(() => { trustStatus.value = 'idle'; trustMessage.value = '' }, 8000)
  }
}

// ── WhoDB settings ─────────────────────────────────────────────────────────

const whodbLoading = ref(false)
const whodbSaving = ref(false)
const whodbError = ref('')
const whodbDisableCredForm = ref(false)
const whodbAutoConns = ref<WhoDBAutoConnection[]>([])
const whodbManualConns = ref<WhoDBManualConnection[]>([])

async function loadWhoDB() {
  whodbLoading.value = true
  whodbError.value = ''
  try {
    const s = await getWhoDBSettings()
    whodbDisableCredForm.value = s.disable_credential_form
    whodbAutoConns.value = s.auto_connections ?? []
    whodbManualConns.value = s.manual_connections ?? []
  } catch (e: unknown) {
    whodbError.value = e instanceof Error ? e.message : 'Failed to load WhoDB settings.'
  } finally {
    whodbLoading.value = false
  }
}

async function saveWhoDB() {
  whodbSaving.value = true
  whodbError.value = ''
  try {
    await putWhoDBSettings({
      disable_credential_form: whodbDisableCredForm.value,
      manual_connections: whodbManualConns.value,
    })
  } catch (e: unknown) {
    whodbError.value = e instanceof Error ? e.message : 'Failed to save WhoDB settings.'
  } finally {
    whodbSaving.value = false
  }
}

// ── Manual connection dialog ────────────────────────────────────────────────

const connDialogOpen = ref(false)
const connDialogMode = ref<'add' | 'edit'>('add')
const connDialogIndex = ref(-1)
const connForm = ref<WhoDBManualConnection>({ type: 'postgres', conn: { alias: '' } })

function openAddConn() {
  connDialogMode.value = 'add'
  connDialogIndex.value = -1
  connForm.value = { type: 'postgres', conn: { alias: '', host: '127.0.0.1', port: '', username: '', password: '', database: '' } }
  connDialogOpen.value = true
}

function openEditConn(idx: number) {
  connDialogMode.value = 'edit'
  connDialogIndex.value = idx
  const c = whodbManualConns.value[idx]!
  connForm.value = { type: c.type, conn: { ...c.conn } }
  connDialogOpen.value = true
}

function deleteConn(idx: number) {
  whodbManualConns.value.splice(idx, 1)
  saveWhoDB()
}

async function saveConn() {
  if (connDialogMode.value === 'add') {
    whodbManualConns.value.push({ ...connForm.value, conn: { ...connForm.value.conn } })
  } else {
    whodbManualConns.value[connDialogIndex.value] = { ...connForm.value, conn: { ...connForm.value.conn } }
  }
  connDialogOpen.value = false
  await saveWhoDB()
}

const connTypeLabel: Record<string, string> = {
  postgres: 'PostgreSQL',
  mysql: 'MySQL',
  redis: 'Redis',
  sqlite3: 'SQLite',
  mongodb: 'MongoDB',
}

const connTypeBadgeVariant = (type: string): 'default' | 'secondary' | 'outline' | 'destructive' => {
  const map: Record<string, 'default' | 'secondary' | 'outline' | 'destructive'> = {
    postgres: 'default',
    mysql: 'secondary',
    redis: 'secondary',
  }
  return map[type] ?? 'outline'
}

// Whether the current connection type needs host/user/pass/db fields
const connNeedsCredentials = computed(() => connForm.value.type !== 'redis')
const connNeedsDatabase = computed(() => connForm.value.type !== 'redis')

function defaultPortForType(type: string): string {
  const m: Record<string, string> = { postgres: '5432', mysql: '3306', redis: '6379', mongodb: '27017' }
  return m[type] ?? ''
}

function onConnTypeChange(type: string) {
  connForm.value.type = type
  connForm.value.conn.port = defaultPortForType(type)
  if (type === 'redis') {
    connForm.value.conn.username = ''
    connForm.value.conn.password = ''
    connForm.value.conn.database = ''
  }
}
</script>

<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-2xl font-semibold tracking-tight">Settings</h1>
    </div>

    <div v-if="store.loading" class="text-muted-foreground text-sm py-8 text-center">Loading…</div>

    <div v-else class="space-y-6">

      <!-- Dashboard -->
      <Card>
        <CardHeader>
          <CardTitle>Dashboard</CardTitle>
          <CardDescription>Address and port the devctl UI listens on.</CardDescription>
        </CardHeader>
        <CardContent class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div class="grid gap-1.5">
            <Label for="devctl_host">Bind Host</Label>
            <Input
              id="devctl_host"
              v-model="store.settings['devctl_host']"
              @change="save('devctl_host', store.settings['devctl_host'] ?? '')"
              class="font-mono"
            />
          </div>
          <div class="grid gap-1.5">
            <Label for="devctl_port">Port</Label>
            <Input
              id="devctl_port"
              v-model="store.settings['devctl_port']"
              @change="save('devctl_port', store.settings['devctl_port'] ?? '')"
              class="font-mono"
            />
          </div>
        </CardContent>
      </Card>

      <!-- Sites -->
      <Card>
        <CardHeader>
          <CardTitle>Sites</CardTitle>
          <CardDescription>Root directory watched for auto-discovered sites.</CardDescription>
        </CardHeader>
        <CardContent>
          <div class="grid gap-1.5">
            <Label for="sites_watch_dir">Watch Directory</Label>
            <Input
              id="sites_watch_dir"
              v-model="store.settings['sites_watch_dir']"
              @change="save('sites_watch_dir', store.settings['sites_watch_dir'] ?? '')"
              placeholder="$HOME/sites"
              class="font-mono"
            />
          </div>
        </CardContent>
      </Card>

      <!-- TLS -->
      <Card>
        <CardHeader>
          <CardTitle>TLS</CardTitle>
          <CardDescription>Caddy internal CA root certificate management.</CardDescription>
        </CardHeader>
        <CardContent class="space-y-3">
          <div class="flex flex-wrap gap-2">
            <Button variant="outline" @click="downloadCert">
              <Download class="w-4 h-4" />
              Download Root Certificate
            </Button>
            <Button variant="outline" :disabled="trusting" @click="trustCert">
              <ShieldCheck class="w-4 h-4" :class="trusting ? 'animate-pulse' : ''" />
              {{ trusting ? 'Trusting…' : 'Trust Certificate' }}
            </Button>
          </div>
          <p v-if="trustStatus === 'done'" class="text-sm text-success whitespace-pre-wrap">{{ trustMessage }}</p>
          <p v-else-if="trustStatus === 'error'" class="text-sm text-destructive whitespace-pre-wrap">{{ trustMessage }}</p>
          <p v-else-if="trustStatus === 'working'" class="text-sm text-muted-foreground">Installing certificate into system and browser trust stores…</p>
        </CardContent>
      </Card>

      <!-- Dump Server -->
      <Card>
        <CardHeader>
          <CardTitle>PHP Dump Server</CardTitle>
          <CardDescription>TCP listener for dump() / dd() calls.</CardDescription>
        </CardHeader>
        <CardContent>
          <div class="grid gap-1.5 max-w-xs">
            <Label for="dump_tcp_port">TCP Port</Label>
            <Input
              id="dump_tcp_port"
              v-model="store.settings['dump_tcp_port']"
              @change="save('dump_tcp_port', store.settings['dump_tcp_port'] ?? '')"
              class="font-mono"
            />
          </div>
        </CardContent>
      </Card>

      <!-- WhoDB (only shown when installed) -->
      <template v-if="servicesStore.whodbInstalled">
        <Card>
          <CardHeader>
            <CardTitle class="flex items-center gap-2">
              <Database class="w-5 h-5" />
              WhoDB
            </CardTitle>
            <CardDescription>Configure database connections for the WhoDB explorer.</CardDescription>
          </CardHeader>
          <CardContent class="space-y-6">

            <div v-if="whodbLoading" class="text-muted-foreground text-sm">Loading WhoDB settings…</div>
            <div v-else-if="whodbError" class="text-destructive text-sm">{{ whodbError }}</div>

            <template v-else>
              <!-- Disable credential form toggle -->
              <div class="flex items-center gap-2">
                <Checkbox
                  id="whodb_disable_cred"
                  :checked="whodbDisableCredForm"
                  @update:checked="(v: boolean) => { whodbDisableCredForm = v; saveWhoDB() }"
                />
                <div>
                  <Label for="whodb_disable_cred" class="cursor-pointer">Disable credential form</Label>
                  <p class="text-xs text-muted-foreground mt-0.5">Hide the login form in the WhoDB UI — useful when all connections are pre-configured.</p>
                </div>
              </div>

              <!-- Auto-detected connections -->
              <div>
                <p class="text-sm font-medium mb-2">Auto-detected connections</p>
                <div v-if="whodbAutoConns.length === 0" class="text-sm text-muted-foreground">
                  No connections detected. Install PostgreSQL, MySQL, or Valkey to auto-populate connections.
                </div>
                <div v-else class="space-y-2">
                  <div
                    v-for="ac in whodbAutoConns"
                    :key="ac.source + ac.type"
                    class="flex items-center gap-3 text-sm px-3 py-2 rounded-md border bg-muted/40"
                  >
                    <Badge :variant="connTypeBadgeVariant(ac.type)" class="shrink-0 capitalize">
                      {{ connTypeLabel[ac.type] ?? ac.type }}
                    </Badge>
                    <span class="font-mono text-xs text-muted-foreground grow truncate">
                      {{ ac.conn.alias }}
                      <template v-if="ac.conn.host"> · {{ ac.conn.host }}:{{ ac.conn.port }}</template>
                    </span>
                    <span class="text-xs text-muted-foreground shrink-0">from {{ ac.source }}</span>
                  </div>
                </div>
              </div>

              <!-- Manual connections -->
              <div>
                <div class="flex items-center justify-between mb-2">
                  <p class="text-sm font-medium">Manual connections</p>
                  <Button variant="outline" size="sm" @click="openAddConn">
                    <Plus class="w-3.5 h-3.5" />
                    Add Connection
                  </Button>
                </div>
                <div v-if="whodbManualConns.length === 0" class="text-sm text-muted-foreground">
                  No manual connections configured.
                </div>
                <div v-else class="space-y-2">
                  <div
                    v-for="(mc, idx) in whodbManualConns"
                    :key="idx"
                    class="flex items-center gap-3 text-sm px-3 py-2 rounded-md border"
                  >
                    <Badge :variant="connTypeBadgeVariant(mc.type)" class="shrink-0 capitalize">
                      {{ connTypeLabel[mc.type] ?? mc.type }}
                    </Badge>
                    <span class="font-mono text-xs text-muted-foreground grow truncate">
                      {{ mc.conn.alias }}
                      <template v-if="mc.conn.host"> · {{ mc.conn.host }}:{{ mc.conn.port }}</template>
                    </span>
                    <div class="flex gap-1 shrink-0">
                      <Button variant="ghost" size="icon-xs" @click="openEditConn(idx)">
                        <Pencil class="w-3.5 h-3.5" />
                      </Button>
                      <Button variant="ghost" size="icon-xs" class="text-destructive hover:text-destructive" @click="deleteConn(idx)">
                        <Trash2 class="w-3.5 h-3.5" />
                      </Button>
                    </div>
                  </div>
                </div>
              </div>

              <p v-if="whodbError" class="text-sm text-destructive">{{ whodbError }}</p>
            </template>
          </CardContent>
        </Card>
      </template>

      <!-- Save & Restart -->
      <div class="flex items-center gap-4 pt-2">
        <Button :disabled="restarting" @click="saveAndRestart">
          <RotateCw class="w-4 h-4" :class="restarting ? 'animate-spin' : ''" />
          Save &amp; Restart
        </Button>
        <span v-if="restartStatus === 'idle'" class="text-xs text-muted-foreground">All settings take effect after restarting.</span>
        <span v-else-if="restartStatus === 'restarting'" class="text-sm text-muted-foreground">Restarting…</span>
        <span v-else-if="restartStatus === 'reconnecting'" class="text-sm text-muted-foreground">Waiting for server…</span>
        <span v-else-if="restartStatus === 'done'" class="text-sm text-success">Restarted successfully.</span>
        <span v-else-if="restartStatus === 'error'" class="text-sm text-destructive">Server did not come back in time. Check journalctl.</span>
      </div>

    </div>
  </div>

  <!-- Add / Edit connection dialog -->
  <Dialog v-model:open="connDialogOpen">
    <DialogContent class="sm:max-w-md">
      <DialogHeader>
        <DialogTitle>{{ connDialogMode === 'add' ? 'Add Connection' : 'Edit Connection' }}</DialogTitle>
      </DialogHeader>

      <div class="space-y-4 py-2">
        <!-- Type -->
        <div class="grid gap-1.5">
          <Label>Type</Label>
          <Select :model-value="connForm.type" @update:model-value="(v) => onConnTypeChange(String(v))">
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="postgres">PostgreSQL</SelectItem>
              <SelectItem value="mysql">MySQL</SelectItem>
              <SelectItem value="redis">Redis</SelectItem>
              <SelectItem value="sqlite3">SQLite</SelectItem>
              <SelectItem value="mongodb">MongoDB</SelectItem>
            </SelectContent>
          </Select>
        </div>

        <!-- Alias -->
        <div class="grid gap-1.5">
          <Label for="conn_alias">Alias</Label>
          <Input id="conn_alias" v-model="connForm.conn.alias" placeholder="my-db" class="font-mono" />
        </div>

        <template v-if="connForm.type !== 'sqlite3'">
          <!-- Host + Port -->
          <div class="grid grid-cols-3 gap-3">
            <div class="col-span-2 grid gap-1.5">
              <Label for="conn_host">Host</Label>
              <Input id="conn_host" v-model="connForm.conn.host" placeholder="127.0.0.1" class="font-mono" />
            </div>
            <div class="grid gap-1.5">
              <Label for="conn_port">Port</Label>
              <Input id="conn_port" v-model="connForm.conn.port" class="font-mono" />
            </div>
          </div>

          <!-- Username + Password (not for Redis) -->
          <template v-if="connNeedsCredentials">
            <div class="grid grid-cols-2 gap-3">
              <div class="grid gap-1.5">
                <Label for="conn_user">Username</Label>
                <Input id="conn_user" v-model="connForm.conn.username" class="font-mono" />
              </div>
              <div class="grid gap-1.5">
                <Label for="conn_pass">Password</Label>
                <Input id="conn_pass" v-model="connForm.conn.password" type="password" class="font-mono" />
              </div>
            </div>
          </template>

          <!-- Database (not for Redis) -->
          <template v-if="connNeedsDatabase">
            <div class="grid gap-1.5">
              <Label for="conn_db">Database</Label>
              <Input id="conn_db" v-model="connForm.conn.database" class="font-mono" />
            </div>
          </template>
        </template>

        <!-- SQLite path -->
        <template v-if="connForm.type === 'sqlite3'">
          <div class="grid gap-1.5">
            <Label for="conn_db_path">Database file path</Label>
            <Input id="conn_db_path" v-model="connForm.conn.database" placeholder="/path/to/file.db" class="font-mono" />
          </div>
        </template>
      </div>

      <DialogFooter class="gap-2">
        <Button variant="outline" size="sm" @click="connDialogOpen = false">Cancel</Button>
        <Button size="sm" :disabled="!connForm.conn.alias" @click="saveConn">
          {{ connDialogMode === 'add' ? 'Add' : 'Save' }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
