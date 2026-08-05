package syncstore

import "changeme/internal/storage"

// The database schema (table names + migrations) is owned by the storage
// package, the single source of truth. syncstore aliases the sync-related
// table names it operates on so its DML never hard-codes a literal that could
// drift from the DDL in storage.Migrate.
const (
	TableVaultItems         = storage.TableVaultItems
	TableSyncOutbox         = storage.TableSyncOutbox
	TableSyncCursors        = storage.TableSyncCursors
	TableSyncAccountBinding = storage.TableSyncAccountBinding
	TableSyncEntityVersions = storage.TableSyncEntityVersions
	TableSyncAppliedOps     = storage.TableSyncAppliedOps
	TableSyncLocalEntities  = storage.TableSyncLocalEntities
	TableSyncLocalMutations = storage.TableSyncLocalMutations
	TableSyncConflicts      = storage.TableSyncConflicts
	TableSyncIdentity       = storage.TableSyncIdentity
)
