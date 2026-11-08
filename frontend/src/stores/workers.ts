import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { Worker, WorkerAction, WorkerType } from '@/lib/api'
import { WORKER_TYPES, createWorker, deleteWorker, listWorkers, runWorkerAction } from '@/lib/api'
import { showNotification } from '@/lib/sw'

const POLL_MS = 5000

export function workerLabel(type: WorkerType): string {
  return WORKER_TYPES.find(t => t.type === type)?.label ?? type
}

export const useWorkersStore = defineStore('workers', () => {
  const workers = ref<Worker[]>([])
  const loaded = ref(false)
  // Workers already reported as crash looping, so each loop notifies once.
  const crashLooping = new Set<number>()
  let timer: ReturnType<typeof setInterval> | null = null

  function notifyNewCrashLoops(list: Worker[], silent: boolean) {
    const now = new Set(list.filter(w => w.crash_loop).map(w => w.id))
    for (const w of list) {
      if (!w.crash_loop || crashLooping.has(w.id) || silent) continue
      showNotification(
        `${workerLabel(w.type)} worker keeps crashing`,
        `${w.domain}: crashed ${w.crashes} times in a row. devctl keeps restarting it.`,
        `devctl-worker-${w.id}`,
        `/sites/${w.site_id}#workers`,
      )
    }
    crashLooping.clear()
    now.forEach(id => crashLooping.add(id))
  }

  async function load() {
    const list = await listWorkers()
    notifyNewCrashLoops(list, !loaded.value)
    workers.value = list
    loaded.value = true
  }

  function startPolling() {
    if (timer) return
    load().catch(() => {})
    timer = setInterval(() => { load().catch(() => {}) }, POLL_MS)
  }

  function forSite(siteId: string): Worker[] {
    return workers.value.filter(w => w.site_id === siteId)
  }

  function upsert(w: Worker) {
    const i = workers.value.findIndex(x => x.id === w.id)
    if (i === -1) workers.value = [...workers.value, w]
    else workers.value.splice(i, 1, w)
    return w
  }

  async function add(siteId: string, type: WorkerType, command = '') {
    return upsert(await createWorker(siteId, type, command))
  }

  async function act(id: number, action: WorkerAction) {
    return upsert(await runWorkerAction(id, action))
  }

  async function remove(id: number) {
    await deleteWorker(id)
    workers.value = workers.value.filter(w => w.id !== id)
  }

  return { workers, loaded, load, startPolling, forSite, add, act, remove }
})
