// Typed fetch wrappers for all REST endpoints.

export interface ServiceState {
  id: string
  label: string
  description: string
  install_version: string
  status: 'running' | 'stopped' | 'pending' | 'unknown' | 'warning'
  version: string
  log: string
  installed: boolean
  installable: boolean
  required: boolean
  has_credentials: boolean
  latest_version: string
  update_available: boolean
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
  public_dir: string
  parent_site_id: string | null
  worktree_branch: string | null
  is_git_repo: number
  git_remote_url: string
  framework: string
  created_at: string
  updated_at: string
}

export interface Branch {
  name: string
  is_remote: boolean
  is_current: boolean
}

export interface WorktreeConfig {
  symlinks: string[]
  copies: string[]
}

export interface CreateWorktreeInput {
  branch: string
  create_branch: boolean
  symlinks: string[]
  copies: string[]
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
  const ct = res.headers.get('Content-Type') ?? ''
  if (!ct.includes('application/json')) return undefined as T
  return res.json()
}

export type ServiceCredentials = Record<string, string>
export type ServiceDetails = Record<string, string>

// --- Services ---
export const getServices = () => request<ServiceState[]>('GET', '/api/services')
export const startService = (id: string) => request<void>('POST', `/api/services/${id}/start`)
export const stopService = (id: string) => request<void>('POST', `/api/services/${id}/stop`)
export const restartService = (id: string) => request<void>('POST', `/api/services/${id}/restart`)
export const clearServiceLogs = (id: string) => request<void>('DELETE', `/api/services/${id}/logs`)
export const getServiceCredentials = (id: string) =>
  request<ServiceCredentials>('GET', `/api/services/${id}/credentials`)
export const getServiceDetails = (id: string) =>
  request<ServiceDetails>('GET', `/api/services/${id}/details`)

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
 * Pass preserveData=true to keep the service's data directory.
 */
export function purgeServiceStream(id: string, callbacks: StreamCallbacks, preserveData = false): AbortController {
  const path = preserveData ? `/api/services/${id}?preserve_data=true` : `/api/services/${id}`
  return runServiceStream('DELETE', path, callbacks)
}

/**
 * Streams update output via SSE-over-fetch.
 * Calls onOutput for each chunk, onDone on success, onError on failure.
 */
export function updateServiceStream(id: string, callbacks: StreamCallbacks): AbortController {
  return runServiceStream('POST', `/api/services/${id}/update`, callbacks)
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
  public_dir?: string
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
export const detectSite = (rootPath: string) =>
  request<{ public_dir: string; framework: string }>('GET', `/api/sites/detect?root_path=${encodeURIComponent(rootPath)}`)
export const refreshSiteMetadata = () =>
  request<{ updated: number }>('POST', '/api/sites/refresh-metadata')

// --- Worktrees ---
export const getSiteBranches = (id: string) =>
  request<Branch[]>('GET', `/api/sites/${id}/branches`)
export const getWorktreeConfig = (id: string) =>
  request<WorktreeConfig>('GET', `/api/sites/${id}/worktree-config`)
export const putWorktreeConfig = (id: string, data: WorktreeConfig) =>
  request<WorktreeConfig>('PUT', `/api/sites/${id}/worktree-config`, data)
export const getSiteWorktrees = (id: string) =>
  request<Site[]>('GET', `/api/sites/${id}/worktrees`)
export const createWorktree = (id: string, data: CreateWorktreeInput) =>
  request<Site>('POST', `/api/sites/${id}/worktrees`, data)
export const removeWorktree = (parentId: string, worktreeId: string) =>
  request<void>('DELETE', `/api/sites/${parentId}/worktrees/${worktreeId}`)

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
export const getResolvedSettings = () => request<Settings>('GET', '/api/settings/resolved')
export const putSettings = (data: Settings) => request<void>('PUT', '/api/settings', data)

// --- Service settings (mailpit, mysql, dns, and php-fpm-* only) ---
export interface MailpitServiceSettings {
  http_port: string
  smtp_port: string
}
export interface MySQLServiceSettings {
  port: string
  bind_address: string
}
export type PHPServiceSettings = PHPSettings
export interface DNSServiceSettings {
  port: string
  target_ip: string
  tld: string
  system_dns_configured: boolean
}

export const getServiceSettings = (id: string) =>
  request<MailpitServiceSettings | MySQLServiceSettings | PHPServiceSettings | DNSServiceSettings>('GET', `/api/services/${id}/settings`)
export const putServiceSettings = (id: string, data: MailpitServiceSettings | MySQLServiceSettings | PHPServiceSettings | DNSServiceSettings) =>
  request<{ status: string }>('PUT', `/api/services/${id}/settings`, data)

// --- DNS system integration ---
export const detectDNSIP = () => request<{ ip: string }>('GET', '/api/dns/detect-ip')
export const checkSystemDNS = () => request<{ configured: boolean }>('GET', '/api/dns/setup')
export const setupSystemDNS = () => request<{ status: string }>('POST', '/api/dns/setup')
export const teardownSystemDNS = () => request<{ status: string }>('DELETE', '/api/dns/setup')

// --- Service config (php-fpm-* and mysql only) ---
export const getServiceConfig = (id: string, file: string) =>
  request<{ content: string }>('GET', `/api/services/${id}/config/${file}`)
export const putServiceConfig = (id: string, file: string, content: string) =>
  request<void>('PUT', `/api/services/${id}/config/${file}`, { content })

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

// Mailpit silently ignores {"IDs":["*"]}; a bodyless DELETE is required to
// delete all messages. Do not pass a body here.
export const deleteAllMessages = () =>
  request<void>('DELETE', '/api/mail/api/v1/messages')

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
  status: 'running' | 'stopped' | 'unknown'
}

export interface PHPSettings {
  upload_max_filesize: string
  memory_limit: string
  max_execution_time: string
  post_max_size: string
}

export const getPHPVersions = () => request<PHPVersion[]>('GET', '/api/php/versions')
export const installPHP = (ver: string) =>
  request<PHPVersion[]>('POST', `/api/php/versions/${ver}/install`, {})
export const uninstallPHP = (ver: string) =>
  request<void>('DELETE', `/api/php/versions/${ver}`)
export const startPHPVersion = (ver: string) =>
  request<void>('POST', `/api/php/versions/${ver}/start`)
export const stopPHPVersion = (ver: string) =>
  request<void>('POST', `/api/php/versions/${ver}/stop`)
export const restartPHPVersion = (ver: string) =>
  request<void>('POST', `/api/php/versions/${ver}/restart`)
export const getPHPSettings = () => request<PHPSettings>('GET', '/api/php/settings')
export const setPHPSettings = (data: PHPSettings) =>
  request<PHPSettings>('PUT', '/api/php/settings', data)

// --- TLS ---
export const getTLSCertURL = () => '/api/tls/cert'
export const trustTLS = () => request<{ status: string; output: string }>('POST', '/api/tls/trust')

// --- System ---
export const restartDevctl = () => request<{ status: string }>('POST', '/api/restart')

// --- SPX Profiler ---
export interface SpxProfile {
  key: string
  php_version: string
  domain: string
  method: string
  uri: string
  wall_time_ms: number
  peak_memory_bytes: number
  called_func_count: number
  timestamp: number
}

export interface SpxFunction {
  name: string
  calls: number
  inclusive_ms: number
  exclusive_ms: number
  inclusive_pct: number
  exclusive_pct: number
}

export interface SpxEvent {
  depth: number
  name: string
  start_ms: number
  duration_ms: number
}

export interface SpxProfileDetail extends SpxProfile {
  functions: SpxFunction[]
  events: SpxEvent[]
}

export const getSpxProfiles = (domain?: string) => {
  const q = domain ? `?domain=${encodeURIComponent(domain)}` : ''
  return request<SpxProfile[]>('GET', `/api/spx/profiles${q}`)
}
export const getSpxProfile = (key: string) =>
  request<SpxProfileDetail>('GET', `/api/spx/profiles/${encodeURIComponent(key)}`)
export const deleteSpxProfile = (key: string) =>
  request<void>('DELETE', `/api/spx/profiles/${encodeURIComponent(key)}`)
export const clearSpxProfiles = (domain?: string) => {
  const q = domain ? `?domain=${encodeURIComponent(domain)}` : ''
  return request<void>('DELETE', `/api/spx/profiles${q}`)
}

// --- Logs ---
export interface LogFileInfo {
  id: string
  name: string
  path: string
  size: number
}

export const getLogs = () => request<LogFileInfo[]>('GET', '/api/logs')
export const clearLog = (id: string) => request<void>('DELETE', `/api/logs/${encodeURIComponent(id)}`)

// --- WhoDB ---
export interface WhoDBConnection {
  alias: string
  host?: string
  port?: string
  username?: string
  password?: string
  database?: string
}

export interface WhoDBManualConnection {
  type: string  // 'postgres' | 'mysql' | 'redis'
  conn: WhoDBConnection
}

export interface WhoDBAutoConnection {
  source: string
  type: string
  conn: WhoDBConnection
}

export interface WhoDBSettings {
  disable_credential_form: boolean
  manual_connections: WhoDBManualConnection[]
  auto_connections: WhoDBAutoConnection[]
}

export const getWhoDBSettings = () =>
  request<WhoDBSettings>('GET', '/api/services/whodb/settings')

export const putWhoDBSettings = (data: Pick<WhoDBSettings, 'disable_credential_form' | 'manual_connections'>) =>
  request<{ status: string }>('PUT', '/api/services/whodb/settings', data)

// --- MaxIO ---

export interface MaxIOBucket {
  name: string
  creationDate: string
}

export interface MaxIOObject {
  key: string
  size: number
  lastModified: string
  etag: string
  storageClass: string
}

export interface MaxIOListResult {
  objects: MaxIOObject[]
  prefixes: string[]  // virtual "folders"
  isTruncated: boolean
}

// XML parsing helpers
function parseXML(text: string): Document {
  return new DOMParser().parseFromString(text, 'application/xml')
}

function getText(el: Element | Document, tag: string): string {
  return el.querySelector(tag)?.textContent ?? ''
}

/** List all buckets. Calls S3 GET / */
export async function listBuckets(): Promise<MaxIOBucket[]> {
  const res = await fetch('/api/maxio/s3/')
  if (!res.ok) throw new Error(`listBuckets: ${res.status} ${res.statusText}`)
  const text = await res.text()
  const doc = parseXML(text)
  return Array.from(doc.querySelectorAll('Bucket')).map(b => ({
    name: getText(b, 'Name'),
    creationDate: getText(b, 'CreationDate'),
  }))
}

/** Create a bucket. */
export async function createBucket(name: string): Promise<void> {
  const res = await fetch(`/api/maxio/s3/${encodeURIComponent(name)}`, { method: 'PUT' })
  if (!res.ok) throw new Error(`createBucket: ${res.status} ${res.statusText}`)
}

/** Delete an empty bucket. */
export async function deleteBucket(name: string): Promise<void> {
  const res = await fetch(`/api/maxio/s3/${encodeURIComponent(name)}`, { method: 'DELETE' })
  if (!res.ok) throw new Error(`deleteBucket: ${res.status} ${res.statusText}`)
}

/** List ALL objects under a prefix recursively (no delimiter — no virtual folder splitting).
 *  Used for subtree moves: fetches every key under a given prefix. */
export async function listAllObjects(bucket: string, prefix: string): Promise<MaxIOObject[]> {
  let continuationToken: string | undefined
  const all: MaxIOObject[] = []
  do {
    const q = new URLSearchParams({ 'list-type': '2', prefix })
    if (continuationToken) q.set('continuation-token', continuationToken)
    const res = await fetch(`/api/maxio/s3/${encodeURIComponent(bucket)}?${q}`)
    if (!res.ok) throw new Error(`listAllObjects: ${res.status} ${res.statusText}`)
    const doc = parseXML(await res.text())
    for (const c of Array.from(doc.querySelectorAll('Contents'))) {
      all.push({
        key: getText(c, 'Key'),
        size: parseInt(getText(c, 'Size') || '0', 10),
        lastModified: getText(c, 'LastModified'),
        etag: getText(c, 'ETag').replace(/"/g, ''),
        storageClass: getText(c, 'StorageClass'),
      })
    }
    const truncated = getText(doc, 'IsTruncated') === 'true'
    continuationToken = truncated ? getText(doc, 'NextContinuationToken') : undefined
  } while (continuationToken)
  return all
}

/** List objects (and common prefixes = virtual folders) in a bucket under prefix. */
export async function listObjects(bucket: string, prefix = ''): Promise<MaxIOListResult> {
  const q = new URLSearchParams({ 'list-type': '2', delimiter: '/', prefix })
  const res = await fetch(`/api/maxio/s3/${encodeURIComponent(bucket)}?${q}`)
  if (!res.ok) throw new Error(`listObjects: ${res.status} ${res.statusText}`)
  const text = await res.text()
  const doc = parseXML(text)

  const objects: MaxIOObject[] = Array.from(doc.querySelectorAll('Contents')).map(c => ({
    key: getText(c, 'Key'),
    size: parseInt(getText(c, 'Size') || '0', 10),
    lastModified: getText(c, 'LastModified'),
    etag: getText(c, 'ETag').replace(/"/g, ''),
    storageClass: getText(c, 'StorageClass'),
  }))

  const prefixes = Array.from(doc.querySelectorAll('CommonPrefixes')).map(p =>
    getText(p, 'Prefix')
  )

  const isTruncated = getText(doc, 'IsTruncated') === 'true'

  return { objects, prefixes, isTruncated }
}

/** Delete a single object. */
export async function deleteObject(bucket: string, key: string): Promise<void> {
  const res = await fetch(`/api/maxio/s3/${encodeURIComponent(bucket)}/${key}`, { method: 'DELETE' })
  if (!res.ok) throw new Error(`deleteObject: ${res.status} ${res.statusText}`)
}

/** Bulk-delete objects. */
export async function deleteObjects(bucket: string, keys: string[]): Promise<void> {
  const objectsXML = keys.map(k => `<Object><Key>${k}</Key></Object>`).join('')
  const body = `<?xml version="1.0" encoding="UTF-8"?><Delete><Quiet>true</Quiet>${objectsXML}</Delete>`
  const md5 = await computeMD5Base64(body)
  const res = await fetch(`/api/maxio/s3/${encodeURIComponent(bucket)}?delete`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/xml',
      'Content-MD5': md5,
    },
    body,
  })
  if (!res.ok) throw new Error(`deleteObjects: ${res.status} ${res.statusText}`)
}

async function computeMD5Base64(text: string): Promise<string> {
  const buf = new TextEncoder().encode(text)
  const hash = await crypto.subtle.digest('MD5', buf).catch(() => null)
  if (!hash) {
    // Fallback: send without Content-MD5 (some S3 impls don't require it)
    return ''
  }
  const arr = Array.from(new Uint8Array(hash))
  const b64 = btoa(String.fromCharCode(...arr))
  return b64
}

/** Upload an object using XHR for progress events. */
export function uploadObject(
  bucket: string,
  key: string,
  file: File,
  onProgress: (pct: number) => void,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('PUT', `/api/maxio/s3/${encodeURIComponent(bucket)}/${key}`)
    xhr.setRequestHeader('Content-Type', file.type || 'application/octet-stream')
    xhr.upload.addEventListener('progress', (e) => {
      if (e.lengthComputable) onProgress(Math.round((e.loaded / e.total) * 100))
    })
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) resolve()
      else reject(new Error(`upload ${key}: ${xhr.status} ${xhr.statusText}`))
    }
    xhr.onerror = () => reject(new Error(`upload ${key}: network error`))
    xhr.send(file)
  })
}

/** Copy an object within the same bucket (S3 server-side copy). */
export async function copyObject(bucket: string, srcKey: string, dstKey: string): Promise<void> {
  const res = await fetch(`/api/maxio/s3/${encodeURIComponent(bucket)}/${dstKey}`, {
    method: 'PUT',
    headers: {
      'x-amz-copy-source': `/${encodeURIComponent(bucket)}/${srcKey}`,
      'Content-Length': '0',
    },
  })
  if (!res.ok) throw new Error(`copyObject: ${res.status} ${res.statusText}`)
}

/** Upload a zero-byte object to create a virtual folder (key ends with '/'). */
export async function createFolder(bucket: string, key: string): Promise<void> {
  const res = await fetch(`/api/maxio/s3/${encodeURIComponent(bucket)}/${key}`, {
    method: 'PUT',
    headers: {
      'Content-Type': 'application/octet-stream',
      'Content-Length': '0',
    },
    body: '',
  })
  if (!res.ok) throw new Error(`createFolder: ${res.status} ${res.statusText}`)
}

/** Get a presigned URL for downloading an object. */
export async function getPresignedUrl(bucket: string, key: string): Promise<string> {
  const q = new URLSearchParams({ bucket, key })
  const data = await request<{ url: string }>('GET', `/api/maxio/presign?${q}`)
  return data.url
}
