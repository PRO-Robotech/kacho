// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// quota_absent_authority_integration_test.go — объявленное отсутствие домена
// величин доезжает до ПУТИ ЗАПРОСА, а не только до тянущего.
//
// # Предмет
//
// Ручка домена величин принимает два законных значения: адрес соседа либо слово
// «не развёрнут». На втором процесс поднимается, тянущий не заводится — и это
// всё, что менялось до сих пор. Списывающий триггер оставался прежним и
// по-прежнему требовал строки учёта, поэтому КАЖДАЯ вставка считаемого вида в
// проекте без строк отвергалась «потолок не назван». Оператор читал одну строку
// журнала про тянущего и получал неработающие мутации.
//
// Задача `PRO-Robotech/kacho#2216`; приёмка
// `docs/specs/sub-phase-KAN-QUOTA-1-limit-authority-leaves-iam-acceptance.md`,
// решение `Д2` (что означает «домен величин не развёрнут») и `Д7` (предмет
// действует уже сегодня), сценарии `KAN-Q4-03` и `KAN-Q4-05`.
//
// # Почему у каждого утверждения здесь есть близнец, отличающийся ОДНИМ фактом
//
// «Мутация проходит» само по себе зеленело бы на триггере, снятом целиком.
// Различающая пара — два мира, отличающиеся ровно объявленным состоянием домена
// величин, — утверждает то, ради чего работа делается: отказ ПЕРЕСТАЛ наступать
// там, где потолок не назначаем никем, и ОСТАЛСЯ там, где его просто не
// назначили.
//
// # Почему счётчик проверяется отдельно
//
// Наивная починка («пусть производитель отказа молча возвращается») оставила бы
// вставку без списания: условный `UPDATE … AND used < limit_value` даёт ноль
// строк и когда строки нет, и когда она полна. На строке, заведённой прежде,
// потребление начало бы отставать от числа строк, которые оно считает, — и это
// стало бы видно ровно в тот момент, когда авторитет вернут. Поэтому при
// объявленном отсутствии списание становится БЕЗУСЛОВНЫМ, а не пропускается.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/corelib/quota"
	kachopg "github.com/PRO-Robotech/kacho/services/vpc/internal/repo/kacho/pg"
)

// declareAuthority объявляет состояние домена величин ТЕМ ЖЕ путём, которым его
// объявляет подъём процесса.
//
// Своего `UPDATE` здесь нет намеренно: фикстура, пишущая состояние сама, была бы
// снисходительнее продукта ровно там, где расхождение и опасно — в написании
// значения закрытого словаря.
func declareAuthority(t testing.TB, ctx context.Context, pool *pgxpool.Pool, state quota.AuthorityState) {
	t.Helper()
	proj, err := quota.NewPgProjection(pool, "kacho_vpc")
	require.NoError(t, err)
	require.NoError(t, proj.RecordAuthority(ctx, state))
}

// createNetworkRow вставляет сеть своей writer-транзакцией и отдаёт отказ как есть.
func createNetworkRow(t testing.TB, ctx context.Context, r *kachopg.Repository, project, name string) error {
	t.Helper()
	w, err := r.Writer(ctx)
	require.NoError(t, err)
	defer w.Abort()
	if _, err := w.Networks().Insert(ctx, newNetwork(project, name)); err != nil {
		return err
	}
	return w.Commit()
}

// TestQuota_KAN_Q4_03_AbsentAuthorityLetsTheMutationThrough — при объявленном
// отсутствии домена величин мутация проходит в проекте БЕЗ единой строки учёта.
//
// Повторов четыре, а не один: предмет в том, что не проходит НИКОГДА, и одна
// попытка этого от гонки не отличает.
func TestQuota_KAN_Q4_03_AbsentAuthorityLetsTheMutationThrough(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const project = "prj-quota-absent-pass"
	declareAuthority(t, ctx, pool, quota.AuthorityAbsent)

	for _, name := range []string{"net-a", "net-b", "net-c", "net-d"} {
		require.NoErrorf(t, createNetworkRow(t, ctx, r, project, name),
			"домен величин объявлен отсутствующим: потолок не назначаем никем, "+
				"и отказ обвинял бы арендатора в том, чего он исправить не может (%s)", name)
	}

	require.Equal(t, int64(-1), quotaUsed(t, ctx, pool, project, "vpc.network"),
		"строки учёта никто не заводил — её и не должно появиться")
}

// TestQuota_KAN_Q4_05_DeployedAuthorityStillRefuses — БЛИЗНЕЦ предыдущей,
// отличающийся ровно одним фактом: домен величин объявлен развёрнутым.
//
// Без него утверждение выше зеленело бы на триггере, снятом целиком.
func TestQuota_KAN_Q4_05_DeployedAuthorityStillRefuses(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const project = "prj-quota-absent-twin"
	declareAuthority(t, ctx, pool, quota.AuthorityPresent)

	err := createNetworkRow(t, ctx, r, project, "net-twin")
	require.Error(t, err, "домен величин развёрнут, а потолок не назначен — это отказ")
	require.ErrorContains(t, err, "has no ceiling stated for vpc.network",
		"тон отказа — часть контракта и остаётся прежним")
}

// TestQuota_AbsentAuthorityStillChargesTheExistingRow — счётчик продолжает
// считать, а не замирает.
//
// Мир отличается от первой пробы ОДНИМ фактом: строка учёта существует и полна.
// Утверждается не «мутация прошла» (это уже сказано выше), а то, что
// потребление не отстало от числа строк, которые оно считает: отставание
// невидимо, пока домена нет, и становится ложью ровно в день его возвращения.
func TestQuota_AbsentAuthorityStillChargesTheExistingRow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const project = "prj-quota-absent-charge"
	seedQuota(t, ctx, pool, project, "vpc.network", 1)
	declareAuthority(t, ctx, pool, quota.AuthorityAbsent)

	require.NoError(t, createNetworkRow(t, ctx, r, project, "net-first"))
	require.Equal(t, int64(1), quotaUsed(t, ctx, pool, project, "vpc.network"))

	// Строка полна. При отсутствующем домене потолок не действует — но счёт идёт.
	require.NoError(t, createNetworkRow(t, ctx, r, project, "net-over"),
		"потолок не действует, пока домен величин объявлен отсутствующим")
	require.Equal(t, int64(2), quotaUsed(t, ctx, pool, project, "vpc.network"),
		"списание обязано остаться безусловным, иначе потребление отстанет от факта")
}

// TestQuota_DeployedAuthorityRefusesTheFullRow — БЛИЗНЕЦ предыдущей, отличающийся
// ровно объявленным состоянием домена величин.
func TestQuota_DeployedAuthorityRefusesTheFullRow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const project = "prj-quota-deployed-full"
	seedQuota(t, ctx, pool, project, "vpc.network", 1)
	declareAuthority(t, ctx, pool, quota.AuthorityPresent)

	require.NoError(t, createNetworkRow(t, ctx, r, project, "net-first"))
	err := createNetworkRow(t, ctx, r, project, "net-over")
	require.Error(t, err, "домен величин развёрнут, строка полна — это отказ")
	require.ErrorContains(t, err, "has reached its limit of 1 vpc.network")
	require.Equal(t, int64(1), quotaUsed(t, ctx, pool, project, "vpc.network"),
		"отказ не списывает")
}

// TestQuota_UnknownAuthorityBehavesAsDeployed — третье состояние словаря
// («объявления ещё не было») ведёт себя как развёрнутый домен.
//
// Утверждение нужно затем, чтобы послабление не расползлось на состояние, о
// котором оператор ничего не объявлял: «неизвестно» не означает «потолков нет».
func TestQuota_UnknownAuthorityBehavesAsDeployed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	pool := quotaTestPool(t, ctx)
	r := kachopg.New(pool, nil)

	const project = "prj-quota-unknown"
	// Ни одного объявления: строка курсора стоит со значением по умолчанию.
	err := createNetworkRow(t, ctx, r, project, "net-unknown")
	require.Error(t, err, "объявления не было — послабление не наступает")
	require.ErrorContains(t, err, "has no ceiling stated for vpc.network")
}
