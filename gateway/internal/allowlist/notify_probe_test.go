// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package allowlist_test

import (
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/allowlist"
)

// TestAllowlist_NotifyProbeSendStaysOff — глагол стендовой пробы notify-probe не
// входит в перечень разрешённых и режется по суффиксу Internal (запрет #6);
// маршрут к нему — только во внутреннем блоке REST края (решение владельца
// 2026-10-08 (1)). Близнец — публичный NoticeService/List того же пакета: он в
// перечне есть.
func TestAllowlist_NotifyProbeSendStaysOff(t *testing.T) {
	const twin = "/kacho.cloud.notify.v1.NoticeService/List"
	if !allowlist.IsAllowed(twin) || allowlist.HasInternalSuffix(twin) {
		t.Fatalf("близнец %q обязан быть разрешён и не-Internal", twin)
	}
	const probe = "/kacho.cloud.notify.v1.InternalNotifyProbeService/Send"
	if allowlist.IsAllowed(probe) {
		t.Errorf("%q в AllowedMethods (запрет #6)", probe)
	}
	if !allowlist.HasInternalSuffix(probe) {
		t.Errorf("%q не опознан HasInternalSuffix", probe)
	}
}
