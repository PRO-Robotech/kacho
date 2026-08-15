// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package serviceerr

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Отказ учёта наружу: три исхода, три РАЗНЫХ кода и три РАЗНЫХ признака.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), V2-3 и DoD S2 п.3.
//
// ПОЧЕМУ ПРОВЕРЯЕТСЯ ПАРА, А НЕ ОДИН КОД. Клиент различает полосы машинно — по
// `reason`-токену в `google.rpc.ErrorInfo`, а не разбором прозы
// (`api-conventions.md` §By-lane code-split). Утверждение про один только код
// осталось бы зелёным, если бы признак потерялся или стал общим на оба исхода —
// то есть ровно на том дефекте, ради которого разделение и делается.
//
// ПОЧЕМУ ЭТО НЕ «ОДИН ОТКАЗ С ДВУМЯ ОТТЕНКАМИ». «Место кончилось» требует от
// администратора ПОДНЯТЬ предел, «потолок не назван» — ЗАВЕСТИ его. Сведи их к
// одному коду, и читающий 429 пойдёт искать, что понизить, там, где ничего не
// назначено.

// errorInfoOf достаёт признак из деталей статуса; отсутствие детали — находка, а
// не «ну и ладно»: без неё клиенту остаётся разбор прозы.
func errorInfoOf(t testing.TB, err error) *errdetails.ErrorInfo {
	t.Helper()
	st, ok := status.FromError(err)
	require.True(t, ok, "отказ обязан быть gRPC-статусом: %v", err)
	for _, d := range st.Details() {
		if info, isInfo := d.(*errdetails.ErrorInfo); isInfo {
			return info
		}
	}
	t.Fatalf("статус обязан нести google.rpc.ErrorInfo — иначе полосы различимы только прозой: %v", err)
	return nil
}

func TestMapRepoErr_QuotaExceeded(t *testing.T) {
	// Текст приходит от единственного производителя отказа (миграция 0041) и
	// является контрактом — поэтому здесь он воспроизведён дословно.
	err := fmt.Errorf("%w: project prj-1 has reached its limit of 4 vpc.network", ErrQuotaExceeded)

	out := MapRepoErr(err)

	st, _ := status.FromError(out)
	assert.Equal(t, codes.ResourceExhausted, st.Code(),
		"исчерпание предела — RESOURCE_EXHAUSTED; край отдаёт по нему 429")
	assert.Equal(t, "project prj-1 has reached its limit of 4 vpc.network", st.Message(),
		"текст производителя — часть контракта и наружу идёт дословно")

	info := errorInfoOf(t, out)
	assert.Equal(t, "QUOTA_EXCEEDED", info.GetReason())
	assert.Equal(t, "vpc.kacho.cloud", info.GetDomain())
}

func TestMapRepoErr_QuotaNotProvisioned(t *testing.T) {
	err := fmt.Errorf("%w: project prj-1 has no ceiling stated for vpc.gateway", ErrQuotaNotProvisioned)

	out := MapRepoErr(err)

	st, _ := status.FromError(out)
	assert.Equal(t, codes.FailedPrecondition, st.Code(),
		"«потолок не назван» — предусловие ПЛАТФОРМЫ; INVALID_ARGUMENT обвинил бы "+
			"вызывающего в том, чего он не присылал. Край отдаёт по этому коду 400")
	assert.Equal(t, "project prj-1 has no ceiling stated for vpc.gateway", st.Message())

	info := errorInfoOf(t, out)
	assert.Equal(t, "QUOTA_NOT_PROVISIONED", info.GetReason())
	assert.Equal(t, "vpc.kacho.cloud", info.GetDomain())
}

// TestMapRepoErr_QuotaOutcomesAreDistinguishable — то, ради чего заведены два
// признака: их нельзя перепутать машинно.
//
// Отдельным утверждением, а не выводом из двух проб выше: те проверяют КАЖДЫЙ
// исход по отдельности и остались бы зелёными, если бы оба признака стали одним
// и тем же значением — при условии, что это значение верно для обоих. Различие
// — самостоятельное свойство, и утверждать его надо самостоятельно.
func TestMapRepoErr_QuotaOutcomesAreDistinguishable(t *testing.T) {
	exceeded := MapRepoErr(fmt.Errorf("%w: x", ErrQuotaExceeded))
	notProvisioned := MapRepoErr(fmt.Errorf("%w: y", ErrQuotaNotProvisioned))

	stE, _ := status.FromError(exceeded)
	stN, _ := status.FromError(notProvisioned)

	assert.NotEqual(t, stE.Code(), stN.Code(), "коды двух исходов обязаны различаться")
	assert.NotEqual(t, errorInfoOf(t, exceeded).GetReason(), errorInfoOf(t, notProvisioned).GetReason(),
		"признаки двух исходов обязаны различаться")
}

// TestMapRepoErr_NonQuotaKeepsNoQuotaReason — положительный контроль к
// отрицанию: признак учёта не приклеивается к чужим отказам.
//
// Без него проба «признак есть» была бы зелёной и на реализации, вешающей
// `QUOTA_EXCEEDED` на всё подряд.
func TestMapRepoErr_NonQuotaKeepsNoQuotaReason(t *testing.T) {
	out := MapRepoErr(fmt.Errorf("%w: Network net-1 not found", ErrNotFound))

	st, _ := status.FromError(out)
	require.Equal(t, codes.NotFound, st.Code())
	for _, d := range st.Details() {
		info, isInfo := d.(*errdetails.ErrorInfo)
		if !isInfo {
			continue
		}
		assert.NotContains(t, info.GetReason(), "QUOTA",
			"признак учёта не вправе появляться на отказе, к учёту не относящемся")
	}
}

// TestMapRepoErrLeakSafe_QuotaKeepsItsCode — внутренний слушатель (:9091) держит
// строгую текстовую политику, но КОД и ПРИЗНАК обязан отдавать те же.
//
// Полоса, теряющая свой код на одном из двух слушателей, даёт один и тот же
// отказ, читаемый по-разному в зависимости от порта, — то же расхождение, что и
// между полосами, только по другой оси.
func TestMapRepoErrLeakSafe_QuotaKeepsItsCode(t *testing.T) {
	out := MapRepoErrLeakSafe(fmt.Errorf("%w: project prj-1 has reached its limit of 4 vpc.network",
		ErrQuotaExceeded), "internal error")

	st, _ := status.FromError(out)
	assert.Equal(t, codes.ResourceExhausted, st.Code())
	assert.Equal(t, "QUOTA_EXCEEDED", errorInfoOf(t, out).GetReason())
	assert.True(t, errors.Is(ErrQuotaExceeded, ErrQuotaExceeded), "sentinel-идентичность сохранена")
}
