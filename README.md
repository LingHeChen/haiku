# Haiku

A minimalist HTTP client that lets you write less and do more.

## Features

- **Indentation-based body** - No more `{}`, `,`, or `"` noise
- **Auto type inference** - `age 25` becomes `25`, not `"25"`
- **Request chaining** - `$_.token` references previous response
- **Unified variables** - `$var` for local, `$env.HOME` for environment
- **Shorthand values** - `_` for null, `[]` for empty array, `{}` for empty object
- **String processors** - `json`... `and `base64`...` for inline data

## Why Haiku?

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

### Comparison


| Feature          | Haiku                    | curl                                  | HTTPie                          |
| ---------------- | ------------------------ | ------------------------------------- | ------------------------------- |
| Nested JSON body | `address` `city Beijing` | `-d '{"address":{"city":"Beijing"}}'` | `address:='{"city":"Beijing"}'` |
| Type inference   | `age 25` → number        | manual                                | `age:=25`                       |
| Request chaining | `$_.token`               | shell scripts                         | not supported                   |
| Variables        | `$var`, `$env.HOME`      | shell only                            | not supported                   |
| Inline JSON      | `json`{"a":1}``          | manual escaping                       | `data:='{"a":1}'`               |


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

Variables can hold simple values, complex objects, or arrays:

```haiku
# Simple values
@base_url "https://api.example.com"
@token "Bearer xxx"
@timeout 30

# Objects using indentation
@user
  name John
  age 25
  role admin

# Arrays using indentation
@tags
  api
  http
  test

# Complex objects using json processor
@config json`{"debug": true, "retries": 3}`

# Use variables
post "$base_url/users"
headers
  Authorization "$token"
body
  user $user
  tags $tags
  config $config
```

### Environment Variables

```haiku
get "https://api.example.com/users"
headers
  Authorization "$env.API_TOKEN"
  X-Home "$env.HOME"
```

> **Note**: Legacy syntax `{{var}}` and `{{$ENV}}` is still supported for backward compatibility.

### Import

```haiku
# config.haiku
@base_url "https://api.example.com"
@token "Bearer xxx"
```

```haiku
# request.haiku
import "config.haiku"

get "$base_url/users"
headers
  Authorization "$token"
```

### Request Chaining

Use `---` to separate multiple requests and `$_` to reference the previous response:

```haiku
# First request: login
post "https://api.example.com/login"
body
  username admin
  password secret

---

# Second request: use token from previous response
get "https://api.example.com/users"
headers
  Authorization "Bearer $_.token"

---

# Third request: use data from previous response
delete "https://api.example.com/users/$_.data.0.id"
```

**Response Reference Syntax:**


| Syntax            | Description                        |
| ----------------- | ---------------------------------- |
| `$_`              | Entire previous response (as JSON) |
| `$_.field`        | Top-level field                    |
| `$_.data.user.id` | Nested field                       |
| `$_.items.0.name` | Array element (0-indexed)          |

### For Loop

Iterate over arrays to send multiple requests:

```haiku
@users json`[
  {"id": 1, "name": "Alice"},
  {"id": 2, "name": "Bob"},
  {"id": 3, "name": "Charlie"}
]`

for $user in $users
  post "https://api.example.com/users"
  headers
    Content-Type "application/json"
  body
    user_id $user.id
    user_name $user.name
```

This generates 3 POST requests, one for each user.

## Type Inference

Values are automatically inferred:


| Input               | Type         | Output         |
| ------------------- | ------------ | -------------- |
| `name John`         | string       | `"John"`       |
| `age 25`            | int          | `25`           |
| `score 98.5`        | float        | `98.5`         |
| `active true`       | bool         | `true`         |
| `note _`            | null         | `null`         |
| `note null`         | null         | `null`         |
| `tags []`           | empty array  | `[]`           |
| `meta {}`           | empty object | `{}`           |
| `name "John Smith"` | string       | `"John Smith"` |


## Quoting Rules

**Must be quoted:**

- URLs: `get "https://example.com/api"`
- Strings with spaces: `name "John Smith"`
- Strings with special characters: `path "/api/v1"`

**Can be unquoted:**

- Simple strings: `name John`
- Numbers: `age 25`
- Booleans: `active true`

### String Processors

Embed pre-processed data directly using processor syntax:

```haiku
post "https://api.example.com/data"
body
  # Inline JSON (supports multi-line)
  config json`{"key": "value", "nested": {"a": 1}}`
  
  # Multi-line JSON
  payload json`{
    "name": "John",
    "age": 25,
    "tags": ["api", "test"]
  }`
  
  # Base64 decode
  message base64`SGVsbG8gV29ybGQh`
```


| Processor | Description | Example |
|-----------|-------------|---------|
| json\`...\` | Embed raw JSON | data json\`{"a":1}\` |
| base64\`...\` | Decode Base64 string | msg base64\`SGVsbG8=\` |


## HTTP Methods

Supported methods: `get`, `post`, `put`, `delete`, `patch`, `head`, `options`

## Roadmap

### Syntax Simplification

- [x] Shorter variable syntax: `$var` instead of `{{var}}`
- [x] Environment variables as object: `$env.HOME` instead of `{{$HOME}}`
- [x] String processors: json\`...\` and base64\`...\` for inline data embedding
- [x] Structured variables: objects and arrays using indentation or json\`...\`
- [ ] URL without quotes: `get https://api.com` instead of `get "https://api.com"`
- [ ] Auto-detect method: no body = GET, has body = POST
- [ ] Common header shortcuts: `json` → `Content-Type: application/json`, `auth token` → `Authorization: Bearer token`
- [ ] Remove `headers`/`body` keywords - use `>` prefix for headers

### Request Features

- [x] Request chaining with `$_`: reference previous response (`$_.token`, `$_.data.id`)
- [x] For loop: iterate over arrays with `for $item in $items`
- [ ] Retry with backoff
- [ ] Timeout configuration
- [ ] Follow redirects option
- [ ] Proxy support
- [ ] Cookie jar

### Response Handling

- [ ] Response assertions: `expect status 200`, `expect body.id exists`
- [ ] Save response to variable: `@user_id = response.id`
- [ ] Output formatting: `--output json|yaml|table`
- [ ] Save response to file

### Testing & Automation

- [ ] Test mode: run multiple requests as test suite
- [ ] Mock server: serve responses defined in .haiku files
- [ ] Request diff: compare responses between environments
- [ ] Generate .haiku from curl command
- [ ] Generate .haiku from OpenAPI/Swagger spec

### Developer Experience

- [ ] VS Code extension with syntax highlighting
- [ ] Watch mode: re-run on file change
- [ ] Interactive mode (REPL)
- [ ] Verbose/debug output

## License

MIT