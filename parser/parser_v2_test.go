package parser

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/LingHeChen/haiku/eval"
	"github.com/LingHeChen/haiku/lexer"
)

func TestLexer(t *testing.T) {
	input := `
@name "John"
@age 25

get "https://api.example.com/users"
headers
  Content-Type "application/json"
body
  name $name
  age $age
`
	tokens := lexer.Tokenize(input)
	for _, tok := range tokens {
		fmt.Println(tok)
	}
}

func TestParserV2Basic(t *testing.T) {
	input := `
@name "John"
@age 25

get "https://api.example.com/users"
headers
  Content-Type "application/json"
body
  name $name
  age $age
`
	program, err := ParseFile(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	fmt.Printf("Parsed %d statements\n", len(program.Statements))
	for i, stmt := range program.Statements {
		fmt.Printf("  [%d] %T\n", i, stmt)
	}
}

func TestParserV2WithEval(t *testing.T) {
	input := `
@name "John"
@age 25

get "https://api.example.com/users"
headers
  Content-Type "application/json"
body
  name $name
  age $age
`
	// Set up the import parser
	eval.SetImportParser(ParseFile)

	program, err := ParseFile(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	evaluator := eval.NewEvaluator()
	requests, err := evaluator.EvalToRequests(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	fmt.Printf("Generated %d requests\n", len(requests))
	for i, req := range requests {
		jsonBytes, _ := json.MarshalIndent(req, "", "  ")
		fmt.Printf("Request %d:\n%s\n", i+1, string(jsonBytes))
	}
}

func TestParserV2StructuredVars(t *testing.T) {
	input := `
@user
  name John
  age 25

@tags
  api
  http

get "https://api.example.com"
body
  user $user
  tags $tags
`
	eval.SetImportParser(ParseFile)

	program, err := ParseFile(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	evaluator := eval.NewEvaluator()
	requests, err := evaluator.EvalToRequests(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	if len(requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(requests))
	}

	jsonBytes, _ := json.MarshalIndent(requests[0], "", "  ")
	fmt.Println(string(jsonBytes))
}

func TestParserV2ForLoop(t *testing.T) {
	input := `
@ids json` + "`[1, 2, 3]`" + `

for $id in $ids
  delete "https://api.example.com/users/$id"
`
	eval.SetImportParser(ParseFile)

	program, err := ParseFile(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	fmt.Printf("Parsed %d statements\n", len(program.Statements))
	for i, stmt := range program.Statements {
		fmt.Printf("  [%d] %T\n", i, stmt)
	}

	evaluator := eval.NewEvaluator()
	requests, err := evaluator.EvalToRequests(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	fmt.Printf("Generated %d requests\n", len(requests))
	for i, req := range requests {
		jsonBytes, _ := json.MarshalIndent(req, "", "  ")
		fmt.Printf("Request %d:\n%s\n", i+1, string(jsonBytes))
	}
}

func TestParserV2ForLoopWithIndex(t *testing.T) {
	input := `
@users json` + "`" + `[{"name": "Alice"}, {"name": "Bob"}]` + "`" + `

for $index, $user in $users
  post "https://api.example.com/users"
  body
    index $index
    name $user.name
`
	eval.SetImportParser(ParseFile)

	program, err := ParseFile(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	fmt.Printf("Parsed %d statements\n", len(program.Statements))

	evaluator := eval.NewEvaluator()
	requests, err := evaluator.EvalToRequests(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	fmt.Printf("Generated %d requests\n", len(requests))
	for i, req := range requests {
		jsonBytes, _ := json.MarshalIndent(req, "", "  ")
		fmt.Printf("Request %d:\n%s\n\n", i+1, string(jsonBytes))
	}

	// Verify index values
	if len(requests) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(requests))
	}
	
	body0 := requests[0]["body"].(map[string]interface{})
	if body0["index"] != int64(0) {
		t.Errorf("expected index 0, got %v", body0["index"])
	}
	
	body1 := requests[1]["body"].(map[string]interface{})
	if body1["index"] != int64(1) {
		t.Errorf("expected index 1, got %v", body1["index"])
	}
}

func TestParserV2ForLoopFile(t *testing.T) {
	content, err := os.ReadFile("../examples/for-loop.haiku")
	if err != nil {
		t.Fatalf("read file error: %v", err)
	}

	eval.SetImportParser(ParseFile)

	program, err := ParseFile(string(content))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	fmt.Printf("Parsed %d statements\n", len(program.Statements))

	evaluator := eval.NewEvaluator()
	requests, err := evaluator.EvalToRequests(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	fmt.Printf("Generated %d requests\n", len(requests))
	for i, req := range requests {
		jsonBytes, _ := json.MarshalIndent(req, "", "  ")
		fmt.Printf("Request %d:\n%s\n\n", i+1, string(jsonBytes))
	}
}

func TestParserV2ParallelForLoop(t *testing.T) {
	input := `
@ids json` + "`[1, 2, 3, 4]`" + `

parallel 2 for $id in $ids
  get "https://api.example.com/users/$id"
`
	eval.SetImportParser(ParseFile)

	program, err := ParseFile(input)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	evaluator := eval.NewEvaluator()
	requests, err := evaluator.EvalToRequests(program)
	if err != nil {
		t.Fatalf("eval error: %v", err)
	}

	if len(requests) != 4 {
		t.Fatalf("expected 4 requests, got %d", len(requests))
	}

	// Order is not guaranteed for parallel collection; verify set membership.
	seen := make(map[string]bool)
	for _, req := range requests {
		url, _ := req["get"].(string)
		seen[url] = true
	}

	for _, id := range []int{1, 2, 3, 4} {
		want := fmt.Sprintf("https://api.example.com/users/%d", id)
		if !seen[want] {
			t.Fatalf("missing request url: %s", want)
		}
	}
}
