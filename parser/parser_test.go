package parser

import (
	"testing"
)

func TestParseSimpleGet(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatalf("Failed to create parser: %v", err)
	}

	input := `get "https://example.com/api"`
	result, err := p.ParseToMap(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if result["get"] != "https://example.com/api" {
		t.Errorf("Expected get URL, got %v", result["get"])
	}
}

func TestParseWithHeaders(t *testing.T) {
	p, _ := New()

	input := `
get "https://example.com/api"
headers
  Accept "application/json"
  Authorization "Bearer token"
`
	result, err := p.ParseToMap(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	headers, ok := result["headers"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected headers to be map, got %T", result["headers"])
	}

	if headers["Accept"] != "application/json" {
		t.Errorf("Expected Accept header, got %v", headers["Accept"])
	}
}

func TestParseWithBody(t *testing.T) {
	p, _ := New()

	input := `
post "https://example.com/api"
body
  name John
  age 25
  active true
`
	result, err := p.ParseToMap(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	body, ok := result["body"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected body to be map, got %T", result["body"])
	}

	if body["name"] != "John" {
		t.Errorf("Expected name=John, got %v", body["name"])
	}

	if body["age"] != int64(25) {
		t.Errorf("Expected age=25, got %v (type %T)", body["age"], body["age"])
	}

	if body["active"] != true {
		t.Errorf("Expected active=true, got %v", body["active"])
	}
}

func TestParseArray(t *testing.T) {
	p, _ := New()

	input := `
post "https://example.com/api"
body
  tags
    api
    http
    100
`
	result, err := p.ParseToMap(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	body := result["body"].(map[string]interface{})
	tags, ok := body["tags"].([]interface{})
	if !ok {
		t.Fatalf("Expected tags to be array, got %T", body["tags"])
	}

	if len(tags) != 3 {
		t.Errorf("Expected 3 tags, got %d", len(tags))
	}

	if tags[0] != "api" {
		t.Errorf("Expected first tag=api, got %v", tags[0])
	}

	if tags[2] != int64(100) {
		t.Errorf("Expected third tag=100, got %v", tags[2])
	}
}

func TestParseVariables(t *testing.T) {
	p, _ := New()

	input := `
@base_url "https://example.com"
@token "secret"

get "{{base_url}}/api"
headers
  Authorization "{{token}}"
`
	result, err := p.ParseToMap(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if result["get"] != "https://example.com/api" {
		t.Errorf("Expected variable substitution, got %v", result["get"])
	}

	headers := result["headers"].(map[string]interface{})
	if headers["Authorization"] != "secret" {
		t.Errorf("Expected token substitution, got %v", headers["Authorization"])
	}
}

func TestParseNestedObject(t *testing.T) {
	p, _ := New()

	input := `
post "https://example.com/api"
body
  user
    name John
    address
      city Beijing
      zip 100000
`
	result, err := p.ParseToMap(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	body := result["body"].(map[string]interface{})
	user := body["user"].(map[string]interface{})
	
	if user["name"] != "John" {
		t.Errorf("Expected name=John, got %v", user["name"])
	}

	address := user["address"].(map[string]interface{})
	if address["city"] != "Beijing" {
		t.Errorf("Expected city=Beijing, got %v", address["city"])
	}

	if address["zip"] != int64(100000) {
		t.Errorf("Expected zip=100000, got %v", address["zip"])
	}
}

func TestTypeInference(t *testing.T) {
	p, _ := New()

	input := `
post "https://example.com"
body
  str John
  int 42
  float 3.14
  bool_true true
  bool_false false
  null_val null
  quoted "hello world"
`
	result, err := p.ParseToMap(input)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	body := result["body"].(map[string]interface{})

	tests := []struct {
		key      string
		expected interface{}
	}{
		{"str", "John"},
		{"int", int64(42)},
		{"float", 3.14},
		{"bool_true", true},
		{"bool_false", false},
		{"null_val", nil},
		{"quoted", "hello world"},
	}

	for _, tt := range tests {
		if body[tt.key] != tt.expected {
			t.Errorf("%s: expected %v (%T), got %v (%T)",
				tt.key, tt.expected, tt.expected, body[tt.key], body[tt.key])
		}
	}
}
