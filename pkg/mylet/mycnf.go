package mylet

const MyCnfTmpl = `[mysqld]
# TODO
ssl = OFF
local_infile = OFF
secure_file_priv = NULL
{{- if ne .Mysql.Status.Version.Major 5}}
mysqlx = OFF
{{- end}}
#skip_name_resolve = ON
#skip-host-cache
max_connections = 2048
max_allowed_packet = 256M
explicit_defaults_for_timestamp = ON

super_read_only = ON
{{- if eq .Mysql.Status.Version.Major 5}}
skip_slave_start = ON
{{- else}}
skip_replica_start = ON
{{- end}}

pid_file = {{.Spec.Name}}.pid
socket = {{.Spec.Name}}.sock
port = {{.Spec.Port}}

datadir = {{.DataDir}}

server_id = {{.Spec.ServerId}}
report_host = {{.Mysql.SoloShortHost .Spec.Id}}
gtid_mode = ON
enforce_gtid_consistency = ON

binlog_format = ROW
log_bin = {{.Spec.Name}}-bin
log_error = {{.Spec.Name}}.err
## 日志过期时间,包括二进制日志(过期自动删除)
expire_logs_days = 7
## 指定每个二进制日志文件的最大大小
max_binlog_size = 1G
## 指定在写入二进制日志之前，用于缓存事务的内存大小
max_binlog_cache_size = 512M
{{- if eq .Mysql.Status.Version.Major 5}}
log_slave_updates = ON
{{- else}}
log_replica_updates = ON
{{- end}}
{{- if .IsReplica}}
sync_binlog = 0

#slave-parallel-type = LOGICAL_CLOCK
#slave-parallel-workers = 4
{{- else}}
sync_binlog = 1

#binlog_group_commit_sync_delay = 100
#binlog_group_commit_sync_no_delay_count = 10
#innodb_flush_log_at_trx_commit =1
{{- end}}
relay_log = {{.Spec.Name}}-relay-bin
relay_log_info_repository = TABLE
relay_log_recovery = ON

{{- if ne .Mysql.Spec.PrimaryMode "Classic" }}
{{- if eq .Mysql.Status.Version.Major 5}}
# <= 8.0.20
binlog_checksum = NONE
# <= 8.0.3
transaction_write_set_extraction = XXHASH64
master_info_repository = TABLE
{{- end}}

disabled_storage_engines = MyISAM,BLACKHOLE,FEDERATED,ARCHIVE,MEMORY
plugin_load_add = group_replication.so
group_replication_group_name = {{.Mysql.Spec.GroupName}}
group_replication_start_on_boot = OFF
group_replication_local_address = {{.GroupReplicationLocalAddress}}
group_replication_group_seeds = {{.Mysql.GroupReplicationGroupSeeds}}
group_replication_bootstrap_group = OFF
{{- end}}

!includedir {{.Spec.Mydir}}/my.cnf.d/
`
