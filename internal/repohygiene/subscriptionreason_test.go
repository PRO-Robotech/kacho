// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// TestSubscriptionShapeReasonContract проверяет наблюдаемые причины двух
// дефектов формы. Заполненность экземпляра сообщения анализатор не проверяет.
func TestSubscriptionShapeReasonContract(t *testing.T) {
	// verifies https://github.com/PRO-Robotech/kacho/issues/2696
	// CI-DT-03/04/05: exact Reason из принятой приёмки, Kind и координата.
	t.Parallel()
	lawful := baseShapeForm()
	if !t.Run("CI-DT-05-lawful", func(t *testing.T) {
		findings, census := shapeAudit(t, lawful, shapeStandLedger(), shapeStandAbsent())
		t.Logf("законная форма:\n%s", lawful.render())
		if census.Types == 0 || census.Fields == 0 || census.Assertions == 0 {
			t.Fatalf("форма не осмотрена: %+v", census)
		}
		if len(findings) != 0 {
			t.Fatalf("законный близнец дал находки: %+v", findings)
		}
	}) {
		t.Fatal("законный близнец не прошёл: дефектные формы не дают вердикта")
	}

	const carrier = "  oneof carrier {\n    google.protobuf.Any state = 10;\n    StateUnavailable state_unavailable = 11;\n  }"
	for _, tc := range []struct {
		name   string
		before string
		after  string
		reason string
	}{
		{
			name:   "CI-DT-03-without-oneof",
			before: carrier,
			after:  "  google.protobuf.Any state = 10;\n  StateUnavailable state_unavailable = 11;",
			reason: "носитель нагрузки не выражен ветвлением `carrier`. Форма обязана объявлять выбор: " +
				"состояние ЛИБО признак, что состояния нет. Заполненность конкретного сообщения этот анализатор не проверяет",
		},
		{
			name:   "CI-DT-04-third-branch",
			before: "    StateUnavailable state_unavailable = 11;",
			after:  "    StateUnavailable state_unavailable = 11;\n    string state_summary = 12;",
			reason: "ветвление носителя несёт 3 ветв(ей), а обязано ровно две: состояние ЛИБО признак, что состояния нет. " +
				"Здесь проверяется число объявленных ветвей, а не заполненность конкретного сообщения",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if strings.Count(lawful.Event, tc.before) != 1 {
				t.Fatal("одно-фактная инъекция невозможна: исходный фрагмент не единственный")
			}
			defect := lawful
			defect.Event = strings.Replace(lawful.Event, tc.before, tc.after, 1)
			restored := defect
			restored.Event = strings.Replace(defect.Event, tc.after, tc.before, 1)
			if defect == lawful || restored != lawful {
				t.Fatal("инъекция не меняет ровно объявленный фрагмент законной формы")
			}
			body := defect.render()
			const eventDeclaration = "\nmessage SubscriptionEvent {\n"
			if strings.Count(body, eventDeclaration) != 1 {
				t.Fatal("нет единственной координаты сообщения события")
			}
			eventLine := strings.Count(body[:strings.Index(body, eventDeclaration)+1], "\n") + 1
			findings, census := shapeAudit(t, defect, shapeStandLedger(), shapeStandAbsent())
			t.Logf("дефектная форма:\n%s", body)
			if census.Types == 0 || census.Fields == 0 || census.Assertions == 0 {
				t.Fatalf("форма не осмотрена: %+v", census)
			}
			if len(findings) != 1 {
				t.Fatalf("одно-фактный дефект: ожидалась одна находка, получено %+v", findings)
			}
			got := findings[0]
			if got.Kind != "carrier-not-a-choice" || got.Where != "proto/corelib/subscription/subscription.proto" || got.Line != eventLine {
				t.Fatalf("неверный Kind или координата: %+v; ожидается carrier-not-a-choice proto/corelib/subscription/subscription.proto:%d", got, eventLine)
			}
			if got.Reason != tc.reason {
				t.Errorf("%s %s:%d: Reason = %q; требуется %q", got.Kind, got.Where, got.Line, got.Reason, tc.reason)
			}
		})
	}
}
