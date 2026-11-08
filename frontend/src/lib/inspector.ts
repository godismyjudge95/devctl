import type { RequestMeta, RequestProblemKind, RequestSummary } from '@/lib/api'

export const problemLabels: Record<RequestProblemKind, string> = {
  n_plus_one: 'N+1',
  duplicate: 'Duplicate',
  slow_query: 'Slow query',
  slow_request: 'Slow request',
}

export function describeUnit(m: RequestMeta): string {
  if (m.kind === 'http') return `${m.method ?? ''} ${m.url ?? ''}`.trim()
  if (m.kind === 'job') return m.job ?? 'job'
  return m.artisan ? `artisan ${m.artisan}` : (m.command || 'command')
}

export function unitStatus(u: RequestSummary): string {
  if (!u.final) return 'running'
  if (u.meta.status) return String(u.meta.status)
  return u.meta.job_status ?? 'done'
}

export function formatMs(ms: number | null | undefined): string {
  if (ms == null) return '-'
  return ms < 10 ? `${ms.toFixed(2)} ms` : `${Math.round(ms)} ms`
}

// problemCounts groups a unit's problems by kind, in the order they appear.
export function problemCounts(u: RequestSummary): { kind: RequestProblemKind; count: number }[] {
  const out: { kind: RequestProblemKind; count: number }[] = []
  for (const p of u.problems) {
    const row = out.find(r => r.kind === p.kind)
    if (row) row.count++
    else out.push({ kind: p.kind, count: 1 })
  }
  return out
}
