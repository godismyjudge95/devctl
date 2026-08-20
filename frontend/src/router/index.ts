import { createRouter, createWebHistory } from 'vue-router'
import ServicesView from '@/views/ServicesView.vue'
import ServiceSettingsView from '@/views/ServiceSettingsView.vue'
import ServiceInstallView from '@/views/ServiceInstallView.vue'
import SitesView from '@/views/SitesView.vue'
import SiteCreateView from '@/views/SiteCreateView.vue'
import SiteSettingsView from '@/views/SiteSettingsView.vue'
import SiteWorktreeView from '@/views/SiteWorktreeView.vue'
import DumpsView from '@/views/DumpsView.vue'
import MailView from '@/views/MailView.vue'
import SpxView from '@/views/SpxView.vue'
import SettingsView from '@/views/SettingsView.vue'
import HelpersView from '@/views/HelpersView.vue'
import HelperInstallView from '@/views/HelperInstallView.vue'
import LogsView from '@/views/LogsView.vue'
import ConfigEditorView from '@/views/ConfigEditorView.vue'
import DatabasesView from '@/views/DatabasesView.vue'
import MaxIOView from '@/views/MaxIOView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/',          redirect: '/services' },
    { path: '/services',  component: ServicesView },
    { path: '/services/install', component: ServiceInstallView },
    { path: '/services/:id/settings', component: ServiceSettingsView },
    { path: '/services/:id/config/:file', component: ConfigEditorView, meta: { fullWidth: true } },
    { path: '/sites',     component: SitesView },
    { path: '/sites/new', component: SiteCreateView },
    { path: '/sites/:id/worktree', component: SiteWorktreeView },
    { path: '/sites/:id', component: SiteSettingsView },
    { path: '/dumps',     component: DumpsView },
    { path: '/mail',      component: MailView,    meta: { fullWidth: true } },
    { path: '/spx',       component: SpxView,     meta: { fullWidth: true } },
    { path: '/databases', component: DatabasesView, meta: { fullWidth: true } },
    { path: '/whodb',     redirect: '/databases' }, // leftover bookmark
    { path: '/maxio',     component: MaxIOView,   meta: { fullWidth: true } },
    { path: '/logs',      component: LogsView,    meta: { fullWidth: true } },
    { path: '/helpers',   component: HelpersView },
    { path: '/helpers/install', component: HelperInstallView },
    { path: '/settings',  component: SettingsView },
  ],
})

export default router
