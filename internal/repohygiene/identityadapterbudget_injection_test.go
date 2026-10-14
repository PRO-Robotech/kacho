// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import "testing"

const identityAdapterHead = "package clients\n\ntype SessionRevocationsAdapter struct{ client, iam, human x }\n\n"

// Глагол без бюджета краснит перепись и называет метод; тот же глагол с
// бюджетом — молчит.
func TestIdentityAdapterInjection_UnboundedVerbIsFoundAndBoundedIsSilent(t *testing.T) {
	t.Parallel()
	bad := identityAdapterHead + "func (a *SessionRevocationsAdapter) Ask(ctx C) error {\n\t_, err := a.client.IsRevoked(ctx, nil)\n\treturn err\n}\n"
	f, c, err := FindUnboundedIdentityAdapterVerbs(bad)
	if err != nil || len(f) != 1 || f[0].Method != "Ask" || c.Verbs != 1 || c.Bounded != 0 {
		t.Fatalf("глагол без бюджета не найден: %+v %+v %v", f, c, err)
	}
	good := identityAdapterHead + "func (a *SessionRevocationsAdapter) Ask(ctx C) error {\n\tctx, cancel, berr := a.bounded(ctx)\n\tdefer cancel()\n\tif berr != nil {\n\t\treturn berr\n\t}\n\t_, err := a.client.IsRevoked(ctx, nil)\n\treturn err\n}\n"
	if f, c, err := FindUnboundedIdentityAdapterVerbs(good); err != nil || len(f) != 0 || c.Bounded != 1 {
		t.Fatalf("законный близнец краснеет: %+v %+v %v", f, c, err)
	}
	// Бюджет, взятый ПОСЛЕ вызова, не ограничивает его.
	late := identityAdapterHead + "func (a *SessionRevocationsAdapter) Ask(ctx C) error {\n\t_, err := a.iam.Check(ctx, nil)\n\t_, _, _ = a.bounded(ctx)\n\treturn err\n}\n"
	if f, _, _ := FindUnboundedIdentityAdapterVerbs(late); len(f) != 1 {
		t.Fatalf("бюджет после вызова принят за предел: %+v", f)
	}
}

// Пустой обход — не вердикт.
func TestIdentityAdapterInjection_EmptyWalkReadsZero(t *testing.T) {
	t.Parallel()
	_, c, err := FindUnboundedIdentityAdapterVerbs("package clients\n")
	if err != nil || c.Methods != 0 || c.Verbs != 0 {
		t.Fatalf("пустой носитель дал перепись %+v %v", c, err)
	}
}
