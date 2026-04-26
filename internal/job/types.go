package job

const (
	JobTypeBulkImport       = "bulk_sim_import"
	JobTypeBulkDisconnect   = "bulk_session_disconnect"
	JobTypeBulkStateChange  = "bulk_state_change"
	JobTypeBulkPolicyAssign = "bulk_policy_assign"
	JobTypeBulkEsimSwitch   = "bulk_esim_switch"
	JobTypeOTACommand       = "ota_command"
	JobTypePurgeSweep       = "purge_sweep"
	JobTypeIPReclaim        = "ip_reclaim"
	JobTypeSLAReport        = "sla_report"
	JobTypePolicyDryRun     = "policy_dry_run"
	JobTypeRolloutStage     = "policy_rollout_stage"
	JobTypeCDRExport        = "cdr_export"
	JobTypeAnomalyBatch     = "anomaly_batch_detection"
	JobTypeS3Archival       = "s3_archival"
	JobTypeDataRetention    = "data_retention"
	JobTypeStorageMonitor   = "storage_monitor"
	JobTypePartitionCreate  = "partition_create"
	JobTypeBackupDaily      = "backup_daily"
	JobTypeBackupWeekly     = "backup_weekly"
	JobTypeBackupMonthly    = "backup_monthly"
	JobTypeBackupVerify     = "backup_verify"
	JobTypeBackupCleanup             = "backup_cleanup"
	JobTypeScheduledReportRun        = "scheduled_report_run"
	JobTypeIPGraceRelease            = "ip_grace_release"
	JobTypeKVKKPurgeDaily            = "kvkk_purge_daily"
	JobTypeDataPortabilityExport     = "data_portability_export"
	JobTypeSMSOutboundSend           = "sms_outbound_send"
	JobTypeWebhookRetry              = "webhook_retry"
	JobTypeScheduledReportSweeper    = "scheduled_report_sweeper"
	JobTypeRoamingRenewal            = "roaming_renewal_sweep"
	JobTypeDataIntegrityScan         = "data_integrity_scan"
	JobTypeAlertsRetention           = "alerts_retention"
	JobTypeStuckRolloutReaper        = "stuck_rollout_reaper"
)

var AllJobTypes = []string{
	JobTypeBulkImport,
	JobTypeBulkDisconnect,
	JobTypeBulkStateChange,
	JobTypeBulkPolicyAssign,
	JobTypeBulkEsimSwitch,
	JobTypeOTACommand,
	JobTypePurgeSweep,
	JobTypeIPReclaim,
	JobTypeSLAReport,
	JobTypePolicyDryRun,
	JobTypeRolloutStage,
	JobTypeCDRExport,
	JobTypeAnomalyBatch,
	JobTypeS3Archival,
	JobTypeDataRetention,
	JobTypeStorageMonitor,
	JobTypePartitionCreate,
	JobTypeBackupDaily,
	JobTypeBackupWeekly,
	JobTypeBackupMonthly,
	JobTypeBackupVerify,
	JobTypeBackupCleanup,
	JobTypeScheduledReportRun,
	JobTypeIPGraceRelease,
	JobTypeKVKKPurgeDaily,
	JobTypeDataPortabilityExport,
	JobTypeSMSOutboundSend,
	JobTypeWebhookRetry,
	JobTypeScheduledReportSweeper,
	JobTypeRoamingRenewal,
	JobTypeDataIntegrityScan,
	JobTypeAlertsRetention,
	JobTypeStuckRolloutReaper,
}
