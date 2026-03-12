<script setup lang="ts">
import { onMounted } from 'vue'
import { useSettingsStore } from '@/stores/settings'
import { Download, ShieldCheck } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import { Separator } from '@/components/ui/separator'

const store = useSettingsStore()
onMounted(() => store.load())

function save(key: string, value: string) {
  store.save({ [key]: value })
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
        <CardContent class="grid grid-cols-2 gap-4">
          <div class="grid gap-1.5">
            <Label for="devctl_host">Bind Host</Label>
            <Input
              id="devctl_host"
              :value="store.settings['devctl_host']"
              @change="save('devctl_host', ($event.target as HTMLInputElement).value)"
              class="font-mono"
            />
          </div>
          <div class="grid gap-1.5">
            <Label for="devctl_port">Port</Label>
            <Input
              id="devctl_port"
              :value="store.settings['devctl_port']"
              @change="save('devctl_port', ($event.target as HTMLInputElement).value)"
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
              :value="store.settings['sites_watch_dir']"
              @change="save('sites_watch_dir', ($event.target as HTMLInputElement).value)"
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
        <CardContent class="flex gap-2">
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
              :value="store.settings['dump_tcp_port']"
              @change="save('dump_tcp_port', ($event.target as HTMLInputElement).value)"
              class="font-mono"
            />
          </div>
        </CardContent>
      </Card>

    </div>
  </div>
</template>
