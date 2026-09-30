// sysbox 跨平台系统维护工具箱
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KevinXC5/sysbox/internal/config"
	"github.com/KevinXC5/sysbox/internal/fsx"
	"github.com/KevinXC5/sysbox/internal/meta"
	"github.com/KevinXC5/sysbox/internal/selfupdate"
	"github.com/KevinXC5/sysbox/internal/tui"
	"github.com/KevinXC5/sysbox/internal/tui/screens"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
	"github.com/KevinXC5/sysbox/internal/uninstall"
)

const usage = `sysbox —— 系统维护工具箱

用法：
  sysbox [选项]            打开界面
  sysbox [选项] <工具>     直接打开某个工具，工具名见 sysbox list
  sysbox update            升级到最新版本
  sysbox uninstall         卸载 sysbox，默认保留配置文件
      --purge              同时删除配置目录
      -y                   跳过确认
  sysbox list              列出全部工具
  sysbox version           显示版本号

选项：
  --dry-run                演练模式：完整走一遍流程，但不做任何修改
  --theme auto|light|dark  主题模式，默认读取配置，未配置时为 auto

配置文件：~/.config/sysbox/config.json
`

func main() {
	selfupdate.CleanupOld()
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	dryRun := flag.Bool("dry-run", false, "")
	themeFlag := flag.String("theme", "", "")
	version := flag.Bool("version", false, "")
	flag.Parse()

	if *version {
		fmt.Println(meta.Name, meta.Version)
		return
	}
	switch cmd := flag.Arg(0); cmd {
	case "version":
		fmt.Println(meta.Name, meta.Version)
	case "help":
		fmt.Print(usage)
	case "list":
		for _, t := range tui.Tools() {
			fmt.Printf("  %-10s %s · %s\n", t.ID, t.Group, t.Name)
		}
	case "update":
		os.Exit(runUpdate())
	case "uninstall":
		os.Exit(runUninstall(flag.Args()[1:], *dryRun))
	default:
		if cmd != "" {
			if _, ok := tui.Find(cmd); !ok {
				fmt.Fprintf(os.Stderr, "sysbox：未知的工具或命令 %q，可用工具见 sysbox list\n", cmd)
				os.Exit(2)
			}
		}
		os.Exit(runTUI(cmd, *dryRun, *themeFlag))
	}
}

// runTUI 启动界面；升级完成后按需用新版本原地重启
func runTUI(start string, dryRun bool, themeFlag string) int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "sysbox：配置文件格式有误，已按默认配置启动：", err)
	}
	mode, err := resolveTheme(themeFlag, cfg.Theme)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sysbox：", err)
		return 2
	}
	// 必须在 Bubble Tea 接管终端之前完成，自动模式要查询终端背景色
	theme.Setup(mode)

	app := tui.New(screens.Env{DryRun: dryRun, Config: cfg}, start)
	if _, err := tea.NewProgram(app, tea.WithAltScreen(), tea.WithFPS(tui.FPS)).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "sysbox：", err)
		return 1
	}
	if app.Restart() {
		exe, err := selfupdate.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, "sysbox：更新已完成，但无法定位重启程序，请手动重新运行 sysbox：", err)
			return 1
		}
		// Unix 上 Exec 成功不会返回；Windows 上等待新版本退出后才把终端交还给 shell。
		if err := selfupdate.Restart(exe, os.Args, os.Environ()); err != nil {
			fmt.Fprintln(os.Stderr, "sysbox：重启或运行新版本失败，请手动重新运行 sysbox：", err)
			return 1
		}
	}
	return 0
}

// resolveTheme 主题优先级：命令行参数 > 环境变量 SYSBOX_THEME > 配置文件 > 自动
func resolveTheme(flagVal, cfgVal string) (theme.Mode, error) {
	if flagVal != "" {
		return theme.ParseMode(flagVal)
	}
	if env := os.Getenv("SYSBOX_THEME"); env != "" {
		return theme.ParseMode(env)
	}
	if m, err := theme.ParseMode(cfgVal); cfgVal != "" && err == nil {
		return m, nil
	}
	return theme.Auto, nil
}

// runUpdate 命令行方式升级，供脚本和没有界面的场景使用
func runUpdate() int {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	c := selfupdate.NewClient(meta.Repo)
	pending, err := c.Pending(ctx, meta.Version)
	if err != nil {
		fmt.Fprintln(os.Stderr, "查询新版本失败：", err)
		return 1
	}
	var rel selfupdate.Release
	switch {
	case len(pending) > 0:
		rel = pending[0]
	case meta.IsDev():
		// 本地构建没有可比较的版本号，直接安装最新发布
		if rel, err = c.Latest(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "查询最新版本失败：", err)
			return 1
		}
		pending = []selfupdate.Release{rel}
	default:
		fmt.Printf("已是最新版本 %s\n", meta.Version)
		return 0
	}
	exe, err := selfupdate.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "无法定位当前可执行文件：", err)
		return 1
	}
	fmt.Printf("升级 %s → %s\n\n更新内容：\n", meta.Version, rel.Tag)
	for _, r := range pending {
		fmt.Printf("\n%s\n", r.Tag)
		for _, l := range selfupdate.Notes(r.Body) {
			if l != "" {
				l = "  " + l
			}
			fmt.Println(l)
		}
	}
	fmt.Println()
	last := time.Time{}
	err = c.Install(ctx, rel, exe, func(done, total int64) {
		if time.Since(last) > 200*time.Millisecond || done == total {
			fmt.Printf("\r  下载 %s / %s", fsx.FormatBytes(done), fsx.FormatBytes(total))
			last = time.Now()
		}
	})
	fmt.Println()
	if err != nil {
		fmt.Fprintln(os.Stderr, "升级失败：", err)
		return 1
	}
	fmt.Printf("已升级到 %s\n", rel.Tag)
	return 0
}

// runUninstall 卸载 sysbox：列出将要执行的操作，确认后删除程序、撤销安装时写入的 PATH，
// 配置目录默认保留，加 --purge 一并删除
func runUninstall(args []string, dryRun bool) int {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	purge := fs.Bool("purge", false, "")
	yes := fs.Bool("y", false, "")
	fs.BoolVar(yes, "yes", false, "")
	fs.BoolVar(&dryRun, "dry-run", dryRun, "")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	exe, err := selfupdate.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "无法定位当前可执行文件：", err)
		return 1
	}
	plan, err := uninstall.NewPlan(exe, *purge)
	if err != nil {
		fmt.Fprintln(os.Stderr, "无法确定配置目录：", err)
		return 1
	}
	fmt.Println("卸载 sysbox 将会：")
	for _, item := range plan.Items() {
		fmt.Println("  " + item)
	}
	if !*purge {
		if dir, err := config.Dir(); err == nil {
			if _, err := os.Stat(dir); err == nil {
				fmt.Printf("\n保留配置目录 %s，加 --purge 可一并删除\n", dir)
			}
		}
	}
	if dryRun {
		fmt.Println("\n演练模式：未做任何修改")
		return 0
	}
	if !*yes && !confirm("\n确认卸载？[y/N] ") {
		fmt.Println("已取消")
		return 1
	}
	if err := plan.Run(); errors.Is(err, uninstall.ErrRemovalPending) {
		// 正在运行的 exe 只能等本进程退出后删除，结果由后台进程输出
		fmt.Println("配置与 PATH 已处理；程序文件将在本进程退出后删除，请留意随后输出的删除结果")
		fmt.Println("已打开的终端仍保留旧的 PATH，重新打开后生效")
		return 0
	} else if err != nil {
		fmt.Fprintln(os.Stderr, "卸载失败：", err)
		return 1
	}
	fmt.Println("已卸载 sysbox")
	if runtime.GOOS == "windows" {
		fmt.Println("已打开的终端仍保留旧的 PATH，重新打开后生效")
	}
	return 0
}

// confirm 读取一行回答，只有 y / yes 视为同意；读不到输入（如管道已关闭）时视为拒绝
func confirm(prompt string) bool {
	fmt.Print(prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}
