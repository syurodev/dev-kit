package codegraph

import (
	"strings"
	"testing"
)

// tableIndex is the shape the feedback described: an entity with a default
// table name, a service that saves and reads it through inherited CRUD, a
// repository with a native delete a second service calls, a derived query, a
// JDBC count, a join table, and an endpoint on each path.
func tableIndex() AnalysisResult {
	const (
		entity    = "acme.OrganizationApiEnablement"
		ctlEnable = "acme.EnablementController.enable()"
		ctlCheck  = "acme.EnablementController.check()"
		ctlReset  = "acme.AdminController.reset()"
		svcEnable = "acme.EnablementService.enable()"
		svcHas    = "acme.EnablementService.has()"
		svcDrop   = "acme.EnablementService.drop()"
		repoPurge = "acme.EnablementRepo.purge(java.lang.String)"
		repoFind  = "acme.EnablementRepo.findByOrg(java.lang.String)"
		daoCount  = "acme.Dao.count()"
		daoTags   = "acme.Dao.tags()"
		apiEntity = "acme.ApiDefinition"
		apiSave   = "acme.ApiService.save()"
		apiScopes = "acme.ApiService.scopes()"
	)
	cls := func(id string) Node {
		n := method(id)
		n.Kind = "class"
		return n
	}
	return AnalysisResult{
		Nodes: []Node{
			cls(entity), cls(apiEntity), method(apiSave), method(apiScopes),
			method(ctlEnable), method(ctlCheck), method(ctlReset),
			method(svcEnable), method(svcHas), method(svcDrop),
			method(repoPurge), method(repoFind), method(daoCount), method(daoTags),
		},
		Edges: []Edge{
			{From: ctlEnable, To: svcEnable, Kind: "call"},
			{From: ctlCheck, To: svcHas, Kind: "call"},
			{From: ctlReset, To: svcDrop, Kind: "call"},
			{From: svcDrop, To: repoPurge, Kind: "call"},
			{From: svcHas, To: repoFind, Kind: "call"},
			{From: ctlCheck, To: daoCount, Kind: "call"},
		},
		EntryPoints: []EntryPoint{
			{ID: "POST /v1/enable", Verb: "POST", Target: "/v1/enable", EntryNode: ctlEnable},
			{ID: "GET /v1/check", Verb: "GET", Target: "/v1/check", EntryNode: ctlCheck},
			{ID: "POST /admin/reset", Verb: "POST", Target: "/admin/reset", EntryNode: ctlReset},
		},
		EntityTables: []EntityTable{
			{Entity: entity, EntityName: "OrganizationApiEnablement", Table: "organization_api_enablement", Kind: "primary", File: "E.java", Line: 3},
			{Entity: entity, EntityName: "OrganizationApiEnablement", Table: "enablement_tags", Kind: "collection", File: "E.java", Line: 8},
			// @Entity(name = "Api") on class ApiDefinition: the JPQL name and the table are different words.
			{Entity: apiEntity, EntityName: "Api", Table: "api_definition", Kind: "primary", File: "A.java", Line: 3},
			{Entity: apiEntity, EntityName: "Api", Table: "api_scopes", Kind: "join", File: "A.java", Line: 7},
		},
		TableAccesses: []TableAccess{
			{From: svcEnable, Table: "organization_api_enablement", Access: "write", Via: TableViaCrud, Detail: "EnablementRepo.save", File: "S.java", Line: 10},
			{From: svcHas, Table: "organization_api_enablement", Access: "read", Via: TableViaCrud, Detail: "EnablementRepo.existsById", File: "S.java", Line: 14},
			{From: repoPurge, Table: "organization_api_enablement", Access: "write", Via: TableViaQuery, Detail: "delete from organization_api_enablement where org = ?1", File: "R.java", Line: 5},
			{From: repoFind, Table: "organization_api_enablement", Access: "read", Via: TableViaDerived, Detail: "findByOrg", File: "R.java", Line: 8},
			{From: daoCount, Table: "organization_api_enablement", Access: "read", Via: TableViaJDBC, Detail: "select count(*) from organization_api_enablement", File: "D.java", Line: 6},
			{From: daoTags, Table: "enablement_tags", Access: "read", Via: TableViaJDBC, Detail: "select tag from enablement_tags", File: "D.java", Line: 9},
			{From: apiSave, Table: "api_definition", Access: "write", Via: TableViaCrud, Detail: "ApiRepo.save", File: "AS.java", Line: 4},
			{From: apiScopes, Table: "api_scopes", Access: "read", Via: TableViaJDBC, Detail: "select * from api_scopes", File: "AS.java", Line: 8},
		},
	}
}

func TestFindTableUsageForTheTableThatIsAboutToBeDropped(t *testing.T) {
	svc := openWith(t, tableIndex())
	report, err := svc.FindTableUsage(t.Context(), TableUsageQuery{Table: "organization_api_enablement"})
	if err != nil {
		t.Fatal(err)
	}

	if len(report.Entities) != 2 || report.Entities[0].Label != "acme.OrganizationApiEnablement" {
		t.Fatalf("entities = %+v: the entity and its collection table", report.Entities)
	}
	if report.AccessTotal != 5 || report.Reads != 3 || report.Writes != 2 {
		t.Fatalf("accesses %d (reads %d, writes %d), want 5 (3, 2): the table's, not the join table's", report.AccessTotal, report.Reads, report.Writes)
	}
	if len(report.Tables) != 1 || report.Tables[0] != "organization_api_enablement" {
		t.Fatalf("tables = %v", report.Tables)
	}

	// The repository method's callers come with it.
	var purge TableAccessDetail
	for _, a := range report.Accesses {
		if a.From == "acme.EnablementRepo.purge(java.lang.String)" {
			purge = a
		}
	}
	if purge.Via != TableViaQuery || len(purge.CalledBy) != 1 || purge.CalledBy[0].NodeID != "acme.EnablementService.drop()" {
		t.Fatalf("purge = %+v", purge)
	}

	// Which endpoints touch it, and how.
	access := map[string]string{}
	for _, e := range report.EntryPoints {
		access[e.ID] = e.Access
	}
	want := map[string]string{
		"POST /v1/enable":   "write",
		"GET /v1/check":     "read",
		"POST /admin/reset": "write",
	}
	if len(access) != len(want) {
		t.Fatalf("entry points = %+v", report.EntryPoints)
	}
	for id, a := range want {
		if access[id] != a {
			t.Fatalf("%s: access = %q, want %q (all: %v)", id, access[id], a, access)
		}
	}
}

func TestFindTableUsageTellsReadFromWriteAndAnEndpointThatDoesBoth(t *testing.T) {
	result := tableIndex()
	// One endpoint that saves and checks.
	result.Edges = append(result.Edges, Edge{From: "acme.EnablementController.enable()", To: "acme.EnablementService.has()", Kind: "call"})
	svc := openWith(t, result)
	report, err := svc.FindTableUsage(t.Context(), TableUsageQuery{Table: "organization_api_enablement"})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range report.EntryPoints {
		if e.ID == "POST /v1/enable" && e.Access != "read, write" {
			t.Fatalf("%s access = %q, want read, write", e.ID, e.Access)
		}
	}
}

// The table, the JPQL entity name and the class are three spellings of one question.
func TestFindTableUsageFindsTheTableByEntityOrClassName(t *testing.T) {
	svc := openWith(t, tableIndex())
	var totals []int
	for _, name := range []string{"organization_api_enablement", "OrganizationApiEnablement", "organizationapienablement"} {
		report, err := svc.FindTableUsage(t.Context(), TableUsageQuery{Table: name})
		if err != nil {
			t.Fatal(err)
		}
		totals = append(totals, report.AccessTotal*100+len(report.Entities)*10+report.EntryPointTotal)
	}
	if totals[0] != totals[1] || totals[1] != totals[2] || totals[0] == 0 {
		t.Fatalf("spellings disagree: %v", totals)
	}
}

// Asking by an entity name that is not also a table name brings all of the entity's tables.
func TestFindTableUsageByEntityNameIncludesItsJoinAndCollectionTables(t *testing.T) {
	svc := openWith(t, tableIndex())
	report, err := svc.FindTableUsage(t.Context(), TableUsageQuery{Table: "Api"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Tables) != 2 || report.AccessTotal != 2 {
		t.Fatalf("tables %v, accesses %d: want both of the entity's tables", report.Tables, report.AccessTotal)
	}
	// The class name is the table's name in other letters (the default naming), so it asks about the table.
	if byClass, _ := svc.FindTableUsage(t.Context(), TableUsageQuery{Table: "ApiDefinition"}); byClass.AccessTotal != 1 {
		t.Fatalf("by class name: %d accesses, want just the table's 1", byClass.AccessTotal)
	}
}

// Asking by a table name is about that table; the entity's other tables are said, not mixed in.
func TestFindTableUsageByTableNameKeepsToThatTable(t *testing.T) {
	svc := openWith(t, tableIndex())
	report, err := svc.FindTableUsage(t.Context(), TableUsageQuery{Table: "api_definition"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Tables) != 1 || report.AccessTotal != 1 {
		t.Fatalf("tables %v, accesses %d", report.Tables, report.AccessTotal)
	}
	said := false
	for _, n := range report.Notes {
		if strings.Contains(n, "api_scopes") {
			said = true
		}
	}
	if !said {
		t.Fatalf("the entity's join table should be mentioned: %v", report.Notes)
	}
}

func TestFindTableUsageCanAskForWritesOnly(t *testing.T) {
	svc := openWith(t, tableIndex())
	report, err := svc.FindTableUsage(t.Context(), TableUsageQuery{Table: "organization_api_enablement", Access: "write"})
	if err != nil {
		t.Fatal(err)
	}
	if report.AccessTotal != 2 || report.Reads != 0 {
		t.Fatalf("accesses %d, reads %d", report.AccessTotal, report.Reads)
	}
	for _, e := range report.EntryPoints {
		if e.Access != "write" {
			t.Fatalf("a writes-only question reached %s by %s", e.ID, e.Access)
		}
	}
	if report.EntryPointTotal != 2 {
		t.Fatalf("entry points = %+v", report.EntryPoints)
	}
}

func TestFindTableUsageByPrefix(t *testing.T) {
	svc := openWith(t, tableIndex())
	exact, _ := svc.FindTableUsage(t.Context(), TableUsageQuery{Table: "enablement"})
	if exact.AccessTotal != 0 || len(exact.Entities) != 0 {
		t.Fatalf("an exact match on a prefix finds nothing: %+v", exact)
	}
	report, err := svc.FindTableUsage(t.Context(), TableUsageQuery{Table: "enablement", Match: MatchPrefix})
	if err != nil {
		t.Fatal(err)
	}
	if report.AccessTotal != 1 || len(report.Tables) != 1 || report.Tables[0] != "enablement_tags" {
		t.Fatalf("prefix: %d accesses to %v", report.AccessTotal, report.Tables)
	}
}

func TestFindTableUsageForAnUnknownTable(t *testing.T) {
	svc := openWith(t, tableIndex())
	report, err := svc.FindTableUsage(t.Context(), TableUsageQuery{Table: "no_such_table"})
	if err != nil {
		t.Fatal(err)
	}
	if report.AccessTotal != 0 || len(report.Notes) == 0 {
		t.Fatalf("report = %+v", report)
	}
	if report.Entities == nil || report.Tables == nil || report.Accesses == nil || report.EntryPoints == nil {
		t.Fatal("empty lists must be [] in JSON, not null")
	}
}

func TestFindTableUsageRejectsBadInput(t *testing.T) {
	svc := openWith(t, tableIndex())
	for name, q := range map[string]TableUsageQuery{
		"match":  {Table: "x", Match: "fuzzy"},
		"access": {Table: "x", Access: "drop"},
		"empty":  {Table: "..."},
		"limit":  {Table: "x", Limit: -1},
	} {
		if _, err := svc.FindTableUsage(t.Context(), q); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestAnIndexWithATableAccessFromAnUnknownNodeIsRejected(t *testing.T) {
	bad := AnalysisResult{Nodes: []Node{method("a")}, TableAccesses: []TableAccess{{From: "missing", Table: "t", Access: "read"}}}
	if err := bad.Validate(); err == nil {
		t.Fatal("a table access from a node the index lacks must fail validation")
	}
	bad = AnalysisResult{Nodes: []Node{method("a")}, EntityTables: []EntityTable{{Entity: "missing", Table: "t"}}}
	if err := bad.Validate(); err == nil {
		t.Fatal("an entity the index lacks must fail validation")
	}
}

func TestSimpleNodeName(t *testing.T) {
	for in, want := range map[string]string{
		"acme.OrganizationApiEnablement": "OrganizationApiEnablement",
		"acme.Outer.Inner":               "Inner",
		"acme.Thing [billing]":           "Thing",
		"Plain":                          "Plain",
	} {
		if got := simpleNodeName(in); got != want {
			t.Errorf("simpleNodeName(%q) = %q, want %q", in, got, want)
		}
	}
}
