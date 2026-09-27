package sysx

import (
	"fmt"
	"strconv"
	"strings"
)

// JoinPIDs 把进程号拼成“123、456”
func JoinPIDs(pids []int) string {
	s := make([]string, len(pids))
	for i, p := range pids {
		s[i] = strconv.Itoa(p)
	}
	return strings.Join(s, "、")
}

// AliveError 进程未能结束
type AliveError struct{ PIDs []int }

func (e AliveError) Error() string {
	return fmt.Sprintf("进程 %s 未能结束", JoinPIDs(e.PIDs))
}
