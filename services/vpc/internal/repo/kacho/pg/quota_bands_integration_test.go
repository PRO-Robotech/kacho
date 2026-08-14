// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	vpcrepo "github.com/PRO-Robotech/kacho/services/vpc/internal/repo"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
)

// Две полосы одного отказа обязаны говорить ОДНО И ТО ЖЕ.
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), DoD S2 п.3 и §7.4: «тексты и признаки обеих полос
// байт-идентичны, и это держится ГЕЙТОМ, а не вниманием».
//
// ПОЧЕМУ ГЕЙТ, А НЕ ВНИМАНИЕ. Полос две по построению: совещательная отвечает
// синхронно, до создания операции (арендатор видит 429 сразу), авторитетная
// живёт в триггере и решает исход. Написанные порознь, они разъезжаются на
// первой же правке текста — и арендатор получает на один и тот же отказ два
// разных сообщения в зависимости от того, какая полоса сработала первой. Что
// хуже, расхождение НЕ ВИДНО ни одной из сторон: каждая по отдельности верна.
//
// ЧЕМ ЭТО ЗАКРЫТО ЗДЕСЬ. Утверждение ниже сравнивает ПРИЗНАК и ТЕКСТ,
// полученные двумя РАЗНЫМИ путями исполнения — вставкой строки ресурса и
// совещательным вызовом — и требует побайтового совпадения текста. Способность
// упасть доказана инъекцией в обе стороны: развести тексты у производителя —
// проба краснеет и печатает оба значения; вернуть общего производителя — молчит.

// quotaBandFailure — то, что видит вызывающий: признак (sentinel, из которого
// service-слой выводит gRPC-код и reason-токен) и текст.
//
// Сравнивается именно ЭТО, а не сырой SQLSTATE: SQLSTATE — деталь производителя,
// до вызывающего он не доходит (`helpers.WrapPgErr` классифицирует его в
// sentinel первым же действием). Проба, утверждающая про SQLSTATE, говорила бы
// о слое, которого арендатор не видит, и осталась бы зелёной, разойдись
// полосы ровно там, где расхождение и наблюдаемо.
type quotaBandFailure struct {
	sentinel error
	message  string
}

// advisoryBand — совещательная полоса: чтение-классификация без записи.
func advisoryBand(t testing.TB, ctx context.Context, r *kachopg.Repository, project, kind string) quotaBandFailure {
	t.Helper()
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	defer func() { _ = rd.Close() }()
	return asBandFailure(t, rd.Quotas().Admit(ctx, vpcrepo.QuotaCarrierProject, project, kind))
}

// authoritativeBand — авторитетная полоса: та же классификация, но как побочный
// исход вставки строки ресурса.
func authoritativeBand(t testing.TB, ctx context.Context, r *kachopg.Repository, project, name string) quotaBandFailure {
	t.Helper()
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	_, ierr := w.Networks().Insert(ctx, newNetwork(project, name))
	return asBandFailure(t, ierr)
}

// asBandFailure достаёт из ошибки то, что является контрактом: признак и текст.
// Ошибка обязана БЫТЬ — полоса, не отказавшая там, где отказ ожидается, не даёт
// материала для сравнения и роняет пробу с этим прямо названным.
func asBandFailure(t testing.TB, err error) quotaBandFailure {
	t.Helper()
	require.Error(t, err, "полоса обязана отказать: без отказа сравнивать нечего")
	switch {
	case errors.Is(err, vpcrepo.ErrQuotaExceeded):
		return quotaBandFailure{sentinel: vpcrepo.ErrQuotaExceeded, message: err.Error()}
	case errors.Is(err, vpcrepo.ErrQuotaNotProvisioned):
		return quotaBandFailure{sentinel: vpcrepo.ErrQuotaNotProvisioned, message: err.Error()}
	}
	t.Fatalf("отказ учёта обязан приезжать признаком учёта, а не общим отказом хранилища: %v", err)
	return quotaBandFailure{}
}

// TestQuota_BothBandsAreByteIdentical_NotProvisioned — «потолок не назван».
func TestQuota_BothBandsAreByteIdentical_NotProvisioned(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const project = "prj-bands-unprovisioned"

	advisory := advisoryBand(t, ctx, r, project, "vpc.network")
	authoritative := authoritativeBand(t, ctx, r, project, "net-bands-unprov")

	assert.Equal(t, vpcrepo.ErrQuotaNotProvisioned, advisory.sentinel,
		"совещательная полоса обязана назвать «потолок не назван»")
	assert.Equal(t, authoritative.sentinel, advisory.sentinel, "признак обеих полос обязан совпадать")
	assert.Equal(t, authoritative.message, advisory.message,
		"текст обеих полос обязан совпадать ПОБАЙТОВО: он часть контракта, а не диагностика")
}

// TestQuota_BothBandsAreByteIdentical_Exceeded — «место кончилось».
//
// Положительный контроль к предыдущей пробе: без него совпадение текстов могло
// бы держаться на том, что обе полосы одинаково молчат о ВТОРОМ исходе.
func TestQuota_BothBandsAreByteIdentical_Exceeded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const project = "prj-bands-exhausted"
	seedQuota(t, ctx, pool, project, "vpc.network", 1)

	// Занимаем единственное место — до этой вставки обе полосы обязаны молчать.
	rd, err := r.Reader(ctx)
	require.NoError(t, err)
	require.NoError(t, rd.Quotas().Admit(ctx, vpcrepo.QuotaCarrierProject, project, "vpc.network"),
		"положительный контроль: пока место есть, совещательная полоса пропускает")
	require.NoError(t, rd.Close())

	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	_, err = w.Networks().Insert(ctx, newNetwork(project, "net-bands-first"))
	require.NoError(t, err)
	require.NoError(t, w.Commit())

	advisory := advisoryBand(t, ctx, r, project, "vpc.network")
	authoritative := authoritativeBand(t, ctx, r, project, "net-bands-second")

	assert.Equal(t, vpcrepo.ErrQuotaExceeded, advisory.sentinel,
		"совещательная полоса обязана назвать «место кончилось»")
	assert.Equal(t, authoritative.sentinel, advisory.sentinel, "признак обеих полос обязан совпадать")
	assert.Equal(t, authoritative.message, advisory.message,
		"текст обеих полос обязан совпадать ПОБАЙТОВО")
}
