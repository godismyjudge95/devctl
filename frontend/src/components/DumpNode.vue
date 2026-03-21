<script setup lang="ts">
import { ref } from 'vue'

const props = defineProps<{ node: any; depth: number }>()
const expanded = ref(true)

const indent = (d: number) => '  '.repeat(d)

function visibilityPrefix(v: string) {
  if (v === 'public') return '+'
  if (v === 'protected') return '#'
  return '-'
}

function visibilityClass(v: string) {
  if (v === 'public') return 'text-green-600 dark:text-green-400'
  if (v === 'protected') return 'text-amber-600 dark:text-amber-400'
  return 'text-red-600 dark:text-red-400'
}
</script>

<template>
  <!-- scalar -->
  <span v-if="node.type === 'scalar'">
    <span v-if="node.kind === 'null'" class="text-gray-400 dark:text-gray-500">null</span>
    <span v-else-if="node.kind === 'bool'" class="text-purple-600 dark:text-purple-400">{{ node.value ? 'true' : 'false' }}</span>
    <span v-else-if="node.kind === 'int'" class="text-blue-600 dark:text-blue-400">{{ node.value }}</span>
    <span v-else-if="node.kind === 'float'" class="text-blue-500 dark:text-blue-300">{{ node.value }}</span>
    <span v-else class="text-gray-500 dark:text-gray-400">{{ node.value }}</span>
  </span>

  <!-- string -->
  <span v-else-if="node.type === 'string'">
    <span class="text-green-600 dark:text-green-400">"{{ node.value }}<span v-if="node.truncated > 0" class="text-amber-500 dark:text-amber-300">…+{{ node.truncated }}</span>"</span>
    <span class="text-gray-400 dark:text-gray-500 text-xs ml-1">({{ node.length }})</span>
  </span>

  <!-- array -->
  <span v-else-if="node.type === 'array'">
    <button class="text-gray-500 dark:text-gray-400 hover:text-foreground" @click="expanded = !expanded">
      {{ expanded ? '▼' : '▶' }}
    </button>
    <span class="text-amber-600 dark:text-amber-400"> array:{{ node.count }}</span>
    <span v-if="expanded">
      <span class="text-gray-400 dark:text-gray-500"> [</span>
      <div v-for="(child, i) in node.children" :key="i" :style="{ marginLeft: (depth + 1) * 16 + 'px' }">
        <DumpNode :node="child.key" :depth="depth + 1" />
        <span class="text-gray-400 dark:text-gray-500"> => </span>
        <DumpNode :node="child.value" :depth="depth + 1" />
      </div>
      <span v-if="node.truncated > 0" :style="{ marginLeft: (depth + 1) * 16 + 'px' }" class="text-amber-500 dark:text-amber-300">
        …{{ node.truncated }} more
      </span>
      <div><span class="text-gray-400 dark:text-gray-500">]</span></div>
    </span>
    <span v-else class="text-gray-400 dark:text-gray-500"> […]</span>
  </span>

  <!-- object -->
  <span v-else-if="node.type === 'object'">
    <button class="text-gray-500 dark:text-gray-400 hover:text-foreground" @click="expanded = !expanded">
      {{ expanded ? '▼' : '▶' }}
    </button>
    <span class="text-blue-700 dark:text-blue-500"> {{ node.class }}</span>
    <span v-if="expanded">
      <span class="text-gray-400 dark:text-gray-500"> {</span>
      <div v-for="(child, i) in node.children" :key="i" :style="{ marginLeft: (depth + 1) * 16 + 'px' }">
        <span :class="visibilityClass(child.visibility)">{{ visibilityPrefix(child.visibility) }}</span>
        <span class="text-foreground"> {{ child.name }}</span>
        <span class="text-gray-400 dark:text-gray-500">: </span>
        <DumpNode :node="child.value" :depth="depth + 1" />
      </div>
      <span v-if="node.truncated > 0" :style="{ marginLeft: (depth + 1) * 16 + 'px' }" class="text-amber-500 dark:text-amber-300">
        …{{ node.truncated }} more
      </span>
      <div><span class="text-gray-400 dark:text-gray-500">}</span></div>
    </span>
    <span v-else class="text-gray-400 dark:text-gray-500"> {…}</span>
  </span>

  <!-- resource -->
  <span v-else-if="node.type === 'resource'" class="text-purple-600 dark:text-purple-400">
    resource({{ node.resourceType }})
  </span>

  <!-- ref -->
  <span v-else-if="node.type === 'ref'" class="text-gray-500 dark:text-gray-400">
    &amp;{{ node.refId }}
  </span>

  <span v-else class="text-gray-400 dark:text-gray-500">{{ JSON.stringify(node) }}</span>
</template>
