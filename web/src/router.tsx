import { createBrowserRouter } from 'react-router-dom'
import { DashboardLayout } from '@/components/layout/dashboard-layout'
import { AuthLayout } from '@/components/layout/auth-layout'
import { ProtectedRoute } from '@/components/auth/protected-route'

import LoginPage from '@/pages/auth/login'
import TwoFactorPage from '@/pages/auth/two-factor'
import OnboardingPage from '@/pages/auth/onboarding'

import DashboardPage from '@/pages/dashboard/index'
import AnalyticsPage from '@/pages/dashboard/analytics'
import AnalyticsCostPage from '@/pages/dashboard/analytics-cost'
import AnalyticsAnomaliesPage from '@/pages/dashboard/analytics-anomalies'

import SimListPage from '@/pages/sims/index'
import SimDetailPage from '@/pages/sims/detail'

import ApnListPage from '@/pages/apns/index'
import ApnDetailPage from '@/pages/apns/detail'

import OperatorListPage from '@/pages/operators/index'
import OperatorDetailPage from '@/pages/operators/detail'

import SessionListPage from '@/pages/sessions/index'

import PolicyListPage from '@/pages/policies/index'
import PolicyEditorPage from '@/pages/policies/editor'

import EsimListPage from '@/pages/esim/index'

import JobListPage from '@/pages/jobs/index'
import AuditLogPage from '@/pages/audit/index'
import NotificationsPage from '@/pages/notifications/index'

import UsersPage from '@/pages/settings/users'
import ApiKeysPage from '@/pages/settings/api-keys'
import IpPoolsPage from '@/pages/settings/ip-pools'
import NotificationConfigPage from '@/pages/settings/notifications'

import SystemHealthPage from '@/pages/system/health'
import TenantManagementPage from '@/pages/system/tenants'

export const router = createBrowserRouter([
  {
    element: <AuthLayout />,
    children: [
      { path: '/login', element: <LoginPage /> },
      { path: '/login/2fa', element: <TwoFactorPage /> },
      { path: '/setup', element: <OnboardingPage /> },
    ],
  },
  {
    element: <ProtectedRoute />,
    children: [
      {
        element: <DashboardLayout />,
        children: [
          { path: '/', element: <DashboardPage /> },
          { path: '/analytics', element: <AnalyticsPage /> },
          { path: '/analytics/cost', element: <AnalyticsCostPage /> },
          { path: '/analytics/anomalies', element: <AnalyticsAnomaliesPage /> },
          { path: '/sims', element: <SimListPage /> },
          { path: '/sims/:id', element: <SimDetailPage /> },
          { path: '/apns', element: <ApnListPage /> },
          { path: '/apns/:id', element: <ApnDetailPage /> },
          { path: '/operators', element: <OperatorListPage /> },
          { path: '/operators/:id', element: <OperatorDetailPage /> },
          { path: '/sessions', element: <SessionListPage /> },
          { path: '/policies', element: <PolicyListPage /> },
          { path: '/policies/:id', element: <PolicyEditorPage /> },
          { path: '/esim', element: <EsimListPage /> },
          { path: '/jobs', element: <JobListPage /> },
          { path: '/audit', element: <AuditLogPage /> },
          { path: '/notifications', element: <NotificationsPage /> },
          { path: '/settings/users', element: <UsersPage /> },
          { path: '/settings/api-keys', element: <ApiKeysPage /> },
          { path: '/settings/ip-pools', element: <IpPoolsPage /> },
          { path: '/settings/notifications', element: <NotificationConfigPage /> },
          { path: '/system/health', element: <SystemHealthPage /> },
          { path: '/system/tenants', element: <TenantManagementPage /> },
        ],
      },
    ],
  },
])
