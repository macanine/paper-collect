package service

// Files are grouped by domain; the package stays one unit because every
// domain shares the Service, its database handle and the job state machine.
//
//	Core          service.go (construction), bootstrap.go, validate.go, audit.go,
//	              accounts.go (users, API keys)
//	Intake        uploads.go (manifest uploads), ingest.go (archive into packs)
//	Library       packs.go, materials.go, collections.go, delete.go (trash),
//	              naming.go (canonical titles and file names), preview.go, stats.go
//	AI catalogue  pipeline_read.go → pipeline_group.go → pipeline_decide.go,
//	              pipeline_ops.go (shared steps), entities.go (organisation and
//	              tag dictionary), ai_providers.go, ocr.go
//	Search        search.go, mathindex.go; text processing lives in
//	              internal/searchtext
//	Storage       drives.go, credentials.go, sync_rules.go, replicas.go,
//	              replica_management.go, pull_cache.go, blob_sources.go
//	Jobs & nodes  jobs.go (claims, leases, state machine), dispatch.go,
//	              job_env.go (master side of job execution), nodes.go,
//	              cluster_settings.go, retention.go
//
// Job execution code that worker nodes also run lives in internal/jobexec and
// never touches the database.
