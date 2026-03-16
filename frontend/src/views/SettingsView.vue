<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useSettingsStore } from '@/stores/settings'
import { Download, ShieldCheck, RotateCw } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'
import { restartDevctl } from '@/lib/api'

const store = useSettingsStore()
onMounted(() => store.load())

function save(key: string, value: string) {
  store.save({ [key]: value })
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
  const res = await fetch('/api/tls/trust', { method: 'POST' })
  if (res.ok) alert('Certificate trusted!')
  else alert('Failed to trust certificate')
}
</script>

<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-2xl font-semibold tracking-tight">Settings</h1>
      <p class="text-sm text-muted-foreground mt-1">Configure devctl. All settings take effect after restarting devctl.</p>
    </div>

    <div v-if="store.loading" class="text-muted-foreground text-sm py-8 text-center">Loading…</div>

    <div v-else class="space-y-4">

      <!-- Dashboard -->
      <Card>
        <CardHeader>
          <CardTitle>Dashboard</CardTitle>
          <CardDescription>Network binding for the devctl web UI.</CardDescription>
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

      <Separator />

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

      <Separator />

      <!-- TLS -->
      <Card>
        <CardHeader>
          <CardTitle>TLS</CardTitle>
          <CardDescription>Caddy internal CA root certificate management.</CardDescription>
        </CardHeader>
        <CardContent class="flex flex-wrap gap-2">
          <Button variant="outline" @click="downloadCert">
            <Download class="w-4 h-4" />
            Download Root Certificate
          </Button>
          <Button variant="outline" @click="trustCert">
            <ShieldCheck class="w-4 h-4" />
            Trust Certificate
          </Button>
        </CardContent>
      </Card>

      <Separator />

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

      <Separator />

      <!-- Save & Restart -->
      <div class="flex items-center gap-4">
        <Button :disabled="restarting" @click="saveAndRestart">
          <RotateCw class="w-4 h-4" :class="restarting ? 'animate-spin' : ''" />
          Save &amp; Restart
        </Button>
        <span v-if="restartStatus === 'restarting'" class="text-sm text-muted-foreground">Restarting…</span>
        <span v-else-if="restartStatus === 'reconnecting'" class="text-sm text-muted-foreground">Waiting for server…</span>
        <span v-else-if="restartStatus === 'done'" class="text-sm text-green-600">Restarted successfully.</span>
        <span v-else-if="restartStatus === 'error'" class="text-sm text-destructive">Server did not come back in time. Check journalctl.</span>
      </div>

    </div>
  </div>
</template>
