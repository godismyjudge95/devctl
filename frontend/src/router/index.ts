import { createRouter, createWebHistory } from 'vue-router'
import ServicesView from '@/views/ServicesView.vue'
import SitesView from '@/views/SitesView.vue'
import DumpsView from '@/views/DumpsView.vue'
import MailView from '@/views/MailView.vue'
import SpxView from '@/views/SpxView.vue'
import SettingsView from '@/views/SettingsView.vue'
import LogsView from '@/views/LogsView.vue'
import ConfigEditorView from '@/views/ConfigEditorView.vue'
import WhoDBView from '@/views/WhoDBView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/',          redirect: '/services' },
    { path: '/services',  component: ServicesView },
    { path: '/services/:id/config/:file', component: ConfigEditorView, meta: { fullWidth: true } },
    { path: '/sites',     component: SitesView },
    { path: '/dumps',     component: DumpsView },
    { path: '/mail',      component: MailView,    meta: { fullWidth: true } },
    { path: '/spx',       component: SpxView,     meta: { fullWidth: true } },
    { path: '/whodb',     component: WhoDBView,   meta: { fullWidth: true } },
    { path: '/logs',      component: LogsView,    meta: { fullWidth: true } },
    { path: '/settings',  component: SettingsView },
  ],
})

export default router
