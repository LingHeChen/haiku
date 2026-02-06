// Package parser provides parsing for Haiku (v2 - AST based)
package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/LingHeChen/haiku/ast"
	"github.com/LingHeChen/haiku/lexer"
)

// ParserV2 is the AST-based parser
type ParserV2 struct {
	l         *lexer.Lexer
	curToken  lexer.Token
	peekToken lexer.Token
	errors    []string
}

// NewV2 creates a new AST-based parser
func NewV2(input string) *ParserV2 {
	p := &ParserV2{
		l: lexer.New(input),
	}
	// Read two tokens to initialize curToken and peekToken
	p.nextToken()
	p.nextToken()
	return p
}

func (p *ParserV2) nextToken() {
	p.curToken = p.peekToken
	p.peekToken = p.l.NextToken()
}

func (p *ParserV2) curTokenIs(t lexer.TokenType) bool {
	return p.curToken.Type == t
}

func (p *ParserV2) peekTokenIs(t lexer.TokenType) bool {
	return p.peekToken.Type == t
}

func (p *ParserV2) expectPeek(t lexer.TokenType) bool {
	if p.peekTokenIs(t) {
		p.nextToken()
		return true
	}
	p.peekError(t)
	return false
}

func (p *ParserV2) peekError(t lexer.TokenType) {
	msg := fmt.Sprintf("line %d: expected %s, got %s",
		p.peekToken.Line, t, p.peekToken.Type)
	p.errors = append(p.errors, msg)
}

func (p *ParserV2) addError(format string, args ...interface{}) {
	msg := fmt.Sprintf("line %d: ", p.curToken.Line) + fmt.Sprintf(format, args...)
	p.errors = append(p.errors, msg)
}

// Errors returns parsing errors
func (p *ParserV2) Errors() []string {
	return p.errors
}

// Parse parses the input and returns the AST
func (p *ParserV2) Parse() (*ast.Program, error) {
	program := &ast.Program{}

	for !p.curTokenIs(lexer.EOF) {
		stmt := p.parseStatement()
		if stmt != nil {
			program.Statements = append(program.Statements, stmt)
		}
		p.nextToken()
	}

	if len(p.errors) > 0 {
		return nil, fmt.Errorf("parse errors:\n%s", strings.Join(p.errors, "\n"))
	}

	return program, nil
}

func (p *ParserV2) parseStatement() ast.Statement {
	// Skip newlines and comments
	for p.curTokenIs(lexer.NEWLINE) || p.curTokenIs(lexer.COMMENT) {
		p.nextToken()
	}

	if p.curTokenIs(lexer.EOF) {
		return nil
	}

	switch p.curToken.Type {
	case lexer.IMPORT:
		return p.parseImportStmt()
	case lexer.AT:
		return p.parseVarDefStmt()
	case lexer.PARALLEL:
		return p.parseParallelForStmt()
	case lexer.FOR:
		return p.parseForStmt(false, 0)
	case lexer.IF:
		return p.parseIfStmt()
	case lexer.ECHO:
		return p.parseEchoStmt()
	case lexer.QUESTION:
		return p.parseQuestionIfStmt()
	case lexer.TRIPLE_DASH:
		return p.parseSeparatorStmt()
	case lexer.GET, lexer.POST, lexer.PUT, lexer.DELETE, lexer.PATCH, lexer.HEAD, lexer.OPTIONS:
		return p.parseRequestStmt()
	case lexer.DEDENT:
		return nil // End of block
	case lexer.HEADERS, lexer.BODY:
		// These are handled inside parseRequestStmt, skip if encountered at top level
		return nil
	case lexer.INDENT:
		// Unexpected indent at top level
		p.nextToken()
		return nil
	default:
		// Skip unknown tokens instead of erroring
		p.nextToken()
		return nil
	}
}

func (p *ParserV2) parseImportStmt() *ast.ImportStmt {
	stmt := &ast.ImportStmt{
		Position: ast.Position{Line: p.curToken.Line, Column: p.curToken.Column},
	}

	if !p.expectPeek(lexer.STRING) {
		return nil
	}

	stmt.Path = p.curToken.Literal
	return stmt
}

func (p *ParserV2) parseVarDefStmt() *ast.VarDefStmt {
	stmt := &ast.VarDefStmt{
		Position: ast.Position{Line: p.curToken.Line, Column: p.curToken.Column},
	}

	// Expect identifier after @ (can be IDENT or a keyword used as identifier)
	p.nextToken()
	if p.curTokenIs(lexer.IDENT) {
		stmt.Name = p.curToken.Literal
	} else if p.curTokenIs(lexer.TIMEOUT) || p.curTokenIs(lexer.HEADERS) || p.curTokenIs(lexer.BODY) {
		// Allow keywords to be used as variable names
		stmt.Name = p.curToken.Literal
	} else {
		p.addError("expected identifier after @")
		return nil
	}

	p.nextToken()

	// Check if there's a value on the same line or an indented block
	if p.curTokenIs(lexer.NEWLINE) {
		// Check for indented block
		p.nextToken()
		if p.curTokenIs(lexer.INDENT) {
			stmt.Value = p.parseBlockExpr()
		}
	} else if !p.curTokenIs(lexer.EOF) && !p.curTokenIs(lexer.DEDENT) {
		// Value on the same line
		stmt.Value = p.parseExpression()
	}

	return stmt
}

func (p *ParserV2) parseParallelForStmt() *ast.ForStmt {
	pos := ast.Position{Line: p.curToken.Line, Column: p.curToken.Column}
	
	p.nextToken() // skip 'parallel'
	
	// Check for optional concurrency number
	concurrency := 0
	if p.curTokenIs(lexer.INT) {
		val, _ := strconv.Atoi(p.curToken.Literal)
		concurrency = val
		p.nextToken()
	}
	
	// Expect 'for'
	if !p.curTokenIs(lexer.FOR) {
		p.addError("expected 'for' after 'parallel'")
		return nil
	}
	
	stmt := p.parseForStmt(true, concurrency)
	if stmt != nil {
		stmt.Position = pos
	}
	return stmt
}

func (p *ParserV2) parseForStmt(parallel bool, concurrency int) *ast.ForStmt {
	stmt := &ast.ForStmt{
		Position:    ast.Position{Line: p.curToken.Line, Column: p.curToken.Column},
		Parallel:    parallel,
		Concurrency: concurrency,
	}

	// Check for simplified syntax: for 10 (without variable and 'in')
	if p.peekTokenIs(lexer.INT) || p.peekTokenIs(lexer.FLOAT) {
		// Simplified syntax: for 10
		p.nextToken()
		stmt.ItemVar = "index" // default variable name
		stmt.Iterable = p.parseExpression()
	} else {
		// Full syntax: for $varname in ...
		// Expect $varname
		if !p.expectPeek(lexer.DOLLAR) {
			return nil
		}
		if !p.expectPeek(lexer.IDENT) {
			return nil
		}
		firstVar := p.curToken.Literal

		p.nextToken()

		// Check for optional index variable: for $i, $item in ...
		if p.curTokenIs(lexer.COMMA) {
			stmt.IndexVar = firstVar
			p.nextToken() // skip comma
			if !p.curTokenIs(lexer.DOLLAR) {
				p.addError("expected $ after comma in for statement")
				return nil
			}
			p.nextToken() // skip $
			if !p.curTokenIs(lexer.IDENT) {
				p.addError("expected identifier after $ in for statement")
				return nil
			}
			stmt.ItemVar = p.curToken.Literal
			p.nextToken()
		} else {
			stmt.ItemVar = firstVar
		}

		// Expect 'in'
		if !p.curTokenIs(lexer.IN) {
			p.addError("expected 'in' in for statement")
			return nil
		}

		p.nextToken()

		// Parse iterable expression
		stmt.Iterable = p.parseExpression()
	}

	// Skip to newline
	for !p.curTokenIs(lexer.NEWLINE) && !p.curTokenIs(lexer.EOF) {
		p.nextToken()
	}

	// Expect indented block
	if p.peekTokenIs(lexer.INDENT) {
		p.nextToken() // NEWLINE
		p.nextToken() // INDENT

		// Parse statements inside the loop
		// Stop at DEDENT, EOF, or TRIPLE_DASH (request separator)
		for !p.curTokenIs(lexer.DEDENT) && !p.curTokenIs(lexer.EOF) && !p.curTokenIs(lexer.TRIPLE_DASH) {
			innerStmt := p.parseStatement()
			if innerStmt != nil {
				stmt.Body = append(stmt.Body, innerStmt)
			}
			p.nextToken()
		}
	}

	return stmt
}

func (p *ParserV2) parseEchoStmt() *ast.EchoStmt {
	stmt := &ast.EchoStmt{
		Position: ast.Position{Line: p.curToken.Line, Column: p.curToken.Column},
	}

	p.nextToken() // skip 'echo'

	// Parse the expression to echo
	if !p.curTokenIs(lexer.NEWLINE) && !p.curTokenIs(lexer.EOF) {
		stmt.Value = p.parseExpression()
	}

	return stmt
}

func (p *ParserV2) parseSeparatorStmt() *ast.SeparatorStmt {
	return &ast.SeparatorStmt{
		Position: ast.Position{Line: p.curToken.Line, Column: p.curToken.Column},
	}
}

func (p *ParserV2) parseIfStmt() *ast.IfStmt {
	stmt := &ast.IfStmt{
		Position: ast.Position{Line: p.curToken.Line, Column: p.curToken.Column},
		Branches: []ast.IfBranch{},
	}

	p.nextToken() // skip 'if'

	// Parse condition expression
	condition := p.parseConditionExpression()

	// Skip to newline
	for !p.curTokenIs(lexer.NEWLINE) && !p.curTokenIs(lexer.EOF) {
		p.nextToken()
	}

	// Parse 'then' branch body
	body := p.parseIndentedBody()

	// Add first branch
	stmt.Branches = append(stmt.Branches, ast.IfBranch{
		Condition: condition,
		Body:      body,
	})

	// Check for else branch: DEDENT followed by ELSE
	if p.curTokenIs(lexer.DEDENT) && p.peekTokenIs(lexer.ELSE) {
		p.nextToken() // skip DEDENT -> curToken = ELSE
		p.nextToken() // skip 'else'

		// Skip to newline
		for !p.curTokenIs(lexer.NEWLINE) && !p.curTokenIs(lexer.EOF) {
			p.nextToken()
		}

		// Parse 'else' branch body
		stmt.Else = p.parseIndentedBody()
		// curToken is at DEDENT
	}

	// curToken is at DEDENT (last token of the if statement)
	return stmt
}

func (p *ParserV2) parseQuestionIfStmt() *ast.IfStmt {
	stmt := &ast.IfStmt{
		Position: ast.Position{Line: p.curToken.Line, Column: p.curToken.Column},
		Branches: []ast.IfBranch{},
	}

	p.nextToken() // skip '?'

	// Parse first condition
	condition := p.parseConditionExpression()

	// Skip to newline
	for !p.curTokenIs(lexer.NEWLINE) && !p.curTokenIs(lexer.EOF) {
		p.nextToken()
	}

	// Parse first branch body (indented block)
	// After return, curToken is at DEDENT
	body := p.parseIndentedBody()
	stmt.Branches = append(stmt.Branches, ast.IfBranch{
		Condition: condition,
		Body:      body,
	})

	// Check for : branches. After parseIndentedBody, curToken is at DEDENT.
	// Peek to see if a COLON follows.
	for p.curTokenIs(lexer.DEDENT) && p.peekTokenIs(lexer.COLON) {
		p.nextToken() // skip DEDENT -> curToken = COLON
		p.nextToken() // skip COLON -> curToken = next token

		if p.curTokenIs(lexer.NEWLINE) {
			// ':' followed by newline -> final else branch (no condition)
			stmt.Else = p.parseIndentedBody()
			// curToken is at DEDENT, which is the "last token" of the if statement
			break
		}

		// ':' followed by condition -> else-if branch
		branchCondition := p.parseConditionExpression()

		// Skip to newline
		for !p.curTokenIs(lexer.NEWLINE) && !p.curTokenIs(lexer.EOF) {
			p.nextToken()
		}

		branchBody := p.parseIndentedBody()
		// curToken is at DEDENT; the for-loop condition will peek for another COLON
		stmt.Branches = append(stmt.Branches, ast.IfBranch{
			Condition: branchCondition,
			Body:      branchBody,
		})
	}

	// curToken is at DEDENT (last token of the if statement)
	// The caller (main Parse loop) will call nextToken() to advance past it
	return stmt
}

// parseIndentedBody parses an indented block of statements.
// Expects curToken to be at NEWLINE with peekToken possibly being INDENT.
// After return, curToken is at DEDENT (the block terminator), NOT past it.
// The caller decides whether to advance past DEDENT.
func (p *ParserV2) parseIndentedBody() []ast.Statement {
	var stmts []ast.Statement

	if !p.peekTokenIs(lexer.INDENT) {
		return stmts
	}

	p.nextToken() // consume NEWLINE, curToken = INDENT
	p.nextToken() // consume INDENT, curToken = first token in block

	for !p.curTokenIs(lexer.DEDENT) && !p.curTokenIs(lexer.EOF) && !p.curTokenIs(lexer.TRIPLE_DASH) {
		innerStmt := p.parseStatement()
		if innerStmt != nil {
			stmts = append(stmts, innerStmt)
		}
		// Only advance if parseStatement didn't already reach a block terminator.
		if !p.curTokenIs(lexer.DEDENT) && !p.curTokenIs(lexer.EOF) && !p.curTokenIs(lexer.TRIPLE_DASH) {
			p.nextToken()
		}
	}

	// Leave curToken at DEDENT - do NOT advance past it
	return stmts
}

func (p *ParserV2) parseConditionExpression() ast.Expression {
	return p.parseLogicalOr()
}

func (p *ParserV2) parseLogicalOr() ast.Expression {
	left := p.parseLogicalAnd()

	for p.curTokenIs(lexer.OR) {
		op := "or"
		pos := ast.Position{Line: p.curToken.Line, Column: p.curToken.Column}
		p.nextToken()
		right := p.parseLogicalAnd()
		left = &ast.BinaryExpr{
			Position: pos,
			Left:     left,
			Operator: op,
			Right:    right,
		}
	}

	return left
}

func (p *ParserV2) parseLogicalAnd() ast.Expression {
	left := p.parseComparison()

	for p.curTokenIs(lexer.AND) {
		op := "and"
		pos := ast.Position{Line: p.curToken.Line, Column: p.curToken.Column}
		p.nextToken()
		right := p.parseComparison()
		left = &ast.BinaryExpr{
			Position: pos,
			Left:     left,
			Operator: op,
			Right:    right,
		}
	}

	return left
}

func (p *ParserV2) parseComparison() ast.Expression {
	left := p.parseUnary()

	for p.curTokenIs(lexer.EQ) || p.curTokenIs(lexer.NE) || 
		 p.curTokenIs(lexer.GT) || p.curTokenIs(lexer.LT) || 
		 p.curTokenIs(lexer.GTE) || p.curTokenIs(lexer.LTE) {
		var op string
		switch p.curToken.Type {
		case lexer.EQ:
			op = "=="
		case lexer.NE:
			op = "!="
		case lexer.GT:
			op = ">"
		case lexer.LT:
			op = "<"
		case lexer.GTE:
			op = ">="
		case lexer.LTE:
			op = "<="
		}
		pos := ast.Position{Line: p.curToken.Line, Column: p.curToken.Column}
		p.nextToken()
		right := p.parseUnary()
		left = &ast.BinaryExpr{
			Position: pos,
			Left:     left,
			Operator: op,
			Right:    right,
		}
	}

	return left
}

func (p *ParserV2) parseUnary() ast.Expression {
	if p.curTokenIs(lexer.NOT) {
		pos := ast.Position{Line: p.curToken.Line, Column: p.curToken.Column}
		op := "not"
		p.nextToken()
		operand := p.parseUnary()
		return &ast.UnaryExpr{
			Position: pos,
			Operator: op,
			Operand:  operand,
		}
	}

	return p.parseConditionPrimary()
}

// parseConditionPrimary parses a primary expression in a condition context.
// Unlike parseExpression which leaves curToken at the last token of the expression,
// this advances curToken past the expression so the caller can check for operators.
func (p *ParserV2) parseConditionPrimary() ast.Expression {
	expr := p.parseExpression()
	p.nextToken() // advance past the expression
	return expr
}

func (p *ParserV2) parseRequestStmt() *ast.RequestStmt {
	stmt := &ast.RequestStmt{
		Position: ast.Position{Line: p.curToken.Line, Column: p.curToken.Column},
		Method:   p.curToken.Literal,
	}

	p.nextToken()

	// Parse URL
	stmt.URL = p.parseExpression()

	// Skip to newline
	for !p.curTokenIs(lexer.NEWLINE) && !p.curTokenIs(lexer.EOF) {
		p.nextToken()
	}

	// Skip newline
	if p.curTokenIs(lexer.NEWLINE) {
		p.nextToken()
	}

	// Parse headers/body sections (they appear at same indent level as the method)
	for {
		// Skip empty lines
		for p.curTokenIs(lexer.NEWLINE) || p.curTokenIs(lexer.COMMENT) {
			p.nextToken()
		}

		if p.curTokenIs(lexer.HEADERS) {
			p.nextToken()
			// Skip newline
			for p.curTokenIs(lexer.NEWLINE) {
				p.nextToken()
			}
			if p.curTokenIs(lexer.INDENT) {
				stmt.Headers = p.parseBlockExpr()
				// After parsing block, we're at DEDENT or next token
				if p.curTokenIs(lexer.DEDENT) {
					p.nextToken()
				}
			}
		} else if p.curTokenIs(lexer.BODY) {
			p.nextToken()
			// Check if body has inline value or block
			if p.curTokenIs(lexer.NEWLINE) {
				p.nextToken()
				if p.curTokenIs(lexer.INDENT) {
					stmt.Body = p.parseBlockExpr()
					// After parsing block, we're at DEDENT or next token
					if p.curTokenIs(lexer.DEDENT) {
						p.nextToken()
					}
				}
			} else if !p.curTokenIs(lexer.EOF) && !p.curTokenIs(lexer.DEDENT) {
				// Inline body value (e.g., body json`...`)
				stmt.Body = p.parseExpression()
				// Skip to newline
				for !p.curTokenIs(lexer.NEWLINE) && !p.curTokenIs(lexer.EOF) {
					p.nextToken()
				}
				if p.curTokenIs(lexer.NEWLINE) {
					p.nextToken()
				}
			}
		} else if p.curTokenIs(lexer.TIMEOUT) {
			p.nextToken()
			// Parse timeout expression (e.g., 30, "30s", "5000ms", 1m)
			// Special handling: if we have a number followed by an identifier, combine them
			stmt.Timeout = p.parseTimeoutExpression()
			// Skip to newline
			for !p.curTokenIs(lexer.NEWLINE) && !p.curTokenIs(lexer.EOF) && !p.curTokenIs(lexer.DEDENT) {
				p.nextToken()
			}
			if p.curTokenIs(lexer.NEWLINE) {
				p.nextToken()
			}
		} else {
			// Not headers, body, or timeout, done parsing this request
			break
		}
	}

	return stmt
}

// parseTimeoutExpression parses a timeout value, handling number+unit combinations like "1m", "30s"
func (p *ParserV2) parseTimeoutExpression() ast.Expression {
	pos := ast.Position{Line: p.curToken.Line, Column: p.curToken.Column}
	
	// If it's a number, check if next token is an identifier (unit)
	if p.curTokenIs(lexer.INT) || p.curTokenIs(lexer.FLOAT) {
		numStr := p.curToken.Literal
		// Peek at next token to see if it's a unit identifier
		if p.peekTokenIs(lexer.IDENT) {
			// Combine number + unit as a string (e.g., "1m", "30s")
			p.nextToken() // move to the unit identifier
			unit := p.curToken.Literal
			combined := numStr + unit
			return &ast.StringLiteral{
				Position: pos,
				Value:    combined,
				Quoted:   false,
			}
		}
		// Just a number, parse normally
		return p.parseExpression()
	}
	
	// Not a number, parse as normal expression
	return p.parseExpression()
}

func (p *ParserV2) parseBlockExpr() *ast.BlockExpr {
	block := &ast.BlockExpr{
		Position: ast.Position{Line: p.curToken.Line, Column: p.curToken.Column},
	}

	p.nextToken() // move past INDENT

	for !p.curTokenIs(lexer.DEDENT) && !p.curTokenIs(lexer.EOF) {
		// Skip newlines and comments
		if p.curTokenIs(lexer.NEWLINE) || p.curTokenIs(lexer.COMMENT) {
			p.nextToken()
			continue
		}

		entry := p.parseEntry()
		if entry != nil {
			block.Entries = append(block.Entries, *entry)
		}

		p.nextToken()
	}

	return block
}

func (p *ParserV2) parseEntry() *ast.Entry {
	entry := &ast.Entry{
		Position: ast.Position{Line: p.curToken.Line, Column: p.curToken.Column},
	}

	// First token could be a key or a standalone value
	if p.curTokenIs(lexer.IDENT) || p.curTokenIs(lexer.STRING) {
		// Save first value
		firstVal := p.curToken.Literal
		firstType := p.curToken.Type

		p.nextToken()

		// Check if there's a value following (making first token a key)
		if !p.curTokenIs(lexer.NEWLINE) && !p.curTokenIs(lexer.DEDENT) && 
		   !p.curTokenIs(lexer.EOF) && !p.curTokenIs(lexer.COMMENT) {
			// First token is key, parse value
			entry.Key = firstVal
			entry.Value = p.parseExpression()
		} else {
			// First token is the value (array item)
			entry.Key = ""
			if firstType == lexer.STRING {
				entry.Value = &ast.StringLiteral{
					Position: entry.Position,
					Value:    firstVal,
					Quoted:   true,
				}
			} else {
				entry.Value = &ast.StringLiteral{
					Position: entry.Position,
					Value:    firstVal,
					Quoted:   false,
				}
			}
		}
	} else {
		// Parse as standalone value (e.g., number, $var)
		entry.Value = p.parseExpression()
	}

	// Check for nested block
	if p.peekTokenIs(lexer.INDENT) {
		p.nextToken() // move to INDENT
		entry.Value = p.parseBlockExpr()
	}

	return entry
}

func (p *ParserV2) parseExpression() ast.Expression {
	left := p.parsePrimary()
	// String concatenation: left + right + ...
	for p.peekTokenIs(lexer.PLUS) {
		p.nextToken() // advance to PLUS
		pos := ast.Position{Line: p.curToken.Line, Column: p.curToken.Column}
		p.nextToken() // advance past PLUS
		right := p.parsePrimary()
		left = &ast.BinaryExpr{
			Position: pos,
			Left:     left,
			Operator: "+",
			Right:    right,
		}
	}
	return left
}

// parsePrimary parses a single expression (no binary operators).
func (p *ParserV2) parsePrimary() ast.Expression {
	pos := ast.Position{Line: p.curToken.Line, Column: p.curToken.Column}

	switch p.curToken.Type {
	case lexer.STRING:
		return &ast.StringLiteral{
			Position: pos,
			Value:    p.curToken.Literal,
			Quoted:   true,
		}

	case lexer.IDENT:
		return &ast.StringLiteral{
			Position: pos,
			Value:    p.curToken.Literal,
			Quoted:   false,
		}

	case lexer.INT:
		val, _ := strconv.ParseInt(p.curToken.Literal, 10, 64)
		return &ast.NumberLiteral{
			Position: pos,
			IntVal:   &val,
		}

	case lexer.FLOAT:
		val, _ := strconv.ParseFloat(p.curToken.Literal, 64)
		return &ast.NumberLiteral{
			Position: pos,
			FloatVal: &val,
		}

	case lexer.TRUE:
		return &ast.BoolLiteral{Position: pos, Value: true}

	case lexer.FALSE:
		return &ast.BoolLiteral{Position: pos, Value: false}

	case lexer.NULL, lexer.UNDERSCORE:
		return &ast.NullLiteral{Position: pos}

	case lexer.EMPTY_ARRAY:
		return &ast.EmptyArrayLiteral{Position: pos}

	case lexer.EMPTY_OBJ:
		return &ast.EmptyObjectLiteral{Position: pos}

	case lexer.DOLLAR:
		return p.parseVarRef()

	case lexer.PROC_STRING:
		return p.parseProcessedString()

	default:
		return nil
	}
}

func (p *ParserV2) parseVarRef() *ast.VarRef {
	ref := &ast.VarRef{
		Position: ast.Position{Line: p.curToken.Line, Column: p.curToken.Column},
	}

	p.nextToken() // move past $

	if !p.curTokenIs(lexer.IDENT) && !p.curTokenIs(lexer.UNDERSCORE) {
		p.addError("expected identifier after $")
		return ref
	}

	if p.curTokenIs(lexer.UNDERSCORE) {
		ref.Name = "_"
	} else {
		ref.Name = p.curToken.Literal
	}

	// Parse path: $var.field.subfield
	for p.peekTokenIs(lexer.DOT) {
		p.nextToken() // move to .
		p.nextToken() // move to field name

		if p.curTokenIs(lexer.IDENT) || p.curTokenIs(lexer.INT) {
			ref.Path = append(ref.Path, p.curToken.Literal)
		} else {
			break
		}
	}

	return ref
}

func (p *ParserV2) parseProcessedString() *ast.ProcessedString {
	// Literal format: processor`content`
	literal := p.curToken.Literal

	// Find backtick position
	idx := strings.Index(literal, "`")
	if idx == -1 {
		return &ast.ProcessedString{
			Position:  ast.Position{Line: p.curToken.Line, Column: p.curToken.Column},
			Processor: literal,
			Content:   "",
		}
	}

	processor := literal[:idx]
	content := literal[idx+1:]
	if len(content) > 0 && content[len(content)-1] == '`' {
		content = content[:len(content)-1]
	}

	return &ast.ProcessedString{
		Position:  ast.Position{Line: p.curToken.Line, Column: p.curToken.Column},
		Processor: processor,
		Content:   content,
	}
}

// ParseFile parses a Haiku file and returns the AST
func ParseFile(input string) (*ast.Program, error) {
	p := NewV2(input)
	return p.Parse()
}
