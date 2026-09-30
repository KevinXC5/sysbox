package tui

// 首页的彩色像素图标。每个字母是一个像素，颜色见 theme.PixelArt 的调色板，'.' 为透明。
// 渲染时每两行像素合成一行字符，分类图标 12×8 占 4 行，列表图标 6×4 占 2 行，与条目的两行文字等高。

// 分类图标
var (
	iconClean = []string{ // 扫帚与闪光
		"..c........n",
		".cCc......n.",
		"..c......n..",
		"......rrrr..",
		".....yYYyy..",
		"....yYyyyyo.",
		"...yYyyyyoo.",
		"..y.y.y.y.o.",
	}
	iconTools = []string{ // 工具箱
		"....SSSS....",
		"...S....S...",
		".rrrrrrrrrr.",
		".rRRrrrrrrr.",
		".xxxxYYxxxx.",
		".rrrrYYrrrr.",
		".rrrrrrrrrr.",
		".xxxxxxxxxx.",
	}
	iconSystem = []string{ // 显示器
		".SSSSSSSSSS.",
		".SBBbbbbbbS.",
		".SBbbbbbbcS.",
		".SbbbbbbccS.",
		".SbbbbbcccS.",
		".SSSSSSSSSS.",
		".....SS.....",
		"...SSSSSS...",
	}
	iconSettings = []string{ // 齿轮
		".....ss.....",
		"...sSSSSs...",
		"...SS..SS...",
		"..sS.cc.Ss..",
		"..sS.cc.Ss..",
		"...SS..SS...",
		"...sSSSSs...",
		".....ss.....",
	}
)

// 工具图标
var (
	iconDocker = []string{ // 集装箱与鲸鱼
		".bb.b.",
		"bbbbbb",
		"BBBBbc",
		".cccc.",
	}
	iconKubernetes = []string{ // 舵轮
		".bBBb.",
		"bBwWBb",
		"bBwwBb",
		".bBBb.",
	}
	iconJetBrains = []string{ // 渐变描边的黑色方块与白色短横
		"ppppcc",
		"pKKKKc",
		"rKwwKy",
		"rrryyy",
	}
	iconVSCode = []string{ // 左侧折角与右侧竖条
		"b...bB",
		".bbb.B",
		".bbb.B",
		"b...bB",
	}
	iconAgent = []string{ // 机器人
		"SSSSSS",
		"ScSScS",
		"SSSSSS",
		".s..s.",
	}
	iconPackage = []string{ // 封箱胶带的纸箱
		"yyYYyy",
		"oooooo",
		"yyYYyy",
		"nnnnnn",
	}
	iconCube = []string{ // 立方体：顶面浅、左面蓝、右面青
		".CCCC.",
		"CCCCCC",
		"bbbccc",
		".bbcc.",
	}
	iconDownload = []string{ // 下载箭头与托盘
		"..oo..",
		".oooo.",
		"..oo..",
		"SSSSSS",
	}
)

// 设置图标
var (
	iconTheme = []string{ // 半明半暗的圆
		".YYpp.",
		"YYYppp",
		"YYYppp",
		".YYpp.",
	}
	iconIcons = []string{ // 彩色色块
		"ppccgg",
		"ppccgg",
		"yyrrbb",
		"yyrrbb",
	}
	iconMotion = []string{ // 闪光
		".c..p.",
		"cCc.Pp",
		".c..p.",
		"...p..",
	}
	iconBell = []string{ // 铃铛
		"..yy..",
		".yYyy.",
		"yyyyyy",
		"..oo..",
	}
	iconCalendar = []string{ // 日历，高亮一天
		"rRrrRr",
		"wwwwww",
		"wSwSpw",
		"wwwwww",
	}
	iconHourglass = []string{ // 沙漏
		"SSSSSS",
		"..yy..",
		".yyyy.",
		"SSSSSS",
	}
	iconFolder = []string{ // 文件夹
		"yyy...",
		"yyyyyy",
		"YYYYYY",
		"oooooo",
	}
	iconGlobe = []string{ // 地球
		".bbbb.",
		"bggbgb",
		"bbggbb",
		".bbbb.",
	}
	iconBolt = []string{ // 闪电
		"...Yy.",
		"..YYy.",
		".yyy..",
		".y....",
	}
)
