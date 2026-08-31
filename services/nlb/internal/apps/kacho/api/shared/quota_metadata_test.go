// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package shared

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/pkg/quota/quotadetail"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// Величины отказа учёта доезжают до клиента МАШИННО (задача продукта #1605).
//
// Производитель отказа уже посчитал носителя, вид, предел и занятое и положил их
// в `DETAIL`. Клиент обязан читать их из `google.rpc.ErrorInfo.metadata`, а не
// разбором прозы: тон сообщения стабилен, но не парсибелен
// (`api-conventions.md` §By-lane code-split).

// refusalMetadataInfo достаёт `ErrorInfo` из статуса; отсутствие детали —
// находка, а не «ну и ладно».
func refusalMetadataInfo(t testing.TB, err error) *errdetails.ErrorInfo {
	t.Helper()
	st, ok := status.FromError(err)
	require.True(t, ok, "отказ обязан быть gRPC-статусом: %v", err)
	for _, d := range st.Details() {
		if info, isInfo := d.(*errdetails.ErrorInfo); isInfo {
			return info
		}
	}
	t.Fatalf("статус обязан нести google.rpc.ErrorInfo: %v", err)
	return nil
}

// Дословно то, что кладёт в DETAIL полоса KQ001 производителя.
const quotaDetailExceeded = `{"carrier_type": "project", "carrier_id": "prj-1", ` +
	`"kind": "loadbalancer.networkloadbalancer", "limit": 4, "used": 4}`

func TestQuotaRefusalCarriesTheProducerAmounts(t *testing.T) {
	const producer = "project prj-1 has reached its limit of 4 loadbalancer.networkloadbalancer"
	err := quotadetail.Attach(
		fmt.Errorf("%w: %s", domain.ErrQuotaExceeded, producer), quotaDetailExceeded)

	out := MapDomainErr(err)

	st, _ := status.FromError(out)
	assert.Equal(t, codes.ResourceExhausted, st.Code())
	// РАСХОЖДЕНИЕ СНЯТО (задача продукта #1658), поэтому здесь РАВЕНСТВО, а не
	// вхождение. Прежняя редакция утверждала вхождение и объясняла почему:
	// `StripSentinel` знал закрытый перечень префиксов, отказа учёта в нём не
	// было, и клиент nlb получал предложение производителя с приклеенным именем
	// внутреннего sentinel'а. Теперь префикс выводится из sentinel'а, который
	// распознал вызывающий, и тон совпадает с пятью остальными владельцами.
	// Вхождение зеленело бы на любом новом префиксе — утверждать надо ТО, что
	// видит клиент, целиком.
	assert.Equal(t, producer, st.Message(),
		"предложение производителя — контракт, и клиент не видит ничего сверх него")

	info := refusalMetadataInfo(t, out)
	assert.Equal(t, "QUOTA_EXCEEDED", info.GetReason())
	assert.Equal(t, "loadbalancer.kacho.cloud", info.GetDomain())
	assert.Equal(t, map[string]string{
		"carrier_type": "project",
		"carrier_id":   "prj-1",
		"kind":         "loadbalancer.networkloadbalancer",
		"limit":        "4",
		"used":         "4",
	}, info.GetMetadata(), "величины производителя обязаны доезжать до клиента")
}

// Полоса «потолок не назван» несёт носителя и вид, но не предел: его не назвали.
func TestQuotaRefusalNotProvisionedCarriesTheCarrierWithoutAmounts(t *testing.T) {
	const detail = `{"carrier_type": "project", "carrier_id": "prj-1", "kind": "loadbalancer.targetgroup"}`
	err := quotadetail.Attach(
		fmt.Errorf("%w: project prj-1 has no ceiling stated for loadbalancer.targetgroup",
			domain.ErrQuotaNotProvisioned), detail)

	out := MapDomainErr(err)

	st, _ := status.FromError(out)
	assert.Equal(t, codes.FailedPrecondition, st.Code())

	info := refusalMetadataInfo(t, out)
	assert.Equal(t, "QUOTA_NOT_PROVISIONED", info.GetReason())
	assert.Equal(t, "prj-1", info.GetMetadata()["carrier_id"])
	assert.NotContains(t, info.GetMetadata(), "limit",
		"предела нет — ключа быть не должно; ноль здесь означал бы названную величину")
}

// ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ к отрицанию выше: без величин отказ остаётся отказом —
// код, признак и текст на месте, метаданных просто нет.
func TestQuotaRefusalWithoutDetailStaysAValidRefusal(t *testing.T) {
	const producer = "project prj-1 has reached its limit of 4 loadbalancer.networkloadbalancer"
	err := fmt.Errorf("%w: %s", domain.ErrQuotaExceeded, producer)

	out := MapDomainErr(err)

	st, _ := status.FromError(out)
	assert.Equal(t, codes.ResourceExhausted, st.Code())
	assert.Contains(t, st.Message(), producer)

	info := refusalMetadataInfo(t, out)
	assert.Equal(t, "QUOTA_EXCEEDED", info.GetReason())
	assert.Empty(t, info.GetMetadata(), "величин не было — выдумывать их нечем")
}

// Обёртка величин не должна ломать распознавание sentinel'а ни на одном пути.
func TestQuotaDetailKeepsTheSentinelRecognisable(t *testing.T) {
	err := quotadetail.Attach(
		fmt.Errorf("%w: project prj-1 has reached its limit of 4 loadbalancer.networkloadbalancer",
			domain.ErrQuotaExceeded), quotaDetailExceeded)

	assert.True(t, errors.Is(err, domain.ErrQuotaExceeded))
}
