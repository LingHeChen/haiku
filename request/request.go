// Package request 提供基于 map 配置的 HTTP 请求功能
package request

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Response 表示 HTTP 响应
type Response struct {
	StatusCode int               // HTTP 状态码
	Status     string            // HTTP 状态文本
	Headers    map[string]string // 响应头
	Body       []byte            // 响应体
	Duration   time.Duration     // 请求耗时
}

// String 返回响应体的字符串形式
func (r *Response) String() string {
	return string(r.Body)
}

// JSON 将响应体解析为 map
func (r *Response) JSON() (map[string]interface{}, error) {
	var result map[string]interface{}
	err := json.Unmarshal(r.Body, &result)
	return result, err
}

// Client HTTP 客户端
type Client struct {
	httpClient *http.Client
	timeout    time.Duration
}

// Option 客户端配置选项
type Option func(*Client)

// WithTimeout 设置请求超时时间
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.timeout = timeout
		c.httpClient.Timeout = timeout
	}
}

// New 创建一个新的 HTTP 客户端
func New(opts ...Option) *Client {
	c := &Client{
		httpClient: &http.Client{},
		timeout:    30 * time.Second,
	}
	c.httpClient.Timeout = c.timeout

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Do 根据 mapData 执行 HTTP 请求
func (c *Client) Do(mapData map[string]interface{}) (*Response, error) {
	start := time.Now()

	// 1. 确定 HTTP 方法和 URL
	method, url, err := extractMethodAndURL(mapData)
	if err != nil {
		return nil, err
	}

	// 2. 准备请求体
	bodyReader, err := prepareBody(mapData)
	if err != nil {
		return nil, err
	}

	// 3. 创建请求
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 4. 添加请求头
	applyHeaders(req, mapData)

	// 5. 执行请求
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// 6. 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// 7. 构建响应对象
	headers := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	return &Response{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Headers:    headers,
		Body:       respBody,
		Duration:   time.Since(start),
	}, nil
}

// extractMethodAndURL 从 mapData 中提取 HTTP 方法和 URL
func extractMethodAndURL(mapData map[string]interface{}) (string, string, error) {
	methods := []string{"get", "post", "put", "delete", "patch", "head", "options"}
	for _, m := range methods {
		if v, ok := mapData[m]; ok {
			return strings.ToUpper(m), v.(string), nil
		}
	}
	return "", "", fmt.Errorf("missing HTTP method (get/post/put/delete/patch/head/options)")
}

// prepareBody 准备请求体
func prepareBody(mapData map[string]interface{}) (io.Reader, error) {
	body, ok := mapData["body"]
	if !ok {
		return nil, nil
	}

	switch b := body.(type) {
	case string:
		return strings.NewReader(b), nil
	case map[string]interface{}, []interface{}:
		jsonBytes, err := json.Marshal(b)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal body: %w", err)
		}
		return bytes.NewReader(jsonBytes), nil
	default:
		return nil, fmt.Errorf("unsupported body type: %T", body)
	}
}

// applyHeaders 应用请求头
func applyHeaders(req *http.Request, mapData map[string]interface{}) {
	headers, ok := mapData["headers"].(map[string]interface{})
	if !ok {
		return
	}
	for k, v := range headers {
		req.Header.Set(k, fmt.Sprintf("%v", v))
	}
}

// ---------------------------------------------------------
// 便捷函数（使用默认客户端）
// ---------------------------------------------------------

var defaultClient = New()

// Do 使用默认客户端执行请求
func Do(mapData map[string]interface{}) (*Response, error) {
	return defaultClient.Do(mapData)
}
