package storage

import (
	"context"
	"database/sql"
)

// migrateWorkspaceMemoryIndex materializes small read models and creates a
// scoped, contentless FTS index: bodies stay solely in the authoritative rows.
func migrateWorkspaceMemoryIndex(ctx context.Context, db *sql.DB) error {
	for _, column := range []struct{ table, name, definition string }{
		{"workspace_memory", "kind", "TEXT NOT NULL DEFAULT ''"},
		{"workspace_memory", "summary_data", "TEXT NOT NULL DEFAULT '{}'"},
		{"workspace_memory_revisions", "summary_data", "TEXT NOT NULL DEFAULT '{}'"},
	} {
		if err := addColumnIfNotExists(ctx, db, column.table, column.name, column.definition); err != nil {
			return err
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
UPDATE workspace_memory SET kind=COALESCE(json_extract(data,'$.kind'),''),summary_data=json_remove(data,'$.body');
UPDATE workspace_memory_revisions SET summary_data=json_object(
 'revision',revision,'operation',operation,
 'title',COALESCE(json_extract(data,'$.title'),''),
 'summary',COALESCE(json_extract(data,'$.summary'),''),
 'updatedAt',COALESCE(json_extract(data,'$.updatedAt'),''),
 'deleted',json(CASE WHEN json_extract(data,'$.deleted') THEN 'true' ELSE 'false' END),
 'source',json(COALESCE(json_extract(data,'$.source'),'{}'))
);
CREATE INDEX IF NOT EXISTS workspace_memory_workspace_kind_updated ON workspace_memory(workspace_id,deleted,kind,updated_at DESC,id DESC);
CREATE VIRTUAL TABLE IF NOT EXISTS workspace_memory_fts USING fts5(workspace_key,title,summary,body,content='',tokenize='unicode61 remove_diacritics 2');
INSERT INTO workspace_memory_fts(workspace_memory_fts)VALUES('delete-all');
INSERT INTO workspace_memory_fts(rowid,workspace_key,title,summary,body) SELECT rowid,'w'||hex(workspace_id),title,summary,body FROM workspace_memory;
CREATE TRIGGER IF NOT EXISTS workspace_memory_fts_insert AFTER INSERT ON workspace_memory BEGIN
 INSERT INTO workspace_memory_fts(rowid,workspace_key,title,summary,body)VALUES(new.rowid,'w'||hex(new.workspace_id),new.title,new.summary,new.body);
END;
CREATE TRIGGER IF NOT EXISTS workspace_memory_fts_delete AFTER DELETE ON workspace_memory BEGIN
 INSERT INTO workspace_memory_fts(workspace_memory_fts,rowid,workspace_key,title,summary,body)VALUES('delete',old.rowid,'w'||hex(old.workspace_id),old.title,old.summary,old.body);
END;
CREATE TRIGGER IF NOT EXISTS workspace_memory_fts_update AFTER UPDATE OF workspace_id,title,summary,body ON workspace_memory BEGIN
 INSERT INTO workspace_memory_fts(workspace_memory_fts,rowid,workspace_key,title,summary,body)VALUES('delete',old.rowid,'w'||hex(old.workspace_id),old.title,old.summary,old.body);
 INSERT INTO workspace_memory_fts(rowid,workspace_key,title,summary,body)VALUES(new.rowid,'w'||hex(new.workspace_id),new.title,new.summary,new.body);
END;`)
	if err != nil {
		return err
	}
	return tx.Commit()
}
