package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/LingHeChen/haiku/ast"
	"github.com/LingHeChen/haiku/eval"
	"github.com/LingHeChen/haiku/parser"
	"github.com/LingHeChen/haiku/request"
)

const version = "0.1.0"

// 输出选项
var (
	outputFile  string // -o file.json
	quietMode   bool   // -q / --quiet
	bodyOnly    bool   // --body-only
	verboseMode bool   // --verbose
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
  --verbose      详细模式，显示请求信息（METHOD URL, Headers, Body）

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

		case "--verbose":
			verboseMode = true
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

	var lastResp *request.Response
	requestCount := 0
	var isParallelRequest bool // 标记当前请求是否来自并行循环
	
	// 使用 channel 进行输出，避免锁阻塞
	type outputMsg struct {
		resp           *request.Response
		req            map[string]interface{}
		duration       time.Duration
		isParallel     bool
		requestNumber  int
	}
	outputChan := make(chan outputMsg, 100) // 缓冲 channel，避免阻塞
	outputDone := make(chan struct{})
	
	// 启动专门的输出 goroutine
	go func() {
		defer close(outputDone)
		for msg := range outputChan {
			if !quietMode && !bodyOnly {
				printResponse(msg.resp, msg.duration, msg.req, msg.isParallel)
				if msg.requestNumber > 1 {
					fmt.Println()
				}
			}
		}
	}()
	
	// 创建 evaluator，带请求回调用于实时执行和输出
	evaluator := eval.NewEvaluator(
		eval.WithBasePath(basePath),
		eval.WithRequestCallback(func(req map[string]interface{}) (map[string]interface{}, error) {
			requestCount++
			start := time.Now()
			
			// 执行请求
			resp, err := request.Do(req)
			if err != nil {
				return nil, err
			}
			
			// 通过 channel 发送输出消息，非阻塞
			select {
			case outputChan <- outputMsg{
				resp:          resp,
				req:           req,
				duration:      time.Since(start),
				isParallel:    isParallelRequest,
				requestNumber: requestCount,
			}:
			default:
				// Channel 满了，直接输出（不应该发生，但作为 fallback）
				if !quietMode && !bodyOnly {
					printResponse(resp, time.Since(start), req, isParallelRequest)
				}
			}
			
			lastResp = resp
			
			// 返回响应体作为下一个请求的 $_ 引用
			if jsonData, err := resp.JSON(); err == nil {
				return jsonData, nil
			}
			return map[string]interface{}{"body": resp.String()}, nil
		}),
	)
	
	// 按语句顺序执行
	for _, stmt := range program.Statements {
		switch s := stmt.(type) {
		case *ast.ImportStmt:
			if err := evaluator.EvalImport(s); err != nil {
				fatal("执行错误: %v", err)
			}
		case *ast.VarDefStmt:
			if err := evaluator.EvalVarDef(s); err != nil {
				fatal("执行错误: %v", err)
			}
		case *ast.RequestStmt:
			// 普通请求：立即执行（已在回调中输出）
			req, err := evaluator.EvalRequest(s)
			if err != nil {
				fatal("请求错误: %v", err)
			}
			if req != nil && evaluator.GetRequestCallback() != nil {
				resp, err := evaluator.GetRequestCallback()(req)
				if err != nil {
					fatal("请求错误: %v", err)
				}
				// Update prevResponse for chaining
				if resp != nil {
					evaluator.SetPrevResponse(resp)
				}
			}
		case *ast.ForStmt:
			if s.Parallel {
				// 并行循环：并发执行，每个请求完成后实时输出
				isParallelRequest = true
				if err := evaluator.EvalParallelForWithOutput(s); err != nil {
					fatal("执行错误: %v", err)
				}
				isParallelRequest = false
			} else {
				// 普通循环：顺序执行（已在回调中输出）
				isParallelRequest = false
				if err := evaluator.EvalForCollect(s); err != nil {
					fatal("执行错误: %v", err)
				}
			}
		case *ast.IfStmt:
			if err := evaluator.EvalIf(s); err != nil {
				fatal("执行错误: %v", err)
			}
		case *ast.EchoStmt:
			if err := evaluator.EvalEcho(s); err != nil {
				fatal("执行错误: %v", err)
			}
		case *ast.SeparatorStmt:
			// 分隔符：跳过
		}
	}
	
	// 关闭输出 channel，等待所有输出完成
	close(outputChan)
	<-outputDone
	
	// 显示并行执行统计（如果有）
	if !quietMode && !bodyOnly {
		all := evaluator.GetAllParallelStats()
		if len(all) > 0 {
			for idx, stats := range all {
				printParallelStats(stats, idx+1)
			}
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
func printParallelStats(stats map[string]interface{}, loopIndex int) {
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
	
	// Use wall_time from stats if available, otherwise fallback
	if wallTime, ok := stats["wall_time"].(string); ok {
		fmt.Printf("  %sWall Time: %s%s\n", dim, wallTime, reset)
	}
	
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

func printResponse(resp *request.Response, totalTime time.Duration, req map[string]interface{}, isParallel bool) {
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
	magenta := "\033[35m"

	// 并行请求简化输出（除非 verbose 模式）
	if isParallel && !verboseMode {
		// 状态颜色
		statusColor := green
		if resp.StatusCode >= 400 {
			statusColor = red
		} else if resp.StatusCode >= 300 {
			statusColor = yellow
		}
		// 只显示状态码和耗时
		fmt.Printf("%s%s%s %s(%v)%s\n", 
			statusColor, resp.Status, reset,
			dim, resp.Duration.Round(time.Millisecond), reset)
		return
	}

	// verbose 模式：显示请求信息
	if verboseMode && req != nil {
		// 提取 METHOD 和 URL
		var method, url string
		for k, v := range req {
			if k != "headers" && k != "body" {
				method = strings.ToUpper(k)
				if str, ok := v.(string); ok {
					url = str
				} else {
					url = fmt.Sprintf("%v", v)
				}
				break
			}
		}
		
		if method != "" && url != "" {
			fmt.Printf("%s%s%s %s%s%s\n", bold, magenta, method, reset, url, reset)
		}
		
		// Request Headers
		if headers, ok := req["headers"].(map[string]interface{}); ok && len(headers) > 0 {
			fmt.Printf("%s%sRequest Headers%s\n", bold, cyan, reset)
			for k, v := range headers {
				fmt.Printf("  %s%s%s: %s\n", dim, k, reset, fmt.Sprintf("%v", v))
			}
		}
		
		// Request Body
		if body, ok := req["body"]; ok && body != nil {
			fmt.Printf("%s%sRequest Body%s\n", bold, cyan, reset)
			bodyStr := formatRequestBody(body)
			fmt.Println(bodyStr)
		}
		
		fmt.Println(dim + strings.Repeat("─", 50) + reset)
	}

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

	// Response Body - 检查是否为空
	bodyStr := resp.String()
	bodyIsEmpty := len(strings.TrimSpace(bodyStr)) == 0
	
	if !bodyIsEmpty {
		fmt.Printf("%s%sResponse Body%s\n", bold, cyan, reset)
		
		// 尝试格式化 JSON
		if jsonData, err := resp.JSON(); err == nil {
			body := formatJSONWithLimit(jsonData)
			fmt.Println(body)
		} else {
			lines := strings.Split(bodyStr, "\n")
			if len(lines) > maxBodyLines {
				fmt.Println(strings.Join(lines[:maxBodyLines], "\n"))
				fmt.Printf("%s... (%d more lines, use -o to save full response)%s\n", dim, len(lines)-maxBodyLines, reset)
			} else {
				fmt.Println(bodyStr)
			}
		}
	}
}

// formatRequestBody 格式化请求体用于显示
func formatRequestBody(body interface{}) string {
	// 尝试格式化为 JSON
	if bodyMap, ok := body.(map[string]interface{}); ok {
		formatted, err := json.MarshalIndent(bodyMap, "", "  ")
		if err == nil {
			return string(formatted)
		}
	}
	if bodySlice, ok := body.([]interface{}); ok {
		formatted, err := json.MarshalIndent(bodySlice, "", "  ")
		if err == nil {
			return string(formatted)
		}
	}
	// 其他类型直接转字符串
	return fmt.Sprintf("%v", body)
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
