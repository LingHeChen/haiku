# Haiku

一个极简的 HTTP 客户端，让你写得更少，做得更多。

> [English](README.md) | 中文

## 特性

- **基于缩进的请求体** - 不再需要 `{}`、`,` 或 `"` 等噪音
- **自动类型推断** - `age 25` 自动识别为数字 `25`，而不是字符串 `"25"`
- **请求链式调用** - `$_.token` 引用上一个响应
- **统一的变量系统** - `$var` 用于局部变量，`$env.HOME` 用于环境变量
- **简写值** - `_` 表示 null，`[]` 表示空数组，`{}` 表示空对象
- **字符串处理器** - `json`...`、`base64`...` 和 `file`...` 用于内联数据
- **条件语句** - `if/else` 和 `? :` 语法支持条件执行
- **循环** - `for` 循环支持并行执行
- **调试输出** - `echo` 语句用于调试变量值

## 为什么选择 Haiku？

手写 JSON 很繁琐：

```bash
curl -X POST https://api.example.com/users \
  -H "Content-Type: application/json" \
  -d '{"name":"John","age":25,"address":{"city":"Beijing","zip":"100000"}}'
```

使用 Haiku：

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

### 对比

| 特性          | Haiku                    | curl                                  | HTTPie                          |
| ---------------- | ------------------------ | ------------------------------------- | ------------------------------- |
| 嵌套 JSON 体 | `address` `city Beijing` | `-d '{"address":{"city":"Beijing"}}'` | `address:='{"city":"Beijing"}'` |
| 类型推断   | `age 25` → 数字        | 手动                                | `age:=25`                       |
| 请求链式调用 | `$_.token`               | shell 脚本                         | 不支持                   |
| 变量        | `$var`, `$env.HOME`      | 仅 shell                            | 不支持                   |
| 条件逻辑| `? $env.ENV == "prod"`   | shell 脚本                         | 不支持                   |
| 循环            | `for $item in $items`     | shell 脚本                         | 不支持                   |
| 内联 JSON      | `json`{"a":1}``          | 手动转义                       | `data:='{"a":1}'`               |


## 安装

```bash
go install github.com/LingHeChen/haiku@latest
```

或从源码构建：

```bash
git clone https://github.com/LingHeChen/haiku.git
cd haiku
go build -o haiku .
```

## 使用方法

```bash
# 执行请求文件
haiku request.haiku

# 仅解析（显示 JSON，不发送请求）
haiku -p request.haiku

# 内联请求
haiku -e 'get "https://httpbin.org/get"'

# 从 stdin 读取
echo 'get "https://httpbin.org/ip"' | haiku -

# 详细模式（显示请求详情）
haiku --verbose request.haiku

# 静默模式（仅显示状态码和耗时）
haiku -q request.haiku

# 保存响应到文件
haiku request.haiku -o response.json
```

## 命令行选项

| 选项 | 说明 |
|--------|-------------|
| `-p, --parse` | 仅解析，显示 JSON 而不发送请求 |
| `-q, --quiet` | 静默模式，仅显示状态码和耗时 |
| `--verbose` | 详细模式，显示请求详情（METHOD URL、请求头、请求体） |
| `--body-only` | 仅输出响应体（便于管道处理） |
| `-o <file>` | 保存响应到文件 |
| `-h, --help` | 显示帮助信息 |
| `-v, --version` | 显示版本 |

**详细模式示例：**

使用 `--verbose` 时，你会看到：
```
POST https://httpbin.org/post
Request Headers
  Content-Type: application/json
Request Body
{
  "name": "John",
  "age": 25
}
──────────────────────────────────────────────────
200 OK (234ms)
──────────────────────────────────────────────────
Response Headers
  Content-Type: application/json
Response Body
{...}
```

## 语法

### 基本请求

```haiku
get "https://api.example.com/users"
headers
  Accept "application/json"
  Authorization "Bearer token"
```

### 带请求体的请求

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

### 变量

变量可以保存简单值、复杂对象或数组：

```haiku
# 简单值
@base_url "https://api.example.com"
@token "Bearer xxx"
@timeout 30s  # 全局默认超时（参见超时配置部分）

# 使用缩进定义对象
@user
  name John
  age 25
  role admin

# 使用缩进定义数组
@tags
  api
  http
  test

# 使用 json 处理器定义复杂对象
@config json`{"debug": true, "retries": 3}`

# 使用变量
post "$base_url/users"
headers
  Authorization "$token"
body
  user $user
  tags $tags
  config $config
```

### 环境变量

```haiku
get "https://api.example.com/users"
headers
  Authorization "$env.API_TOKEN"
  X-Home "$env.HOME"
```

> **注意**：为了向后兼容，仍支持旧语法 `{{var}}` 和 `{{$ENV}}`。

### 导入

导入语句允许你在文件间共享变量和配置：

```haiku
# config.haiku
? $env.ENV == "production"
  @base_url "https://api.prod.com"
  @timeout 60s
: $env.ENV == "testing"
  @base_url "https://api.test.com"
  @timeout 30s
:
  @base_url "http://localhost"
  @timeout 10s

@token "Bearer xxx"
```

```haiku
# request.haiku
import "config.haiku"

get "$base_url/users"
headers
  Authorization "$token"
```

**注意：** 导入的文件可以包含任何语句类型，包括条件语句（`if`/`?`）、变量定义，甚至其他导入。所有语句按顺序执行，因此在导入文件中条件设置的变量在导入后可用。

### 条件语句

Haiku 支持两种语法风格的条件执行：

**传统的 if/else 语法：**

```haiku
if $env.ENV == "production"
  @base_url "https://api.prod.com"
  @timeout 60s
else
  @base_url "https://api.dev.com"
  @timeout 30s
```

**使用 `?` 和 `:` 的简写语法（支持多分支）：**

```haiku
? $env.ENV == "production"
  @base_url "https://api.prod.com"
  @timeout 60s
: $env.ENV == "testing"
  @base_url "https://api.test.com"
  @timeout 30s
:
  @base_url "https://api.dev.com"
  @timeout 10s
```

**支持的比较运算符：**
- `==` - 等于
- `!=` - 不等于
- `>` - 大于
- `<` - 小于
- `>=` - 大于等于
- `<=` - 小于等于

**逻辑运算符：**
- `and` - 逻辑与
- `or` - 逻辑或
- `not` - 逻辑非

**示例：**

```haiku
# 字符串比较
? $env.REGION == "us-east"
  @endpoint "https://us-east.api.com"
: $env.REGION == "eu-west"
  @endpoint "https://eu-west.api.com"
:
  @endpoint "https://default.api.com"

# 数值比较
@max_retries 3
? $max_retries > 5
  @timeout 120s
: $max_retries > 0
  @timeout 60s
:
  @timeout 30s

# 逻辑运算符
? $env.DEBUG == "true" and $env.ENV != "production"
  @log_level "debug"
  @verbose true
:
  @log_level "info"
  @verbose false
```

### Echo 语句（调试输出）

使用 `echo` 将值打印到 stderr 进行调试：

```haiku
@base_url "https://api.example.com"
echo "base_url = $base_url"

? $env.ENV == "production"
  @timeout 60s
  echo "Using production timeout: $timeout"
:
  @timeout 30s
  echo "Using dev timeout: $timeout"

get "$base_url/users"
```

输出：
```
[echo] base_url = https://api.example.com
[echo] Using dev timeout: 30s
```

`echo` 语句会计算表达式并将其打印到 stderr，便于调试变量值和条件分支，而不会干扰请求/响应输出。

### 请求链式调用

使用 `---` 分隔多个请求，使用 `$_` 引用上一个响应：

```haiku
# 第一个请求：登录
post "https://api.example.com/login"
body
  username admin
  password secret

---

# 第二个请求：使用上一个响应的 token
get "https://api.example.com/users"
headers
  Authorization "Bearer $_.token"

---

# 第三个请求：使用上一个响应的数据
delete "https://api.example.com/users/$_.data.0.id"
```

**响应引用语法：**

| 语法            | 说明                        |
| ----------------- | ---------------------------------- |
| `$_`              | 整个上一个响应（作为 JSON） |
| `$_.field`        | 顶层字段                    |
| `$_.data.user.id` | 嵌套字段                       |
| `$_.items.0.name` | 数组元素（0 索引）          |

### 超时配置

配置全局或每个请求的超时：

```haiku
# 全局默认超时（30 秒）
@timeout 30s

# 请求 1：使用全局超时
get "https://api.example.com/users"

---

# 请求 2：使用请求级超时覆盖
get "https://api.example.com/slow-endpoint"
timeout 60s

---

# 请求 3：使用毫秒
post "https://api.example.com/upload"
timeout 5000ms

---

# 请求 4：使用分钟
get "https://api.example.com/report"
timeout 2m

---

# 请求 5：数值（默认为秒）
get "https://api.example.com/health"
timeout 10
```

**超时优先级：**
1. 请求级超时（最高优先级）
2. 全局超时（`@timeout` 变量）
3. 默认值（30 秒）

**支持的时间单位：**
- `s`, `sec`, `second`, `seconds` - 秒
- `ms`, `msec`, `millisecond`, `milliseconds` - 毫秒
- `m`, `min`, `minute`, `minutes` - 分钟
- 不带单位的数值默认为秒

### For 循环

遍历数组发送多个请求：

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

这会生成 3 个 POST 请求，每个用户一个。

**带索引变量：**

```haiku
for $index, $user in $users
  post "https://api.example.com/users"
  body
    position $index
    name $user.name
```

**数值循环：**

你可以遍历数字生成一系列请求：

```haiku
# 完整语法：for $i in 10（生成 0, 1, 2, ..., 9）
for $i in 10
  get "https://api.example.com/users/$i"

# 简化语法：for 10（使用默认变量 $index）
for 5
  post "https://api.example.com/users"
  body
    iteration $index
    message "Request #$index"
```

数值循环生成从 `0` 到 `N-1` 的值（例如，`for 10` 生成 `0, 1, 2, ..., 9`）。

### 并行 For 循环

使用 `parallel for` 并发运行循环请求（适用于负载测试）。

**无限制并发：**

```haiku
@urls json`[
  "https://httpbin.org/delay/1",
  "https://httpbin.org/delay/1",
  "https://httpbin.org/delay/1"
]`

parallel for $url in $urls
  get $url
```

**限制并发（最多 N 个 worker）：**

```haiku
@endpoints json`[
  "https://httpbin.org/get?id=1",
  "https://httpbin.org/get?id=2",
  "https://httpbin.org/get?id=3",
  "https://httpbin.org/get?id=4"
]`

parallel 2 for $endpoint in $endpoints
  get $endpoint
```

运行 `parallel for` 时，Haiku 会打印每个循环的统计信息（总数/成功/失败和耗时）。

## 类型推断

值会自动推断：

| 输入               | 类型         | 输出         |
| ------------------- | ------------ | -------------- |
| `name John`         | 字符串       | `"John"`       |
| `age 25`            | 整数          | `25`           |
| `score 98.5`        | 浮点数        | `98.5`         |
| `active true`       | 布尔值         | `true`         |
| `note _`            | null         | `null`         |
| `note null`         | null         | `null`         |
| `tags []`           | 空数组  | `[]`           |
| `meta {}`           | 空对象 | `{}`           |
| `name "John Smith"` | 字符串       | `"John Smith"` |


## 引号规则

**必须加引号：**

- URL：`get "https://example.com/api"`
- 包含空格的字符串：`name "John Smith"`
- 包含特殊字符的字符串：`path "/api/v1"`

**可以不加引号：**

- 简单字符串：`name John`
- 数字：`age 25`
- 布尔值：`active true`

### 字符串处理器

使用处理器语法直接嵌入预处理数据：

```haiku
post "https://api.example.com/data"
body
  # 内联 JSON（支持多行）
  config json`{"key": "value", "nested": {"a": 1}}`
  
  # 多行 JSON
  payload json`{
    "name": "John",
    "age": 25,
    "tags": ["api", "test"]
  }`
  
  # Base64 解码
  message base64`SGVsbG8gV29ybGQh`
  
  # 读取并解析文件（自动检测 JSON）
  config file`config.json`
```


| 处理器 | 说明 | 示例 |
|-----------|-------------|---------|
| json\`...\` | 嵌入原始 JSON | data json\`{"a":1}\` |
| base64\`...\` | 解码 Base64 字符串 | msg base64\`SGVsbG8=\` |
| file\`...\` | 读取文件并解析为 JSON（或作为字符串返回） | config file\`config.json\` |


## HTTP 方法

支持的方法：`get`, `post`, `put`, `delete`, `patch`, `head`, `options`

## 路线图

### 语法简化

- [x] 更短的变量语法：`$var` 替代 `{{var}}`
- [x] 环境变量作为对象：`$env.HOME` 替代 `{{$HOME}}`
- [x] 字符串处理器：json\`...\` 和 base64\`...\` 用于内联数据嵌入
- [x] 结构化变量：使用缩进或 json\`...\` 定义对象和数组
- [ ] URL 不加引号：`get https://api.com` 替代 `get "https://api.com"`
- [ ] 自动检测方法：无 body = GET，有 body = POST
- [ ] 常用 header 快捷方式：`json` → `Content-Type: application/json`，`auth token` → `Authorization: Bearer token`
- [ ] 移除 `headers`/`body` 关键字 - 使用 `>` 前缀表示 headers

### 请求特性

- [x] 使用 `$_` 的请求链式调用：引用上一个响应（`$_.token`, `$_.data.id`）
- [x] For 循环：使用 `for $item in $items` 遍历数组
- [x] 并行 for 循环：使用 `parallel for` 并发执行请求
- [x] 超时配置：全局和每个请求的超时，支持多种时间单位
- [x] 条件语句：`if/else` 和 `? :` 语法用于条件变量赋值
- [ ] 带退避的重试
- [ ] 跟随重定向选项
- [ ] 代理支持
- [ ] Cookie 管理

### 响应处理

- [x] 保存响应到文件：`-o <file>` 选项
- [ ] 响应断言：`expect status 200`，`expect body.id exists`
- [ ] 保存响应到变量：`@user_id = response.id`
- [ ] 输出格式化：`--output json|yaml|table`

### 测试与自动化

- [ ] 测试模式：将多个请求作为测试套件运行
- [ ] Mock 服务器：提供 .haiku 文件中定义的响应
- [ ] 请求差异：比较不同环境之间的响应
- [ ] 从 curl 命令生成 .haiku
- [ ] 从 OpenAPI/Swagger 规范生成 .haiku

### 开发体验

- [x] 详细/调试输出：`--verbose` 标志和 `echo` 语句
- [ ] VS Code 扩展（语法高亮）
- [ ] Watch 模式：文件更改时重新运行
- [ ] 交互式模式（REPL）

## 许可证

MIT
