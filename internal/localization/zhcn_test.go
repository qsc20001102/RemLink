package localization

import (
	"strings"
	"testing"
)

func TestStableValuesRemainVisibleInChineseLabels(t *testing.T) {
	for _, test := range []struct {
		got, chinese, stable string
	}{
		{SessionStatus("FAILED"), "失败", "FAILED"},
		{NodeStatus("UNSTABLE"), "连接不稳定", "UNSTABLE"},
		{RouteResult("DEFAULT_ONLY"), "仅有默认路由", "DEFAULT_ONLY"},
		{Reason("SITE_NO_ROUTE"), "没有通往远程网段", "SITE_NO_ROUTE"},
	} {
		if !strings.Contains(test.got, test.chinese) || !strings.Contains(test.got, test.stable) {
			t.Fatalf("label %q does not contain %q and %q", test.got, test.chinese, test.stable)
		}
	}
}

func TestErrorMessageTranslatesKnownErrorsAndPreservesUnknown(t *testing.T) {
	known := "decrypt WireGuard private key: unprotect secret with DPAPI: invalid state"
	if got := ErrorMessage(known); !strings.Contains(got, "无法解密") || !strings.Contains(got, known) {
		t.Fatalf("known error translation = %q", got)
	}
	unknown := "a future diagnostic with important detail"
	if got := ErrorMessage(unknown); got != unknown {
		t.Fatalf("unknown error translation = %q, want original", got)
	}
}
