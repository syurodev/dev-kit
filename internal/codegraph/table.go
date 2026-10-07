package codegraph

import "strings"

// EntityTable is a JPA entity and a table it is stored in. An entity has a
// primary table and may have secondary, join and collection ones. What the
// annotations say, not what the database holds: a replaced naming strategy or a
// Liquibase rename is invisible here.
type EntityTable struct {
	// Entity is the entity class's node id.
	Entity string `json:"entity"`
	// EntityName is the name JPQL uses for it.
	EntityName string `json:"entityName"`
	Table      string `json:"table"`
	Schema     string `json:"schema,omitempty"`
	// Kind is primary, secondary, join or collection.
	Kind string `json:"kind"`
	File string `json:"file"`
	Line int    `json:"line"`
}

// The ways a TableAccess is made.
const (
	TableViaQuery         = "repository-query"   // a @Query
	TableViaDerived       = "repository-derived" // findByX…
	TableViaCrud          = "repository-crud"    // save/findById… inherited from Spring Data
	TableViaJDBC          = "jdbc"               // a SQL string passed to query/update/execute
	TableViaEntityManager = "entity-manager"     // createQuery/createNativeQuery
)

// TableAccess is one place that reads or writes a table. From is the node that
// does it: the repository method that declares the query, or the method that
// hands the SQL to JDBC. Whoever calls a repository method is found by the
// call edge, not recorded here.
type TableAccess struct {
	From   string `json:"from"`
	Table  string `json:"table"`
	Schema string `json:"schema,omitempty"`
	// Access is "read" or "write".
	Access string `json:"access"`
	Via    string `json:"via"`
	// Detail is the statement (folded, clipped) or, for inherited CRUD, the repository method.
	Detail string `json:"detail,omitempty"`
	File   string `json:"file"`
	Line   int    `json:"line"`
}

// simpleNodeName is the last dotted segment of a node id, without the
// " [module]" the analyzer adds to tell two same-named classes apart.
func simpleNodeName(id string) string {
	if i := strings.Index(id, " ["); i >= 0 {
		id = id[:i]
	}
	return id[strings.LastIndex(id, ".")+1:]
}
