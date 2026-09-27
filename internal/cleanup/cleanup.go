// Package cleanup 定义“扫描 → 分类 → 勾选 → 删除”类清理工具的通用模型。
// 各清理工具实现 Source 接口，界面层用同一套页面展示和执行。
package cleanup

// Tone 分类的语义色调，界面据此选择颜色
type Tone int

const (
	ToneGood   Tone = iota // 可清理
	ToneWarn               // 需要注意，如删除后需重新下载
	ToneInfo               // 保留、仅展示
	ToneMuted              // 未识别、未覆盖
	ToneDanger             // 异常、检查失败
)

// Category 条目分类
type Category struct {
	Label   string
	Sub     string // 概览卡片上的副标题
	Tone    Tone
	Primary bool // 主分类：卡片显示已勾选的大小与数量
}

// Item 扫描出的一个条目
type Item struct {
	Path         string
	Name         string
	Group        string // 所属分组，如 IDE 目录名、agent 名称
	Category     int    // Source.Categories() 的下标
	Note         string // 条目说明
	IsDir        bool
	Selectable   bool // 能否勾选删除
	Selected     bool // 默认是否勾选
	Irreversible bool // 删除后无法恢复，确认时额外警示
	Size         int64
	Sized        bool // 扫描时已统计大小，界面无需再统计
	Ref          any  // 数据源私有数据，删除前复核用
}

// Root 完成页上对比清理前后大小的根目录
type Root struct {
	Label     string
	Path      string
	Untouched bool // 只展示大小，本工具不会改动
}

// Notice 删除前检查的结果
type Notice struct {
	Blocking bool // 为真时禁止继续删除
	Title    string
	Lines    []string
}

// Source 一个清理数据源
type Source interface {
	// Title 面包屑上的名称
	Title() string
	// Categories 全部分类，顺序即界面标签页顺序
	Categories() []Category
	// Scan 扫描并分类，progress 用于汇报当前阶段
	Scan(progress func(stage string)) ([]Item, error)
	// Roots 完成页对比用的根目录，可为空
	Roots() []Root
	// Check 删除前检查，如相关程序是否在运行；无需提示时返回 nil
	Check() *Notice
	// Remove 删除单个条目，实现方负责路径安全复核
	Remove(it Item) error
	// Notes 确认弹窗里的注意事项
	Notes() []string
	// Tip 完成页底部的提示
	Tip() string
}
