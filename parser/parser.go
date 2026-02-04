// Package parser 提供 Haiku 配置格式的解析功能
// Haiku 是一种基于缩进的配置格式，可以转换为 JSON
package parser

import (
	"encoding/json"
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
	String      *QuotedString `parser:"  @String"`
	Float       *float64      `parser:"| @Float"`
	Int         *int64        `parser:"| @Int"`
	Bool        *Boolean      `parser:"| @('true' | 'false')"`
	EmptyArray  *string       `parser:"| @EmptyArray"`  // 空数组 []
	EmptyObject *string       `parser:"| @EmptyObject"` // 空对象 {}
	Raw         *string       `parser:"| @Ident"`       // 处理无引号字符串

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

// preprocess 将基于缩进的格式转换为基于大括号的格式
func preprocess(input string) string {
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

	return sb.String()
}

// ---------------------------------------------------------
// 变量处理
// ---------------------------------------------------------

// 变量定义正则: @name "value" 或 @name value（等号可选）
var varDefRegex = regexp.MustCompile(`^\s*@(\w+)\s*=?\s*"?([^"]*)"?\s*$`)

// 变量引用正则: {{name}} 或 {{$ENV_VAR}}
var varRefRegex = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// import 正则: import "filename"
var importRegex = regexp.MustCompile(`^\s*import\s+"([^"]+)"`)

// extractVariables 从输入中提取变量定义
func extractVariables(input string) map[string]string {
	vars := make(map[string]string)
	lines := strings.Split(input, "\n")

	for _, line := range lines {
		if matches := varDefRegex.FindStringSubmatch(line); matches != nil {
			name := matches[1]
			value := matches[2]
			vars[name] = value
		}
	}

	return vars
}

// extractVariablesWithImports 从输入中提取变量，支持 import
func extractVariablesWithImports(input string, basePath string) map[string]string {
	vars := make(map[string]string)
	lines := strings.Split(input, "\n")

	for _, line := range lines {
		// 处理 import
		if matches := importRegex.FindStringSubmatch(line); matches != nil {
			importPath := matches[1]
			// 相对路径处理
			if basePath != "" && !strings.HasPrefix(importPath, "/") {
				importPath = basePath + "/" + importPath
			}
			// 读取导入的文件
			if content, err := os.ReadFile(importPath); err == nil {
				// 递归提取变量（支持嵌套 import）
				importedVars := extractVariablesWithImports(string(content), dirPath(importPath))
				for k, v := range importedVars {
					vars[k] = v
				}
			}
			continue
		}

		// 处理变量定义
		if matches := varDefRegex.FindStringSubmatch(line); matches != nil {
			name := matches[1]
			value := matches[2]
			vars[name] = value
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
	return varRefRegex.ReplaceAllStringFunc(input, func(match string) string {
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
}

// ---------------------------------------------------------
// 自定义 Lexer（支持带连字符的标识符，如 Content-Type）
// ---------------------------------------------------------

var haikuLexer = lexer.MustSimple([]lexer.SimpleRule{
	{Name: "String", Pattern: `"(?:[^"\\]|\\.)*"`},
	{Name: "Float", Pattern: `\d+\.\d+`},
	{Name: "Int", Pattern: `\d+`},
	{Name: "EmptyArray", Pattern: `\[\]`},   // 空数组
	{Name: "EmptyObject", Pattern: `\{\}`},  // 空对象
	{Name: "Ident", Pattern: `[a-zA-Z_][a-zA-Z0-9_-]*`}, // 支持连字符
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

// New 创建一个新的 Haiku 解析器
func New() (*Parser, error) {
	p, err := participle.Build[Config](
		participle.Lexer(haikuLexer),
		participle.Elide("Whitespace"), // 跳过空白
	)
	if err != nil {
		return nil, err
	}
	return &Parser{parser: p}, nil
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