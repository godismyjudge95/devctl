import { toast } from 'vue-sonner'
import { useSitesStore } from '@/stores/sites'

/** Share actions with user feedback, used by the site list and the share row. */
export function useSiteShare() {
  const store = useSitesStore()

  async function start(id: string, domain: string) {
    try {
      const sh = await store.share(id)
      if (sh?.status === 'failed') {
        toast.error(`Could not share ${domain}`, { description: sh.error })
      } else if (sh?.status === 'active') {
        toast.success(`${domain} is public`, { description: sh.url })
      }
    } catch (e: any) {
      toast.error(`Could not share ${domain}`, { description: e.message })
    }
  }

  async function stop(id: string, domain: string) {
    try {
      await store.unshare(id)
      toast.success(`Stopped sharing ${domain}`)
    } catch (e: any) {
      toast.error(`Could not stop sharing ${domain}`, { description: e.message })
    }
  }

  function copy(url: string) {
    navigator.clipboard.writeText(url).then(
      () => toast.success('Public URL copied'),
      () => toast.error('Could not copy the URL'),
    )
  }

  return { start, stop, copy }
}
