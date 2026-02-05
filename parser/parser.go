// Package parser 提供 Haiku 配置格式的解析功能
// Haiku 是一种基于缩进的配置格式，可以转换为 JSON
package parser

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

// ---------------------------------------------------------
// AST 定义
// ---------------------------------------------------------

// Config 表示配置文件的根结构
type Config struct {
	Entries []*Entry `parser:"(@@ (';' @@)*)?"` // 用分号分隔
}

// Entry 表示一个配置项（键值对或列表元素）
type Entry struct {
	Key   string `parser:"(@Ident | @String)?"` // Key 可选
	Value *Value `parser:"@@?"`                 // Value 也可选，用于处理列表元素
}

// Value 表示配置值，支持多种类型
type Value struct {
	Processed   *ProcessedString `parser:"  @ProcessedString"` // json`...`, yaml`...`
	String      *QuotedString    `parser:"| @String"`
	Float       *float64         `parser:"| @Float"`
	Int         *int64           `parser:"| @Int"`
	Bool        *Boolean         `parser:"| @('true' | 'false')"`
	EmptyArray  *string          `parser:"| @EmptyArray"`  // 空数组 []
	EmptyObject *string          `parser:"| @EmptyObject"` // 空对象 {}
	VarRef      *string          `parser:"| @VarRef"`      // 变量引用 $var, $env.VAR
	Raw         *string          `parser:"| @Ident"`       // 处理无引号字符串

	// 嵌套结构：预处理会把缩进变成 { ... }
	Block *Config `parser:"| '{' @@ '}'"`
}

// ---------------------------------------------------------
// 自定义类型
// ---------------------------------------------------------

// QuotedString 自定义类型用于去除引号
type QuotedString string

// Capture 实现 participle 的 Capture 接口
func (s *QuotedString) Capture(values []string) error {
	v := values[0]
	// 去除首尾引号
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		*s = QuotedString(v[1 : len(v)-1])
	} else {
		*s = QuotedString(v)
	}
	return nil
}

// ProcessedString 处理器字符串类型，如 json`...`, yaml`...`
type ProcessedString struct {
	Processor string // json, yaml, base64, file 等
	Content   string // 反引号内的内容
}

// Capture 实现 participle 的 Capture 接口
func (p *ProcessedString) Capture(values []string) error {
	v := values[0]
	// 找到反引号位置
	idx := strings.Index(v, "`")
	if idx == -1 {
		return nil
	}
	p.Processor = v[:idx]
	// 去除反引号
	p.Content = v[idx+1 : len(v)-1]
	return nil
}

// Boolean 自定义类型用于正确捕获布尔值
type Boolean bool

// Capture 实现 participle 的 Capture 接口
func (b *Boolean) Capture(values []string) error {
	*b = values[0] == "true"
	return nil
}

// ---------------------------------------------------------
// JSON 序列化
// ---------------------------------------------------------

// MarshalJSON 自定义 Config 的 JSON 序列化
func (c *Config) MarshalJSON() ([]byte, error) {
	if len(c.Entries) == 0 {
		return []byte("{}"), nil
	}

	// 启发式判断：如果所有元素都只有 Key 没有 Value -> 它是数组（Key 实际上是值）
	isList := true
	for _, e := range c.Entries {
		if e.Value != nil {
			isList = false
			break
		}
	}

	if isList {
		// 转换成 JSON Array - Key 实际上是值
		list := make([]interface{}, len(c.Entries))
		for i, e := range c.Entries {
			list[i] = e.Key
		}
		return json.Marshal(list)
	} else {
		// 转换成 JSON Object
		m := make(map[string]interface{})
		for _, e := range c.Entries {
			k := e.Key
			if k != "" {
				m[k] = e.Value
			}
		}
		return json.Marshal(m)
	}
}

// MarshalJSON 自定义 Value 的 JSON 序列化
func (v *Value) MarshalJSON() ([]byte, error) {
	if v.Processed != nil {
		// 处理 json`...`, yaml`...` 等
		result := processString(v.Processed.Processor, v.Processed.Content)
		return json.Marshal(result)
	}
	if v.String != nil {
		return json.Marshal(v.String)
	}
	if v.Float != nil {
		return json.Marshal(v.Float)
	}
	if v.Int != nil {
		return json.Marshal(v.Int)
	}
	if v.Bool != nil {
		return json.Marshal(v.Bool)
	}
	if v.EmptyArray != nil {
		return []byte("[]"), nil
	}
	if v.EmptyObject != nil {
		return []byte("{}"), nil
	}
	if v.VarRef != nil {
		// 变量引用，作为字符串保留（后续在 Map 级别替换）
		return json.Marshal(v.VarRef)
	}
	if v.Raw != nil {
		// 处理 _ 作为 null
		if *v.Raw == "_" {
			return []byte("null"), nil
		}
		return json.Marshal(v.Raw)
	}
	if v.Block != nil {
		return v.Block.MarshalJSON()
	}
	return []byte("null"), nil
}

// ---------------------------------------------------------
// 预处理器
// ---------------------------------------------------------

// 处理器字符串正则（匹配 json`...` 等，包括多行）
var processedStringRegex = regexp.MustCompile("(?s)[a-zA-Z_][a-zA-Z0-9_]*`[^`]*`")

// preprocess 将基于缩进的格式转换为基于大括号的格式
func preprocess(input string) string {
	// 1. 保护处理器字符串（json`...` 等），避免被预处理破坏
	placeholders := make(map[string]string)
	placeholderIdx := 0
	input = processedStringRegex.ReplaceAllStringFunc(input, func(match string) string {
		placeholder := fmt.Sprintf("__PROC_%d__", placeholderIdx)
		placeholders[placeholder] = match
		placeholderIdx++
		return placeholder
	})

	// 2. 正常预处理
	var sb strings.Builder
	lines := strings.Split(input, "\n")
	stack := []int{0}
	firstContent := true

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// 跳过空行、注释、变量定义、import 语句
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "@") || strings.HasPrefix(trimmed, "import ") {
			continue
		}

		// 计算原始缩进（空格数）
		indent := 0
		for _, char := range line {
			if char == ' ' {
				indent++
			} else if char == '\t' {
				indent += 4
			} else {
				break
			}
		}

		current := stack[len(stack)-1]

		if indent > current {
			stack = append(stack, indent)
			sb.WriteString(" { ")
		} else {
			for indent < current {
				stack = stack[:len(stack)-1]
				current = stack[len(stack)-1]
				sb.WriteString(" } ")
			}
			if !firstContent {
				sb.WriteString(" ; ")
			}
		}

		sb.WriteString(trimmed)
		firstContent = false
	}

	// 闭合剩余的块
	for len(stack) > 1 {
		stack = stack[:len(stack)-1]
		sb.WriteString(" }")
	}

	result := sb.String()

	// 3. 恢复处理器字符串
	for placeholder, original := range placeholders {
		result = strings.ReplaceAll(result, placeholder, original)
	}

	return result
}

// ---------------------------------------------------------
// 变量处理
// ---------------------------------------------------------

// 变量定义正则: @name 开头（支持单行和多行）
var varDefStartRegex = regexp.MustCompile(`^(\s*)@(\w+)\s*=?\s*(.*)$`)

// 处理器字符串正则（用于变量值）
var varProcessorRegex = regexp.MustCompile("^([a-zA-Z_][a-zA-Z0-9_]*)`([^`]*)`$")

// 新变量引用正则: $var, $env.VAR, $_.field
var varRefRegex = regexp.MustCompile(`\$(\w+(?:\.\w+)*)`)

// 完整变量引用正则（用于检测值是否完全是变量引用）
var fullVarRefRegex = regexp.MustCompile(`^\$(\w+)$`)

// 旧变量引用正则（兼容）: {{name}} 或 {{$ENV_VAR}}
var legacyVarRefRegex = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// import 正则: import "filename"
var importRegex = regexp.MustCompile(`^\s*import\s+"([^"]+)"`)

// extractStructuredVariables 从输入中提取结构化变量定义
// 支持简单值、处理器和多行缩进
func extractStructuredVariables(input string, basePath string) map[string]interface{} {
	vars := make(map[string]interface{})
	lines := strings.Split(input, "\n")
	i := 0

	for i < len(lines) {
		line := lines[i]

		// 处理 import
		if matches := importRegex.FindStringSubmatch(line); matches != nil {
			importPath := matches[1]
			if basePath != "" && !strings.HasPrefix(importPath, "/") {
				importPath = basePath + "/" + importPath
			}
			if content, err := os.ReadFile(importPath); err == nil {
				importedVars := extractStructuredVariables(string(content), dirPath(importPath))
				for k, v := range importedVars {
					vars[k] = v
				}
			}
			i++
			continue
		}

		// 处理变量定义 @name ...
		if matches := varDefStartRegex.FindStringSubmatch(line); matches != nil {
			baseIndent := matches[1]
			varName := matches[2]
			valueStr := strings.TrimSpace(matches[3])

			if valueStr != "" {
				// 同一行有值
				vars[varName] = parseVariableValue(valueStr)
			} else {
				// 多行缩进块
				rawLines := []string{}
				baseIndentLen := len(baseIndent)
				i++

				// 收集缩进的子行（保留原始内容）
				for i < len(lines) {
					nextLine := lines[i]
					if strings.TrimSpace(nextLine) == "" {
						// 空行保留
						rawLines = append(rawLines, "")
						i++
						continue
					}

					// 计算缩进
					trimmed := strings.TrimLeft(nextLine, " \t")
					currentIndent := len(nextLine) - len(trimmed)

					// 如果缩进比变量定义行更深，属于这个块
					if currentIndent > baseIndentLen {
						rawLines = append(rawLines, nextLine)
						i++
					} else {
						// 缩进结束，退出
						break
					}
				}

				// 找到最小公共缩进并去除
				if len(rawLines) > 0 {
					minIndent := -1
					for _, line := range rawLines {
						if strings.TrimSpace(line) == "" {
							continue
						}
						trimmed := strings.TrimLeft(line, " \t")
						indent := len(line) - len(trimmed)
						if minIndent == -1 || indent < minIndent {
							minIndent = indent
						}
					}

					// 去除最小公共缩进
					blockLines := make([]string, len(rawLines))
					for j, line := range rawLines {
						if strings.TrimSpace(line) == "" || len(line) < minIndent {
							blockLines[j] = ""
						} else {
							blockLines[j] = line[minIndent:]
						}
					}

					blockContent := strings.Join(blockLines, "\n")
					vars[varName] = parseBlockToValue(blockContent)
				}
				continue
			}
			i++
			continue
		}

		i++
	}

	return vars
}

// parseVariableValue 解析变量值（单行）
func parseVariableValue(valueStr string) interface{} {
	// 检查是否是处理器语法 processor`content`
	if matches := varProcessorRegex.FindStringSubmatch(valueStr); matches != nil {
		processor := matches[1]
		content := matches[2]
		return processString(processor, content)
	}

	// 检查是否是带引号的字符串
	if len(valueStr) >= 2 && valueStr[0] == '"' && valueStr[len(valueStr)-1] == '"' {
		return valueStr[1 : len(valueStr)-1]
	}

	// 使用类型推断
	return inferType(valueStr)
}

// parseBlockToValue 将缩进块解析为结构化值
func parseBlockToValue(blockContent string) interface{} {
	// 使用现有的解析逻辑
	p, err := New()
	if err != nil {
		return blockContent
	}

	// 预处理（缩进 → 大括号）
	bracedCode := preprocess(blockContent)

	// 解析
	config, err := p.parser.ParseString("", bracedCode)
	if err != nil {
		return blockContent
	}

	// 判断是对象还是数组
	if len(config.Entries) == 0 {
		return map[string]interface{}{}
	}

	// 检查是否是列表
	isList := true
	for _, e := range config.Entries {
		if e.Key != "" && e.Value != nil {
			isList = false
			break
		}
	}

	if isList {
		return config.ToSlice()
	}
	return config.ToMap()
}

// extractVariablesWithImports 从输入中提取变量（兼容旧接口，返回字符串 map）
func extractVariablesWithImports(input string, basePath string) map[string]string {
	structuredVars := extractStructuredVariables(input, basePath)
	vars := make(map[string]string)

	for k, v := range structuredVars {
		switch val := v.(type) {
		case string:
			vars[k] = val
		case int64:
			vars[k] = strconv.FormatInt(val, 10)
		case float64:
			vars[k] = strconv.FormatFloat(val, 'f', -1, 64)
		case bool:
			vars[k] = strconv.FormatBool(val)
		default:
			// 复杂类型转为 JSON
			if jsonBytes, err := json.Marshal(val); err == nil {
				vars[k] = string(jsonBytes)
			}
		}
	}

	return vars
}

// dirPath 获取文件所在目录
func dirPath(filePath string) string {
	lastSlash := strings.LastIndex(filePath, "/")
	if lastSlash == -1 {
		return ""
	}
	return filePath[:lastSlash]
}

// substituteVariables 替换输入中的变量引用
func substituteVariables(input string, vars map[string]string) string {
	// 先处理旧语法 {{var}} 和 {{$ENV}}（兼容）
	input = legacyVarRefRegex.ReplaceAllStringFunc(input, func(match string) string {
		// 提取变量名 (去掉 {{ 和 }})
		name := match[2 : len(match)-2]
		name = strings.TrimSpace(name)

		// 环境变量引用 (以 $ 开头)
		if strings.HasPrefix(name, "$") {
			envName := name[1:]
			if val := os.Getenv(envName); val != "" {
				return val
			}
			return match // 保留原样
		}

		// 普通变量
		if val, ok := vars[name]; ok {
			return val
		}

		// 未找到，保留原样
		return match
	})

	// 处理新语法 $var, $env.VAR
	input = varRefRegex.ReplaceAllStringFunc(input, func(match string) string {
		// 提取变量名 (去掉 $)
		name := match[1:]

		// 环境变量引用: $env.VAR
		if strings.HasPrefix(name, "env.") {
			envName := name[4:] // 去掉 "env."
			if val := os.Getenv(envName); val != "" {
				return val
			}
			return match // 保留原样
		}

		// 上一个响应引用: $_.field（预留，暂不实现）
		if strings.HasPrefix(name, "_") {
			// TODO: 实现响应链式引用
			return match
		}

		// 普通变量
		if val, ok := vars[name]; ok {
			return val
		}

		// 未找到，保留原样
		return match
	})

	return input
}

// ---------------------------------------------------------
// 自定义 Lexer（支持带连字符的标识符，如 Content-Type）
// ---------------------------------------------------------

var haikuLexer = lexer.MustSimple([]lexer.SimpleRule{
	{Name: "ProcessedString", Pattern: "[a-zA-Z_][a-zA-Z0-9_]*`[\\s\\S]*?`"}, // json`...`, yaml`...` (支持多行)
	{Name: "String", Pattern: `"(?:[^"\\]|\\.)*"`},
	{Name: "Float", Pattern: `\d+\.\d+`},
	{Name: "Int", Pattern: `\d+`},
	{Name: "EmptyArray", Pattern: `\[\]`},                        // 空数组
	{Name: "EmptyObject", Pattern: `\{\}`},                       // 空对象
	{Name: "VarRef", Pattern: `\$[a-zA-Z_][a-zA-Z0-9_.]*`},       // 变量引用 $var, $env.VAR
	{Name: "Ident", Pattern: `[a-zA-Z_][a-zA-Z0-9_-]*`},          // 支持连字符
	{Name: "Punct", Pattern: `[{};]`},
	{Name: "Whitespace", Pattern: `[ \t]+`},
})

// ---------------------------------------------------------
// 公开 API
// ---------------------------------------------------------

// Parser Haiku 解析器
type Parser struct {
	parser *participle.Parser[Config]
}

// 全局单例解析器（避免重复初始化）
var defaultParser *Parser
var defaultParserErr error

func init() {
	// 程序启动时初始化解析器
	p, err := participle.Build[Config](
		participle.Lexer(haikuLexer),
		participle.Elide("Whitespace"), // 跳过空白
	)
	if err != nil {
		defaultParserErr = err
		return
	}
	defaultParser = &Parser{parser: p}
}

// New 返回全局解析器实例（单例模式）
func New() (*Parser, error) {
	if defaultParserErr != nil {
		return nil, defaultParserErr
	}
	return defaultParser, nil
}

// Parse 解析 Haiku 格式的字符串，返回 Config AST
func (p *Parser) Parse(input string) (*Config, error) {
	return p.ParseWithBasePath(input, "")
}

// ParseWithBasePath 解析 Haiku 格式的字符串，支持相对路径的 import
func (p *Parser) ParseWithBasePath(input string, basePath string) (*Config, error) {
	// 1. 提取变量（支持 import）
	vars := extractVariablesWithImports(input, basePath)

	// 2. 替换变量引用
	input = substituteVariables(input, vars)

	// 3. 预处理（缩进 → 大括号）
	bracedCode := preprocess(input)

	// 4. 解析
	return p.parser.ParseString("", bracedCode)
}

// ParseToJSON 解析 Haiku 格式的字符串并转换为 JSON 字节数组
func (p *Parser) ParseToJSON(input string) ([]byte, error) {
	config, err := p.Parse(input)
	if err != nil {
		return nil, err
	}
	return json.Marshal(config)
}

// ParseToJSONIndent 解析 Haiku 格式的字符串并转换为格式化的 JSON 字节数组
func (p *Parser) ParseToJSONIndent(input string, prefix, indent string) ([]byte, error) {
	config, err := p.Parse(input)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(config, prefix, indent)
}

// ParseToMap 解析 Haiku 格式的字符串并转换为 map[string]interface{}
func (p *Parser) ParseToMap(input string) (map[string]interface{}, error) {
	return p.ParseToMapWithBasePath(input, "")
}

// ParseToMapWithBasePath 解析 Haiku 格式的字符串，支持 import
func (p *Parser) ParseToMapWithBasePath(input string, basePath string) (map[string]interface{}, error) {
	config, err := p.ParseWithBasePath(input, basePath)
	if err != nil {
		return nil, err
	}
	return config.ToMap(), nil
}

// ToMap 将 Config 转换为 map[string]interface{}
func (c *Config) ToMap() map[string]interface{} {
	if len(c.Entries) == 0 {
		return map[string]interface{}{}
	}

	m := make(map[string]interface{})
	for _, e := range c.Entries {
		if e.Key != "" {
			m[e.Key] = e.Value.ToInterface()
		}
	}
	return m
}

// ToSlice 将 Config 转换为 []interface{}（用于列表场景）
func (c *Config) ToSlice() []interface{} {
	list := make([]interface{}, len(c.Entries))
	for i, e := range c.Entries {
		if e.Value != nil {
			// 有 Value 时使用 Value（如数字 100）
			list[i] = e.Value.ToInterface()
		} else if e.Key != "" {
			// 只有 Key 时，Key 就是值（如 api, http）
			list[i] = inferType(e.Key)
		} else {
			list[i] = nil
		}
	}
	return list
}

// ToInterface 将 Value 转换为 interface{}
func (v *Value) ToInterface() interface{} {
	if v == nil {
		return nil
	}
	if v.Processed != nil {
		return processString(v.Processed.Processor, v.Processed.Content)
	}
	if v.String != nil {
		return string(*v.String)
	}
	if v.Float != nil {
		return *v.Float
	}
	if v.Int != nil {
		return *v.Int
	}
	if v.Bool != nil {
		return bool(*v.Bool)
	}
	if v.EmptyArray != nil {
		return []interface{}{}
	}
	if v.EmptyObject != nil {
		return map[string]interface{}{}
	}
	if v.VarRef != nil {
		// 变量引用，作为字符串返回（后续在 Map 级别替换）
		return *v.VarRef
	}
	if v.Raw != nil {
		// 智能类型推断
		return inferType(*v.Raw)
	}
	if v.Block != nil {
		// 判断是数组还是对象
		// 数组：所有元素要么只有 Key（无 Value），要么只有 Value（无 Key）
		// 对象：所有元素同时有 Key 和 Value
		isList := true
		for _, e := range v.Block.Entries {
			// 如果同时有 Key 和 Value，说明是对象
			if e.Key != "" && e.Value != nil {
				isList = false
				break
			}
		}
		if isList {
			return v.Block.ToSlice()
		}
		return v.Block.ToMap()
	}
	return nil
}

// processString 处理字符串处理器
func processString(processor, content string) interface{} {
	switch processor {
	case "json":
		var result interface{}
		if err := json.Unmarshal([]byte(content), &result); err != nil {
			// 解析失败返回原始字符串
			return content
		}
		return result
	case "base64":
		decoded, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			return content
		}
		return string(decoded)
	case "file":
		data, err := os.ReadFile(content)
		if err != nil {
			return content
		}
		// 尝试解析为 JSON
		var result interface{}
		if err := json.Unmarshal(data, &result); err == nil {
			return result
		}
		return string(data)
	default:
		// 未知处理器，返回原始内容
		return content
	}
}

// inferType 智能推断字符串值的实际类型
func inferType(s string) interface{} {
	// 尝试布尔值
	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}

	// 尝试 null（支持 _, null, nil）
	if s == "_" || s == "null" || s == "nil" {
		return nil
	}

	// 尝试整数
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}

	// 尝试浮点数
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}

	// 默认作为字符串
	return s
}

// ---------------------------------------------------------
// 链式调用支持
// ---------------------------------------------------------

// SplitRequests 将输入按 --- 分割成多个请求
func SplitRequests(input string) []string {
	// 使用 --- 分割（独占一行）
	parts := regexp.MustCompile(`(?m)^---\s*$`).Split(input, -1)
	
	var requests []string
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			requests = append(requests, part)
		}
	}
	
	return requests
}

// getNestedValue 从 map 中获取嵌套字段的值
// 支持路径如 "data.user.id"
func getNestedValue(data interface{}, path string) interface{} {
	if path == "" {
		return data
	}
	
	parts := strings.Split(path, ".")
	current := data
	
	for _, part := range parts {
		switch v := current.(type) {
		case map[string]interface{}:
			current = v[part]
		case []interface{}:
			// 支持数组索引
			if idx, err := strconv.Atoi(part); err == nil && idx >= 0 && idx < len(v) {
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

// ExtractVariables 从整个输入中提取变量（包括 import）
// 用于在分割请求之前提取全局变量（返回字符串 map，兼容旧代码）
func ExtractVariables(input string, basePath string) map[string]string {
	return extractVariablesWithImports(input, basePath)
}

// ExtractStructuredVariables 从整个输入中提取结构化变量（包括 import）
// 返回 map[string]interface{}，支持数组、对象等复杂类型
func ExtractStructuredVariables(input string, basePath string) map[string]interface{} {
	return extractStructuredVariables(input, basePath)
}

// ParseToMapWithResponse 解析请求，支持上一个响应的引用
func (p *Parser) ParseToMapWithResponse(input string, basePath string, prevResponse map[string]interface{}) (map[string]interface{}, error) {
	// 从当前请求块提取结构化变量
	vars := extractStructuredVariables(input, basePath)
	return p.ParseToMapWithStructuredVars(input, vars, prevResponse)
}

// ParseToMapWithVars 解析请求，使用预先提取的变量（兼容旧接口）
func (p *Parser) ParseToMapWithVars(input string, vars map[string]string, prevResponse map[string]interface{}) (map[string]interface{}, error) {
	// 转换为结构化变量
	structuredVars := make(map[string]interface{})
	for k, v := range vars {
		structuredVars[k] = v
	}
	return p.ParseToMapWithStructuredVars(input, structuredVars, prevResponse)
}

// ParseToMapWithStructuredVars 解析请求，使用结构化变量
func (p *Parser) ParseToMapWithStructuredVars(input string, vars map[string]interface{}, prevResponse map[string]interface{}) (map[string]interface{}, error) {
	// 1. 移除变量定义行（避免被解析为请求内容）
	input = removeVariableDefinitions(input)

	// 2. 预处理（缩进 → 大括号）
	bracedCode := preprocess(input)

	// 3. 解析
	config, err := p.parser.ParseString("", bracedCode)
	if err != nil {
		return nil, err
	}

	// 4. 转换为 Map
	result := config.ToMap()

	// 5. 在 Map 级别替换变量引用（支持结构化值）
	if len(vars) > 0 {
		result = substituteVariablesInMap(result, vars).(map[string]interface{})
	}

	// 6. 在 Map 级别替换 $_ 响应引用（保留 JSON 结构）
	if prevResponse != nil {
		result = substituteResponseInMap(result, prevResponse).(map[string]interface{})
	}

	return result, nil
}

// removeVariableDefinitions 移除输入中的变量定义行
func removeVariableDefinitions(input string) string {
	lines := strings.Split(input, "\n")
	var result []string
	i := 0

	for i < len(lines) {
		line := lines[i]

		// 跳过 import 行
		if importRegex.MatchString(line) {
			i++
			continue
		}

		// 跳过变量定义行及其缩进块
		if matches := varDefStartRegex.FindStringSubmatch(line); matches != nil {
			baseIndent := matches[1]
			baseIndentLen := len(baseIndent)
			valueStr := strings.TrimSpace(matches[3])

			i++
			// 如果同一行没有值，跳过后续缩进块
			if valueStr == "" {
				for i < len(lines) {
					nextLine := lines[i]
					if strings.TrimSpace(nextLine) == "" {
						i++
						continue
					}
					trimmed := strings.TrimLeft(nextLine, " \t")
					currentIndent := len(nextLine) - len(trimmed)
					if currentIndent > baseIndentLen {
						i++
					} else {
						break
					}
				}
			}
			continue
		}

		result = append(result, line)
		i++
	}

	return strings.Join(result, "\n")
}

// substituteVariablesOnly 只替换普通变量和环境变量，不处理 $_ 响应引用
func substituteVariablesOnly(input string, vars map[string]string) string {
	// 先处理旧语法 {{var}} 和 {{$ENV}}（兼容）
	input = legacyVarRefRegex.ReplaceAllStringFunc(input, func(match string) string {
		name := match[2 : len(match)-2]
		name = strings.TrimSpace(name)

		if strings.HasPrefix(name, "$") {
			envName := name[1:]
			if val := os.Getenv(envName); val != "" {
				return val
			}
			return match
		}

		if val, ok := vars[name]; ok {
			return val
		}

		return match
	})

	// 处理新语法 $var, $env.VAR（但不处理 $_）
	input = varRefRegex.ReplaceAllStringFunc(input, func(match string) string {
		name := match[1:] // 去掉 $

		// 响应引用 $_ 保留不替换，留到 Map 级别处理
		if strings.HasPrefix(name, "_") {
			return match
		}

		// 环境变量引用: $env.VAR
		if strings.HasPrefix(name, "env.") {
			envName := name[4:]
			if val := os.Getenv(envName); val != "" {
				return val
			}
			return match
		}

		// 普通变量
		if val, ok := vars[name]; ok {
			return val
		}

		return match
	})

	return input
}

// substituteResponseInMap 在 Map 级别替换 $_ 响应引用
func substituteResponseInMap(v interface{}, prevResponse map[string]interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for k, v := range val {
			result[k] = substituteResponseInMap(v, prevResponse)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, v := range val {
			result[i] = substituteResponseInMap(v, prevResponse)
		}
		return result
	case string:
		// 检查是否是 $_ 引用
		if strings.HasPrefix(val, "$_") {
			return resolveResponseRef(val, prevResponse)
		}
		return val
	default:
		return val
	}
}

// substituteVariablesInMap 在 Map 级别替换变量引用（支持结构化值）
func substituteVariablesInMap(v interface{}, vars map[string]interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for k, v := range val {
			result[k] = substituteVariablesInMap(v, vars)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, v := range val {
			result[i] = substituteVariablesInMap(v, vars)
		}
		return result
	case string:
		return resolveVariableRef(val, vars)
	default:
		return val
	}
}

// resolveVariableRef 解析变量引用并返回实际值
func resolveVariableRef(val string, vars map[string]interface{}) interface{} {
	// 检查是否是完整的变量引用 $varname（不含其他字符）
	if matches := fullVarRefRegex.FindStringSubmatch(val); matches != nil {
		varName := matches[1]
		// 跳过 $_ 响应引用和 $env 环境变量
		if varName != "_" && !strings.HasPrefix(varName, "env") {
			if varVal, ok := vars[varName]; ok {
				return varVal // 返回结构化值
			}
		}
	}

	// 处理字符串内插（只替换字符串类型的变量）
	result := varRefRegex.ReplaceAllStringFunc(val, func(match string) string {
		name := match[1:] // 去掉 $

		// 响应引用 $_ 保留
		if strings.HasPrefix(name, "_") {
			return match
		}

		// 环境变量引用: $env.VAR
		if strings.HasPrefix(name, "env.") {
			envName := name[4:]
			if envVal := os.Getenv(envName); envVal != "" {
				return envVal
			}
			return match
		}

		// 普通变量 - 只有字符串类型才能内插
		if varVal, ok := vars[name]; ok {
			switch v := varVal.(type) {
			case string:
				return v
			case int64:
				return strconv.FormatInt(v, 10)
			case float64:
				return strconv.FormatFloat(v, 'f', -1, 64)
			case bool:
				return strconv.FormatBool(v)
			default:
				// 复杂类型转为 JSON
				if jsonBytes, err := json.Marshal(v); err == nil {
					return string(jsonBytes)
				}
			}
		}

		return match
	})

	return result
}

// resolveResponseRef 解析 $_ 引用并返回实际值
func resolveResponseRef(ref string, prevResponse map[string]interface{}) interface{} {
	if prevResponse == nil {
		return ref
	}

	// $_ 返回整个响应
	if ref == "$_" {
		return prevResponse
	}

	// $_.field.subfield 返回嵌套字段
	if strings.HasPrefix(ref, "$_.") {
		path := ref[3:] // 去掉 "$_."
		value := getNestedValue(prevResponse, path)
		if value != nil {
			return value
		}
	}

	// 未找到，返回原值
	return ref
}