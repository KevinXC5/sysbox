package containers

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/KevinXC5/sysbox/internal/sysx"
)

type recordingRunner struct {
	commands []sysx.Cmd
	output   string
	err      error
}

func (r *recordingRunner) Run(_ context.Context, command sysx.Cmd) (string, error) {
	r.commands = append(r.commands, command)
	return r.output, r.err
}

func TestDockerDecodeAndRemoval(t *testing.T) {
	items, err := decodeDocker(`{"ID":"sha256:abc","Repository":"app","Tag":"dev","Size":"12MB"}
{"ID":"sha256:def","Repository":"<none>","Tag":"<none>"}`, "images")
	if err != nil || len(items) != 2 || items[0].ID != "app:dev" || items[1].ID != "sha256:def" {
		t.Fatalf("镜像标签和无标签镜像解析错误：%+v，%v", items, err)
	}
	c := New(false)
	c.Target = "remote"
	for _, item := range items {
		plan, err := c.Plan("images", item, Action{ID: "rmi", Mutates: true}, "")
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"--context", "remote", "image", "rm", item.ID}
		if !reflect.DeepEqual(plan.Command.Args, want) {
			t.Fatalf("删除应只针对所选标签且不强制：%v", plan.Command.Args)
		}
	}
}
func TestDryRunDoesNotExecuteMutations(t *testing.T) {
	runner := &recordingRunner{output: "成功"}
	c := New(false)
	c.Runner = runner
	for _, action := range []Action{{ID: "stop", Mutates: true}, {ID: "exec", Mutates: true, Interactive: true}} {
		plan, err := c.Plan("containers", Item{ID: "abc"}, action, "/bin/sh")
		if err != nil {
			t.Fatal(err)
		}
		out, err := c.Execute(context.Background(), plan, true)
		if err != nil || !strings.Contains(out, "演练模式") {
			t.Fatalf("演练失败：%s，%v", out, err)
		}
	}
	if len(runner.commands) != 0 {
		t.Fatal("演练不应执行修改命令")
	}
	plan, _ := c.Plan("containers", Item{ID: "abc"}, Action{ID: "logs"}, "")
	_, err := c.Execute(context.Background(), plan, true)
	if err != nil || len(runner.commands) != 1 {
		t.Fatal("演练仍应允许读取日志")
	}
}
func TestKubeNamespacePinnedToResource(t *testing.T) {
	c := New(true)
	c.Target = "production"
	item := Item{Name: "app", Namespace: "payments", State: "3", Containers: []string{"app", "sidecar"}}
	for _, kind := range []string{"deployments", "configmaps", "pods"} {
		for _, action := range c.Actions(kind, item) {
			value := ""
			if action.ID == "scale" {
				value = "4"
			}
			if action.ID == "exec" {
				value = "sidecar"
			}
			plan, err := c.Plan(kind, item, action, value)
			if err != nil {
				t.Fatalf("%s/%s：%v", kind, action.ID, err)
			}
			prefix := []string{"--context", "production", "--request-timeout=30s", "--namespace", "payments"}
			if !reflect.DeepEqual(plan.Command.Args[:len(prefix)], prefix) {
				t.Fatalf("操作未固定集群和命名空间：%v", plan.Command.Args)
			}
		}
	}
	if _, err := c.Plan("pods", Item{Name: "app"}, Action{ID: "delete", Mutates: true}, ""); err == nil {
		t.Fatal("不能对缺少命名空间的资源执行操作")
	}
}
func TestKubeNodeDrainAndScaleValidation(t *testing.T) {
	c := New(true)
	c.Target = "test"
	plan, err := c.Plan("nodes", Item{Name: "worker"}, Action{ID: "drain", Mutates: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range plan.Command.Args {
		if arg == "--force" || arg == "--delete-emptydir-data" || arg == "--disable-eviction" || arg == "--namespace" {
			t.Fatalf("排空节点使用了不合适的参数：%s", arg)
		}
	}
	for _, value := range []string{"-1", "abc", "1; touch file", "1\n2"} {
		if _, err := c.Plan("deployments", Item{Name: "app", Namespace: "default"}, Action{ID: "scale", Prompt: "副本"}, value); err == nil {
			t.Fatalf("应拒绝非法副本数：%q", value)
		}
	}
	if _, err := c.Plan("deployments", Item{Name: "app", Namespace: "default"}, Action{ID: "scale", Prompt: "副本"}, "0"); err != nil {
		t.Fatal(err)
	}
}
func TestDecodeKubeResources(t *testing.T) {
	tests := []struct{ kind, data, name, namespace, column string }{
		{"deployments", `{"items":[{"metadata":{"name":"app","namespace":"team","uid":"u1"},"spec":{"replicas":3,"template":{"spec":{"containers":[{"name":"app","image":"app:v1"}]}}},"status":{"readyReplicas":2,"updatedReplicas":3}}]}`, "app", "team", "2/3 就绪"},
		{"configmaps", `{"items":[{"metadata":{"name":"config","namespace":"team"},"data":{"a":"1"},"binaryData":{"b":"Yg=="}}]}`, "config", "team", "2 个键"},
		{"pods", `{"items":[{"metadata":{"name":"pod","namespace":"team"},"spec":{"containers":[{"name":"app"},{"name":"sidecar"}]},"status":{"phase":"Running","containerStatuses":[{"ready":true,"restartCount":2},{"ready":false,"restartCount":1}]}}]}`, "pod", "team", "Running · 1/2"},
		{"nodes", `{"items":[{"metadata":{"name":"node"},"spec":{"unschedulable":true},"status":{"conditions":[{"type":"Ready","status":"True"}],"nodeInfo":{"kubeletVersion":"v1.34"}}}]}`, "node", "", "Ready · 已禁调度"},
	}
	for _, test := range tests {
		t.Run(test.kind, func(t *testing.T) {
			items, err := decodeKube(test.data, test.kind)
			if err != nil || len(items) != 1 || items[0].Name != test.name || items[0].Namespace != test.namespace || items[0].Columns[0] != test.column {
				t.Fatalf("资源解析错误：%+v，%v", items, err)
			}
		})
	}
}

func TestDockerDetailsAreStructured(t *testing.T) {
	out := `[{"Id":"abc","Created":"2026-09-30T01:00:00Z","RestartCount":2,"State":{"Status":"running","Health":{"Status":"healthy"}},"Config":{"Image":"nginx:1.29","Labels":{"app":"test"}},"HostConfig":{"RestartPolicy":{"Name":"unless-stopped"}},"NetworkSettings":{"Ports":{"80/tcp":[{"HostIp":"127.0.0.1","HostPort":"8080"}]},"Networks":{"bridge":{"IPAddress":"172.17.0.2"}}},"Mounts":[{"Type":"bind","Source":"/tmp/test","Destination":"/data","RW":false}]}]`
	fields, err := decodeDockerDetails(out, "containers", Item{Name: "web"})
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, field := range fields {
		values[field.Label] = field.Value
	}
	if values["名称"] != "web" || values["状态"] != "running" || values["健康状态"] != "healthy" || values["镜像"] != "nginx:1.29" || values["重启策略"] != "unless-stopped" || !strings.Contains(values["端口"], "127.0.0.1:8080") || !strings.Contains(values["挂载"], "只读") {
		t.Fatalf("基础信息不完整：%v", values)
	}
	if _, err := decodeDockerDetails(`[]`, "containers", Item{}); err == nil {
		t.Fatal("资源不存在时应提示刷新")
	}
}
func TestDockerImageDetails(t *testing.T) {
	fields, err := decodeDockerDetails(`[{"Id":"sha256:abc","Size":2048,"Os":"linux","Architecture":"arm64","RepoTags":["app:v1","app:latest"]}]`, "images", Item{Name: "app:v1"})
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, field := range fields {
		values[field.Label] = field.Value
	}
	if values["平台"] != "linux/arm64" || !strings.Contains(values["标签"], "app:latest") || values["镜像 ID"] != "sha256:abc" {
		t.Fatalf("镜像信息不完整：%v", values)
	}
}
func TestKubeFieldsIncludeOperationalStatus(t *testing.T) {
	items, err := decodeKube(`{"items":[{"metadata":{"name":"pod","namespace":"team"},"spec":{"containers":[{"name":"app","image":"app:v1"}],"nodeName":"worker"},"status":{"phase":"Running","podIP":"10.0.0.1","containerStatuses":[{"ready":false,"state":{"waiting":{"reason":"CrashLoopBackOff"}}}]}}]}`, "pods")
	if err != nil || len(items) != 1 {
		t.Fatalf("Pod 解析失败：%v", err)
	}
	values := map[string]string{}
	for _, field := range items[0].Fields {
		values[field.Label] = field.Value
	}
	if values["状态"] != "CrashLoopBackOff" || values["所在节点"] != "worker" || values["Pod IP"] != "10.0.0.1" || values["镜像"] != "app:v1" {
		t.Fatalf("Pod 基础信息不完整：%v", values)
	}
}

func TestDockerDetailedRuntimeAndMounts(t *testing.T) {
	data := `[{"Id":"container-id","Image":"sha256:image-id","State":{"Status":"exited","Pid":42,"ExitCode":7,"OOMKilled":true,"Health":{"Status":"unhealthy","FailingStreak":2,"Log":[{"Start":"2026-09-30T01:00:00Z","ExitCode":1,"Output":"connection refused"}]}},"Config":{"Entrypoint":["/app/start"],"Cmd":["--port","8080"],"WorkingDir":"/app","User":"1000","Env":["APP_MODE=test","ADMIN_PASSWORD=example-secret","API_TOKEN=example-token","TOKEN_HOURLY_LIMIT=100"],"Volumes":{"/data":{}},"ExposedPorts":{"8080/tcp":{}},"Healthcheck":{"Test":["CMD","/app/check"],"Interval":5000000000,"Timeout":2000000000,"Retries":3}},"Mounts":[{"Type":"volume","Name":"app-data","Driver":"local","Source":"/var/lib/docker/volumes/app-data/_data","Destination":"/data","RW":true},{"Type":"bind","Source":"/tmp/config","Destination":"/app/config","Mode":"ro","Propagation":"rprivate","RW":false}],"HostConfig":{"NanoCpus":1500000000,"Memory":268435456,"MemorySwap":536870912,"PidsLimit":128,"ReadonlyRootfs":true,"LogConfig":{"Type":"json-file","Config":{"max-size":"10m"}},"PortBindings":{"8080/tcp":[{"HostIp":"127.0.0.1","HostPort":"18080"}]},"Ulimits":[{"Name":"nofile","Soft":1024,"Hard":2048}]},"NetworkSettings":{"Ports":{"8080/tcp":null},"Networks":{"app-net":{"IPAddress":"172.20.0.2","IPPrefixLen":16,"Gateway":"172.20.0.1","MacAddress":"02:42:ac:14:00:02","Aliases":["app"]}}}}]`
	fields, err := decodeDockerDetails(data, "containers", Item{Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, field := range fields {
		values[field.Label] = field.Value
	}
	for _, expected := range []struct{ label, value string }{{"挂载", "app-data"}, {"挂载", "/data"}, {"挂载", "rprivate"}, {"网络", "172.20.0.1"}, {"网络", "MAC="}, {"端口", "18080"}, {"CPU 限制", "1.5 核"}, {"内存限制", "256"}, {"检测间隔", "5s"}, {"最近检查", "connection refused"}, {"退出码", "7"}, {"只读根目录", "是"}, {"入口程序", "/app/start"}, {"Ulimits", "nofile"}, {"日志选项", "max-size=10m"}} {
		if !strings.Contains(values[expected.label], expected.value) {
			t.Fatalf("缺少重要信息 %s：%s", expected.label, values[expected.label])
		}
	}
	if strings.Contains(values["环境变量"], "example-secret") || strings.Contains(values["环境变量"], "example-token") || !strings.Contains(values["环境变量"], "TOKEN_HOURLY_LIMIT=100") {
		t.Fatal("敏感值应隐藏，普通配置仍需显示")
	}
}
func TestImageDeclaredVolumesAndHealth(t *testing.T) {
	fields, err := decodeDockerDetails(`[{"Id":"sha256:abc","Size":2048,"Os":"linux","Architecture":"arm64","Variant":"v8","Config":{"Volumes":{"/app/data":{}},"Env":["MODE=production"],"Entrypoint":["/entrypoint.sh"],"Cmd":["serve"],"Healthcheck":{"Test":["CMD","/health"],"Timeout":5000000000},"Labels":{"org.opencontainers.image.version":"v1.2"}},"RootFS":{"Type":"layers","Layers":["sha256:layer1","sha256:layer2"]}}]`, "images", Item{Name: "app:v1"})
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, field := range fields {
		values[field.Label] = field.Value
	}
	if values["声明卷"] != "/app/data" || values["平台"] != "linux/arm64/v8" || values["层数"] != "2" || values["检测超时"] != "5s" || values["版本"] != "v1.2" {
		t.Fatalf("镜像重要信息缺失：%v", values)
	}
}

type usageRunner struct{ commands []sysx.Cmd }

func (r *usageRunner) Run(_ context.Context, c sysx.Cmd) (string, error) {
	r.commands = append(r.commands, c)
	switch {
	case strings.Contains(c.String(), "image inspect"):
		return `[{"Id":"sha256:current","Config":{"Volumes":{"/data":{}}}}]`, nil
	case strings.Contains(c.String(), "ps -a"):
		return "first\nsecond", nil
	default:
		return `[{"Image":"sha256:current","Name":"/using-current","State":{"Status":"running"},"Mounts":[{"Type":"bind","Source":"/tmp/current-data","Destination":"/data","RW":true}]},{"Image":"sha256:descendant","Name":"/using-other","Mounts":[{"Source":"/tmp/other"}]}]`, nil
	}
}
func TestImageUsagePinnedToActualImageID(t *testing.T) {
	c := New(false)
	runner := &usageRunner{}
	c.Runner = runner
	c.Target = "test"
	fields, err := c.Details(context.Background(), "images", Item{ID: "app:latest", Name: "app:latest"})
	if err != nil {
		t.Fatal(err)
	}
	all := ""
	usageIndex, networkIndex := -1, -1
	for i, field := range fields {
		all += field.Value + "\n"
		if field.Value == "关联容器" {
			usageIndex = i
		}
		if field.Value == "网络与端口" {
			networkIndex = i
		}
	}
	if !strings.Contains(all, "using-current") || !strings.Contains(all, "/tmp/current-data") || strings.Contains(all, "using-other") || strings.Contains(all, "/tmp/other") {
		t.Fatal("镜像关联挂载应只包含真正使用所选镜像的容器")
	}
	if usageIndex < 0 || usageIndex >= networkIndex {
		t.Fatal("关联挂载应优先显示，不能埋在镜像层和标签之后")
	}
	if len(runner.commands) != 3 || !strings.Contains(runner.commands[1].String(), "ancestor=sha256:current") {
		t.Fatal("镜像关联查询应固定实际 ID")
	}
}
func TestCurrentNamespaceFromKubeconfig(t *testing.T) {
	runner := &recordingRunner{output: "payments\n"}
	c := &Client{Kubernetes: true, Target: "production", Runner: runner}
	namespace, err := c.CurrentNamespace(context.Background())
	if err != nil || namespace != "payments" {
		t.Fatalf("应读取 kubeconfig 中的默认命名空间：%q，%v", namespace, err)
	}
	want := []string{"--context", "production", "config", "view", "--minify", "-o", "jsonpath={.contexts[0].context.namespace}"}
	if got := runner.commands[0].Args; !reflect.DeepEqual(got, want) {
		t.Fatalf("读取命名空间的参数错误：%v", got)
	}
	runner.output = "  \n"
	if namespace, err := c.CurrentNamespace(context.Background()); err != nil || namespace != "default" {
		t.Fatalf("未配置命名空间时应回退为 default：%q，%v", namespace, err)
	}
	runner.err = errors.New("boom")
	if _, err := c.CurrentNamespace(context.Background()); err == nil {
		t.Fatal("读取失败应返回错误")
	}
}
