package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/LingHeChen/haiku/ast"
	"github.com/LingHeChen/haiku/eval"
	"github.com/LingHeChen/haiku/parser"
	"github.com/LingHeChen/haiku/request"
)

const version = "0.1.0"

// 输出选项
var (
	outputFile string // -o file.json
	quietMode  bool   // -q / --quiet
	bodyOnly   bool   // --body-only
)

// 输出长度限制
const maxBodyLines = 50

const usage = `haiku - 人类友好的 HTTP 客户端

用法:
  haiku <file.haiku>          执行请求文件
  haiku -p <file.haiku>       只解析，显示 JSON（不发请求）
  haiku -                     从 stdin 读取
  haiku -e '<request>'        执行内联请求
  haiku -h                    显示帮助

选项:
  -o <file>      保存响应到文件
  -q, --quiet    静默模式，只显示状态码和耗时
  --body-only    只输出 body（方便管道处理）

示例:
  # 执行文件
  haiku api/get-users.haiku

  # 只解析不发送
  haiku -p api/get-users.haiku

  # 内联请求
  haiku -e 'get "https://httpbin.org/get"'

  # 保存响应到文件
  haiku api/get-users.haiku -o response.json

  # 只显示状态
  haiku api/get-users.haiku -q

文件格式 (.haiku):
  # 导入其他文件的变量
  import "config.haiku"

  # 变量定义
  @base_url "https://api.example.com"
  @token "Bearer xxx"

  # 请求（使用变量）
  get "$base_url/users"
  headers
    Accept "application/json"
    Authorization "$token"
    X-Home "$env.HOME"
  body
    name John
    age 25
    active true
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

		case "-q", "--quiet":
			quietMode = true
			i++

		case "--body-only":
			bodyOnly = true
			i++

		case "-o":
			if i+1 >= len(args) {
				fatal("错误: -o 需要文件名参数")
			}
			outputFile = args[i+1]
			i += 2

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
	// 使用 v2 AST 架构
	eval.SetImportParser(parser.ParseFile)
	
	program, err := parser.ParseFile(input)
	if err != nil {
		fatal("解析错误: %v", err)
	}

	evaluator := eval.NewEvaluator(eval.WithBasePath(basePath))
	requests, err := evaluator.EvalToRequests(program)
	if err != nil {
		fatal("执行错误: %v", err)
	}

	for i, req := range requests {
		if len(requests) > 1 {
			fmt.Printf("--- Request %d ---\n", i+1)
		}
		
		jsonBytes, _ := json.MarshalIndent(req, "", "  ")
		fmt.Println(string(jsonBytes))
		
		if len(requests) > 1 && i < len(requests)-1 {
			fmt.Println()
		}
	}
}

func execute(input string, basePath string) {
	// 使用 v2 AST 架构
	eval.SetImportParser(parser.ParseFile)
	
	program, err := parser.ParseFile(input)
	if err != nil {
		fatal("解析错误: %v", err)
	}

	// 检查是否包含 parallel for
	hasParallel := containsParallelFor(program)
	
	var lastResp *request.Response
	totalStart := time.Now()
	
	// 并行执行统计
	var parallelStats struct {
		Total   int
		Success int
		Failed  int
		Times   []time.Duration
	}
	var statsMu sync.Mutex

	if hasParallel {
		// 使用带回调的 evaluator 来真正执行请求
		evaluator := eval.NewEvaluator(
			eval.WithBasePath(basePath),
			eval.WithRequestCallback(func(req map[string]interface{}) (map[string]interface{}, error) {
				resp, err := request.Do(req)
				if err != nil {
					statsMu.Lock()
					parallelStats.Failed++
					statsMu.Unlock()
					return nil, err
				}
				
				statsMu.Lock()
				parallelStats.Success++
				parallelStats.Times = append(parallelStats.Times, resp.Duration)
				statsMu.Unlock()
				
				lastResp = resp
				
				// 返回响应体作为下一个请求的 $_ 引用
				if jsonData, err := resp.JSON(); err == nil {
					return jsonData, nil
				}
				return map[string]interface{}{"body": resp.String()}, nil
			}),
		)
		
		_, err = evaluator.Eval(program)
		if err != nil {
			fatal("执行错误: %v", err)
		}
		
		// 显示并行执行统计（每个 parallel for 一份）
		if !quietMode && !bodyOnly {
			all := evaluator.GetAllParallelStats()
			if len(all) > 0 {
				for idx, stats := range all {
					printParallelStats(stats, idx+1, time.Since(totalStart))
				}
			} else if stats := evaluator.GetParallelStats(); stats != nil {
				printParallelStats(stats, 1, time.Since(totalStart))
			}
		}
	} else {
		// 顺序执行（原逻辑）
		evaluator := eval.NewEvaluator(eval.WithBasePath(basePath))
		requests, err := evaluator.EvalToRequests(program)
		if err != nil {
			fatal("执行错误: %v", err)
		}

		for i, mapData := range requests {
			// 如果有多个请求，显示序号（非静默模式）
			if len(requests) > 1 && !quietMode && !bodyOnly {
				fmt.Printf("\033[1m\033[36m[Request %d/%d]\033[0m\n", i+1, len(requests))
			}

			start := time.Now()

			// 发送请求
			resp, err := request.Do(mapData)
			if err != nil {
				fatal("请求错误: %v", err)
			}

			// 输出结果
			printResponse(resp, time.Since(start))

			lastResp = resp
			
			// 如果有多个请求，添加分隔
			if len(requests) > 1 && i < len(requests)-1 && !quietMode && !bodyOnly {
				fmt.Println()
			}
		}

		// 如果有多个请求，显示总耗时
		if len(requests) > 1 && !quietMode && !bodyOnly {
			fmt.Printf("\n\033[2mTotal: %d requests in %v\033[0m\n", len(requests), time.Since(totalStart).Round(time.Millisecond))
		}
	}

	// 保存到文件（只保存最后一个响应）
	if outputFile != "" && lastResp != nil {
		saveToFile(lastResp)
	}
}

// containsParallelFor 检查程序是否包含 parallel for 语句
func containsParallelFor(program *ast.Program) bool {
	for _, stmt := range program.Statements {
		if forStmt, ok := stmt.(*ast.ForStmt); ok {
			if forStmt.Parallel {
				return true
			}
		}
	}
	return false
}

// printParallelStats 打印并行执行统计
func printParallelStats(stats map[string]interface{}, loopIndex int, totalTime time.Duration) {
	// 颜色码
	reset := "\033[0m"
	bold := "\033[1m"
	green := "\033[32m"
	red := "\033[31m"
	cyan := "\033[36m"
	dim := "\033[2m"

	fmt.Println()
	fmt.Printf("%s%s═══ Parallel Execution Stats (loop %d) ═══%s\n", bold, cyan, loopIndex, reset)
	
	total, _ := stats["total"].(int)
	success, _ := stats["success"].(int)
	failed, _ := stats["failed"].(int)
	
	successColor := green
	if success < total {
		successColor = red
	}
	
	fmt.Printf("  Total:    %d requests\n", total)
	fmt.Printf("  Success:  %s%d%s\n", successColor, success, reset)
	if failed > 0 {
		fmt.Printf("  Failed:   %s%d%s\n", red, failed, reset)
	}
	
	if avgTime, ok := stats["avg_time"].(string); ok {
		fmt.Printf("  Avg Time: %s\n", avgTime)
	}
	if minTime, ok := stats["min_time"].(string); ok {
		fmt.Printf("  Min Time: %s\n", minTime)
	}
	if maxTime, ok := stats["max_time"].(string); ok {
		fmt.Printf("  Max Time: %s\n", maxTime)
	}
	
	fmt.Printf("  %sWall Time: %v%s\n", dim, totalTime.Round(time.Millisecond), reset)
	fmt.Printf("%s%s══════════════════════════════════%s\n", bold, cyan, reset)
}

// saveToFile 保存响应到文件
func saveToFile(resp *request.Response) {
	var content []byte
	
	// 尝试格式化 JSON
	if jsonData, err := resp.JSON(); err == nil {
		content, _ = json.MarshalIndent(jsonData, "", "  ")
	} else {
		content = resp.Body
	}
	
	if err := os.WriteFile(outputFile, content, 0644); err != nil {
		fatal("保存文件失败: %v", err)
	}
	
	if !quietMode && !bodyOnly {
		fmt.Printf("\033[2m响应已保存到 %s\033[0m\n", outputFile)
	}
}

func printResponse(resp *request.Response, totalTime time.Duration) {
	// body-only 模式：只输出原始 body
	if bodyOnly {
		if jsonData, err := resp.JSON(); err == nil {
			formatted, _ := json.MarshalIndent(jsonData, "", "  ")
			fmt.Println(string(formatted))
		} else {
			fmt.Println(resp.String())
		}
		return
	}

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

	// quiet 模式：只显示状态行
	if quietMode {
		return
	}

	fmt.Println(dim + strings.Repeat("─", 50) + reset)

	// Headers
	fmt.Printf("%s%sHeaders%s\n", bold, cyan, reset)
	for k, v := range resp.Headers {
		fmt.Printf("  %s%s%s: %s\n", dim, k, reset, v)
	}
	fmt.Println(dim + strings.Repeat("─", 50) + reset)

	// Body
	fmt.Printf("%s%sBody%s\n", bold, cyan, reset)
	
	// 尝试格式化 JSON
	if jsonData, err := resp.JSON(); err == nil {
		body := formatJSONWithLimit(jsonData)
		fmt.Println(body)
	} else {
		body := resp.String()
		lines := strings.Split(body, "\n")
		if len(lines) > maxBodyLines {
			fmt.Println(strings.Join(lines[:maxBodyLines], "\n"))
			fmt.Printf("%s... (%d more lines, use -o to save full response)%s\n", dim, len(lines)-maxBodyLines, reset)
		} else {
			fmt.Println(body)
		}
	}
}

// formatJSONWithLimit 格式化 JSON，如果太长则只显示顶层结构
func formatJSONWithLimit(data map[string]interface{}) string {
	// 先尝试完整格式化
	formatted, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", data)
	}
	
	lines := strings.Split(string(formatted), "\n")
	
	// 如果行数在限制内，返回完整内容
	if len(lines) <= maxBodyLines {
		return string(formatted)
	}
	
	// 太长了，只显示顶层结构
	summary := make(map[string]interface{})
	for k, v := range data {
		switch val := v.(type) {
		case map[string]interface{}:
			summary[k] = fmt.Sprintf("{...} (%d keys)", len(val))
		case []interface{}:
			summary[k] = fmt.Sprintf("[...] (%d items)", len(val))
		case string:
			if len(val) > 100 {
				summary[k] = val[:100] + "..."
			} else {
				summary[k] = val
			}
		default:
			summary[k] = v
		}
	}
	
	summaryJSON, _ := json.MarshalIndent(summary, "", "  ")
	return string(summaryJSON) + "\n\033[2m... (response too long, use -o to save full response)\033[0m"
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "\033[31m"+format+"\033[0m\n", args...)
	os.Exit(1)
}
