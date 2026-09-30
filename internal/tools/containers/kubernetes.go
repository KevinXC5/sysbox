package containers

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func (c *Client) kubeList(ctx context.Context, kind, namespace string) ([]Item, error) {
	args := []string{"get", kind, "-o", "json"}
	if kind != "nodes" {
		if namespace == "*" {
			args = append(args, "--all-namespaces")
		} else {
			args = append(args, "--namespace", namespace)
		}
	}
	out, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return decodeKube(out, kind)
}
func decodeKube(out, kind string) ([]Item, error) {
	var list struct {
		Items []struct {
			Metadata struct {
				Name, Namespace, UID, CreationTimestamp, DeletionTimestamp string
				Labels                                                     map[string]string
			}
			Spec struct {
				Replicas      int
				Unschedulable bool
				NodeName      string
				Selector      struct{ MatchLabels map[string]string }
				Strategy      struct{ Type string }
				Containers    []struct{ Name, Image string }
				Template      struct {
					Spec struct {
						Containers []struct{ Name, Image string }
					}
				}
			}
			Status struct {
				Phase                                             string
				ReadyReplicas, UpdatedReplicas, AvailableReplicas int
				PodIP, HostIP                                     string
				Addresses                                         []struct{ Type, Address string }
				Capacity, Allocatable                             map[string]string
				ContainerStatuses                                 []struct {
					Ready        bool
					RestartCount int
					State        struct {
						Waiting    struct{ Reason string }
						Terminated struct{ Reason string }
					}
				}
				Conditions []struct{ Type, Status string }
				NodeInfo   struct{ KubeletVersion, OperatingSystem, Architecture, ContainerRuntimeVersion string }
			}
			Data       map[string]string
			BinaryData map[string]string
		}
	}
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return nil, fmt.Errorf("kubectl 返回数据格式有误：%w", err)
	}
	var items []Item
	for _, row := range list.Items {
		item := Item{ID: row.Metadata.UID, Name: row.Metadata.Name, Namespace: row.Metadata.Namespace}
		containers := row.Spec.Containers
		if kind == "deployments" {
			containers = row.Spec.Template.Spec.Containers
		}
		images := ""
		for _, container := range containers {
			item.Containers = append(item.Containers, container.Name)
			if images != "" {
				images += ", "
			}
			images += container.Image
		}
		item.Fields = []Field{{"名称", item.Name}, {"命名空间", item.Namespace}, {"创建时间", readableTime(row.Metadata.CreationTimestamp)}}
		switch kind {
		case "deployments":
			item.State = strconv.Itoa(row.Spec.Replicas)
			item.Columns = []string{fmt.Sprintf("%d/%d 就绪", row.Status.ReadyReplicas, row.Spec.Replicas), fmt.Sprintf("%d 已更新", row.Status.UpdatedReplicas), images}
			item.Fields = append(item.Fields, Field{"期望副本", strconv.Itoa(row.Spec.Replicas)}, Field{"就绪副本", strconv.Itoa(row.Status.ReadyReplicas)}, Field{"可用副本", strconv.Itoa(row.Status.AvailableReplicas)}, Field{"更新副本", strconv.Itoa(row.Status.UpdatedReplicas)}, Field{"更新策略", row.Spec.Strategy.Type}, Field{"容器", strings.Join(item.Containers, ", ")}, Field{"镜像", images}, Field{"选择器", mapLines(row.Spec.Selector.MatchLabels)})
		case "configmaps":
			item.Columns = []string{fmt.Sprintf("%d 个键", len(row.Data)+len(row.BinaryData))}
			item.Fields = append(item.Fields, Field{"文本键数", strconv.Itoa(len(row.Data))}, Field{"二进制键数", strconv.Itoa(len(row.BinaryData))}, Field{"数据键", strings.Join(sortedKeys(row.Data), ", ")}, Field{"二进制键", strings.Join(sortedKeys(row.BinaryData), ", ")})
		case "pods":
			ready, restarts := 0, 0
			phase := row.Status.Phase
			for _, status := range row.Status.ContainerStatuses {
				if status.Ready {
					ready++
				}
				restarts += status.RestartCount
				if status.State.Waiting.Reason != "" {
					phase = status.State.Waiting.Reason
				}
				if status.State.Terminated.Reason != "" && !status.Ready {
					phase = status.State.Terminated.Reason
				}
			}
			if row.Metadata.DeletionTimestamp != "" {
				phase = "Terminating"
			}
			item.Columns = []string{fmt.Sprintf("%s · %d/%d", phase, ready, len(containers)), fmt.Sprintf("%d 次重启", restarts), row.Spec.NodeName}
			item.Fields = append(item.Fields, Field{"状态", phase}, Field{"就绪容器", fmt.Sprintf("%d/%d", ready, len(containers))}, Field{"重启次数", strconv.Itoa(restarts)}, Field{"所在节点", row.Spec.NodeName}, Field{"Pod IP", row.Status.PodIP}, Field{"主机 IP", row.Status.HostIP}, Field{"容器", strings.Join(item.Containers, ", ")}, Field{"镜像", images})
		case "nodes":
			status := "NotReady"
			for _, condition := range row.Status.Conditions {
				if condition.Type == "Ready" && condition.Status == "True" {
					status = "Ready"
				}
			}
			if row.Spec.Unschedulable {
				status += " · 已禁调度"
				item.State = "cordoned"
			}
			item.Columns = []string{status, row.Status.NodeInfo.KubeletVersion}
			var addresses []string
			for _, address := range row.Status.Addresses {
				addresses = append(addresses, address.Type+" · "+address.Address)
			}
			item.Fields = []Field{{"名称", item.Name}, {"状态", status}, {"创建时间", readableTime(row.Metadata.CreationTimestamp)}, {"Kubelet", row.Status.NodeInfo.KubeletVersion}, {"平台", row.Status.NodeInfo.OperatingSystem + "/" + row.Status.NodeInfo.Architecture}, {"运行时", row.Status.NodeInfo.ContainerRuntimeVersion}, {"地址", strings.Join(addresses, "\n")}, {"容量", mapLines(row.Status.Capacity)}, {"可分配", mapLines(row.Status.Allocatable)}}
		}
		if len(row.Metadata.Labels) > 0 {
			item.Fields = append(item.Fields, Field{"标签", mapLines(row.Metadata.Labels)})
		}
		items = append(items, item)
	}
	return items, nil
}
func kubeActions(kind string, item Item) []Action {
	actions := []Action{{ID: "describe", Label: "查看资源详情"}, {ID: "yaml", Label: "查看 YAML"}}
	switch kind {
	case "deployments":
		actions = append(actions,
			Action{ID: "scale", Label: "扩缩容", Prompt: "副本数（0 表示停止全部副本）", Default: item.State, Mutates: true},
			Action{ID: "restart", Label: "滚动重启", Mutates: true},
			Action{ID: "rollout", Label: "查看发布状态"}, Action{ID: "logs", Label: "实时日志"},
			Action{ID: "edit", Label: "编辑 YAML", Mutates: true, Interactive: true})
	case "configmaps":
		actions = append(actions, Action{ID: "edit", Label: "编辑 YAML", Mutates: true, Interactive: true, Note: "使用 KUBE_EDITOR / EDITOR 编辑；保存后提交到集群"})
	case "pods":
		actions = append(actions, Action{ID: "logs", Label: "实时日志"})
		if len(item.Containers) > 0 {
			actions = append(actions, Action{ID: "exec", Label: "进入容器终端", Prompt: "容器名称（使用 /bin/sh）", Default: item.Containers[0], Mutates: true, Interactive: true})
		}
	case "nodes":
		if item.State == "cordoned" {
			actions = append(actions, Action{ID: "uncordon", Label: "恢复调度", Mutates: true})
		} else {
			actions = append(actions, Action{ID: "cordon", Label: "禁止调度", Mutates: true})
		}
		return append(actions, Action{ID: "drain", Label: "排空节点", Mutates: true, Note: "驱逐节点上的 Pod；遵守 PDB，忽略 DaemonSet；有本地数据或非托管 Pod 时停止，不强制驱逐"})
	}
	return append(actions, Action{ID: "delete", Label: "删除资源", Mutates: true, Note: "删除所选资源；由控制器管理的 Pod 通常会自动重建"})
}
func kubeArgs(kind string, item Item, action, value string) ([]string, error) {
	resource := kind + "/" + item.Name
	var args []string
	switch action {
	case "describe":
		args = []string{"describe", resource}
	case "yaml":
		args = []string{"get", resource, "-o", "yaml"}
	case "edit":
		args = []string{"edit", resource}
	case "scale":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("副本数必须是非负整数")
		}
		args = []string{"scale", resource, "--replicas=" + strconv.Itoa(n)}
	case "restart":
		args = []string{"rollout", "restart", resource}
	case "rollout":
		args = []string{"rollout", "status", resource, "--timeout=30s"}
	case "logs":
		args = []string{"logs", resource, "--all-containers=true", "--prefix=true", "--tail=200", "--timestamps=true", "--max-log-requests=10"}
	case "exec":
		valid := false
		for _, name := range item.Containers {
			if name == value {
				valid = true
			}
		}
		if !valid {
			return nil, fmt.Errorf("请选择有效的容器名称：%v", item.Containers)
		}
		args = []string{"exec", "-it", item.Name, "-c", value, "--", "/bin/sh"}
	case "delete":
		args = []string{"delete", resource, "--wait=false"}
	case "cordon", "uncordon":
		args = []string{action, item.Name}
	case "drain":
		args = []string{"drain", item.Name, "--ignore-daemonsets", "--timeout=120s"}
	default:
		return nil, fmt.Errorf("不支持的 Kubernetes 操作：%s", action)
	}
	// 即使列表处于全部命名空间，操作也绑定资源的实际命名空间。
	if kind != "nodes" {
		if item.Namespace == "" {
			return nil, fmt.Errorf("资源缺少命名空间，请刷新列表")
		}
		args = append([]string{"--namespace", item.Namespace}, args...)
	}
	return args, nil
}
