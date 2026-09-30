package search

import "testing"

func TestFuzzyQueries(t *testing.T) {
	tests := []struct {
		query  string
		fields []string
		want   bool
	}{
		{"", []string{"nginx"}, true},
		{"NGNX", []string{"nginx:1.29"}, true},
		{"tvhk", []string{"tavily-hikari"}, true},
		{"api prod", []string{"api-server-58f96bd57f", "production"}, true},
		{"api staging", []string{"api-server", "production"}, false},
		{"读挂", []string{"只读目录挂载"}, true},
		{"nxgn", []string{"nginx"}, false},
		{"127 8080", []string{"127.0.0.1:8080"}, true},
		{"   \t", nil, true},
	}
	for _, test := range tests {
		_, ok := Score(test.query, test.fields...)
		if ok != test.want {
			t.Errorf("查询 %q，字段 %q：命中=%v，期望=%v", test.query, test.fields, ok, test.want)
		}
	}
}
func TestExactMatchesRankFirst(t *testing.T) {
	exact, _ := Score("api", "api")
	prefix, _ := Score("api", "api-server")
	fuzzy, _ := Score("api", "application")
	metadata, _ := Score("api", "other-service", "api")
	if exact <= prefix || prefix <= fuzzy || exact <= metadata {
		t.Fatalf("查询排序异常：%d %d %d %d", exact, prefix, fuzzy, metadata)
	}
}
