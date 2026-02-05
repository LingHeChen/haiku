// Package lexer provides tokenization for Haiku with indent support
package lexer

import (
	"fmt"
	"strings"
	"unicode"
)

// TokenType represents the type of token
type TokenType int

const (
	// Special tokens
	EOF TokenType = iota
	ILLEGAL
	NEWLINE
	INDENT
	DEDENT

	// Literals
	STRING      // "quoted string"
	IDENT       // identifier, unquoted string
	INT         // 123
	FLOAT       // 45.6
	PROC_STRING // json`...`, base64`...`

	// Keywords
	IMPORT
	FOR
	IN
	GET
	POST
	PUT
	DELETE
	PATCH
	HEAD
	OPTIONS
	HEADERS
	BODY
	TRUE
	FALSE
	NULL

	// Symbols
	AT          // @
	DOLLAR      // $
	DOT         // .
	UNDERSCORE  // _ (as null)
	EMPTY_ARRAY // []
	EMPTY_OBJ   // {}
	TRIPLE_DASH // ---
	COMMENT     // # comment
)

var tokenNames = map[TokenType]string{
	EOF:         "EOF",
	ILLEGAL:     "ILLEGAL",
	NEWLINE:     "NEWLINE",
	INDENT:      "INDENT",
	DEDENT:      "DEDENT",
	STRING:      "STRING",
	IDENT:       "IDENT",
	INT:         "INT",
	FLOAT:       "FLOAT",
	PROC_STRING: "PROC_STRING",
	IMPORT:      "IMPORT",
	FOR:         "FOR",
	IN:          "IN",
	GET:         "GET",
	POST:        "POST",
	PUT:         "PUT",
	DELETE:      "DELETE",
	PATCH:       "PATCH",
	HEAD:        "HEAD",
	OPTIONS:     "OPTIONS",
	HEADERS:     "HEADERS",
	BODY:        "BODY",
	TRUE:        "TRUE",
	FALSE:       "FALSE",
	NULL:        "NULL",
	AT:          "AT",
	DOLLAR:      "DOLLAR",
	DOT:         "DOT",
	UNDERSCORE:  "UNDERSCORE",
	EMPTY_ARRAY: "EMPTY_ARRAY",
	EMPTY_OBJ:   "EMPTY_OBJ",
	TRIPLE_DASH: "TRIPLE_DASH",
	COMMENT:     "COMMENT",
}

func (t TokenType) String() string {
	if name, ok := tokenNames[t]; ok {
		return name
	}
	return fmt.Sprintf("TokenType(%d)", t)
}

// Token represents a lexical token
type Token struct {
	Type    TokenType
	Literal string
	Line    int
	Column  int
}

func (t Token) String() string {
	return fmt.Sprintf("Token{%s, %q, L%d:C%d}", t.Type, t.Literal, t.Line, t.Column)
}

// Lexer tokenizes Haiku source code
type Lexer struct {
	input        string
	pos          int  // current position
	readPos      int  // next position
	ch           byte // current char
	line         int
	column       int
	indentStack  []int // stack of indentation levels
	pendingTokens []Token // tokens to emit (for DEDENT)
	atLineStart  bool
}

// New creates a new Lexer
func New(input string) *Lexer {
	l := &Lexer{
		input:       input,
		line:        1,
		column:      0,
		indentStack: []int{0}, // start with indent level 0
		atLineStart: true,
	}
	l.readChar()
	return l
}

func (l *Lexer) readChar() {
	if l.readPos >= len(l.input) {
		l.ch = 0
	} else {
		l.ch = l.input[l.readPos]
	}
	l.pos = l.readPos
	l.readPos++
	l.column++
}

func (l *Lexer) peekChar() byte {
	if l.readPos >= len(l.input) {
		return 0
	}
	return l.input[l.readPos]
}

// NextToken returns the next token
func (l *Lexer) NextToken() Token {
	// Return pending tokens first (DEDENT tokens)
	if len(l.pendingTokens) > 0 {
		tok := l.pendingTokens[0]
		l.pendingTokens = l.pendingTokens[1:]
		return tok
	}

	// Handle indentation at line start
	if l.atLineStart {
		l.atLineStart = false
		tok := l.handleIndentation()
		if tok.Type != ILLEGAL {
			return tok
		}
		// If no indent/dedent, continue to read normal token
	}

	l.skipSpaces() // skip spaces (but not newlines)

	var tok Token
	tok.Line = l.line
	tok.Column = l.column

	switch l.ch {
	case 0:
		// Generate remaining DEDENTs at EOF
		if len(l.indentStack) > 1 {
			l.indentStack = l.indentStack[:len(l.indentStack)-1]
			tok.Type = DEDENT
			tok.Literal = ""
			return tok
		}
		tok.Type = EOF
		tok.Literal = ""

	case '\n':
		tok.Type = NEWLINE
		tok.Literal = "\n"
		l.readChar()
		l.line++
		l.column = 0
		l.atLineStart = true

	case '\r':
		l.readChar()
		if l.ch == '\n' {
			tok.Type = NEWLINE
			tok.Literal = "\n"
			l.readChar()
			l.line++
			l.column = 0
			l.atLineStart = true
		} else {
			tok.Type = NEWLINE
			tok.Literal = "\n"
			l.line++
			l.column = 0
			l.atLineStart = true
		}

	case '#':
		tok.Type = COMMENT
		tok.Literal = l.readComment()

	case '@':
		tok.Type = AT
		tok.Literal = "@"
		l.readChar()

	case '$':
		tok.Type = DOLLAR
		tok.Literal = "$"
		l.readChar()

	case '.':
		tok.Type = DOT
		tok.Literal = "."
		l.readChar()

	case '"':
		tok.Type = STRING
		tok.Literal = l.readString()

	case '[':
		if l.peekChar() == ']' {
			tok.Type = EMPTY_ARRAY
			tok.Literal = "[]"
			l.readChar()
			l.readChar()
		} else {
			tok.Type = ILLEGAL
			tok.Literal = string(l.ch)
			l.readChar()
		}

	case '{':
		if l.peekChar() == '}' {
			tok.Type = EMPTY_OBJ
			tok.Literal = "{}"
			l.readChar()
			l.readChar()
		} else {
			tok.Type = ILLEGAL
			tok.Literal = string(l.ch)
			l.readChar()
		}

	case '-':
		if l.peekChar() == '-' {
			start := l.pos
			l.readChar() // second -
			if l.peekChar() == '-' {
				l.readChar() // third -
				l.readChar() // move past
				tok.Type = TRIPLE_DASH
				tok.Literal = "---"
			} else {
				// Just "--", treat as identifier or illegal
				tok.Type = IDENT
				tok.Literal = l.input[start:l.pos]
				l.readChar()
			}
		} else if isDigit(l.peekChar()) {
			// Negative number
			tok.Literal = l.readNumber()
			if strings.Contains(tok.Literal, ".") {
				tok.Type = FLOAT
			} else {
				tok.Type = INT
			}
		} else {
			// Part of identifier (e.g., Content-Type)
			tok.Type = IDENT
			tok.Literal = l.readIdentifier()
		}

	default:
		if isDigit(l.ch) {
			tok.Literal = l.readNumber()
			if strings.Contains(tok.Literal, ".") {
				tok.Type = FLOAT
			} else {
				tok.Type = INT
			}
		} else if isIdentStart(l.ch) {
			tok.Literal = l.readIdentifier()
			// Check if it's followed by backtick (processed string)
			if l.ch == '`' {
				processor := tok.Literal
				content := l.readBacktickContent()
				tok.Type = PROC_STRING
				tok.Literal = processor + "`" + content + "`"
			} else {
				tok.Type = lookupKeyword(tok.Literal)
			}
		} else {
			tok.Type = ILLEGAL
			tok.Literal = string(l.ch)
			l.readChar()
		}
	}

	return tok
}

func (l *Lexer) handleIndentation() Token {
	// Skip empty lines and comments
	for {
		// Count leading spaces/tabs
		indent := 0
		startPos := l.pos
		for l.ch == ' ' || l.ch == '\t' {
			if l.ch == ' ' {
				indent++
			} else {
				indent += 4 // treat tab as 4 spaces
			}
			l.readChar()
		}

		// Skip empty lines
		if l.ch == '\n' || l.ch == '\r' {
			l.readChar()
			if l.ch == '\n' && l.input[l.pos-1] == '\r' {
				l.readChar()
			}
			l.line++
			l.column = 0
			continue
		}

		// Skip comment lines
		if l.ch == '#' {
			l.readComment()
			if l.ch == '\n' || l.ch == '\r' {
				l.readChar()
				if l.ch == '\n' && l.input[l.pos-1] == '\r' {
					l.readChar()
				}
				l.line++
				l.column = 0
				continue
			}
			continue
		}

		// Handle EOF
		if l.ch == 0 {
			break
		}

		// Now we have actual content
		currentIndent := l.indentStack[len(l.indentStack)-1]

		if indent > currentIndent {
			// INDENT
			l.indentStack = append(l.indentStack, indent)
			return Token{Type: INDENT, Literal: "", Line: l.line, Column: startPos + 1}
		} else if indent < currentIndent {
			// DEDENT (possibly multiple)
			for len(l.indentStack) > 1 && l.indentStack[len(l.indentStack)-1] > indent {
				l.indentStack = l.indentStack[:len(l.indentStack)-1]
				l.pendingTokens = append(l.pendingTokens, Token{
					Type: DEDENT, Literal: "", Line: l.line, Column: startPos + 1,
				})
			}
			if len(l.pendingTokens) > 0 {
				tok := l.pendingTokens[0]
				l.pendingTokens = l.pendingTokens[1:]
				return tok
			}
		}
		// Same indent level, no token to emit
		break
	}

	return Token{Type: ILLEGAL} // Signal to continue with normal tokenization
}

func (l *Lexer) skipSpaces() {
	for l.ch == ' ' || l.ch == '\t' {
		l.readChar()
	}
}

func (l *Lexer) readComment() string {
	start := l.pos
	for l.ch != '\n' && l.ch != '\r' && l.ch != 0 {
		l.readChar()
	}
	return l.input[start:l.pos]
}

func (l *Lexer) readString() string {
	l.readChar() // skip opening quote
	start := l.pos
	for {
		if l.ch == '"' || l.ch == 0 {
			break
		}
		if l.ch == '\\' {
			l.readChar() // skip escape char
		}
		l.readChar()
	}
	str := l.input[start:l.pos]
	if l.ch == '"' {
		l.readChar() // skip closing quote
	}
	return str
}

func (l *Lexer) readBacktickContent() string {
	l.readChar() // skip opening backtick
	start := l.pos
	for l.ch != '`' && l.ch != 0 {
		l.readChar()
	}
	content := l.input[start:l.pos]
	if l.ch == '`' {
		l.readChar() // skip closing backtick
	}
	return content
}

func (l *Lexer) readNumber() string {
	start := l.pos
	if l.ch == '-' {
		l.readChar()
	}
	for isDigit(l.ch) {
		l.readChar()
	}
	if l.ch == '.' && isDigit(l.peekChar()) {
		l.readChar() // consume '.'
		for isDigit(l.ch) {
			l.readChar()
		}
	}
	return l.input[start:l.pos]
}

func (l *Lexer) readIdentifier() string {
	start := l.pos
	for isIdentChar(l.ch) {
		l.readChar()
	}
	return l.input[start:l.pos]
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func isIdentStart(ch byte) bool {
	return unicode.IsLetter(rune(ch)) || ch == '_'
}

func isIdentChar(ch byte) bool {
	return unicode.IsLetter(rune(ch)) || unicode.IsDigit(rune(ch)) || ch == '_' || ch == '-'
}

var keywords = map[string]TokenType{
	"import":  IMPORT,
	"for":     FOR,
	"in":      IN,
	"get":     GET,
	"post":    POST,
	"put":     PUT,
	"delete":  DELETE,
	"patch":   PATCH,
	"head":    HEAD,
	"options": OPTIONS,
	"headers": HEADERS,
	"body":    BODY,
	"true":    TRUE,
	"false":   FALSE,
	"null":    NULL,
	"nil":     NULL,
	"_":       UNDERSCORE,
}

func lookupKeyword(ident string) TokenType {
	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return IDENT
}

// Tokenize returns all tokens from input
func Tokenize(input string) []Token {
	l := New(input)
	var tokens []Token
	for {
		tok := l.NextToken()
		tokens = append(tokens, tok)
		if tok.Type == EOF {
			break
		}
	}
	return tokens
}
