// internal/lsp/protocol.go
package lsp

// The slice of the Language Server Protocol CodeGraph needs, and no more.
//
// Hand-written rather than generated from the spec's metamodel: the protocol
// is enormous and an indexer uses six requests of it. What is here is what a
// whole-project index is built from — the symbols a file declares, the calls
// each of them makes, and where an import points.

// Position is a zero-based line and character offset. Characters are counted
// in UTF-16 code units unless the server agreed to another encoding during
// initialize, which is why nothing here builds one from a byte offset without
// going through utf16Offset.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// SymbolKind values, named only where this package uses them. The numbers are
// the protocol's and are not ours to change.
const (
	SymbolKindClass       = 5
	SymbolKindMethod      = 6
	SymbolKindProperty    = 7
	SymbolKindField       = 8
	SymbolKindConstructor = 9
	SymbolKindInterface   = 11
	SymbolKindFunction    = 12
	SymbolKindVariable    = 13
	SymbolKindConstant    = 14
	SymbolKindEnum        = 10
)

// DocumentSymbol is one declaration a file makes, with the ones nested inside
// it. Range covers the whole declaration; SelectionRange covers just its name,
// which is the position every other request has to be made at.
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail"`
	Kind           int              `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children"`
}

type CallHierarchyItem struct {
	Name           string `json:"name"`
	Kind           int    `json:"kind"`
	Detail         string `json:"detail"`
	URI            string `json:"uri"`
	Range          Range  `json:"range"`
	SelectionRange Range  `json:"selectionRange"`
	// Data is opaque server state that must be handed back verbatim on the
	// outgoingCalls request; dropping it makes the second call fail on servers
	// that use it.
	Data any `json:"data,omitempty"`
}

type CallHierarchyOutgoingCall struct {
	To CallHierarchyItem `json:"to"`
	// FromRanges are where in the CALLER the calls sit — one entry per call
	// expression, which is what makes a call site list rather than an edge.
	FromRanges []Range `json:"fromRanges"`
}

// Location is what a definition request answers with. Some servers answer with
// LocationLink instead, which is why the decoder accepts both shapes.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

type locationLink struct {
	TargetURI   string `json:"targetUri"`
	TargetRange Range  `json:"targetRange"`
}
