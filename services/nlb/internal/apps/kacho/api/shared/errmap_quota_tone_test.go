// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package shared

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/kacho/services/nlb/internal/domain"
)

// Тон отказа по пределу — часть контракта, и он один на всех владельцев учёта.
//
// ПРЕДМЕТ (задача продукта #1658). Отказ производит ОДИН производитель на
// платформу (`pkg/quota/refusal.sql.tmpl`): он называет носителя, предел и вид
// одним предложением. Мост SQLSTATE→sentinel оборачивает это предложение
// sentinel'ом своего домена, а мапперу наружу положено префикс снять — иначе
// клиент, ключующийся на текст, читает у шести владельцев два разных продукта.
//
// ПОЧЕМУ РАВЕНСТВО, А НЕ ВХОЖДЕНИЕ. Вхождение зеленеет на любом префиксе:
// именно им и была записана прежняя редакция соседней пробы, пока расхождение
// стояло предметом отдельной задачи. Утверждать надо ТО, что видит клиент,
// целиком.

// producerExceeded / producerNotProvisioned — дословно то, что печатает
// производитель на своих двух полосах (`KQ001` / `KQ002`).
const (
	producerExceeded       = "project prj-1 has reached its limit of 4 loadbalancer.networkloadbalancer"
	producerNotProvisioned = "project prj-1 has no ceiling stated for loadbalancer.listener"
)

func TestQuotaRefusalTextReachesTheClientWithoutTheSentinelPrefix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		err      error
		wantCode codes.Code
		wantText string
	}{
		{
			name:     "место кончилось",
			err:      fmt.Errorf("%w: %s", domain.ErrQuotaExceeded, producerExceeded),
			wantCode: codes.ResourceExhausted,
			wantText: producerExceeded,
		},
		{
			name:     "предел не назван",
			err:      fmt.Errorf("%w: %s", domain.ErrQuotaNotProvisioned, producerNotProvisioned),
			wantCode: codes.FailedPrecondition,
			wantText: producerNotProvisioned,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			st, ok := status.FromError(MapDomainErr(tc.err))
			require.True(t, ok, "отказ обязан быть gRPC-статусом")
			assert.Equal(t, tc.wantCode, st.Code())
			// ПОЛОЖИТЕЛЬНАЯ ПОЛОВИНА: предложение производителя доехало целиком.
			// Без неё «префикса нет» зеленело бы и на пустом сообщении.
			assert.Equal(t, tc.wantText, st.Message(),
				"клиент обязан видеть предложение производителя и ничего сверх него")
			// ОТРИЦАТЕЛЬНАЯ ПОЛОВИНА, названная своими словами: имя sentinel'а —
			// внутреннее, наружу оно не выходит ни у одного из шести владельцев.
			assert.NotContains(t, st.Message(), domain.ErrQuotaExceeded.Error()+":")
			assert.NotContains(t, st.Message(), domain.ErrQuotaNotProvisioned.Error()+":")
		})
	}
}

// Отрицание выше обязано уметь покраснеть: sentinel, обёрнутый ВОКРУГ отказа
// учёта дважды, всё равно доезжает одним предложением.
func TestQuotaRefusalToneSurvivesDoubleWrapping(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("%w: %s", domain.ErrQuotaExceeded, producerExceeded)
	st, ok := status.FromError(MapDomainErr(err))
	require.True(t, ok)
	require.NotEmpty(t, st.Message(), "пустое сообщение обесценивает отрицание")
	assert.Equal(t, producerExceeded, st.Message())
}

// Отказ, не относящийся к учёту, тоном учёта не становится — законный близнец
// для обеих проб выше.
func TestNonQuotaRefusalKeepsItsOwnTone(t *testing.T) {
	t.Parallel()
	st, ok := status.FromError(MapDomainErr(
		fmt.Errorf("%w: NetworkLoadBalancer nlb-x not found", domain.ErrNotFound)))
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	assert.Equal(t, "NetworkLoadBalancer nlb-x not found", st.Message())
}

// Внутренний отказ наружу текста не выносит: `security.md` §Hardening п.1 —
// `INTERNAL` никогда не эхает `err.Error()`.
func TestInternalRefusalCarriesNoProducerText(t *testing.T) {
	t.Parallel()
	raw := errors.New(`pq: function kacho_nlb.kacho_quota_refuse(text) does not exist`)
	st, ok := status.FromError(MapDomainErr(
		fmt.Errorf("%w: quota accounting: %v", domain.ErrInternal, raw)))
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "internal database error", st.Message())
	assert.NotContains(t, st.Message(), "kacho_nlb")
}
