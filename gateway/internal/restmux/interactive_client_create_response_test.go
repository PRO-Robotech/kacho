// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"strings"
	"testing"

	operationv1 "github.com/PRO-Robotech/corelib/api/corelib/operation"
	"google.golang.org/protobuf/types/known/anypb"
)

// interactive_client_create_response_test.go — край отдаёт операцию `Create`
// интерактивного клиента с ответом НОВОГО типа (kacho#2896, служба —
// PRO-Robotech/kaname#405, решение Р8 её приёмки).
//
// ПРЕДМЕТ. Служба с ревизии `ca8d1b641` кладёт в `Operation.response` операции
// `Create` не `InteractiveClient`, а `CreateInteractiveClientResponse`: секрет
// конфиденциального клиента выдаётся один раз, в ответе создания. Край переводит
// `Any` в JSON реестром типов СВОЕГО процесса, а туда тип попадает только из
// стабов пиненного модуля службы. Пин без этого типа — операция `Create` на REST
// отвечает 500 на штатном пути, при исправной службе.
//
// ПОЧЕМУ АДРЕС СТРОКОЙ, а не Go-типом — тот же довод, что у соседней пробы
// `operation_any_resolution_test.go`: ссылка на тип втянула бы его в тестовый
// бинарь сама и проба зеленела бы независимо от того, что линкует пакет,
// строящий маршаллеры края. Тело — ноль байт: для разрешения адреса оно не
// нужно, а разрешение и есть предмет.
//
// ЗАКОННЫЙ БЛИЗНЕЦ — операция `Update` того же клиента: её ответ прежнего типа
// `InteractiveClient`, и край отдаёт её на любом пине. Меняется ровно один факт
// — адрес типа ответа.
//
// ГРАНИЦА. Проба судит реестр края, а не то, что служба на стенде ДЕЙСТВИТЕЛЬНО
// кладёт этот тип: это п.3 предиката задачи, прогон через край на сведённой
// сборке волны.

const (
	interactiveClientCreateResponseURL = "type.googleapis.com/kaname.cloud.iam.v1.CreateInteractiveClientResponse"
	interactiveClientUpdateResponseURL = "type.googleapis.com/kaname.cloud.iam.v1.InteractiveClient"
)

func operationRespondingWith(typeURL string) *operationv1.Operation {
	return &operationv1.Operation{
		Id:     "opsdeadbeefdeadbeef0",
		Done:   true,
		Result: &operationv1.Operation_Response{Response: &anypb.Any{TypeUrl: typeURL}},
	}
}

// TestEdgeRendersTheInteractiveClientCreateResponse — оба боевых маршаллера
// края отдают операцию `Create` с ответом нового типа и её близнеца `Update`.
func TestEdgeRendersTheInteractiveClientCreateResponse(t *testing.T) {
	marshalers := map[string]*strictEnumMarshaler{
		"public":   newStrictEnumMarshaler(newPublicJSONPb()),
		"internal": newStrictEnumMarshaler(newInternalJSONPb()),
	}
	cases := []struct{ verb, typeURL string }{
		{"Create", interactiveClientCreateResponseURL},
		{"Update (близнец)", interactiveClientUpdateResponseURL},
	}
	rendered := 0
	for name, m := range marshalers {
		for _, c := range cases {
			t.Run(name+"/"+c.verb, func(t *testing.T) {
				body, err := m.Marshal(operationRespondingWith(c.typeURL))
				if err != nil {
					t.Fatalf("край не отдал операцию %s с ответом %s: %v — вызывающий получает 500 "+
						"на штатном пути", c.verb, c.typeURL, err)
				}
				if !strings.Contains(string(body), c.typeURL) {
					t.Fatalf("в теле нет адреса типа ответа %q; тело: %s", c.typeURL, body)
				}
				rendered++
			})
		}
	}
	t.Logf("перепись: маршаллеров %d · глаголов %d · отдано операций %d", len(marshalers), len(cases), rendered)
}
