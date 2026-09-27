package cursorui

import "testing"

func TestParseLsofAndAnalyze(t *testing.T) {
	out := "p1261\nfcwd\ntDIR\nn/\n" +
		"ftxt\ntREG\nn/Applications/Some App.app/Contents/MacOS/Some App\n" +
		"ftxt\ntREG\nn/Users/me/.Trash/Old.app/Contents/Resources/x.png\n" +
		"f5r\ntREG\nn/Library/Input Methods/Squirrel.app/Contents/Info.plist\n" +
		"f9u\ntIPv4\nn127.0.0.1:5000->127.0.0.1:6000\n" +
		"ftxt\ntREG\nn/System/Library/Input Methods/KoreanIM.app/Contents/MacOS/KoreanIM\n"
	files := ParseLsof(out)
	if len(files) != 6 {
		t.Fatalf("期望 6 个文件，实际 %d", len(files))
	}
	if files[1].Name != "/Applications/Some App.app/Contents/MacOS/Some App" {
		t.Errorf("含空格路径解析有误：%q", files[1].Name)
	}

	apps, sysApps, findings, suspects := Analyze(files)
	wantApps := []string{"/Applications/Some App.app", "/Library/Input Methods/Squirrel.app", "/Users/me/.Trash/Old.app"}
	if len(apps) != len(wantApps) {
		t.Fatalf("App 列表有误：%v", apps)
	}
	for i := range wantApps {
		if apps[i] != wantApps[i] {
			t.Errorf("App[%d] = %q，期望 %q", i, apps[i], wantApps[i])
		}
	}
	if sysApps != 1 {
		t.Errorf("系统自带输入法应单独计数：%d", sysApps)
	}
	if len(findings) != 4 || findings[0].Level != LevelWarn {
		t.Errorf("结论有误：%+v", findings)
	}
	if len(suspects) != 3 {
		t.Errorf("可疑明细应为 3 条，实际 %d", len(suspects))
	}
}

func TestAnalyzeClean(t *testing.T) {
	_, _, findings, _ := Analyze([]OpenFile{{Type: "REG", Name: "/usr/lib/dyld"}, {Type: "REG", Name: "/System/Library/Input Methods/TCIM.app/Contents/Info.plist"}})
	if len(findings) != 1 || findings[0].Level != LevelOK {
		t.Errorf("只涉及系统组件时应给出系统自身卡住的结论：%+v", findings)
	}
}
