# 常用开发命令
.PHONY: build run dry test lint record install clean

LDFLAGS := -X github.com/KevinXC5/sysbox/internal/meta.Version=dev

# 编译到 bin/sysbox
build:
	go build -ldflags "$(LDFLAGS)" -o bin/sysbox ./cmd/sysbox

# 运行界面
run: build
	./bin/sysbox

# 演练模式运行，不做任何修改
dry: build
	./bin/sysbox --dry-run

# 格式检查、静态检查与测试
lint:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	go vet ./...

test: lint
	go test -race ./...

# 录制 README 截图
record:
	./scripts/record.sh

# 安装到 ~/.local/bin，覆盖通过 install.sh 安装的版本
install: build
	install -m 755 bin/sysbox $(HOME)/.local/bin/sysbox

clean:
	rm -rf bin dist
