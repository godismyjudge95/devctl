import type { ServiceCredentials } from '@/lib/api'

/** Services whose credentials can be opened via a standard connection URI. */
export type DbClientServiceId = 'postgres' | 'mysql' | 'redis'

export function supportsDbClientOpen(serviceId: string): serviceId is DbClientServiceId {
  return serviceId === 'postgres' || serviceId === 'mysql' || serviceId === 'redis'
}

/** Encode user:password@ for URI authority (omit empty password segment). */
function userInfo(user: string, password: string): string {
  const u = encodeURIComponent(user)
  if (!password) {
    return user ? `${u}@` : ''
  }
  return `${u}:${encodeURIComponent(password)}@`
}

/** TablePlus query params (name, env) — ignored by other clients. */
function withClientParams(url: string, connectionName: string): string {
  const params = new URLSearchParams({
    name: connectionName,
    env: 'development',
  })
  const sep = url.includes('?') ? '&' : '?'
  return `${url}${sep}${params.toString()}`
}

/**
 * Build a connection URI for TablePlus and other DB clients that import from URL.
 * @see https://docs.tableplus.com/gui-tools/manage-connections
 */
export function buildDbClientUrl(
  serviceId: DbClientServiceId,
  creds: ServiceCredentials,
  serviceLabel: string,
): string | null {
  const name = `devctl ${serviceLabel}`

  switch (serviceId) {
    case 'postgres': {
      const host = creds.DB_HOST || '127.0.0.1'
      const port = creds.DB_PORT || '5432'
      const user = creds.DB_USERNAME || 'root'
      const password = creds.DB_PASSWORD ?? ''
      const base = `postgresql://${userInfo(user, password)}${host}:${port}/postgres`
      return withClientParams(base, name)
    }
    case 'mysql': {
      const host = creds.DB_HOST || '127.0.0.1'
      const port = creds.DB_PORT || '3306'
      const user = creds.DB_USERNAME || 'root'
      const password = creds.DB_PASSWORD ?? ''
      const base = `mysql://${userInfo(user, password)}${host}:${port}/mysql`
      return withClientParams(base, name)
    }
    case 'redis': {
      const host = creds.REDIS_HOST || '127.0.0.1'
      const port = creds.REDIS_PORT || '6379'
      const password = creds.REDIS_PASSWORD ?? ''
      const base = password
        ? `redis://:${encodeURIComponent(password)}@${host}:${port}`
        : `redis://${host}:${port}`
      return withClientParams(base, name)
    }
    default:
      return null
  }
}

/** Open a connection URI in the OS-registered handler (e.g. TablePlus). */
export function openInDbClient(url: string): void {
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.rel = 'noopener noreferrer'
  document.body.appendChild(anchor)
  anchor.click()
  document.body.removeChild(anchor)
}