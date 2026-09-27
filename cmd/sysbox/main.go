// sysbox 个人 macOS 维护工具箱
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KevinXC5/sysbox/internal/config"
	"github.com/KevinXC5/sysbox/internal/fsx"
	"github.com/KevinXC5/sysbox/internal/meta"
	"github.com/KevinXC5/sysbox/internal/selfupdate"
	"github.com/KevinXC5/sysbox/internal/tui"
	"github.com/KevinXC5/sysbox/internal/tui/screens"
	"github.com/KevinXC5/sysbox/internal/ui/theme"
)

const usage = `sysbox —— 个人 macOS 维护工具箱

用法：
  sysbox [选项]            打开界面
  sysbox [选项] <工具>     直接打开某个工具，工具名见 sysbox list
  sysbox update            升级到最新版本
  sysbox list              列出全部工具
  sysbox version           显示版本号

选项：
  --dry-run                演练模式：完整走一遍流程，但不做任何修改
  --theme auto|light|dark  主题模式，默认读取配置，未配置时为 auto

配置文件：~/.config/sysbox/config.json
`

func main() {
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
	if _, err := tea.NewProgram(app, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "sysbox：", err)
		return 1
	}
	if app.Restart() {
		if exe, err := selfupdate.Executable(); err == nil {
			_ = syscall.Exec(exe, os.Args, os.Environ())
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
	rel, err := c.Latest(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "查询最新版本失败：", err)
		return 1
	}
	if !meta.IsDev() && !selfupdate.Newer(rel.Tag, meta.Version) {
		fmt.Printf("已是最新版本 %s\n", meta.Version)
		return 0
	}
	exe, err := selfupdate.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "无法定位当前可执行文件：", err)
		return 1
	}
	fmt.Printf("升级 %s → %s\n", meta.Version, rel.Tag)
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
