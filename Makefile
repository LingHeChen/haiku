.PHONY: build test clean install

# 构建
build:
	go build -o haiku .

# 测试
test:
	go test ./... -v

# 清理
clean:
	rm -f haiku

# 安装到 $GOPATH/bin
install:
	go install .

# 运行示例
example:
	./haiku -p examples/post.haiku
