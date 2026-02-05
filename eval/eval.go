// Package eval provides interpretation and execution of Haiku AST
package eval

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/LingHeChen/haiku/ast"
)

// Scope represents a variable scope
type Scope struct {
	vars   map[string]interface{}
	parent *Scope
}

// NewScope creates a new scope
func NewScope(parent *Scope) *Scope {
	return &Scope{
		vars:   make(map[string]interface{}),
		parent: parent,
	}
}

// Set sets a variable in this scope
func (s *Scope) Set(name string, value interface{}) {
	s.vars[name] = value
}

// Get gets a variable, looking up parent scopes
func (s *Scope) Get(name string) (interface{}, bool) {
	if val, ok := s.vars[name]; ok {
		return val, true
	}
	if s.parent != nil {
		return s.parent.Get(name)
	}
	return nil, false
}

// Evaluator interprets the AST
type Evaluator struct {
	scope             *Scope
	prevResponse      map[string]interface{}
	basePath          string
	requestCallback   func(req map[string]interface{}) (map[string]interface{}, error)
	collectedRequests []map[string]interface{}
}

// EvalOption is a functional option for Evaluator
type EvalOption func(*Evaluator)

// WithBasePath sets the base path for imports
func WithBasePath(path string) EvalOption {
	return func(e *Evaluator) {
		e.basePath = path
	}
}

// WithRequestCallback sets the callback for executing requests
func WithRequestCallback(cb func(req map[string]interface{}) (map[string]interface{}, error)) EvalOption {
	return func(e *Evaluator) {
		e.requestCallback = cb
	}
}

// NewEvaluator creates a new Evaluator
func NewEvaluator(opts ...EvalOption) *Evaluator {
	e := &Evaluator{
		scope: NewScope(nil),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Eval evaluates the program
func (e *Evaluator) Eval(program *ast.Program) ([]map[string]interface{}, error) {
	e.collectedRequests = nil

	for _, stmt := range program.Statements {
		err := e.evalStatementCollect(stmt)
		if err != nil {
			return nil, err
		}
	}

	// Execute collected requests if callback is set
	if e.requestCallback != nil {
		for _, req := range e.collectedRequests {
			response, err := e.requestCallback(req)
			if err != nil {
				return e.collectedRequests, err
			}
			e.prevResponse = response
		}
	}

	return e.collectedRequests, nil
}

// EvalToRequests evaluates the program and returns request maps without executing
func (e *Evaluator) EvalToRequests(program *ast.Program) ([]map[string]interface{}, error) {
	e.collectedRequests = nil

	for _, stmt := range program.Statements {
		err := e.evalStatementCollect(stmt)
		if err != nil {
			return nil, err
		}
	}

	return e.collectedRequests, nil
}

func (e *Evaluator) evalStatement(stmt ast.Statement) (map[string]interface{}, error) {
	switch s := stmt.(type) {
	case *ast.ImportStmt:
		return nil, e.evalImport(s)
	case *ast.VarDefStmt:
		return nil, e.evalVarDef(s)
	case *ast.RequestStmt:
		return e.evalRequest(s)
	case *ast.ForStmt:
		return nil, e.evalForCollect(s)
	case *ast.SeparatorStmt:
		// Separator doesn't produce output
		return nil, nil
	}
	return nil, nil
}

func (e *Evaluator) evalStatementCollect(stmt ast.Statement) error {
	switch s := stmt.(type) {
	case *ast.ImportStmt:
		return e.evalImport(s)
	case *ast.VarDefStmt:
		return e.evalVarDef(s)
	case *ast.RequestStmt:
		req, err := e.evalRequest(s)
		if err != nil {
			return err
		}
		if req != nil {
			e.collectedRequests = append(e.collectedRequests, req)
			// Use as mock response for chaining
			e.prevResponse = req
		}
		return nil
	case *ast.ForStmt:
		return e.evalForCollect(s)
	case *ast.SeparatorStmt:
		return nil
	}
	return nil
}

func (e *Evaluator) evalImport(stmt *ast.ImportStmt) error {
	path := stmt.Path
	if e.basePath != "" && !strings.HasPrefix(path, "/") {
		path = e.basePath + "/" + path
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("import error: %w", err)
	}

	// Parse and evaluate the imported file
	importProgram, err := parseImportedFile(string(content))
	if err != nil {
		return fmt.Errorf("import parse error: %w", err)
	}

	// Only extract variable definitions from imports
	for _, stmt := range importProgram.Statements {
		if varDef, ok := stmt.(*ast.VarDefStmt); ok {
			e.evalVarDef(varDef)
		}
	}

	return nil
}

func (e *Evaluator) evalVarDef(stmt *ast.VarDefStmt) error {
	if stmt.Value == nil {
		e.scope.Set(stmt.Name, nil)
		return nil
	}

	val := e.evalExpr(stmt.Value)
	e.scope.Set(stmt.Name, val)
	return nil
}

func (e *Evaluator) evalRequest(stmt *ast.RequestStmt) (map[string]interface{}, error) {
	req := make(map[string]interface{})

	// Method
	req[stmt.Method] = e.evalExprToValue(stmt.URL)

	// Headers
	if stmt.Headers != nil {
		req["headers"] = e.evalBlockToMap(stmt.Headers)
	}

	// Body
	if stmt.Body != nil {
		bodyVal := e.evalExpr(stmt.Body)
		req["body"] = bodyVal
	}

	return req, nil
}

func (e *Evaluator) evalFor(stmt *ast.ForStmt) error {
	return e.evalForCollect(stmt)
}

func (e *Evaluator) evalForCollect(stmt *ast.ForStmt) error {
	// Evaluate iterable
	iterable := e.evalExpr(stmt.Iterable)

	// Get slice to iterate
	var items []interface{}
	switch v := iterable.(type) {
	case []interface{}:
		items = v
	case map[string]interface{}:
		// Iterate over values
		for _, val := range v {
			items = append(items, val)
		}
	default:
		return fmt.Errorf("for loop: cannot iterate over %T", iterable)
	}

	// Iterate
	for i, item := range items {
		// Create new scope for loop iteration
		loopScope := NewScope(e.scope)
		loopScope.Set(stmt.ItemVar, item)
		if stmt.IndexVar != "" {
			loopScope.Set(stmt.IndexVar, int64(i))
		}

		// Temporarily switch scope
		oldScope := e.scope
		e.scope = loopScope

		// Execute body statements and collect requests
		for _, bodyStmt := range stmt.Body {
			err := e.evalStatementCollect(bodyStmt)
			if err != nil {
				e.scope = oldScope
				return err
			}
		}

		// Restore scope
		e.scope = oldScope
	}

	return nil
}

func (e *Evaluator) evalExpr(expr ast.Expression) interface{} {
	switch ex := expr.(type) {
	case *ast.StringLiteral:
		// Check for variable interpolation in quoted strings
		if ex.Quoted {
			return e.interpolateString(ex.Value)
		}
		return e.inferType(ex.Value)

	case *ast.NumberLiteral:
		if ex.IntVal != nil {
			return *ex.IntVal
		}
		return *ex.FloatVal

	case *ast.BoolLiteral:
		return ex.Value

	case *ast.NullLiteral:
		return nil

	case *ast.EmptyArrayLiteral:
		return []interface{}{}

	case *ast.EmptyObjectLiteral:
		return map[string]interface{}{}

	case *ast.VarRef:
		return e.evalVarRef(ex)

	case *ast.ProcessedString:
		return e.evalProcessedString(ex)

	case *ast.BlockExpr:
		if ex.IsArray() {
			return e.evalBlockToSlice(ex)
		}
		return e.evalBlockToMap(ex)

	case *ast.Literal:
		return ex.Value
	}

	return nil
}

func (e *Evaluator) evalExprToValue(expr ast.Expression) interface{} {
	return e.evalExpr(expr)
}

func (e *Evaluator) evalVarRef(ref *ast.VarRef) interface{} {
	// Handle $_ (previous response)
	if ref.Name == "_" {
		if e.prevResponse == nil {
			return nil
		}
		if len(ref.Path) == 0 {
			return e.prevResponse
		}
		return getNestedValue(e.prevResponse, ref.Path)
	}

	// Handle $env.VAR
	if ref.Name == "env" && len(ref.Path) > 0 {
		return os.Getenv(ref.Path[0])
	}

	// Regular variable
	val, ok := e.scope.Get(ref.Name)
	if !ok {
		return nil
	}

	// If there's a path, navigate into the value
	if len(ref.Path) > 0 {
		return getNestedValue(val, ref.Path)
	}

	return val
}

func (e *Evaluator) evalProcessedString(ps *ast.ProcessedString) interface{} {
	switch ps.Processor {
	case "json":
		var result interface{}
		if err := json.Unmarshal([]byte(ps.Content), &result); err != nil {
			return ps.Content
		}
		return result

	case "base64":
		decoded, err := base64.StdEncoding.DecodeString(ps.Content)
		if err != nil {
			return ps.Content
		}
		return string(decoded)

	case "file":
		data, err := os.ReadFile(ps.Content)
		if err != nil {
			return ps.Content
		}
		// Try to parse as JSON
		var result interface{}
		if err := json.Unmarshal(data, &result); err == nil {
			return result
		}
		return string(data)
	}

	return ps.Content
}

func (e *Evaluator) evalBlockToMap(block *ast.BlockExpr) map[string]interface{} {
	result := make(map[string]interface{})
	for _, entry := range block.Entries {
		if entry.Key != "" {
			result[entry.Key] = e.evalExpr(entry.Value)
		}
	}
	return result
}

func (e *Evaluator) evalBlockToSlice(block *ast.BlockExpr) []interface{} {
	result := make([]interface{}, 0, len(block.Entries))
	for _, entry := range block.Entries {
		result = append(result, e.evalExpr(entry.Value))
	}
	return result
}

func (e *Evaluator) interpolateString(s string) string {
	// Simple variable interpolation: $var or $var.path
	// This is a simplified implementation
	result := s

	// Find all $varname or $varname.path patterns
	i := 0
	for i < len(result) {
		if result[i] == '$' {
			// Find the end of variable reference
			j := i + 1
			for j < len(result) && (isIdentChar(result[j]) || result[j] == '.') {
				j++
			}
			if j > i+1 {
				varRef := result[i+1 : j]
				value := e.resolveVarPath(varRef)
				valueStr := fmt.Sprintf("%v", value)
				result = result[:i] + valueStr + result[j:]
				i += len(valueStr)
				continue
			}
		}
		i++
	}

	return result
}

func (e *Evaluator) resolveVarPath(path string) interface{} {
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return nil
	}

	name := parts[0]

	// Handle $_
	if name == "_" {
		if e.prevResponse == nil {
			return nil
		}
		if len(parts) == 1 {
			return e.prevResponse
		}
		return getNestedValue(e.prevResponse, parts[1:])
	}

	// Handle $env
	if name == "env" && len(parts) > 1 {
		return os.Getenv(parts[1])
	}

	// Regular variable
	val, ok := e.scope.Get(name)
	if !ok {
		return "$" + path // Return original if not found
	}

	if len(parts) == 1 {
		return val
	}

	return getNestedValue(val, parts[1:])
}

func (e *Evaluator) inferType(s string) interface{} {
	// Boolean
	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}

	// Null
	if s == "_" || s == "null" || s == "nil" {
		return nil
	}

	// Integer
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}

	// Float
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}

	// String
	return s
}

// Helper functions

func getNestedValue(data interface{}, path []string) interface{} {
	current := data
	for _, key := range path {
		switch v := current.(type) {
		case map[string]interface{}:
			current = v[key]
		case []interface{}:
			if idx, err := strconv.Atoi(key); err == nil && idx >= 0 && idx < len(v) {
				current = v[idx]
			} else {
				return nil
			}
		default:
			return nil
		}
	}
	return current
}

func isIdentChar(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '_'
}

// parseImportedFile is a placeholder - will be connected to the parser
var parseImportedFile func(string) (*ast.Program, error)

// SetImportParser sets the function to parse imported files
func SetImportParser(fn func(string) (*ast.Program, error)) {
	parseImportedFile = fn
}
