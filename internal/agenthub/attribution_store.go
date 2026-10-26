package agenthub

import "database/sql"

func scanAttribution(account, generation, started, workspace sql.NullString, version, profile, override sql.NullInt64) (*TurnAttribution, error) {
	if !version.Valid {
		return nil, nil
	}
	a := &TurnAttribution{ContextVersion: int(version.Int64), Facts: RuntimeUsageFacts{AccountID: account.String, AccountGeneration: generation.String, WorkspaceID: workspace.String}}
	if profile.Valid {
		v := profile.Int64
		a.Facts.ProfileRevision = &v
	}
	if override.Valid {
		v := override.Int64
		a.Facts.OverrideRevision = &v
	}
	if started.Valid {
		t, e := parseStoreTime(started.String)
		if e != nil {
			return nil, e
		}
		a.StartedAt = t
	}
	return a, nil
}
