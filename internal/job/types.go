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
}
