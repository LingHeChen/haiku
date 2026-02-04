# Haiku

A human-friendly HTTP client with simplified syntax for request bodies.

## Why?

Writing JSON by hand is tedious:

```bash
curl -X POST https://api.example.com/users \
  -H "Content-Type: application/json" \
  -d '{"name":"John","age":25,"address":{"city":"Beijing","zip":"100000"}}'
```

With Haiku:

```bash
haiku -e '
post "https://api.example.com/users"
headers
  Content-Type "application/json"
body
  name John
  age 25
  address
    city Beijing
    zip 100000
'
```

## Installation

```bash
go install github.com/LingHeChen/haiku@latest
```

Or build from source:

```bash
git clone https://github.com/LingHeChen/haiku.git
cd haiku
go build -o haiku .
```

## Usage

```bash
# Execute a request file
haiku request.haiku

# Parse only (show JSON, no request)
haiku -p request.haiku

# Inline request
haiku -e 'get "https://httpbin.org/get"'

# From stdin
echo 'get "https://httpbin.org/ip"' | haiku -
```

## Syntax

### Basic Request

```haiku
get "https://api.example.com/users"
headers
  Accept "application/json"
  Authorization "Bearer token"
```

### Request with Body

```haiku
post "https://api.example.com/users"
headers
  Content-Type "application/json"
body
  name John
  age 25
  active true
  tags
    api
    http
```

### Variables

```haiku
# Define variables
@base_url "https://api.example.com"
@token "Bearer xxx"

# Use variables
get "{{base_url}}/users"
headers
  Authorization "{{token}}"
```

### Environment Variables

```haiku
get "https://api.example.com/users"
headers
  Authorization "{{$API_TOKEN}}"
  X-Home "{{$HOME}}"
```

### Import

```haiku
# config.haiku
@base_url "https://api.example.com"
@token "Bearer xxx"
```

```haiku
# request.haiku
import "config.haiku"

get "{{base_url}}/users"
headers
  Authorization "{{token}}"
```

## Type Inference

Values are automatically inferred:

| Input | Type | Output |
|-------|------|--------|
| `name John` | string | `"John"` |
| `age 25` | int | `25` |
| `score 98.5` | float | `98.5` |
| `active true` | bool | `true` |
| `note _` | null | `null` |
| `note null` | null | `null` |
| `tags []` | empty array | `[]` |
| `meta {}` | empty object | `{}` |
| `name "John Smith"` | string | `"John Smith"` |

## Quoting Rules

**Must be quoted:**
- URLs: `get "https://example.com/api"`
- Strings with spaces: `name "John Smith"`
- Strings with special characters: `path "/api/v1"`

**Can be unquoted:**
- Simple strings: `name John`
- Numbers: `age 25`
- Booleans: `active true`

## HTTP Methods

Supported methods: `get`, `post`, `put`, `delete`, `patch`, `head`, `options`

## License

MIT
