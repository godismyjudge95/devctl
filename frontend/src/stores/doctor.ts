import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import type { DoctorCheck } from '@/lib/api'
import { fixDoctor, getDoctor } from '@/lib/api'

export const useDoctorStore = defineStore('doctor', () => {
  const checks = ref<DoctorCheck[]>([])
  const loading = ref(false)
  // Check IDs with a fix in flight; '*' while "Fix all" runs.
  const fixing = ref<Set<string>>(new Set())

  const failed = computed(() => checks.value.filter(c => c.severity === 'fail').length)
  const warnings = computed(() => checks.value.filter(c => c.severity === 'warn').length)
  const passed = computed(() => checks.value.filter(c => c.severity === 'ok').length)
  const fixable = computed(() => checks.value.filter(c => c.fixable))

  async function run() {
    loading.value = true
    try {
      checks.value = (await getDoctor()).checks
    } finally {
      loading.value = false
    }
  }

  // Returns the fix outcomes so the view can report failures.
  async function fix(id?: string) {
    const key = id ?? '*'
    fixing.value = new Set(fixing.value).add(key)
    try {
      const res = await fixDoctor(id ? [id] : [])
      checks.value = res.checks
      return res.fixes
    } finally {
      const next = new Set(fixing.value)
      next.delete(key)
      fixing.value = next
    }
  }

  return { checks, loading, fixing, failed, warnings, passed, fixable, run, fix }
})
