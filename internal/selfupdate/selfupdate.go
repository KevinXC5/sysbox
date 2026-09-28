// Package selfupdate 从 GitHub Release 检查并安装 sysbox 的新版本。
//
// 每次推送到 main 分支，CI 都会发布一个新版本，附带各架构的二进制和 checksums.txt。
// 仓库公开时直接下载；私有时依次尝试 GITHUB_TOKEN、GH_TOKEN 环境变量和 gh auth token 取得访问令牌。
package selfupdate

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Release 一个发布版本
type Release struct {
	Tag    string  `json:"tag_name"`
	Assets []Asset `json:"assets"`
}

// Asset 发布附件
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"url"` // API 地址，配合 Accept: application/octet-stream 下载，私有仓库也可用
	Size int64  `json:"size"`
}

// Client GitHub 发布查询与下载
type Client struct {
	Repo    string // owner/name
	APIBase string // 默认 https://api.github.com
	Token   string
	HTTP    *http.Client
}

// NewClient 创建客户端并自动获取访问令牌（没有也可以，公开仓库无需令牌）
func NewClient(repo string) *Client {
	return &Client{
		Repo:    repo,
		APIBase: "https://api.github.com",
		Token:   token(),
		HTTP:    &http.Client{Timeout: 5 * time.Minute},
	}
}

// token 依次读取环境变量和 gh 的登录凭据
func token() string {
	for _, k := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

func (c *Client) request(ctx context.Context, url, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound && c.Token == "" {
			return nil, errors.New("找不到发布版本；私有仓库需要先执行 gh auth login 或设置 GITHUB_TOKEN")
		}
		return nil, fmt.Errorf("GitHub 返回 HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

// Latest 查询最新发布
func (c *Client) Latest(ctx context.Context) (Release, error) {
	var r Release
	resp, err := c.request(ctx, fmt.Sprintf("%s/repos/%s/releases/latest", c.APIBase, c.Repo), "application/vnd.github+json")
	if err != nil {
		return r, err
	}
	defer resp.Body.Close()
	return r, json.NewDecoder(resp.Body).Decode(&r)
}

// AssetName 当前系统与架构对应的发布产物名，Windows 带 .exe 后缀
func AssetName() string { return AssetNameFor(runtime.GOOS, runtime.GOARCH) }

// AssetNameFor 按系统和架构拼出发布产物名。Windows 为 sysbox-windows-<arch>.exe，其余为 sysbox-<os>-<arch>
func AssetNameFor(goos, goarch string) string {
	name := fmt.Sprintf("sysbox-%s-%s", goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// Newer 判断 latest 是否比 current 新；current 为 dev 时总是返回 false
func Newer(latest, current string) bool {
	a, okA := parse(latest)
	b, okB := parse(current)
	if !okA || !okB {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

// parse 解析 v1.2.3 形式的版本号
func parse(v string) ([3]int, bool) {
	var n [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return n, false
	}
	for i, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil {
			return n, false
		}
		n[i] = x
	}
	return n, true
}

// Progress 下载进度回调
type Progress func(done, total int64)

// Install 下载当前架构的二进制，校验后原子替换 exe
func (c *Client) Install(ctx context.Context, rel Release, exe string, progress Progress) error {
	bin, sums := find(rel, AssetName()), find(rel, "checksums.txt")
	if bin == nil {
		return fmt.Errorf("版本 %s 没有 %s", rel.Tag, AssetName())
	}
	if sums == nil {
		return fmt.Errorf("版本 %s 缺少 checksums.txt", rel.Tag)
	}
	want, err := c.checksum(ctx, *sums, bin.Name)
	if err != nil {
		return err
	}

	// 临时文件放在目标同目录，保证最后的 rename 是原子操作
	tmp, err := os.CreateTemp(filepath.Dir(exe), ".sysbox-update-*")
	if err != nil {
		return fmt.Errorf("无法写入 %s：%w", filepath.Dir(exe), err)
	}
	defer os.Remove(tmp.Name())

	resp, err := c.request(ctx, bin.URL, "application/octet-stream")
	if err != nil {
		tmp.Close()
		return err
	}
	defer resp.Body.Close()
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(tmp, h), &progressReader{r: resp.Body, total: bin.Size, fn: progress})
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return errors.New("SHA-256 校验失败，未替换当前版本")
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	return replaceExecutable(tmp.Name(), exe)
}

func find(rel Release, name string) *Asset {
	for i := range rel.Assets {
		if rel.Assets[i].Name == name {
			return &rel.Assets[i]
		}
	}
	return nil
}

// checksum 从 checksums.txt（sha256sum 格式）中读取指定文件的哈希
func (c *Client) checksum(ctx context.Context, sums Asset, name string) (string, error) {
	resp, err := c.request(ctx, sums.URL, "application/octet-stream")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return f[0], nil
		}
	}
	return "", fmt.Errorf("checksums.txt 中没有 %s", name)
}

type progressReader struct {
	r           io.Reader
	done, total int64
	fn          Progress
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if p.fn != nil {
		p.fn(p.done, p.total)
	}
	return n, err
}

// Executable 当前可执行文件的真实路径
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}
