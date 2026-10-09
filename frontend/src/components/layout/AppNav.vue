<script setup lang="ts">
import type { Component } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { ArrowUpCircle, Download, Moon, Sun } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import StatusDot from '@/components/layout/StatusDot.vue'

export interface NavItem {
  path: string
  label: string
  icon: Component
}

export interface NavGroup {
  label: string
  items: NavItem[]
}

export interface NavBadge {
  value: number | string
  tone: 'muted' | 'alert' | 'live'
}

defineProps<{
  groups: NavGroup[]
  badge: (path: string) => NavBadge | null
  version: string
  updateAvailable: boolean
  latestVersion: string
  updating: boolean
  installable: boolean
  isDark: boolean
}>()

const emit = defineEmits<{
  navigate: []
  update: []
  install: []
  toggleDark: []
}>()

const route = useRoute()
</script>

<template>
  <div class="flex h-full flex-col">
    <div class="flex h-14 shrink-0 items-center gap-2.5 px-4">
      <img src="/logo-transparent.png" class="size-6 shrink-0" alt="" />
      <div class="min-w-0 leading-tight">
        <div class="text-sm font-semibold tracking-tight">devctl</div>
        <div class="font-mono text-xs text-muted-foreground">{{ version || 'dev' }}</div>
      </div>
    </div>

    <nav class="flex-1 space-y-5 overflow-y-auto px-3 py-2" aria-label="Main">
      <div v-for="group in groups" :key="group.label">
        <div class="mb-1 px-2 text-xs font-medium text-muted-foreground">{{ group.label }}</div>
        <div class="space-y-0.5">
          <RouterLink
            v-for="item in group.items"
            :key="item.path"
            :to="item.path"
            class="flex h-8 items-center gap-2.5 rounded-lg px-2 text-sm transition-colors"
            :class="route.path.startsWith(item.path)
              ? 'bg-sidebar-accent font-medium text-sidebar-accent-foreground'
              : 'text-muted-foreground hover:bg-sidebar-accent/60 hover:text-sidebar-foreground'"
            @click="emit('navigate')"
          >
            <component :is="item.icon" class="size-4 shrink-0" />
            <span class="truncate">{{ item.label }}</span>
            <span
              v-if="badge(item.path)"
              class="ml-auto flex h-5 min-w-5 items-center justify-center rounded-full px-1.5 text-xs font-medium tabular-nums"
              :class="{
                'bg-muted text-muted-foreground': badge(item.path)?.tone === 'muted',
                'bg-destructive-soft text-destructive-soft-foreground': badge(item.path)?.tone === 'alert',
                'bg-success-soft text-success-soft-foreground': badge(item.path)?.tone === 'live',
              }"
            >{{ badge(item.path)?.value }}</span>
          </RouterLink>
        </div>
      </div>
    </nav>

    <div class="shrink-0 space-y-2 border-t border-sidebar-border p-3">
      <Button
        v-if="updateAvailable"
        variant="outline"
        size="sm"
        class="w-full justify-start border-warning/40 text-warning-soft-foreground hover:bg-warning-soft hover:text-warning-soft-foreground"
        :disabled="updating"
        :title="`Update to ${latestVersion}`"
        @click="emit('update')"
      >
        <ArrowUpCircle class="size-4" />
        Update to {{ latestVersion }}
      </Button>
      <Button
        v-if="installable"
        variant="outline"
        size="sm"
        class="w-full justify-start"
        @click="emit('install')"
      >
        <Download class="size-4" />
        Install app
      </Button>
      <div class="flex items-center justify-between pl-2">
        <StatusDot status="connected" label="Connected" />
        <Button
          variant="ghost"
          size="icon-sm"
          :aria-label="isDark ? 'Use light theme' : 'Use dark theme'"
          @click="emit('toggleDark')"
        >
          <Sun v-if="isDark" class="size-4" />
          <Moon v-else class="size-4" />
        </Button>
      </div>
    </div>
  </div>
</template>
