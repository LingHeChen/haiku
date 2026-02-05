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

	// Expect identifier after @
	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	stmt.Name = p.curToken.Literal

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

func (p *ParserV2) parseSeparatorStmt() *ast.SeparatorStmt {
	return &ast.SeparatorStmt{
		Position: ast.Position{Line: p.curToken.Line, Column: p.curToken.Column},
	}
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
		} else {
			// Not headers or body, done parsing this request
			break
		}
	}

	return stmt
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
