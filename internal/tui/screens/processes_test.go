package screens

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/KevinXC5/sysbox/internal/tools/processes"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type fakeProcessProvider struct {
	ended []processes.Process
	force bool
}

func (f *fakeProcessProvider) Snapshot(context.Context) (processes.Snapshot, error) {
	return processes.Snapshot{}, nil
}
func (f *fakeProcessProvider) Details(context.Context, processes.Process) (processes.Details, error) {
	return processes.Details{}, nil
}
func (f *fakeProcessProvider) Terminate(_ context.Context, p processes.Process, force bool) error {
	f.ended = append(f.ended, p)
	f.force = force
	return nil
}
func processKey(key string) tea.KeyMsg {
	switch key {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}
func processFixtures() []processes.Process {
	return []processes.Process{{PID: 10, Started: "start-a", Path: "/a", Name: "node", CPU: 20}, {PID: 20, Started: "start-b", Path: "/b", Name: "java", CPU: 10}}
}
func TestProcessRefreshPreservesIdentityAndScroll(t *testing.T) {
	p := newProcessPage(Env{}, &fakeProcessProvider{})
	defer p.Close()
	items := processFixtures()
	p.Update(processSnapshotMsg{snapshot: processes.Snapshot{Processes: items}})
	p.cursor = 1
	p.Update(processKey("enter"))
	p.detailOffsets[0] = 8
	items[1].CPU = 90
	p.Update(processSnapshotMsg{snapshot: processes.Snapshot{Processes: items}})
	current, _ := p.current()
	if current.PID != 20 || p.detailOffsets[0] != 8 {
		t.Fatalf("刷新改变了选中目标或滚动位置：%+v %d", current, p.detailOffsets[0])
	}
	p.Update(processDetailsMsg{identity: processes.Identity(items[0]), details: processes.Details{Warning: "旧目标结果"}})
	if p.details.Warning != "" {
		t.Fatal("旧目标详情覆盖当前进程")
	}
}
func TestProcessConfirmationKeepsOriginalTarget(t *testing.T) {
	fake := &fakeProcessProvider{}
	p := newProcessPage(Env{}, fake)
	defer p.Close()
	items := processFixtures()
	p.Update(processSnapshotMsg{snapshot: processes.Snapshot{Processes: items}})
	p.Update(processKey("down"))
	p.Update(processKey("D"))
	if len(fake.ended) != 0 || !p.Typing() {
		t.Fatal("操作必须先确认")
	}
	// 刷新发生排序变化，确认仍绑定原来的身份。
	items[1].CPU = 90
	p.Update(processSnapshotMsg{snapshot: processes.Snapshot{Processes: items}})
	cmd := p.Update(processKey("y"))
	result := cmd()
	p.Update(result)
	if len(fake.ended) != 1 || fake.ended[0].PID != 10 || !fake.force {
		t.Fatalf("确认目标发生变化：%+v", fake)
	}
}
func TestProcessDryRunDoesNotTerminate(t *testing.T) {
	fake := &fakeProcessProvider{}
	p := newProcessPage(Env{DryRun: true}, fake)
	defer p.Close()
	p.Update(processSnapshotMsg{snapshot: processes.Snapshot{Processes: processFixtures()}})
	p.Update(processKey("down"))
	p.Update(processKey("d"))
	cmd := p.Update(processKey("y"))
	p.Update(cmd())
	if len(fake.ended) != 0 || !strings.Contains(p.note, "演练") {
		t.Fatal("演练执行了真实结束操作")
	}
}
func TestProcessTimerAndExitedSelection(t *testing.T) {
	p := newProcessPage(Env{}, &fakeProcessProvider{})
	defer p.Close()
	p.Update(processSnapshotMsg{snapshot: processes.Snapshot{Processes: processFixtures()}})
	generation := p.timerGeneration
	p.Update(processKey("down"))
	p.Update(processKey("space"))
	p.Update(processKey("p"))
	if p.Update(processTickMsg{generation}) != nil {
		t.Fatal("暂停后旧计时器触发刷新")
	}
	p.Update(processSnapshotMsg{snapshot: processes.Snapshot{Processes: []processes.Process{{PID: 10, Started: "reused", Path: "/a", Name: "node"}}}})
	if len(p.selected) != 0 {
		t.Fatal("PID 重用后仍保留旧的多选目标")
	}
	if p.interval != 2*time.Second {
		t.Fatal("默认刷新间隔应为2秒")
	}
}
func TestProcessBodyFitsAndShowsEnvironmentWarning(t *testing.T) {
	p := newProcessPage(Env{}, &fakeProcessProvider{})
	defer p.Close()
	p.Update(processSnapshotMsg{snapshot: processes.Snapshot{Processes: processFixtures()}})
	p.Update(processKey("down"))
	p.Update(processKey("enter"))
	p.detailLoading = false
	p.details = processes.Details{Warning: "权限不足，无法读取"}
	for _, size := range [][2]int{{120, 30}, {80, 24}, {60, 18}} {
		body := p.Body(size[0], size[1])
		if lipgloss.Width(body) > size[0] || lipgloss.Height(body) > size[1] {
			t.Fatalf("界面溢出 %v：%dx%d", size, lipgloss.Width(body), lipgloss.Height(body))
		}
	}
	if !strings.Contains(p.Body(120, 30), "权限不足") {
		t.Fatal("没有显示环境读取限制")
	}
}

func TestProcessTableHasNoInitialSelection(t *testing.T) {
	p := newProcessPage(Env{}, &fakeProcessProvider{})
	defer p.Close()
	items := processFixtures()
	p.Update(processSnapshotMsg{snapshot: processes.Snapshot{Processes: items}})
	if _, ok := p.current(); ok || p.cursor != -1 || p.sortKey != 0 || !p.descending {
		t.Fatal("进入页面应按CPU降序显示且没有选中目标")
	}
	body := p.Body(120, 30)
	if strings.Contains(body, "▌") || strings.Contains(body, "基本信息") {
		t.Fatal("表格默认不应有高亮或详情面板")
	}
	items[1].CPU = 90
	p.Update(processSnapshotMsg{snapshot: processes.Snapshot{Processes: items}})
	if p.cursor != -1 || p.items[0].PID != 20 {
		t.Fatal("刷新应保持无选择状态并更新排序")
	}
	p.Update(processKey("down"))
	if p.cursor != 0 {
		t.Fatal("主动方向键才进入选择")
	}
	items[0].CPU = 100
	p.Update(processSnapshotMsg{snapshot: processes.Snapshot{Processes: items}})
	if p.cursor != 0 {
		t.Fatal("高亮行不能跟随CPU排序到处移动")
	}
	p.Update(processKey("enter"))
	if p.modal == nil || p.modal.PID != 10 {
		t.Fatal("回车应打开当前进程弹窗")
	}
	if lipgloss.Height(p.Body(120, 30)) != 30 {
		t.Fatal("弹窗应充分利用主体高度")
	}
	p.Update(processKey("esc"))
	if p.modal != nil || p.cursor != -1 {
		t.Fatal("关闭弹窗应回到无高亮表格")
	}
}

func TestProcessDetailsShowsAllColumnsAndMasksSecrets(t *testing.T) {
	p := newProcessPage(Env{}, &fakeProcessProvider{})
	defer p.Close()
	items := processFixtures()
	items[0].Ports = []processes.Port{{Protocol: "TCP", Local: "127.0.0.1:8080", Number: 8080, State: "LISTEN"}}
	p.Update(processSnapshotMsg{snapshot: processes.Snapshot{Processes: items}})
	p.Update(processKey("down"))
	p.Update(processKey("enter"))
	p.detailLoading = false
	p.details = processes.Details{Environment: map[string]string{"HTTP_PROXY": "http://localhost:7890", "ACCESS_TOKEN": "private-value"}}
	body := p.Body(120, 30)
	for _, value := range []string{"基本信息", "端口与连接", "环境变量", "名称：node", "127.0.0.1:8080", "HTTP_PROXY=", "ACCESS_TOKEN=***"} {
		if !strings.Contains(body, value) {
			t.Fatalf("详情未同时显示 %q", value)
		}
	}
	if strings.Contains(body, "private-value") || strings.Contains(body, "Tab") {
		t.Fatal("默认不能暴露凭据或要求切换Tab")
	}
	p.Update(processKey("v"))
	if !strings.Contains(p.Body(120, 30), "private-value") {
		t.Fatal("三列布局中仍应支持主动展开凭据")
	}
}

func TestProcessDetailsScrollsOnlyFocusedColumn(t *testing.T) {
	p := newProcessPage(Env{}, &fakeProcessProvider{})
	defer p.Close()
	items := processFixtures()
	items[0].Command = strings.Repeat("long-command-argument ", 120)
	for i := 0; i < 40; i++ {
		items[0].Ports = append(items[0].Ports, processes.Port{Protocol: "TCP", Local: "127.0.0.1:8080", State: "LISTEN"})
	}
	p.Update(processSnapshotMsg{snapshot: processes.Snapshot{Processes: items}})
	p.Update(processKey("down"))
	p.Update(processKey("enter"))
	p.detailLoading = false
	p.details.Environment = map[string]string{"PATH": strings.Repeat("/long/path:", 120)}
	p.Body(120, 18)
	if p.detailFocus != 0 {
		t.Fatal("打开详情应默认高亮基本信息")
	}
	for i := 0; i < 3; i++ {
		p.Update(processKey("down"))
	}
	p.Body(120, 18)
	if p.detailOffsets != [3]int{3, 0, 0} {
		t.Fatalf("滚动基本信息影响其他列：%v", p.detailOffsets)
	}
	p.Update(processKey("right"))
	p.Update(processKey("down"))
	p.Body(120, 18)
	if p.detailFocus != 1 || p.detailOffsets != [3]int{3, 1, 0} {
		t.Fatalf("端口列未独立滚动：%v", p.detailOffsets)
	}
	p.Update(processKey("right"))
	p.Update(processKey("down"))
	p.Body(120, 18)
	if p.detailFocus != 2 || p.detailOffsets != [3]int{3, 1, 1} {
		t.Fatalf("环境列未独立滚动：%v", p.detailOffsets)
	}
	p.Update(processKey("left"))
	p.Update(processKey("left"))
	p.Update(processSnapshotMsg{snapshot: processes.Snapshot{Processes: items}})
	p.Body(120, 18)
	if p.detailFocus != 0 || p.detailOffsets != [3]int{3, 1, 1} {
		t.Fatal("切回列或自动刷新丢失滚动位置")
	}
}
