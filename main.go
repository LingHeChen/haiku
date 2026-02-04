package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/LingHeChen/haiku/parser"
	"github.com/LingHeChen/haiku/request"
)

const version = "0.1.0"

const usage = `haiku - 人类友好的 HTTP 客户端

用法:
  haiku <file.haiku>          执行请求文件
  haiku -p <file.haiku>       只解析，显示 JSON（不发请求）
  haiku -                     从 stdin 读取
  haiku -e '<request>'        执行内联请求
  haiku -h                    显示帮助

示例:
  # 执行文件
  haiku api/get-users.haiku

  # 只解析不发送
  haiku -p api/get-users.haiku

  # 内联请求
  haiku -e 'get "https://httpbin.org/get"'

文件格式 (.haiku):
  # 导入其他文件的变量
  import "config.haiku"

  # 变量定义
  @base_url "https://api.example.com"
  @token "Bearer xxx"

  # 请求（使用变量）
  get "{{base_url}}/users"
  headers
    Accept "application/json"
    Authorization "{{token}}"
    X-Home "{{$HOME}}"          # 环境变量用 $
  body
    name John
    age 25                      # 自动推断为数字
    active true                 # 自动推断为布尔
    tags
      api
      http
`

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		fmt.Print(usage)
		os.Exit(0)
	}

	var input string
	var basePath string // 用于解析相对 import 路径
	parseOnly := false

	// 处理 flags
	i := 0
	for i < len(args) {
		switch args[i] {
		case "-h", "--help":
			fmt.Print(usage)
			os.Exit(0)

		case "-v", "--version":
			fmt.Printf("haiku version %s\n", version)
			os.Exit(0)

		case "-p", "--parse":
			parseOnly = true
			i++

		case "-e":
			if i+1 >= len(args) {
				fatal("错误: -e 需要参数")
			}
			input = args[i+1]
			basePath = "." // 当前目录
			i += 2

		case "-":
			// 从 stdin 读取
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				fatal("读取 stdin 失败: %v", err)
			}
			input = string(data)
			basePath = "." // 当前目录
			i++

		default:
			// 读取文件
			filename := args[i]
			data, err := os.ReadFile(filename)
			if err != nil {
				fatal("读取文件失败: %v", err)
			}
			input = string(data)
			// 获取文件所在目录作为 basePath
			basePath = dirPath(filename)
			i++
		}
	}

	if input == "" {
		fatal("错误: 没有输入")
	}

	if parseOnly {
		// 只解析，显示 JSON
		showParsed(input, basePath)
	} else {
		// 解析并执行
		execute(input, basePath)
	}
}

// dirPath 获取文件所在目录
func dirPath(filePath string) string {
	lastSlash := strings.LastIndex(filePath, "/")
	if lastSlash == -1 {
		return "."
	}
	return filePath[:lastSlash]
}

func showParsed(input string, basePath string) {
	p, err := parser.New()
	if err != nil {
		fatal("初始化解析器失败: %v", err)
	}

	// 先从整个文件提取变量（确保 import 和变量定义对所有请求可用）
	vars := parser.ExtractVariables(input, basePath)

	// 分割多个请求
	requests := parser.SplitRequests(input)
	
	var prevResponse map[string]interface{}
	
	for i, reqInput := range requests {
		if len(requests) > 1 {
			fmt.Printf("--- Request %d ---\n", i+1)
		}
		
		mapData, err := p.ParseToMapWithVars(reqInput, vars, prevResponse)
		if err != nil {
			fatal("解析错误: %v", err)
		}

		jsonBytes, _ := json.MarshalIndent(mapData, "", "  ")
		fmt.Println(string(jsonBytes))
		
		// 模拟响应（parse-only 模式没有真实响应）
		prevResponse = mapData
		
		if len(requests) > 1 && i < len(requests)-1 {
			fmt.Println()
		}
	}
}

func execute(input string, basePath string) {
	// 解析器
	p, err := parser.New()
	if err != nil {
		fatal("初始化解析器失败: %v", err)
	}

	// 先从整个文件提取变量（确保 import 和变量定义对所有请求可用）
	vars := parser.ExtractVariables(input, basePath)

	// 分割多个请求（用 --- 分隔）
	requests := parser.SplitRequests(input)
	
	var prevResponse map[string]interface{}
	totalStart := time.Now()

	for i, reqInput := range requests {
		// 如果有多个请求，显示序号
		if len(requests) > 1 {
			fmt.Printf("\033[1m\033[36m[Request %d/%d]\033[0m\n", i+1, len(requests))
		}

		start := time.Now()
		
		// 解析，传入全局变量和上一个响应
		mapData, err := p.ParseToMapWithVars(reqInput, vars, prevResponse)
		if err != nil {
			fatal("解析错误: %v", err)
		}

		// 发送请求
		resp, err := request.Do(mapData)
		if err != nil {
			fatal("请求错误: %v", err)
		}

		// 输出结果
		printResponse(resp, time.Since(start))

		// 保存响应用于下一个请求
		prevResponse, _ = resp.JSON()
		
		// 如果有多个请求，添加分隔
		if len(requests) > 1 && i < len(requests)-1 {
			fmt.Println()
		}
	}

	// 如果有多个请求，显示总耗时
	if len(requests) > 1 {
		fmt.Printf("\n\033[2mTotal: %d requests in %v\033[0m\n", len(requests), time.Since(totalStart).Round(time.Millisecond))
	}
}

func printResponse(resp *request.Response, totalTime time.Duration) {
	// 颜色码
	reset := "\033[0m"
	bold := "\033[1m"
	dim := "\033[2m"
	green := "\033[32m"
	red := "\033[31m"
	cyan := "\033[36m"
	yellow := "\033[33m"

	// 状态颜色
	statusColor := green
	if resp.StatusCode >= 400 {
		statusColor = red
	} else if resp.StatusCode >= 300 {
		statusColor = yellow
	}

	// 状态行
	fmt.Printf("%s%s%s %s(%v)%s\n", 
		statusColor, resp.Status, reset,
		dim, resp.Duration.Round(time.Millisecond), reset)
	fmt.Println(dim + strings.Repeat("─", 50) + reset)

	// Headers
	fmt.Printf("%s%sHeaders%s\n", bold, cyan, reset)
	for k, v := range resp.Headers {
		fmt.Printf("  %s%s%s: %s\n", dim, k, reset, v)
	}
	fmt.Println(dim + strings.Repeat("─", 50) + reset)

	// Body
	fmt.Printf("%s%sBody%s\n", bold, cyan, reset)
	body := resp.String()
	
	// 尝试格式化 JSON
	if jsonData, err := resp.JSON(); err == nil {
		if formatted, err := json.MarshalIndent(jsonData, "", "  "); err == nil {
			body = string(formatted)
		}
	}
	fmt.Println(body)
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "\033[31m"+format+"\033[0m\n", args...)
	os.Exit(1)
}
