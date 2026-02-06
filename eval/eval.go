// Package eval provides interpretation and execution of Haiku AST
package eval

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

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
	defaultTimeout    time.Duration // global default timeout
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
		scope:          NewScope(nil),
		defaultTimeout: 30 * time.Second, // default 30 seconds
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
	case *ast.IfStmt:
		return nil, e.evalIf(s)
	case *ast.EchoStmt:
		return nil, e.evalEcho(s)
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
	case *ast.IfStmt:
		return e.evalIf(s)
	case *ast.EchoStmt:
		return e.evalEcho(s)
	case *ast.SeparatorStmt:
		return nil
	}
	return nil
}

// EvalImport evaluates an import statement (public method)
func (e *Evaluator) EvalImport(stmt *ast.ImportStmt) error {
	return e.evalImport(stmt)
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

	// Evaluate all statements in the imported file (including if statements, variable definitions, etc.)
	for _, stmt := range importProgram.Statements {
		if err := e.evalStatementCollect(stmt); err != nil {
			return fmt.Errorf("import evaluation error: %w", err)
		}
	}

	return nil
}

// EvalVarDef evaluates a variable definition (public method)
func (e *Evaluator) EvalVarDef(stmt *ast.VarDefStmt) error {
	return e.evalVarDef(stmt)
}

func (e *Evaluator) evalVarDef(stmt *ast.VarDefStmt) error {
	if stmt.Value == nil {
		e.scope.Set(stmt.Name, nil)
		return nil
	}

	val := e.evalExpr(stmt.Value)
	e.scope.Set(stmt.Name, val)
	
	// Special handling for @timeout variable
	if stmt.Name == "timeout" {
		if timeout, err := parseTimeout(val); err == nil {
			e.defaultTimeout = timeout
		}
	}
	
	return nil
}

// EvalRequest evaluates a request statement (public method)
func (e *Evaluator) EvalRequest(stmt *ast.RequestStmt) (map[string]interface{}, error) {
	return e.evalRequest(stmt)
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

	// Timeout: request-level timeout takes precedence over global timeout
	if stmt.Timeout != nil {
		timeoutVal := e.evalExpr(stmt.Timeout)
		if timeout, err := parseTimeout(timeoutVal); err == nil {
			req["timeout"] = timeout
		} else {
			return nil, fmt.Errorf("invalid timeout value: %v", timeoutVal)
		}
	} else if e.defaultTimeout > 0 {
		// Use global default timeout if no request-level timeout specified
		req["timeout"] = e.defaultTimeout
	}

	return req, nil
}

// ParallelStats holds statistics from parallel execution
type ParallelStats struct {
	Total     int
	Success   int
	Failed    int
	TotalTime time.Duration
	MinTime   time.Duration
	MaxTime   time.Duration
	AvgTime   time.Duration
}

func (e *Evaluator) evalFor(stmt *ast.ForStmt) error {
	return e.evalForCollect(stmt)
}

// EvalForCollect evaluates a for loop and collects/executes requests (public method)
func (e *Evaluator) EvalForCollect(stmt *ast.ForStmt) error {
	return e.evalForCollect(stmt)
}

// EvalIf evaluates an if statement (public method)
func (e *Evaluator) EvalIf(stmt *ast.IfStmt) error {
	return e.evalIf(stmt)
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
	case int64:
		// Convert number to range [0, 1, 2, ..., N-1]
		if v < 0 {
			return fmt.Errorf("for loop: cannot iterate over negative number %d", v)
		}
		items = make([]interface{}, v)
		for i := int64(0); i < v; i++ {
			items[i] = i
		}
	case float64:
		// Convert float to int and create range
		n := int64(v)
		if v < 0 || float64(n) != v {
			return fmt.Errorf("for loop: cannot iterate over non-positive integer %g", v)
		}
		items = make([]interface{}, n)
		for i := int64(0); i < n; i++ {
			items[i] = i
		}
	default:
		return fmt.Errorf("for loop: cannot iterate over %T", iterable)
	}

	// Handle parallel execution
	// Note: When called from EvalToRequests (no callback), we still need to collect requests
	// When called from main.go with callback, EvalParallelForWithOutput is used instead
	if stmt.Parallel {
		// For request collection (no callback), use evalParallelFor but disable output
		// For execution (with callback), EvalParallelForWithOutput is used in main.go
		return e.evalParallelFor(stmt, items)
	}

	// Sequential iteration
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

		// Execute body statements
		for _, bodyStmt := range stmt.Body {
			if reqStmt, ok := bodyStmt.(*ast.RequestStmt); ok {
				// Evaluate request
				req, err := e.evalRequest(reqStmt)
				if err != nil {
					e.scope = oldScope
					return err
				}
				if req != nil {
					// Execute request if callback is set (for real-time output)
					if e.requestCallback != nil {
						resp, err := e.requestCallback(req)
						if err != nil {
							e.scope = oldScope
							return err
						}
						// Update prevResponse for chaining
						if resp != nil {
							e.prevResponse = resp
						}
					} else {
						// No callback: just collect
						e.collectedRequests = append(e.collectedRequests, req)
						e.prevResponse = req
					}
				}
			} else {
				// Non-request statement: use evalStatementCollect
				err := e.evalStatementCollect(bodyStmt)
				if err != nil {
					e.scope = oldScope
					return err
				}
			}
		}

		// Restore scope
		e.scope = oldScope
	}

	return nil
}

func (e *Evaluator) evalParallelFor(stmt *ast.ForStmt, items []interface{}) error {
	if len(items) == 0 {
		return nil
	}

	// Determine concurrency limit
	concurrency := stmt.Concurrency
	if concurrency <= 0 {
		concurrency = len(items) // Unlimited (all at once)
	}

	// Create semaphore for concurrency control
	sem := make(chan struct{}, concurrency)
	
	// WaitGroup for synchronization
	var wg sync.WaitGroup
	
	// Mutex for thread-safe collection
	var mu sync.Mutex
	var errors []error
	var parallelRequests []map[string]interface{}
	
	// Statistics
	var stats ParallelStats
	stats.Total = len(items)
	var times []time.Duration

	for i, item := range items {
		wg.Add(1)
		
		go func(idx int, itm interface{}) {
			defer wg.Done()
			
			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()
			
			start := time.Now()
			
			// Create new scope for loop iteration
			loopScope := NewScope(e.scope)
			loopScope.Set(stmt.ItemVar, itm)
			if stmt.IndexVar != "" {
				loopScope.Set(stmt.IndexVar, int64(idx))
			}
			
			// Create a temporary evaluator for this goroutine
			// to avoid concurrent access issues
			tempEval := &Evaluator{
				scope:          loopScope,
				prevResponse:   e.prevResponse,
				basePath:       e.basePath,
				requestCallback: e.requestCallback,
				defaultTimeout: e.defaultTimeout, // Copy default timeout
			}
			
			// Evaluate body statements
			var iterRequests []map[string]interface{}
			for _, bodyStmt := range stmt.Body {
				if reqStmt, ok := bodyStmt.(*ast.RequestStmt); ok {
					req, err := tempEval.evalRequest(reqStmt)
					if err != nil {
						mu.Lock()
						errors = append(errors, err)
						stats.Failed++
						mu.Unlock()
						return
					}
					if req != nil {
						iterRequests = append(iterRequests, req)
						
						// Don't execute callback here - it's handled by EvalParallelForWithOutput
						// This function is only for collecting requests (EvalToRequests)
					}
				}
			}
			
			elapsed := time.Since(start)
			
			mu.Lock()
			parallelRequests = append(parallelRequests, iterRequests...)
			times = append(times, elapsed)
			if len(errors) == 0 || errors[len(errors)-1] == nil {
				stats.Success++
			}
			mu.Unlock()
		}(i, item)
	}
	
	wg.Wait()
	
	// Calculate statistics
	if len(times) > 0 {
		stats.MinTime = times[0]
		stats.MaxTime = times[0]
		var totalTime time.Duration
		for _, t := range times {
			totalTime += t
			if t < stats.MinTime {
				stats.MinTime = t
			}
			if t > stats.MaxTime {
				stats.MaxTime = t
			}
		}
		stats.TotalTime = totalTime
		stats.AvgTime = totalTime / time.Duration(len(times))
	}
	
	// Add collected requests (but mark them as already executed if callback was set)
	if e.requestCallback == nil {
		e.collectedRequests = append(e.collectedRequests, parallelRequests...)
	}
	
	// Store stats in a special variable for potential output
	statsMap := map[string]interface{}{
		"total":      stats.Total,
		"success":    stats.Success,
		"failed":     stats.Failed,
		"total_time": stats.TotalTime.String(),
		"min_time":   stats.MinTime.String(),
		"max_time":   stats.MaxTime.String(),
		"avg_time":   stats.AvgTime.String(),
	}
	e.scope.Set("_parallel_stats", statsMap) // keep last stats for compatibility

	// Also append to a list so multiple parallel loops are visible
	if existing, ok := e.scope.Get("_parallel_stats_list"); ok {
		if list, ok := existing.([]interface{}); ok {
			list = append(list, statsMap)
			e.scope.Set("_parallel_stats_list", list)
		} else {
			e.scope.Set("_parallel_stats_list", []interface{}{statsMap})
		}
	} else {
		e.scope.Set("_parallel_stats_list", []interface{}{statsMap})
	}
	
	if len(errors) > 0 {
		return fmt.Errorf("parallel execution had %d errors, first: %v", len(errors), errors[0])
	}
	
	return nil
}

// GetParallelStats returns the stats from the last parallel execution
func (e *Evaluator) GetParallelStats() map[string]interface{} {
	if val, ok := e.scope.Get("_parallel_stats"); ok {
		if stats, ok := val.(map[string]interface{}); ok {
			return stats
		}
	}
	return nil
}

// GetAllParallelStats returns the stats from all parallel loops (in order)
func (e *Evaluator) GetAllParallelStats() []map[string]interface{} {
	val, ok := e.scope.Get("_parallel_stats_list")
	if !ok {
		return nil
	}
	list, ok := val.([]interface{})
	if !ok {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// GetRequestCallback returns the request callback function
func (e *Evaluator) GetRequestCallback() func(req map[string]interface{}) (map[string]interface{}, error) {
	return e.requestCallback
}

// SetPrevResponse sets the previous response for chaining
func (e *Evaluator) SetPrevResponse(resp map[string]interface{}) {
	e.prevResponse = resp
}

// EvalParallelForWithOutput evaluates a parallel for loop with real-time output
func (e *Evaluator) EvalParallelForWithOutput(stmt *ast.ForStmt) error {
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
	case int64:
		// Convert number to range [0, 1, 2, ..., N-1]
		if v < 0 {
			return fmt.Errorf("for loop: cannot iterate over negative number %d", v)
		}
		items = make([]interface{}, v)
		for i := int64(0); i < v; i++ {
			items[i] = i
		}
	case float64:
		// Convert float to int and create range
		n := int64(v)
		if v < 0 || float64(n) != v {
			return fmt.Errorf("for loop: cannot iterate over non-positive integer %g", v)
		}
		items = make([]interface{}, n)
		for i := int64(0); i < n; i++ {
			items[i] = i
		}
	default:
		return fmt.Errorf("for loop: cannot iterate over %T", iterable)
	}

	if len(items) == 0 {
		return nil
	}

	// Record start time for wall time calculation
	loopStartTime := time.Now()

	// Determine concurrency limit
	concurrency := stmt.Concurrency
	if concurrency <= 0 {
		concurrency = len(items) // Unlimited (all at once)
	}

	// Create semaphore for concurrency control
	sem := make(chan struct{}, concurrency)
	
	// WaitGroup for synchronization
	var wg sync.WaitGroup
	
	// Mutex for thread-safe collection
	var mu sync.Mutex
	var errors []error
	
	// Statistics
	var stats ParallelStats
	stats.Total = len(items)
	var times []time.Duration

	for i, item := range items {
		wg.Add(1)
		
		go func(idx int, itm interface{}) {
			defer wg.Done()
			
			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()
			
			start := time.Now()
			
			// Create new scope for loop iteration
			loopScope := NewScope(e.scope)
			loopScope.Set(stmt.ItemVar, itm)
			if stmt.IndexVar != "" {
				loopScope.Set(stmt.IndexVar, int64(idx))
			}
			
			// Create a temporary evaluator for this goroutine
			tempEval := &Evaluator{
				scope:          loopScope,
				prevResponse:   e.prevResponse,
				basePath:       e.basePath,
				requestCallback: e.requestCallback,
				defaultTimeout: e.defaultTimeout, // Copy default timeout
			}
			
			// Evaluate body statements and execute requests with real-time output
			for _, bodyStmt := range stmt.Body {
				if reqStmt, ok := bodyStmt.(*ast.RequestStmt); ok {
					req, err := tempEval.evalRequest(reqStmt)
					if err != nil {
						mu.Lock()
						errors = append(errors, err)
						stats.Failed++
						mu.Unlock()
						return
					}
					if req != nil && e.requestCallback != nil {
						// Execute request and output immediately
						_, err := e.requestCallback(req)
						if err != nil {
							mu.Lock()
							errors = append(errors, err)
							stats.Failed++
							mu.Unlock()
							return
						}
					}
				}
			}
			
			elapsed := time.Since(start)
			
			mu.Lock()
			times = append(times, elapsed)
			stats.Success++
			mu.Unlock()
		}(i, item)
	}
	
	wg.Wait()
	
	// Calculate wall time (actual elapsed time for the parallel loop)
	wallTime := time.Since(loopStartTime)
	
	// Calculate statistics
	if len(times) > 0 {
		stats.MinTime = times[0]
		stats.MaxTime = times[0]
		var totalTime time.Duration
		for _, t := range times {
			totalTime += t
			if t < stats.MinTime {
				stats.MinTime = t
			}
			if t > stats.MaxTime {
				stats.MaxTime = t
			}
		}
		stats.TotalTime = totalTime
		stats.AvgTime = totalTime / time.Duration(len(times))
	}
	
	// Store stats in a special variable for potential output
	statsMap := map[string]interface{}{
		"total":      stats.Total,
		"success":    stats.Success,
		"failed":     stats.Failed,
		"total_time":  stats.TotalTime.String(),
		"min_time":    stats.MinTime.String(),
		"max_time":    stats.MaxTime.String(),
		"avg_time":    stats.AvgTime.String(),
		"wall_time":   wallTime.String(),
	}
	e.scope.Set("_parallel_stats", statsMap) // keep last stats for compatibility

	// Also append to a list so multiple parallel loops are visible
	if existing, ok := e.scope.Get("_parallel_stats_list"); ok {
		if list, ok := existing.([]interface{}); ok {
			list = append(list, statsMap)
			e.scope.Set("_parallel_stats_list", list)
		} else {
			e.scope.Set("_parallel_stats_list", []interface{}{statsMap})
		}
	} else {
		e.scope.Set("_parallel_stats_list", []interface{}{statsMap})
	}
	
	if len(errors) > 0 {
		return fmt.Errorf("parallel execution had %d errors, first: %v", len(errors), errors[0])
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

	case *ast.BinaryExpr:
		return e.evalBinaryExpr(ex)

	case *ast.UnaryExpr:
		return e.evalUnaryExpr(ex)
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

// EvalEcho evaluates an echo statement (public method)
func (e *Evaluator) EvalEcho(stmt *ast.EchoStmt) error {
	return e.evalEcho(stmt)
}

func (e *Evaluator) evalEcho(stmt *ast.EchoStmt) error {
	if stmt.Value == nil {
		fmt.Fprintln(os.Stderr, "[echo]")
		return nil
	}
	val := e.evalExpr(stmt.Value)
	fmt.Fprintf(os.Stderr, "[echo] %v\n", val)
	return nil
}

func (e *Evaluator) evalIf(stmt *ast.IfStmt) error {
	// Try each branch in order
	for _, branch := range stmt.Branches {
		condition := e.evalExpr(branch.Condition)
		if e.isTruthy(condition) {
			// Execute this branch
			for _, s := range branch.Body {
				if err := e.evalStatementCollect(s); err != nil {
					return err
				}
			}
			return nil
		}
	}
	
	// No branch matched, execute else if present
	for _, s := range stmt.Else {
		if err := e.evalStatementCollect(s); err != nil {
			return err
		}
	}
	
	return nil
}

func (e *Evaluator) evalBinaryExpr(expr *ast.BinaryExpr) interface{} {
	left := e.evalExpr(expr.Left)
	right := e.evalExpr(expr.Right)

	switch expr.Operator {
	case "==":
		return e.compareValues(left, right) == 0
	case "!=":
		return e.compareValues(left, right) != 0
	case ">":
		return e.compareValues(left, right) > 0
	case "<":
		return e.compareValues(left, right) < 0
	case ">=":
		return e.compareValues(left, right) >= 0
	case "<=":
		return e.compareValues(left, right) <= 0
	case "and":
		return e.isTruthy(left) && e.isTruthy(right)
	case "or":
		return e.isTruthy(left) || e.isTruthy(right)
	default:
		return false
	}
}

func (e *Evaluator) evalUnaryExpr(expr *ast.UnaryExpr) interface{} {
	operand := e.evalExpr(expr.Operand)

	switch expr.Operator {
	case "not":
		return !e.isTruthy(operand)
	default:
		return operand
	}
}

func (e *Evaluator) compareValues(left, right interface{}) int {
	// Handle nil/null comparisons
	if left == nil && right == nil {
		return 0
	}
	if left == nil {
		return -1
	}
	if right == nil {
		return 1
	}

	// Type-based comparison
	switch l := left.(type) {
	case string:
		r, ok := right.(string)
		if !ok {
			return -1
		}
		if l < r {
			return -1
		}
		if l > r {
			return 1
		}
		return 0

	case int64:
		switch r := right.(type) {
		case int64:
			if l < r {
				return -1
			}
			if l > r {
				return 1
			}
			return 0
		case float64:
			lf := float64(l)
			if lf < r {
				return -1
			}
			if lf > r {
				return 1
			}
			return 0
		default:
			return -1
		}

	case float64:
		switch r := right.(type) {
		case float64:
			if l < r {
				return -1
			}
			if l > r {
				return 1
			}
			return 0
		case int64:
			rf := float64(r)
			if l < rf {
				return -1
			}
			if l > rf {
				return 1
			}
			return 0
		default:
			return -1
		}

	case bool:
		r, ok := right.(bool)
		if !ok {
			return -1
		}
		if !l && r {
			return -1
		}
		if l && !r {
			return 1
		}
		return 0

	default:
		// For other types, convert to string and compare
		ls := fmt.Sprintf("%v", left)
		rs := fmt.Sprintf("%v", right)
		if ls < rs {
			return -1
		}
		if ls > rs {
			return 1
		}
		return 0
	}
}

func (e *Evaluator) isTruthy(val interface{}) bool {
	if val == nil {
		return false
	}

	switch v := val.(type) {
	case bool:
		return v
	case int64:
		return v != 0
	case float64:
		return v != 0.0
	case string:
		return v != ""
	case []interface{}:
		return len(v) > 0
	case map[string]interface{}:
		return len(v) > 0
	default:
		return true
	}
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

// parseTimeout parses a timeout value and returns a time.Duration
// Supports: number (seconds), "30s", "5000ms", "2m"
func parseTimeout(val interface{}) (time.Duration, error) {
	switch v := val.(type) {
	case int64:
		// Number without unit: treat as seconds
		return time.Duration(v) * time.Second, nil
	case float64:
		// Float number: treat as seconds
		return time.Duration(v * float64(time.Second)), nil
	case string:
		// String with unit: "30s", "5000ms", "2m"
		return parseTimeoutString(v)
	default:
		return 0, fmt.Errorf("unsupported timeout type: %T", val)
	}
}

// parseTimeoutString parses timeout strings like "30s", "5000ms", "2m"
func parseTimeoutString(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty timeout string")
	}

	// Try to parse as number first (default to seconds)
	if num, err := strconv.ParseFloat(s, 64); err == nil {
		return time.Duration(num * float64(time.Second)), nil
	}

	// Parse with unit suffix
	var numStr string
	var unit string
	
	// Find where the number ends
	for i, r := range s {
		if (r >= '0' && r <= '9') || r == '.' {
			numStr += string(r)
		} else {
			unit = s[i:]
			break
		}
	}
	
	if numStr == "" {
		return 0, fmt.Errorf("no number found in timeout string: %s", s)
	}
	
	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number in timeout string: %s", numStr)
	}
	
	unit = strings.ToLower(strings.TrimSpace(unit))
	switch unit {
	case "s", "sec", "second", "seconds":
		return time.Duration(num * float64(time.Second)), nil
	case "ms", "msec", "millisecond", "milliseconds":
		return time.Duration(num * float64(time.Millisecond)), nil
	case "m", "min", "minute", "minutes":
		return time.Duration(num * float64(time.Minute)), nil
	default:
		return 0, fmt.Errorf("unknown timeout unit: %s (supported: s, ms, m)", unit)
	}
}

// parseImportedFile is a placeholder - will be connected to the parser
var parseImportedFile func(string) (*ast.Program, error)

// SetImportParser sets the function to parse imported files
func SetImportParser(fn func(string) (*ast.Program, error)) {
	parseImportedFile = fn
}
