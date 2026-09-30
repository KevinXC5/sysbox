package containers

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/KevinXC5/sysbox/internal/fsx"
)

// Field 是信息表的一行；Label 为空时，Value 是分组标题。
type Field struct{ Label, Value string }

type dockerHealthcheck struct {
	Test                                          []string
	Interval, Timeout, StartPeriod, StartInterval int64
	Retries                                       int
}
type dockerMount struct {
	Type, Name, Source, Destination, Driver, Mode, Propagation string
	RW                                                         bool
}
type dockerObject struct {
	ID, Name, Image, Created, Architecture, Os, Variant, Author, DockerVersion, Driver, LogPath, Path, Platform string
	Args, RepoTags, RepoDigests                                                                                 []string
	Size, SizeRw, SizeRootFs                                                                                    int64
	RestartCount                                                                                                int
	State                                                                                                       struct {
		Status, StartedAt, FinishedAt, Error string
		ExitCode, Pid                        int
		OOMKilled, Paused, Restarting, Dead  bool
		Health                               struct {
			Status        string
			FailingStreak int
			Log           []struct {
				Start, End string
				ExitCode   int
				Output     string
			}
		}
	}
	Config struct {
		Image, User, WorkingDir, Hostname, Domainname, StopSignal string
		StopTimeout                                               *int
		Entrypoint, Cmd, Env                                      []string
		Labels                                                    map[string]string
		ExposedPorts, Volumes                                     map[string]json.RawMessage
		Healthcheck                                               *dockerHealthcheck
		Tty, OpenStdin                                            bool
	}
	HostConfig struct {
		RestartPolicy struct {
			Name              string
			MaximumRetryCount int
		}
		NetworkMode, Runtime, VolumeDriver, PidMode, IpcMode, UsernsMode, CgroupnsMode           string
		NanoCpus, CpuPeriod, CpuQuota, CpuShares, Memory, MemoryReservation, MemorySwap, ShmSize int64
		CpusetCpus, CpusetMems                                                                   string
		PidsLimit                                                                                *int64
		Privileged, ReadonlyRootfs, AutoRemove, Init                                             bool
		Binds, VolumesFrom, Dns, DnsSearch, DnsOptions, ExtraHosts, CapAdd, CapDrop, SecurityOpt []string
		Tmpfs                                                                                    map[string]string
		LogConfig                                                                                struct {
			Type   string
			Config map[string]string
		}
		PortBindings map[string][]struct{ HostIP, HostPort string }
		Ulimits      []struct {
			Name       string
			Soft, Hard int64
		}
		Devices        []struct{ PathOnHost, PathInContainer, CgroupPermissions string }
		DeviceRequests []struct {
			Driver       string
			Count        int
			DeviceIDs    []string
			Capabilities [][]string
		}
	}
	NetworkSettings struct {
		Ports    map[string][]struct{ HostIP, HostPort string }
		Networks map[string]struct {
			IPAddress, Gateway, MacAddress, GlobalIPv6Address, IPv6Gateway, NetworkID, EndpointID string
			IPPrefixLen, GlobalIPv6PrefixLen                                                      int
			Aliases, DNSNames                                                                     []string
		}
	}
	Mounts []dockerMount
	RootFS struct {
		Type   string
		Layers []string
	}
	Metadata    struct{ LastTagTime string }
	GraphDriver struct {
		Name string
		Data map[string]string
	}
}

func (c *Client) Details(ctx context.Context, kind string, item Item) ([]Field, error) {
	if c.Kubernetes {
		return item.Fields, nil
	}
	args := []string{"container", "inspect", item.ID}
	if kind == "images" {
		args = []string{"image", "inspect", item.ID}
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	objects, err := readDockerObjects(out)
	if err != nil {
		return nil, err
	}
	if len(objects) != 1 {
		return nil, fmt.Errorf("资源已变化，请刷新列表")
	}
	object := objects[0]
	fields := dockerFields(object, kind, item)
	if kind == "images" {
		// 镜像声明卷与实际宿主机挂载分别展示；按镜像 ID 核对，避免同名标签更新后混入旧容器。
		usage, err := c.imageUsage(ctx, object.ID)
		if err != nil {
			fields = append(fields, Field{"关联信息", "读取失败，按 r 重试：" + err.Error()})
		} else {
			for i, field := range fields {
				if field.Label == "" && field.Value == "网络与端口" {
					fields = slices.Insert(fields, i, usage...)
					break
				}
			}
		}
	}
	return fields, nil
}
func readDockerObjects(out string) ([]dockerObject, error) {
	var objects []dockerObject
	if err := json.Unmarshal([]byte(out), &objects); err != nil {
		return nil, fmt.Errorf("Docker 资源信息解析失败：%w", err)
	}
	return objects, nil
}
func decodeDockerDetails(out, kind string, item Item) ([]Field, error) {
	objects, err := readDockerObjects(out)
	if err != nil {
		return nil, err
	}
	if len(objects) != 1 {
		return nil, fmt.Errorf("资源已变化，请刷新列表")
	}
	return dockerFields(objects[0], kind, item), nil
}
func dockerFields(o dockerObject, kind string, item Item) []Field {
	fields := []Field{{"名称", item.Name}}
	if kind == "images" {
		platform := o.Os + "/" + o.Architecture
		if o.Variant != "" {
			platform += "/" + o.Variant
		}
		fields = append(fields, Field{"镜像 ID", o.ID}, Field{"大小", fsx.FormatBytes(o.Size)}, Field{"平台", platform}, Field{"创建时间", readableTime(o.Created)}, Field{"标签", strings.Join(o.RepoTags, "\n")}, Field{"摘要", strings.Join(o.RepoDigests, "\n")}, Field{"作者", o.Author})
		for _, entry := range []struct{ label, key string }{{"标题", "title"}, {"说明", "description"}, {"版本", "version"}, {"来源", "source"}, {"许可证", "licenses"}} {
			if value := o.Config.Labels["org.opencontainers.image."+entry.key]; value != "" {
				fields = append(fields, Field{entry.label, value})
			}
		}
		fields = append(fields, Field{"", "卷与挂载"}, Field{"声明卷", strings.Join(sortedKeys(o.Config.Volumes), "\n")}, Field{"卷说明", "镜像声明数据目录；实际挂载路径见关联容器"})
	} else {
		fields = append(fields, Field{"状态", o.State.Status}, Field{"健康状态", o.State.Health.Status}, Field{"镜像", o.Config.Image}, Field{"容器 ID", o.ID}, Field{"镜像 ID", o.Image}, Field{"创建时间", readableTime(o.Created)}, Field{"启动时间", readableTime(o.State.StartedAt)}, Field{"进程 PID", strconv.Itoa(o.State.Pid)}, Field{"重启次数", strconv.Itoa(o.RestartCount)})
		if o.State.Status != "running" && o.State.Status != "paused" {
			fields = append(fields, Field{"退出码", strconv.Itoa(o.State.ExitCode)}, Field{"停止时间", readableTime(o.State.FinishedAt)})
		}
		if o.State.OOMKilled {
			fields = append(fields, Field{"内存终止", "是"})
		}
		if o.State.Error != "" {
			fields = append(fields, Field{"错误", o.State.Error})
		}
		fields = append(fields, Field{"", "卷与挂载"}, Field{"挂载", mountLines(o.Mounts)}, Field{"声明卷", strings.Join(sortedKeys(o.Config.Volumes), "\n")}, Field{"卷驱动", o.HostConfig.VolumeDriver})
		if len(o.HostConfig.Tmpfs) > 0 {
			fields = append(fields, Field{"临时文件系统", mapLines(o.HostConfig.Tmpfs)})
		}
		if len(o.HostConfig.VolumesFrom) > 0 {
			fields = append(fields, Field{"继承挂载", strings.Join(o.HostConfig.VolumesFrom, "\n")})
		}
	}
	fields = append(fields, Field{"", "网络与端口"}, Field{"声明端口", strings.Join(sortedKeys(o.Config.ExposedPorts), ", ")})
	if kind != "images" {
		var ports, networks []string
		// 停止的容器仍然显示配置的映射；运行时使用实际分配的端口。
		mappings := make(map[string][]struct{ HostIP, HostPort string })
		for port, bindings := range o.HostConfig.PortBindings {
			mappings[port] = bindings
		}
		for port, bindings := range o.NetworkSettings.Ports {
			if len(bindings) > 0 || len(mappings[port]) == 0 {
				mappings[port] = bindings
			}
		}
		for _, port := range sortedKeys(mappings) {
			bindings := mappings[port]
			if len(bindings) == 0 {
				ports = append(ports, port+"（未发布）")
			}
			for _, binding := range bindings {
				ports = append(ports, binding.HostIP+":"+binding.HostPort+" → "+port)
			}
		}
		for _, name := range sortedKeys(o.NetworkSettings.Networks) {
			network := o.NetworkSettings.Networks[name]
			parts := []string{name}
			if network.IPAddress != "" {
				parts = append(parts, "IPv4="+network.IPAddress+"/"+strconv.Itoa(network.IPPrefixLen))
			}
			if network.Gateway != "" {
				parts = append(parts, "网关="+network.Gateway)
			}
			if network.GlobalIPv6Address != "" {
				parts = append(parts, "IPv6="+network.GlobalIPv6Address+"/"+strconv.Itoa(network.GlobalIPv6PrefixLen))
			}
			if network.IPv6Gateway != "" {
				parts = append(parts, "IPv6 网关="+network.IPv6Gateway)
			}
			if network.MacAddress != "" {
				parts = append(parts, "MAC="+network.MacAddress)
			}
			if len(network.Aliases) > 0 {
				parts = append(parts, "别名="+strings.Join(network.Aliases, ", "))
			}
			if len(network.DNSNames) > 0 {
				parts = append(parts, "DNS 名称="+strings.Join(network.DNSNames, ", "))
			}
			networks = append(networks, strings.Join(parts, "\n"))
		}
		fields = append(fields, Field{"网络模式", o.HostConfig.NetworkMode}, Field{"端口", strings.Join(ports, "\n")}, Field{"网络", strings.Join(networks, "\n")}, Field{"DNS", strings.Join(o.HostConfig.Dns, ", ")}, Field{"DNS 搜索域", strings.Join(o.HostConfig.DnsSearch, ", ")}, Field{"DNS 选项", strings.Join(o.HostConfig.DnsOptions, ", ")}, Field{"Hosts", strings.Join(o.HostConfig.ExtraHosts, "\n")})
	}
	// 入口程序与启动参数属于资源运行配置，界面不会展示 sysbox 发出的操作命令。
	fields = append(fields, Field{"", "运行配置"}, Field{"入口程序", strings.Join(o.Config.Entrypoint, "\n")}, Field{"启动参数", strings.Join(o.Config.Cmd, "\n")}, Field{"工作目录", o.Config.WorkingDir}, Field{"运行用户", o.Config.User}, Field{"环境变量", environmentLines(o.Config.Env)}, Field{"停止信号", o.Config.StopSignal})
	if o.Config.StopTimeout != nil {
		fields = append(fields, Field{"停止超时", fmt.Sprintf("%d 秒", *o.Config.StopTimeout)})
	}
	if kind != "images" {
		policy := o.HostConfig.RestartPolicy.Name
		if policy == "on-failure" && o.HostConfig.RestartPolicy.MaximumRetryCount > 0 {
			policy += fmt.Sprintf("（最多 %d 次）", o.HostConfig.RestartPolicy.MaximumRetryCount)
		}
		fields = append(fields, Field{"主机名", o.Config.Hostname}, Field{"域名", o.Config.Domainname}, Field{"重启策略", policy}, Field{"自动删除", yesNo(o.HostConfig.AutoRemove)}, Field{"终端", yesNo(o.Config.Tty)}, Field{"标准输入", yesNo(o.Config.OpenStdin)})
		fields = append(fields, Field{"", "资源与权限"}, Field{"CPU 限制", cpuLimit(o)}, Field{"CPU 权重", numberOrDefault(o.HostConfig.CpuShares)}, Field{"CPU 绑定", o.HostConfig.CpusetCpus}, Field{"内存节点", o.HostConfig.CpusetMems}, Field{"内存限制", bytesOrUnlimited(o.HostConfig.Memory)}, Field{"内存保留", bytesOrUnlimited(o.HostConfig.MemoryReservation)}, Field{"内存与交换", swapLimit(o.HostConfig.MemorySwap)}, Field{"共享内存", fsx.FormatBytes(o.HostConfig.ShmSize)}, Field{"特权模式", yesNo(o.HostConfig.Privileged)}, Field{"只读根目录", yesNo(o.HostConfig.ReadonlyRootfs)}, Field{"运行时", o.HostConfig.Runtime}, Field{"PID 模式", o.HostConfig.PidMode}, Field{"IPC 模式", o.HostConfig.IpcMode}, Field{"用户命名空间", o.HostConfig.UsernsMode}, Field{"Init 进程", yesNo(o.HostConfig.Init)})
		if o.HostConfig.PidsLimit != nil {
			fields = append(fields, Field{"进程上限", numberOrDefault(*o.HostConfig.PidsLimit)})
		}
		if len(o.HostConfig.CapAdd) > 0 {
			fields = append(fields, Field{"新增能力", strings.Join(o.HostConfig.CapAdd, ", ")})
		}
		if len(o.HostConfig.CapDrop) > 0 {
			fields = append(fields, Field{"移除能力", strings.Join(o.HostConfig.CapDrop, ", ")})
		}
		if len(o.HostConfig.SecurityOpt) > 0 {
			fields = append(fields, Field{"安全选项", strings.Join(o.HostConfig.SecurityOpt, "\n")})
		}
		var limits, devices []string
		for _, limit := range o.HostConfig.Ulimits {
			limits = append(limits, fmt.Sprintf("%s：软 %d / 硬 %d", limit.Name, limit.Soft, limit.Hard))
		}
		for _, device := range o.HostConfig.Devices {
			devices = append(devices, device.PathOnHost+" → "+device.PathInContainer+"（"+device.CgroupPermissions+"）")
		}
		for _, request := range o.HostConfig.DeviceRequests {
			devices = append(devices, fmt.Sprintf("驱动=%s，数量=%d，设备=%s，能力=%v", request.Driver, request.Count, strings.Join(request.DeviceIDs, ", "), request.Capabilities))
		}
		if len(limits) > 0 {
			fields = append(fields, Field{"Ulimits", strings.Join(limits, "\n")})
		}
		if len(devices) > 0 {
			fields = append(fields, Field{"设备 / GPU", strings.Join(devices, "\n")})
		}
	}
	if o.Config.Healthcheck != nil || o.State.Health.Status != "" {
		fields = append(fields, Field{"", "健康检查"})
		if health := o.Config.Healthcheck; health != nil {
			fields = append(fields, Field{"检测方式", strings.Join(health.Test, " ")}, Field{"检测间隔", dockerDuration(health.Interval)}, Field{"检测超时", dockerDuration(health.Timeout)}, Field{"启动宽限", dockerDuration(health.StartPeriod)}, Field{"启动间隔", dockerDuration(health.StartInterval)}, Field{"失败阈值", numberOrDefault(int64(health.Retries))})
		}
		if kind != "images" {
			fields = append(fields, Field{"连续失败", strconv.Itoa(o.State.Health.FailingStreak)})
			var checks []string
			for _, check := range o.State.Health.Log {
				checks = append(checks, fmt.Sprintf("%s · 退出码 %d\n%s", readableTime(check.Start), check.ExitCode, strings.TrimSpace(check.Output)))
			}
			if len(checks) > 0 {
				fields = append(fields, Field{"最近检查", strings.Join(checks, "\n")})
			}
		}
	}
	fields = append(fields, Field{"", "日志与存储"})
	if kind == "images" {
		fields = append(fields, Field{"存储类型", o.RootFS.Type}, Field{"层数", strconv.Itoa(len(o.RootFS.Layers))}, Field{"镜像层", strings.Join(o.RootFS.Layers, "\n")}, Field{"最近标签", readableTime(o.Metadata.LastTagTime)}, Field{"构建版本", o.DockerVersion})
	} else {
		fields = append(fields, Field{"日志驱动", o.HostConfig.LogConfig.Type}, Field{"日志选项", mapLines(o.HostConfig.LogConfig.Config)}, Field{"日志路径", o.LogPath}, Field{"存储驱动", o.GraphDriver.Name})
	}
	if len(o.Config.Labels) > 0 {
		fields = append(fields, Field{"", "标签信息"}, Field{"标签信息", mapLines(o.Config.Labels)})
	}
	return fields
}
func (c *Client) imageUsage(ctx context.Context, imageID string) ([]Field, error) {
	out, err := c.run(ctx, "ps", "-a", "--filter", "ancestor="+imageID, "--no-trunc", "--format", "{{.ID}}")
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(out)
	fields := []Field{{"", "关联容器"}}
	if len(ids) == 0 {
		return append(fields, Field{"使用情况", "暂无容器使用此镜像"}), nil
	}
	out, err = c.run(ctx, append([]string{"container", "inspect"}, ids...)...)
	if err != nil {
		return nil, err
	}
	objects, err := readDockerObjects(out)
	if err != nil {
		return nil, err
	}
	found := false
	for _, object := range objects {
		if object.Image != imageID {
			continue
		}
		found = true
		name := strings.TrimPrefix(object.Name, "/")
		fields = append(fields, Field{"容器", name + " · " + object.State.Status}, Field{"容器挂载", name + "\n" + mountLines(object.Mounts)})
	}
	if !found {
		fields = append(fields, Field{"使用情况", "暂无容器使用此镜像"})
	}
	return fields, nil
}
func mountLines(mounts []dockerMount) string {
	var lines []string
	for _, mount := range mounts {
		mode := "只读"
		if mount.RW {
			mode = "读写"
		}
		meta := []string{mount.Type, mode}
		if mount.Name != "" {
			meta = append(meta, "卷="+mount.Name)
		}
		if mount.Driver != "" {
			meta = append(meta, "驱动="+mount.Driver)
		}
		if mount.Mode != "" {
			meta = append(meta, "选项="+mount.Mode)
		}
		if mount.Propagation != "" {
			meta = append(meta, "传播="+mount.Propagation)
		}
		lines = append(lines, mount.Source+" → "+mount.Destination+"\n"+strings.Join(meta, " · "))
	}
	return strings.Join(lines, "\n")
}
func environmentLines(env []string) string {
	lines := append([]string(nil), env...)
	for i, line := range lines {
		key, _, ok := strings.Cut(line, "=")
		upper := strings.ToUpper(key)
		sensitive := strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "PASSWD") || strings.Contains(upper, "SECRET")
		for _, suffix := range []string{"TOKEN", "API_KEY", "ACCESS_KEY", "PRIVATE_KEY", "CREDENTIAL", "AUTHORIZATION"} {
			sensitive = sensitive || strings.HasSuffix(upper, suffix)
		}
		if ok && sensitive {
			lines[i] = key + "=••••••"
		}
	}
	return strings.Join(lines, "\n")
}
func swapLimit(value int64) string {
	if value == 0 {
		return "默认（由引擎决定）"
	}
	return bytesOrUnlimited(value)
}
func cpuLimit(o dockerObject) string {
	if o.HostConfig.NanoCpus > 0 {
		return fmt.Sprintf("%g 核", float64(o.HostConfig.NanoCpus)/1e9)
	}
	if o.HostConfig.CpuQuota > 0 && o.HostConfig.CpuPeriod > 0 {
		return fmt.Sprintf("%g 核", float64(o.HostConfig.CpuQuota)/float64(o.HostConfig.CpuPeriod))
	}
	return "不限"
}
func yesNo(value bool) string {
	if value {
		return "是"
	}
	return "否"
}
func numberOrDefault(value int64) string {
	if value <= 0 {
		return "默认 / 不限"
	}
	return strconv.FormatInt(value, 10)
}
func bytesOrUnlimited(value int64) string {
	if value <= 0 {
		return "不限"
	}
	return fsx.FormatBytes(value)
}
func dockerDuration(value int64) string {
	if value == 0 {
		return "默认"
	}
	return time.Duration(value).String()
}
func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func mapLines(values map[string]string) string {
	var lines []string
	for _, key := range sortedKeys(values) {
		lines = append(lines, key+"="+values[key])
	}
	return strings.Join(lines, "\n")
}
func readableTime(value string) string {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return value
	}
	if parsed.IsZero() || parsed.Year() <= 1 {
		return "—"
	}
	return parsed.Local().Format("2006-01-02 15:04:05")
}
