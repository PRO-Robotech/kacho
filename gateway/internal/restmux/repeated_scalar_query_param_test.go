// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"net/url"
	"strings"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/grpc-ecosystem/grpc-gateway/v2/utilities"

	vpcv1 "github.com/PRO-Robotech/kacho/pkg/api/kacho/cloud/vpc/v1"
)

// TestRepeatedScalarQueryParamIsRefused закрепляет исход, на котором стоит
// сквозной кейс `*-LST-DOUBLE-PROJECT-PARAM` (issue #698).
//
// ПРЕДМЕТ. До #698 кейс утверждал `oneOf([200, 400])` под заголовком
// «200 (last wins) или 400». «last wins» не происходит НИКОГДА: `project_id` —
// скалярное поле запроса, и разбор параметров строки запроса отвергает второе
// значение целиком, а сгенерированный обработчик края переводит отказ в
// `InvalidArgument` → 400. Кейс поэтому утверждает ОДИН исход и ПАРУ
// (400 и код 3) плюс имя поля в сообщении.
//
// ЗАЧЕМ ЭТО ЗДЕСЬ, А НЕ ТОЛЬКО В КЕЙСЕ. Исход задаёт БИБЛИОТЕКА, а не наш код:
// сменится её поведение при обновлении — и e2e-кейс покраснеет на поднятом
// стенде, то есть дорого и поздно. Здесь то же свойство меряется на сборке, за
// миллисекунды, и падение прямо называет предмет.
//
// КОНТРОЛЬ В ОБЕ СТОРОНЫ обязателен: без первой половины «отвергает» было бы
// неотличимо от «отвергает вообще всё», и утверждение о втором значении осталось
// бы недоказанным.
func TestRepeatedScalarQueryParamIsRefused(t *testing.T) {
	// Фильтр путевых параметров пуст: у `/vpc/v1/networks` их нет, и `projectId`
	// приходит именно строкой запроса — то же, что делает сгенерированный
	// `request_NetworkService_List_0`.
	pathParams := utilities.NewDoubleArray([][]string{})

	t.Run("одно значение принимается", func(t *testing.T) {
		var req vpcv1.ListNetworksRequest
		if err := runtime.PopulateQueryParameters(&req,
			url.Values{"projectId": {"prj00000000000000001"}}, pathParams); err != nil {
			t.Fatalf("одиночный projectId обязан приниматься, получено: %v", err)
		}
		if got := req.GetProjectId(); got != "prj00000000000000001" {
			t.Fatalf("project_id не долетел: %q", got)
		}
	})

	t.Run("второе значение отвергается и поле названо", func(t *testing.T) {
		var req vpcv1.ListNetworksRequest
		err := runtime.PopulateQueryParameters(&req,
			url.Values{"projectId": {"prj00000000000000001", "prj00000000000000002"}}, pathParams)
		if err == nil {
			t.Fatalf("дубликат projectId ПРИНЯТ (project_id=%q) — тогда сквозной кейс "+
				"*-LST-DOUBLE-PROJECT-PARAM утверждает исход, которого больше нет",
				req.GetProjectId())
		}
		// Кейс утверждает, что сообщение называет поле: без этой половины третье
		// утверждение кейса было бы украшением — у него не было бы производителя.
		if msg := err.Error(); !strings.Contains(msg, "project_id") {
			t.Fatalf("сообщение отказа не называет поле: %q", msg)
		}
	})
}
