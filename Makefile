# 常用开发命令
.PHONY: build cross run dry test lint record install release clean

LDFLAGS := -X github.com/KevinXC5/sysbox/internal/meta.Version=dev

# 编译到 bin/sysbox
build:
	go build -ldflags "$(LDFLAGS)" -o bin/sysbox ./cmd/sysbox

# 交叉编译全部支持的平台（macOS、Windows 的 amd64 与 arm64），与 CI 一致；不支持 Linux
PLATFORMS := darwin/amd64 darwin/arm64 windows/amd64 windows/arm64
cross:
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; echo "编译 $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -o /dev/null ./... || exit 1; \
	done

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

# 给 origin/main 的最新提交打下一个补丁版本标签并推送，触发发布工作流
release:
	@git fetch -q --tags origin main
	@test -z "$$(git status --porcelain)" || { echo "工作区有未提交的改动"; exit 1; }
	@test "$$(git rev-parse HEAD)" = "$$(git rev-parse origin/main)" || { echo "当前提交与 origin/main 不一致，先推送或同步 main"; exit 1; }
	@last=$$(git describe --tags --abbrev=0 --match 'v*' 2>/dev/null || echo v0.1.0); \
	if [ -n "$(V)" ]; then next=$(V); else next=$$(echo $$last | awk -F. '{ printf "%s.%s.%d", $$1, $$2, $$3 + 1 }'); fi; \
	echo "$${last} → $${next}"; \
	git tag -a $$next -m $$next && git push -q origin $$next && \
	echo "已推送 $${next}，发布进度：https://github.com/KevinXC5/sysbox/actions/workflows/release.yml"

clean:
	rm -rf bin dist
