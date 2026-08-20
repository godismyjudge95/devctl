<script setup lang="ts">
import { computed, type Component } from 'vue'
import { Globe, HardDrive, Server } from 'lucide-vue-next'
import caddySvg from '@/assets/services/caddy.svg?raw'
import valkeySvg from '@/assets/services/valkey.svg?raw'
import postgresSvg from '@/assets/services/postgres.svg?raw'
import mysqlSvg from '@/assets/services/mysql.svg?raw'
import meilisearchSvg from '@/assets/services/meilisearch.svg?raw'
import typesenseSvg from '@/assets/services/typesense.svg?raw'
import mailpitSvg from '@/assets/services/mailpit.svg?raw'
import reverbSvg from '@/assets/services/reverb.svg?raw'
import clickhouseSvg from '@/assets/services/clickhouse.svg?raw'
import phpSvg from '@/assets/services/php.svg?raw'

const props = defineProps<{
  id: string
  size?: 'sm' | 'md'
}>()

const ICONS: Record<string, string> = {
  caddy: caddySvg,
  redis: valkeySvg,
  postgres: postgresSvg,
  mysql: mysqlSvg,
  meilisearch: meilisearchSvg,
  typesense: typesenseSvg,
  mailpit: mailpitSvg,
  reverb: reverbSvg,
  clickhouse: clickhouseSvg,
}

const FALLBACK: Record<string, Component> = {
  dns: Globe,
  maxio: HardDrive,
}

const svg = computed(() => {
  if (props.id.startsWith('php-fpm-')) return phpSvg
  return ICONS[props.id] ?? ''
})

const fallback = computed(() => {
  if (svg.value) return null
  return FALLBACK[props.id] ?? Server
})
</script>

<template>
  <span
    class="inline-flex items-center justify-center rounded-lg bg-muted/80 shrink-0 overflow-hidden"
    :class="size === 'sm' ? 'size-7' : 'size-8'"
    aria-hidden="true"
  >
    <span
      v-if="svg"
      class="inline-flex items-center justify-center size-[58%] [&_svg]:size-full"
      v-html="svg"
    />
    <component v-else :is="fallback" class="size-3.5 text-muted-foreground" />
  </span>
</template>
