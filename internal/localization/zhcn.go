// Package localization provides Chinese operator-facing labels while keeping
// protocol codes and persisted enum values stable.
package localization

import "strings"

var sessionStatuses = map[string]string{
	"IDLE":           "空闲",
	"CREATING":       "正在创建",
	"PREPARING_SITE": "正在准备现场端",
	"READY":          "准备就绪",
	"ACTIVE":         "活动中",
	"STOPPING":       "正在停止",
	"CLOSED":         "已关闭",
	"FAILED":         "失败",
}

var nodeStatuses = map[string]string{
	"ONLINE":   "在线",
	"UNSTABLE": "连接不稳定",
	"OFFLINE":  "离线",
}

var routeResults = map[string]string{
	"DIRECT":           "直连路由",
	"ROUTED":           "明确路由",
	"DEFAULT_ONLY":     "仅有默认路由",
	"NO_ROUTE":         "没有路由",
	"OVERLAY_CONFLICT": "与 Overlay 网段冲突",
}

var errorCodes = map[string]string{
	"SERVER_UNREACHABLE":      "无法连接服务器",
	"JOIN_TOKEN_INVALID":      "Join Token 无效",
	"NODE_AUTH_FAILED":        "节点身份认证失败",
	"OVERLAY_LOCAL_CONFLICT":  "Overlay 网段与本地网络冲突",
	"ENGINEER_SESSION_EXISTS": "Engineer 已存在未结束的会话",
	"SITE_OFFLINE":            "现场端离线",
	"CIDR_INVALID":            "远程网段格式无效",
	"CIDR_LOCAL_CONFLICT":     "远程网段与 Engineer 本地网络冲突",
	"CIDR_OVERLAY_CONFLICT":   "远程网段与 Overlay 网段冲突",
	"SITE_NO_ROUTE":           "现场端没有通往远程网段的明确路由",
	"NETSTACK_UNAVAILABLE":    "现场端 netstack 网关不可用",
	"FLOW_LIMIT_REACHED":      "现场端连接流数量已达到上限",
	"SESSION_TIMEOUT":         "会话建立超时",
	"SESSION_INJECT_FAILED":   "会话数据包注入失败",
	"NODE_RUNTIME_REBUILT":    "节点网络运行时已重建",
	"NODE_REVOKED":            "节点已被撤销",
	"ADMIN_DISCONNECT":        "管理员强制断开",
	"ENGINEER_OPERATOR":       "Engineer 操作员主动断开",
}

func bilingual(value string, labels map[string]string) string {
	value = strings.TrimSpace(value)
	if label, ok := labels[value]; ok {
		return label + "（" + value + "）"
	}
	return value
}

// SessionStatus returns a Chinese label with the stable wire value retained.
func SessionStatus(value string) string { return bilingual(value, sessionStatuses) }

// NodeStatus returns a Chinese label with the stable persisted value retained.
func NodeStatus(value string) string { return bilingual(value, nodeStatuses) }

// RouteResult returns a Chinese route decision with the stable result retained.
func RouteResult(value string) string { return bilingual(value, routeResults) }

// Reason translates a stable error/reason code without hiding its original value.
func Reason(value string) string { return bilingual(value, errorCodes) }

// ErrorMessage translates common Windows/operator errors. Unknown errors are
// deliberately preserved verbatim so diagnostics are never lost.
func ErrorMessage(value string) string {
	trimmed := strings.TrimSpace(value)
	translations := []struct {
		fragment string
		chinese  string
	}{
		{"administrator privileges are required to manage the RemLink Wintun adapter", "需要以管理员身份运行，才能管理 RemLink Wintun 网卡"},
		{"decrypt WireGuard private key: unprotect secret with DPAPI", "无法解密 WireGuard 私钥：identity.json 不是由当前 Windows 系统生成，需重新注册节点"},
		{"Join Token is required for first registration", "首次注册需要在 YAML 中填写 Join Token"},
		{"configured Server URL differs from persisted Node identity", "YAML 中的 Server URL 与已保存的节点身份不一致"},
		{"Site has no route to Remote CIDR", "现场端没有通往远程网段的明确路由"},
		{"Engineer Session runtime is not ready", "Engineer 会话运行时尚未就绪"},
		{"Node Session runtime is not initialized", "节点会话运行时尚未初始化"},
		{"Site Session runtime is not ready", "Site 会话运行时尚未就绪"},
	}
	for _, translation := range translations {
		if strings.Contains(trimmed, translation.fragment) {
			return translation.chinese + "（原始错误：" + trimmed + "）"
		}
	}
	return Reason(trimmed)
}
