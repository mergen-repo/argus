import { lazy, Suspense } from 'react'
import { createBrowserRouter } from 'react-router-dom'
import { DashboardLayout } from '@/components/layout/dashboard-layout'
import { AuthLayout } from '@/components/layout/auth-layout'
import { ProtectedRoute } from '@/components/auth/protected-route'
import { ErrorBoundary } from '@/components/error-boundary'

import LoginPage from '@/pages/auth/login'
import TwoFactorPage from '@/pages/auth/two-factor'
import OnboardingPage from '@/pages/auth/onboarding'
import ChangePasswordPage from '@/pages/auth/change-password'

import NotFoundPage from '@/pages/not-found'

const DashboardPage = lazy(() => import('@/pages/dashboard/index'))
const SimListPage = lazy(() => import('@/pages/sims/index'))

const AnalyticsPage = lazy(() => import('@/pages/dashboard/analytics'))
const AnalyticsCostPage = lazy(() => import('@/pages/dashboard/analytics-cost'))
const AnalyticsAnomaliesPage = lazy(() => import('@/pages/dashboard/analytics-anomalies'))

const SimDetailPage = lazy(() => import('@/pages/sims/detail'))

const ApnListPage = lazy(() => import('@/pages/apns/index'))
const ApnDetailPage = lazy(() => import('@/pages/apns/detail'))

const OperatorListPage = lazy(() => import('@/pages/operators/index'))
const OperatorDetailPage = lazy(() => import('@/pages/operators/detail'))

const SessionListPage = lazy(() => import('@/pages/sessions/index'))

const PolicyListPage = lazy(() => import('@/pages/policies/index'))
const PolicyEditorPage = lazy(() => import('@/pages/policies/editor'))

const EsimListPage = lazy(() => import('@/pages/esim/index'))

const JobListPage = lazy(() => import('@/pages/jobs/index'))
const AuditLogPage = lazy(() => import('@/pages/audit/index'))
const NotificationsPage = lazy(() => import('@/pages/notifications/index'))

const UsersPage = lazy(() => import('@/pages/settings/users'))
const ApiKeysPage = lazy(() => import('@/pages/settings/api-keys'))
const IpPoolsPage = lazy(() => import('@/pages/settings/ip-pools'))
const IpPoolDetailPage = lazy(() => import('@/pages/settings/ip-pool-detail'))
const NotificationConfigPage = lazy(() => import('@/pages/settings/notifications'))
const KnowledgeBasePage = lazy(() => import('@/pages/settings/knowledgebase'))
const ReliabilityPage = lazy(() => import('@/pages/settings/reliability'))
const SecurityPage = lazy(() => import('@/pages/settings/security'))
const ActiveSessionsPage = lazy(() => import('@/pages/settings/sessions'))

const SystemHealthPage = lazy(() => import('@/pages/system/health'))
const TenantManagementPage = lazy(() => import('@/pages/system/tenants'))

const AlertsPage = lazy(() => import('@/pages/alerts/index'))
const SLADashboardPage = lazy(() => import('@/pages/sla/index'))
const TopologyPage = lazy(() => import('@/pages/topology/index'))
const ReportsPage = lazy(() => import('@/pages/reports/index'))
const CapacityPage = lazy(() => import('@/pages/capacity/index'))
const SIMComparePage = lazy(() => import('@/pages/sims/compare'))
const ViolationsPage = lazy(() => import('@/pages/violations/index'))
const WebhooksPage = lazy(() => import('@/pages/webhooks/index'))
const SMSPage = lazy(() => import('@/pages/sms/index'))
const DataPortabilityPage = lazy(() => import('@/pages/compliance/data-portability'))

const RoamingAgreementsPage = lazy(() => import('@/pages/roaming/index'))
const RoamingAgreementDetailPage = lazy(() => import('@/pages/roaming/detail'))

const OpsPerformancePage = lazy(() => import('@/pages/ops/performance'))
const OpsErrorsPage = lazy(() => import('@/pages/ops/errors'))
const OpsAAATrafficPage = lazy(() => import('@/pages/ops/aaa-traffic'))
const OpsInfraPage = lazy(() => import('@/pages/ops/infra'))
const OpsJobsPage = lazy(() => import('@/pages/ops/jobs'))
const OpsBackupPage = lazy(() => import('@/pages/ops/backup'))
const OpsDeploysPage = lazy(() => import('@/pages/ops/deploys'))
const OpsIncidentsPage = lazy(() => import('@/pages/ops/incidents'))

const AdminTenantResourcesPage = lazy(() => import('@/pages/admin/tenant-resources'))
const AdminQuotasPage = lazy(() => import('@/pages/admin/quotas'))
const AdminCostPage = lazy(() => import('@/pages/admin/cost'))
const AdminCompliancePage = lazy(() => import('@/pages/admin/compliance'))
const AdminSecurityEventsPage = lazy(() => import('@/pages/admin/security-events'))
const AdminGlobalSessionsPage = lazy(() => import('@/pages/admin/sessions-global'))
const AdminAPIUsagePage = lazy(() => import('@/pages/admin/api-usage'))
const AdminDSARPage = lazy(() => import('@/pages/admin/dsar'))
const AdminPurgeHistoryPage = lazy(() => import('@/pages/admin/purge-history'))
const AdminDeliveryPage = lazy(() => import('@/pages/admin/delivery'))
const AdminKillSwitchesPage = lazy(() => import('@/pages/admin/kill-switches'))
const AdminMaintenancePage = lazy(() => import('@/pages/admin/maintenance'))

function LazyFallback() {
  return (
    <div className="flex items-center justify-center h-full min-h-[200px]">
      <div className="h-6 w-6 animate-spin rounded-full border-2 border-accent border-t-transparent" />
    </div>
  )
}

function lazySuspense(Component: React.LazyExoticComponent<React.ComponentType>) {
  return (
    <ErrorBoundary>
      <Suspense fallback={<LazyFallback />}>
        <Component />
      </Suspense>
    </ErrorBoundary>
  )
}

export const router = createBrowserRouter([
  {
    element: <AuthLayout />,
    children: [
      { path: '/login', element: <LoginPage /> },
      { path: '/login/2fa', element: <TwoFactorPage /> },
      { path: '/setup', element: <OnboardingPage /> },
      { path: '/auth/change-password', element: <ChangePasswordPage /> },
    ],
  },
  {
    element: <ProtectedRoute />,
    children: [
      {
        element: <DashboardLayout />,
        children: [
          { path: '/', element: lazySuspense(DashboardPage) },
          { path: '/analytics', element: lazySuspense(AnalyticsPage) },
          { path: '/analytics/cost', element: lazySuspense(AnalyticsCostPage) },
          { path: '/analytics/anomalies', element: lazySuspense(AnalyticsAnomaliesPage) },
          { path: '/sims', element: lazySuspense(SimListPage) },
          { path: '/sims/compare', element: lazySuspense(SIMComparePage) },
          { path: '/sims/:id', element: lazySuspense(SimDetailPage) },
          { path: '/apns', element: lazySuspense(ApnListPage) },
          { path: '/apns/:id', element: lazySuspense(ApnDetailPage) },
          { path: '/operators', element: lazySuspense(OperatorListPage) },
          { path: '/operators/:id', element: lazySuspense(OperatorDetailPage) },
          { path: '/sessions', element: lazySuspense(SessionListPage) },
          { path: '/policies', element: lazySuspense(PolicyListPage) },
          { path: '/policies/:id', element: lazySuspense(PolicyEditorPage) },
          { path: '/esim', element: lazySuspense(EsimListPage) },
          { path: '/jobs', element: lazySuspense(JobListPage) },
          { path: '/audit', element: lazySuspense(AuditLogPage) },
          { path: '/notifications', element: lazySuspense(NotificationsPage) },
          { path: '/settings/users', element: lazySuspense(UsersPage) },
          { path: '/settings/api-keys', element: lazySuspense(ApiKeysPage) },
          { path: '/settings/ip-pools', element: lazySuspense(IpPoolsPage) },
          { path: '/settings/ip-pools/:poolId', element: lazySuspense(IpPoolDetailPage) },
          { path: '/settings/notifications', element: lazySuspense(NotificationConfigPage) },
          { path: '/settings/knowledgebase', element: lazySuspense(KnowledgeBasePage) },
          { path: '/settings/reliability', element: lazySuspense(ReliabilityPage) },
          { path: '/settings/security', element: lazySuspense(SecurityPage) },
          { path: '/settings/sessions', element: lazySuspense(ActiveSessionsPage) },
          { path: '/alerts', element: lazySuspense(AlertsPage) },
          { path: '/sla', element: lazySuspense(SLADashboardPage) },
          { path: '/topology', element: lazySuspense(TopologyPage) },
          { path: '/reports', element: lazySuspense(ReportsPage) },
          { path: '/capacity', element: lazySuspense(CapacityPage) },
          { path: '/violations', element: lazySuspense(ViolationsPage) },
          { path: '/webhooks', element: lazySuspense(WebhooksPage) },
          { path: '/sms', element: lazySuspense(SMSPage) },
          { path: '/compliance/data-portability', element: lazySuspense(DataPortabilityPage) },
          { path: '/roaming-agreements', element: lazySuspense(RoamingAgreementsPage) },
          { path: '/roaming-agreements/:id', element: lazySuspense(RoamingAgreementDetailPage) },
          { path: '/system/health', element: lazySuspense(SystemHealthPage) },
          { path: '/system/tenants', element: lazySuspense(TenantManagementPage) },
          { path: '/ops/performance', element: lazySuspense(OpsPerformancePage) },
          { path: '/ops/errors', element: lazySuspense(OpsErrorsPage) },
          { path: '/ops/aaa-traffic', element: lazySuspense(OpsAAATrafficPage) },
          { path: '/ops/infra', element: lazySuspense(OpsInfraPage) },
          { path: '/ops/jobs', element: lazySuspense(OpsJobsPage) },
          { path: '/ops/backup', element: lazySuspense(OpsBackupPage) },
          { path: '/ops/deploys', element: lazySuspense(OpsDeploysPage) },
          { path: '/ops/incidents', element: lazySuspense(OpsIncidentsPage) },
          { path: '/admin/resources', element: lazySuspense(AdminTenantResourcesPage) },
          { path: '/admin/quotas', element: lazySuspense(AdminQuotasPage) },
          { path: '/admin/cost', element: lazySuspense(AdminCostPage) },
          { path: '/admin/compliance', element: lazySuspense(AdminCompliancePage) },
          { path: '/admin/security-events', element: lazySuspense(AdminSecurityEventsPage) },
          { path: '/admin/sessions', element: lazySuspense(AdminGlobalSessionsPage) },
          { path: '/admin/api-usage', element: lazySuspense(AdminAPIUsagePage) },
          { path: '/admin/dsar', element: lazySuspense(AdminDSARPage) },
          { path: '/admin/purge-history', element: lazySuspense(AdminPurgeHistoryPage) },
          { path: '/admin/delivery', element: lazySuspense(AdminDeliveryPage) },
          { path: '/admin/kill-switches', element: lazySuspense(AdminKillSwitchesPage) },
          { path: '/admin/maintenance', element: lazySuspense(AdminMaintenancePage) },
          { path: '*', element: <NotFoundPage /> },
        ],
      },
    ],
  },
])
