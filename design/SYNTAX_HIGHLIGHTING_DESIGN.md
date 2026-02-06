# Haiku 语法高亮设计文档

## 设计目标

为 Haiku 语言提供语法高亮支持，提升开发体验。优先支持 VS Code，后续可扩展到其他编辑器。

## 语法元素分类

基于 lexer 的 TokenType，我们需要为以下元素设计高亮：

### 1. 关键字 (Keywords)

**控制流关键字：**
- `import` - 导入语句
- `for` - 循环
- `in` - 循环中的关键字
- `parallel` - 并行执行

**HTTP 方法关键字：**
- `get`, `post`, `put`, `delete`, `patch`, `head`, `options`

**请求结构关键字：**
- `headers` - 请求头
- `body` - 请求体
- `timeout` - 超时设置

**布尔值和空值：**
- `true`, `false`
- `null`, `nil`
- `_` (underscore as null)

**颜色方案：**
- 控制流关键字：蓝色/紫色（如 `keyword.control`）
- HTTP 方法：绿色/青色（如 `entity.name.function`）
- 请求结构关键字：黄色/橙色（如 `keyword.other`）

### 2. 字面量 (Literals)

**字符串：**
- 带引号字符串：`"string"`
- 无引号字符串：`identifier`
- 处理器字符串：`json`\`...\``, `base64`\`...\``

**数字：**
- 整数：`123`
- 浮点数：`45.6`

**特殊字面量：**
- 空数组：`[]`
- 空对象：`{}`

**颜色方案：**
- 字符串：绿色（如 `string.quoted.double`）
- 数字：蓝色（如 `constant.numeric`）
- 处理器字符串：紫色（如 `string.quoted.other`）

### 3. 变量和引用 (Variables & References)

**变量定义：**
- `@variable_name` - 变量定义

**变量引用：**
- `$variable` - 局部变量
- `$env.HOME` - 环境变量
- `$_` - 前一个响应
- `$_.field` - 响应字段访问

**颜色方案：**
- 变量定义：黄色（如 `variable.other.readwrite`）
- 变量引用：青色（如 `variable.other.readwrite`）
- 环境变量：浅蓝色（如 `variable.other.environment`）

### 4. 注释 (Comments)

- `# comment` - 单行注释

**颜色方案：**
- 注释：灰色（如 `comment.line.number-sign`）

### 5. 分隔符 (Separators)

- `---` - 请求分隔符
- `.` - 字段访问符
- `,` - 列表分隔符

**颜色方案：**
- 分隔符：浅灰色（如 `punctuation.separator`）

### 6. URL

- `"https://api.example.com/path"` - URL 字符串

**颜色方案：**
- URL：深蓝色/紫色（如 `string.quoted.double.url`）

## VS Code TextMate Grammar 设计

### 文件结构

```
.vscode/
  └── haiku.tmLanguage.json  # TextMate 语法定义
```

### 优先级设计

1. **注释** - 最高优先级，避免注释中的内容被高亮
2. **字符串** - 包括 URL、处理器字符串
3. **关键字** - HTTP 方法、控制流关键字
4. **变量** - 变量定义和引用
5. **数字** - 数字字面量
6. **分隔符** - 标点符号

### 正则表达式模式

基于 lexer 的实现，设计对应的正则表达式：

```regex
# 注释
#.*$

# HTTP 方法关键字
\b(get|post|put|delete|patch|head|options)\b

# 控制流关键字
\b(import|for|in|parallel)\b

# 请求结构关键字
\b(headers|body|timeout)\b

# 布尔值
\b(true|false|null|nil)\b

# 变量定义
@[a-zA-Z_][a-zA-Z0-9_]*

# 变量引用
\$[a-zA-Z_][a-zA-Z0-9_.]*

# 处理器字符串
\b(json|base64|file)`[^`]*`

# 带引号字符串
"[^"]*"

# URL (特殊字符串)
"(https?://[^"]+)"

# 数字
\b\d+\.?\d*\b

# 空数组/对象
\[\]|\{\}
```

## 颜色主题适配

### Light Theme (默认)

- 关键字：`#0000FF` (蓝色)
- HTTP 方法：`#008000` (绿色)
- 字符串：`#008000` (绿色)
- 数字：`#0000FF` (蓝色)
- 变量：`#001080` (深蓝色)
- 注释：`#6A9955` (灰色)

### Dark Theme

- 关键字：`#569CD6` (浅蓝色)
- HTTP 方法：`#4EC9B0` (青色)
- 字符串：`#CE9178` (橙色)
- 数字：`#B5CEA8` (浅绿色)
- 变量：`#9CDCFE` (浅蓝色)
- 注释：`#6A9955` (灰色)

## 实现计划

### Phase 1: VS Code TextMate Grammar
- [ ] 创建 `.vscode/haiku.tmLanguage.json`
- [ ] 实现基础语法高亮（关键字、字符串、数字）
- [ ] 实现变量高亮
- [ ] 实现注释高亮

### Phase 2: 增强功能
- [ ] URL 特殊高亮
- [ ] 处理器字符串特殊高亮
- [ ] 缩进可视化（可选）
- [ ] 括号匹配（如果有）

### Phase 3: VS Code 扩展
- [ ] 创建完整的 VS Code 扩展
- [ ] 添加代码片段 (snippets)
- [ ] 添加自动完成 (autocomplete)
- [ ] 添加错误检测 (linting)

### Phase 4: 其他编辑器支持
- [ ] Vim/Neovim
- [ ] Sublime Text
- [ ] JetBrains IDEs
- [ ] Emacs

## 示例高亮效果

```haiku
# 这是注释 - 灰色
@base_url "https://api.example.com"  # @变量定义 - 黄色, "字符串" - 绿色

get "$base_url/users"  # get - 绿色, $变量引用 - 青色
headers  # headers - 黄色
  Content-Type "application/json"  # 字符串 - 绿色
body  # body - 黄色
  name John  # name - 默认, John - 字符串
  age 25  # 25 - 蓝色数字

for $user in $users  # for/in - 蓝色, $变量 - 青色
  post "$base_url/users"  # post - 绿色
  body
    user_id $user.id  # $变量引用 - 青色
```

## 参考资源

- [TextMate Grammar 文档](https://macromates.com/manual/en/language_grammars)
- [VS Code 语法高亮指南](https://code.visualstudio.com/api/language-extensions/syntax-highlight-guide)
- [TextMate 作用域命名约定](https://www.sublimetext.com/docs/scope_naming.html)
