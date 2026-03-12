// Typed fetch wrappers for all REST endpoints.

export interface ServiceState {
  id: string
  label: string
  status: 'running' | 'stopped' | 'pending' | 'unknown'
  version: string
  log: string
  installed: boolean
  installable: boolean
  required: boolean
}

export interface Site {
  id: string
  domain: string
  root_path: string
  php_version: string
  aliases: string   // JSON array string
  spx_enabled: number
  https: number
  auto_discovered: number
  created_at: string
  updated_at: string
}

export interface Dump {
  id: number
  file: string | null
  line: number | null
  nodes: string   // JSON string
  timestamp: number
  site_domain: string | null
}

export interface Settings {
  [key: string]: string
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: body ? { 'Content-Type': 'application/json' } : {},
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(err.error ?? res.statusText)
  }
  if (res.status === 204) return undefined as T
  return res.json()
}

export type ServiceCredentials = Record<string, string>

// --- Services ---
export const getServices = () => request<ServiceState[]>('GET', '/api/services')
export const startService = (id: string) => request<void>('POST', `/api/services/${id}/start`)
export const stopService = (id: string) => request<void>('POST', `/api/services/${id}/stop`)
export const restartService = (id: string) => request<void>('POST', `/api/services/${id}/restart`)
export const getServiceCredentials = (id: string) =>
  request<ServiceCredentials>('GET', `/api/services/${id}/credentials`)

export interface StreamCallbacks {
  onOutput: (chunk: string) => void
  onDone: () => void
  onError: (message: string) => void
}

/**
 * Streams install output via SSE-over-fetch.
 * Calls onOutput for each chunk, onDone on success, onError on failure.
 * Returns an AbortController so the caller can cancel.
 */
export function installServiceStream(id: string, callbacks: StreamCallbacks): AbortController {
  return runServiceStream('POST', `/api/services/${id}/install`, callbacks)
}

/**
 * Streams purge output via SSE-over-fetch.
 */
export function purgeServiceStream(id: string, callbacks: StreamCallbacks): AbortController {
  return runServiceStream('DELETE', `/api/services/${id}`, callbacks)
}

function runServiceStream(method: string, path: string, callbacks: StreamCallbacks): AbortController {
  const ctrl = new AbortController()

  ;(async () => {
    let res: Response
    try {
      res = await fetch(path, { method, signal: ctrl.signal })
    } catch (e: any) {
      if (e?.name !== 'AbortError') callbacks.onError(e?.message ?? 'Network error')
      return
    }

    if (!res.ok || !res.body) {
      const err = await res.json().catch(() => ({ error: res.statusText }))
      callbacks.onError(err.error ?? res.statusText)
      return
    }

    const reader = res.body.getReader()
    const decoder = new TextDecoder()
    let buf = ''

    while (true) {
      let done: boolean, value: Uint8Array | undefined
      try {
        ;({ done, value } = await reader.read())
      } catch {
        break
      }
      if (done) break

      buf += decoder.decode(value, { stream: true })
      const events = buf.split('\n\n')
      buf = events.pop() ?? ''

      for (const block of events) {
        if (!block.trim()) continue
        let eventType = 'message'
        let data = ''
        for (const line of block.split('\n')) {
          if (line.startsWith('event: ')) eventType = line.slice(7).trim()
          else if (line.startsWith('data: ')) data = line.slice(6)
        }
        if (!data) continue
        try {
          const parsed = JSON.parse(data)
          if (eventType === 'output') {
            callbacks.onOutput(typeof parsed === 'string' ? parsed : JSON.stringify(parsed))
          } else if (eventType === 'done') {
            callbacks.onDone()
          } else if (eventType === 'error') {
            callbacks.onError(parsed.error ?? 'Unknown error')
          }
        } catch {
          // ignore malformed SSE data
        }
      }
    }
  })()

  return ctrl
}

export interface SiteInput {
  domain?: string
  root_path?: string
  php_version?: string
  aliases?: string[]
  spx_enabled?: number
  https?: number
  auto_discovered?: number
}

// --- Sites ---
export const getSites = () => request<Site[]>('GET', '/api/sites')
export const createSite = (data: SiteInput) =>
  request<Site>('POST', '/api/sites', data)
export const getSite = (id: string) => request<Site>('GET', `/api/sites/${id}`)
export const updateSite = (id: string, data: SiteInput) =>
  request<Site>('PUT', `/api/sites/${id}`, data)
export const deleteSite = (id: string) => request<void>('DELETE', `/api/sites/${id}`)
export const enableSPX = (id: string) => request<void>('POST', `/api/sites/${id}/spx/enable`)
export const disableSPX = (id: string) => request<void>('POST', `/api/sites/${id}/spx/disable`)

// --- Dumps ---
export const getDumps = (params?: { page?: number; limit?: number; site?: string }) => {
  const q = new URLSearchParams()
  if (params?.page) q.set('page', String(params.page))
  if (params?.limit) q.set('limit', String(params.limit))
  if (params?.site) q.set('site', params.site)
  return request<Dump[]>('GET', `/api/dumps?${q}`)
}
export const clearDumps = () => request<void>('DELETE', '/api/dumps')

// --- Settings ---
export const getSettings = () => request<Settings>('GET', '/api/settings')
export const putSettings = (data: Settings) => request<void>('PUT', '/api/settings', data)

// --- Mail ---
export const getMailConfig = () =>
  request<{ http_port: string; smtp_port: string }>('GET', '/api/mail/config')

export interface MailAddress {
  Name: string
  Address: string
}

export interface MailAttachmentMeta {
  PartID: string
  FileName: string
  ContentType: string
  Size: number
}

export interface MailMessage {
  ID: string
  MessageID: string
  Read: boolean
  From: MailAddress
  To: MailAddress[]
  Cc: MailAddress[] | null
  Bcc: MailAddress[] | null
  ReplyTo: MailAddress[]
  Subject: string
  Created: string
  Tags: string[]
  Size: number
  Attachments: number
  Snippet: string
}

export interface MailMessageDetail extends Omit<MailMessage, 'Attachments'> {
  ReturnPath: string
  Date: string
  Text: string
  HTML: string
  Inline: MailAttachmentMeta[]
  Attachments: MailAttachmentMeta[]
}

export interface MailListResponse {
  total: number
  unread: number
  count: number
  start: number
  tags: string[]
  messages: MailMessage[]
}

export interface MailWsEvent {
  Type: 'new' | 'update' | 'delete' | 'stats'
  Data: unknown
}

export const listMessages = (limit = 25, start = 0) =>
  request<MailListResponse>('GET', `/api/mail/api/v1/messages?limit=${limit}&start=${start}`)

export const getMessage = (id: string) =>
  request<MailMessageDetail>('GET', `/api/mail/api/v1/message/${id}`)

export const getMessageHeaders = (id: string) =>
  request<Record<string, string[]>>('GET', `/api/mail/api/v1/message/${id}/headers`)

export const getRawMessage = async (id: string): Promise<string> => {
  const res = await fetch(`/api/mail/api/v1/message/${id}/raw`)
  if (!res.ok) throw new Error(res.statusText)
  return res.text()
}

export const searchMessages = (query: string, limit = 25, start = 0) =>
  request<MailListResponse>('GET', `/api/mail/api/v1/search?query=${encodeURIComponent(query)}&limit=${limit}&start=${start}`)

export const deleteMessages = (ids: string[]) =>
  request<void>('DELETE', '/api/mail/api/v1/messages', { IDs: ids })

export const deleteAllMessages = () =>
  request<void>('DELETE', '/api/mail/api/v1/messages', { IDs: ['*'] })

export const markRead = (ids: string[], read: boolean) =>
  request<void>('PUT', '/api/mail/api/v1/messages', { IDs: ids, Read: read })

export const getTags = () =>
  request<string[]>('GET', '/api/mail/api/v1/tags')

export const mailHtmlUrl = (id: string) => `/api/mail/view/${id}.html`
export const mailPartUrl = (id: string, partID: string) => `/api/mail/api/v1/message/${id}/part/${partID}`

// --- PHP ---
export interface PHPVersion {
  version: string
  fpm_socket: string
  extensions: string[]
}

export interface PHPSettings {
  upload_max_filesize: string
  memory_limit: string
  max_execution_time: string
  post_max_size: string
}

export const getPHPVersions = () => request<PHPVersion[]>('GET', '/api/php/versions')
export const installPHP = (ver: string, extensions?: string[]) =>
  request<PHPVersion[]>('POST', `/api/php/versions/${ver}/install`, { extensions })
export const uninstallPHP = (ver: string) =>
  request<void>('DELETE', `/api/php/versions/${ver}`)
export const getPHPSettings = () => request<PHPSettings>('GET', '/api/php/settings')
export const setPHPSettings = (data: PHPSettings) =>
  request<PHPSettings>('PUT', '/api/php/settings', data)

// --- TLS ---
export const getTLSCertURL = () => '/api/tls/cert'
export const trustTLS = () => request<{ status: string; output: string }>('POST', '/api/tls/trust')
