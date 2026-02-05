// Package ast defines the Abstract Syntax Tree for Haiku
package ast

// Node is the base interface for all AST nodes
type Node interface {
	nodeType() string
	Pos() Position
}

// Position represents a location in source code
type Position struct {
	Line   int
	Column int
}

// ---------------------------------------------------------
// Program (root node)
// ---------------------------------------------------------

// Program represents a complete Haiku file
type Program struct {
	Statements []Statement
}

func (p *Program) nodeType() string { return "Program" }
func (p *Program) Pos() Position {
	if len(p.Statements) > 0 {
		return p.Statements[0].Pos()
	}
	return Position{Line: 1, Column: 1}
}

// ---------------------------------------------------------
// Statements
// ---------------------------------------------------------

// Statement is the interface for all statement types
type Statement interface {
	Node
	statementNode()
}

// ImportStmt: import "file.haiku"
type ImportStmt struct {
	Position Position
	Path     string
}

func (s *ImportStmt) nodeType() string  { return "ImportStmt" }
func (s *ImportStmt) Pos() Position     { return s.Position }
func (s *ImportStmt) statementNode()    {}

// VarDefStmt: @name value
type VarDefStmt struct {
	Position Position
	Name     string
	Value    Expression
}

func (s *VarDefStmt) nodeType() string  { return "VarDefStmt" }
func (s *VarDefStmt) Pos() Position     { return s.Position }
func (s *VarDefStmt) statementNode()    {}

// RequestStmt: get "url" headers ... body ... timeout ...
type RequestStmt struct {
	Position Position
	Method   string
	URL      Expression
	Headers  *BlockExpr
	Body     Expression // can be BlockExpr or other Expression
	Timeout  Expression // optional timeout expression (e.g., 30, "30s", "5000ms")
}

func (s *RequestStmt) nodeType() string  { return "RequestStmt" }
func (s *RequestStmt) Pos() Position     { return s.Position }
func (s *RequestStmt) statementNode()    {}

// ForStmt: for $item in $items ... or parallel [N] for $item in $items ...
type ForStmt struct {
	Position    Position
	Parallel    bool        // true if this is a parallel for loop
	Concurrency int         // max concurrent requests (0 means unlimited)
	IndexVar    string      // optional, for "for $i, $item in ..."
	ItemVar     string      // loop variable name
	Iterable    Expression  // the collection to iterate
	Body        []Statement // statements inside the loop
}

func (s *ForStmt) nodeType() string  { return "ForStmt" }
func (s *ForStmt) Pos() Position     { return s.Position }
func (s *ForStmt) statementNode()    {}

// SeparatorStmt: --- (request separator)
type SeparatorStmt struct {
	Position Position
}

func (s *SeparatorStmt) nodeType() string  { return "SeparatorStmt" }
func (s *SeparatorStmt) Pos() Position     { return s.Position }
func (s *SeparatorStmt) statementNode()    {}

// ---------------------------------------------------------
// Expressions
// ---------------------------------------------------------

// Expression is the interface for all expression types
type Expression interface {
	Node
	exprNode()
}

// Literal: "string", 123, 45.6, true, false, null, [], {}
type Literal struct {
	Position Position
	Value    interface{} // string, int64, float64, bool, nil, []interface{}, map[string]interface{}
}

func (e *Literal) nodeType() string  { return "Literal" }
func (e *Literal) Pos() Position     { return e.Position }
func (e *Literal) exprNode()         {}

// StringLiteral: "quoted string" or unquoted-string
type StringLiteral struct {
	Position Position
	Value    string
	Quoted   bool
}

func (e *StringLiteral) nodeType() string  { return "StringLiteral" }
func (e *StringLiteral) Pos() Position     { return e.Position }
func (e *StringLiteral) exprNode()         {}

// NumberLiteral: 123, 45.6
type NumberLiteral struct {
	Position Position
	IntVal   *int64
	FloatVal *float64
}

func (e *NumberLiteral) nodeType() string  { return "NumberLiteral" }
func (e *NumberLiteral) Pos() Position     { return e.Position }
func (e *NumberLiteral) exprNode()         {}

// BoolLiteral: true, false
type BoolLiteral struct {
	Position Position
	Value    bool
}

func (e *BoolLiteral) nodeType() string  { return "BoolLiteral" }
func (e *BoolLiteral) Pos() Position     { return e.Position }
func (e *BoolLiteral) exprNode()         {}

// NullLiteral: null, nil, _
type NullLiteral struct {
	Position Position
}

func (e *NullLiteral) nodeType() string  { return "NullLiteral" }
func (e *NullLiteral) Pos() Position     { return e.Position }
func (e *NullLiteral) exprNode()         {}

// EmptyArrayLiteral: []
type EmptyArrayLiteral struct {
	Position Position
}

func (e *EmptyArrayLiteral) nodeType() string  { return "EmptyArrayLiteral" }
func (e *EmptyArrayLiteral) Pos() Position     { return e.Position }
func (e *EmptyArrayLiteral) exprNode()         {}

// EmptyObjectLiteral: {}
type EmptyObjectLiteral struct {
	Position Position
}

func (e *EmptyObjectLiteral) nodeType() string  { return "EmptyObjectLiteral" }
func (e *EmptyObjectLiteral) Pos() Position     { return e.Position }
func (e *EmptyObjectLiteral) exprNode()         {}

// VarRef: $name, $obj.field, $arr.0, $env.HOME, $_
type VarRef struct {
	Position Position
	Name     string   // base variable name (e.g., "name", "env", "_")
	Path     []string // field path (e.g., ["field", "subfield"] for $obj.field.subfield)
}

func (e *VarRef) nodeType() string  { return "VarRef" }
func (e *VarRef) Pos() Position     { return e.Position }
func (e *VarRef) exprNode()         {}

// FullPath returns the complete variable path as a string
func (e *VarRef) FullPath() string {
	if len(e.Path) == 0 {
		return e.Name
	}
	result := e.Name
	for _, p := range e.Path {
		result += "." + p
	}
	return result
}

// ProcessedString: json`...`, base64`...`, file`...`
type ProcessedString struct {
	Position  Position
	Processor string // "json", "base64", "file", etc.
	Content   string // content inside backticks
}

func (e *ProcessedString) nodeType() string  { return "ProcessedString" }
func (e *ProcessedString) Pos() Position     { return e.Position }
func (e *ProcessedString) exprNode()         {}

// BlockExpr: indented block of key-value pairs or list items
type BlockExpr struct {
	Position Position
	Entries  []Entry
}

func (e *BlockExpr) nodeType() string  { return "BlockExpr" }
func (e *BlockExpr) Pos() Position     { return e.Position }
func (e *BlockExpr) exprNode()         {}

// IsArray returns true if this block represents an array (all entries have no key)
func (e *BlockExpr) IsArray() bool {
	for _, entry := range e.Entries {
		if entry.Key != "" && entry.Value != nil {
			return false
		}
	}
	return true
}

// Entry represents a key-value pair or a list item in a block
type Entry struct {
	Position Position
	Key      string     // empty for array items
	Value    Expression // the value (can be another BlockExpr for nesting)
}

// ---------------------------------------------------------
// Utility functions
// ---------------------------------------------------------

// IsHTTPMethod checks if a string is a valid HTTP method
func IsHTTPMethod(s string) bool {
	switch s {
	case "get", "post", "put", "delete", "patch", "head", "options":
		return true
	}
	return false
}
