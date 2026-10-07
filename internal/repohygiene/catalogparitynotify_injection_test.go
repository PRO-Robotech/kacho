// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// catalogparitynotify_injection_test.go — NTF1-C07: снятие аннотации права с
// `Claim` ленты — находка обхода «не несёт аннотации» с именем метода.
//
// Вход — НАСТОЯЩИЕ аннотации методов ленты из слинкованных стабов
// `corelib.notify` (тот же обход, что у гейта); порча меняет ровно один факт —
// аннотации `Claim` обнулены. Близнец — те же методы с аннотациями как есть.

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/PRO-Robotech/corelib/authz/catalogderive"
)

func TestNTF1C07Injection_ClaimWithoutAnnotationIsAFinding(t *testing.T) {
	t.Parallel()
	got := map[string]catalogderive.Annotations{}
	catalogderive.RangeAnnotated([]string{"corelib.notify"}, func(fullMethod string, _ protoreflect.MethodDescriptor,
		a catalogderive.Annotations) {
		got[fullMethod] = a
	})
	for _, m := range notifyFeedMethods {
		a, ok := got[m]
		if !ok {
			t.Fatalf("проверка НЕ ИСПОЛНЯЛАСЬ: метод %s не обойдён — стабы corelib.notify не слинкованы", m)
		}
		// Близнец: аннотации как есть — полоса есть, находки нет.
		lane, finding := laneMismatch(m, a)
		if finding != "" || lane == "" {
			t.Errorf("близнец %s: полоса %q, находка %q — ожидалась полоса без находки", m, lane, finding)
		}
		t.Logf("близнец %s → полоса %s", m, lane)
	}
	claim := notifyFeedMethods[0]
	_, finding := laneMismatch(claim, catalogderive.Annotations{})
	if !strings.Contains(finding, claim) || !strings.Contains(finding, "не несёт аннотации") {
		t.Errorf("снятие аннотации с %s не дало находки с именем метода: %q", claim, finding)
	}
	t.Logf("порча: аннотации %s сняты → %s", claim, finding)
}
