import { createRouter, createWebHistory } from 'vue-router'
import ServicesView from '@/views/ServicesView.vue'
import SitesView from '@/views/SitesView.vue'
import PhpView from '@/views/PhpView.vue'
import DumpsView from '@/views/DumpsView.vue'
import MailView from '@/views/MailView.vue'
import SettingsView from '@/views/SettingsView.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/',          redirect: '/services' },
    { path: '/services',  component: ServicesView },
    { path: '/sites',     component: SitesView },
    { path: '/php',       component: PhpView },
    { path: '/dumps',     component: DumpsView },
    { path: '/mail',      component: MailView, meta: { fullWidth: true } },
    { path: '/settings',  component: SettingsView },
  ],
})

export default router
