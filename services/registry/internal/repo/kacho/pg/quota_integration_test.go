// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/registry/internal/domain"
	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
	kachopg "github.com/PRO-Robotech/kacho/services/registry/internal/repo/kacho/pg"
)

// Учёт числа ресурсов у kacho-registry: списание, возврат, два исхода отказа и
// ось вложенности «репозиториев в одном реестре».
//
// Приёмка `docs/specs/sub-phase-quota-v2-materialised-usage-acceptance.md`
// (APPROVED, раунд 2), DoD S4 п.1, сценарии QV2-11/12/14/30/31/32.
//
// # Почему пробы этого файла заводят строки учёта САМИ
//
// Их предмет — поведение при заведённой и при ОТСУТСТВУЮЩЕЙ строке. Заведи им
// строку общая фикстура пакета, они утверждали бы про состояние, которого не
// создавали, а исход «потолок не назван» стал бы невыразимым. Поэтому их
// идентичности (`prj-regq-*`) в перечень фикстуры не входят намеренно.

// seedRegQuota заводит одну строку учёта ТЕМ ЖЕ оператором, что и продукт.
func seedRegQuota(t testing.TB, pool *pgxpool.Pool, carrierType, carrierID, kind string, limit int64) {
	t.Helper()
	n, err := kachopg.MaterializeQuotas(context.Background(), pool, []kachopg.QuotaRow{{
		CarrierType:   carrierType,
		CarrierID:     carrierID,
		Kind:          kind,
		Limit:         limit,
		SourceScope:   "DEFAULT",
		LimitRevision: 0,
		AccountID:     "acc-regq",
	}})
	require.NoError(t, err)
	require.Equal(t, int64(1), n, "перепись: заведена ровно одна строка учёта")
}

// seedRegNestedDefault заводит проектный резолв вложенного вида.
func seedRegNestedDefault(t testing.TB, pool *pgxpool.Pool, projectID, kind string, limit int64) {
	t.Helper()
	n, err := kachopg.MaterializeNestedDefaults(context.Background(), pool, []kachopg.QuotaRow{{
		CarrierID:     projectID,
		Kind:          kind,
		Limit:         limit,
		SourceScope:   "DEFAULT",
		LimitRevision: 0,
		AccountID:     "acc-regq",
	}})
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
}

// regQuotaUsed читает потребление. Отсутствие строки — отдельный исход, а не
// ноль: ноль означает «строка есть и пуста».
func regQuotaUsed(t testing.TB, pool *pgxpool.Pool, carrierType, carrierID, kind string) (int64, bool) {
	t.Helper()
	var used int64
	err := pool.QueryRow(context.Background(),
		`SELECT used FROM kacho_registry.project_resource_quotas
		  WHERE carrier_type = $1 AND carrier_id = $2 AND kind = $3`,
		carrierType, carrierID, kind).Scan(&used)
	if err != nil {
		require.ErrorIs(t, err, pgx.ErrNoRows)
		return 0, false
	}
	return used, true
}

// pgErrCode достаёт SQLSTATE. Отсутствие кода — отдельный исход, а не пустая
// строка: без этого проба, получившая сетевую ошибку вместо отказа учёта,
// сравнивала бы пустое с пустым и зеленела.
func pgErrCode(t testing.TB, err error) string {
	t.Helper()
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr, "ожидался отказ базы, получено: %v", err)
	return pgErr.Code
}

// insertRepoRegistration заводит строку регистрации репозитория напрямую — тем
// же оператором, что продукт (`applyRepoRegistration`), но без окружающей эмиссии
// интента: предмет этих проб — учёт, а не очередь.
func insertRepoRegistration(ctx context.Context, pool *pgxpool.Pool, registryID, repo string) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO kacho_registry.registry_repository_registration (registry_id, repo)
		 VALUES ($1, $2) ON CONFLICT (registry_id, repo) DO NOTHING`,
		registryID, repo)
	return err
}

// TestQuota_REG_NotProvisionedIsRefusal — «не сказано» = ОТКАЗ, а не «без предела».
func TestQuota_REG_NotProvisionedIsRefusal(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRegistryRepo(pool)
	ctx := context.Background()

	const project = "prj-regq-noceiling"
	noCeiling := newReg(project, "reg-no-ceiling", nil)
	_, _, err := repo.Insert(ctx, noCeiling, regIntent(noCeiling))
	require.Error(t, err, "вставка без строки учёта обязана быть отвергнута")
	assert.ErrorIs(t, err, regerrors.ErrQuotaNotProvisioned,
		"исход обязан быть отличим от исчерпания: %v", err)

	// Положительный контроль: тот же путь при заведённой строке проходит.
	pool2 := setupTestDB(t)
	repo2 := kachopg.NewRegistryRepo(pool2)
	const okProject = "prj-regq-ceiling"
	seedRegQuota(t, pool2, "project", okProject, "registry.registries", 4)

	okReg := newReg(okProject, "reg-ok", nil)
	_, _, err = repo2.Insert(ctx, okReg, regIntent(okReg))
	require.NoError(t, err)

	used, ok := regQuotaUsed(t, pool2, "project", okProject, "registry.registries")
	require.True(t, ok)
	assert.Equal(t, int64(1), used, "вставка списывает ровно одно место")
}

// TestQuota_REG_ExceededAndRefund — исчерпание отвергает, удаление возвращает.
func TestQuota_REG_ExceededAndRefund(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRegistryRepo(pool)
	ctx := context.Background()

	const project = "prj-regq-exhaust"
	const kind = "registry.registries"
	seedRegQuota(t, pool, "project", project, kind, 1)

	first := newReg(project, "reg-first", nil)
	_, _, err := repo.Insert(ctx, first, regIntent(first))
	require.NoError(t, err)

	second := newReg(project, "reg-second", nil)
	_, _, err = repo.Insert(ctx, second, regIntent(second))
	require.Error(t, err, "второй реестр при пределе в один обязан быть отвергнут")
	assert.ErrorIs(t, err, regerrors.ErrQuotaExceeded, "got %v", err)

	used, ok := regQuotaUsed(t, pool, "project", project, kind)
	require.True(t, ok)
	assert.Equal(t, int64(1), used, "отвергнутая вставка места НЕ занимает")

	require.NoError(t, repo.Delete(ctx, first.ID, domain.RegisterIntent{}))
	used, ok = regQuotaUsed(t, pool, "project", project, kind)
	require.True(t, ok)
	assert.Equal(t, int64(0), used, "удаление возвращает место")

	// Положительный контроль: освободившееся место снова занимается.
	third := newReg(project, "reg-third", nil)
	_, _, err = repo.Insert(ctx, third, regIntent(third))
	require.NoError(t, err)
}

// TestQuota_REG_NestedCarrierIsTheParent — предел вложенности принадлежит
// РОДИТЕЛЮ, а не проекту (QV2-30).
func TestQuota_REG_NestedCarrierIsTheParent(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRegistryRepo(pool)
	ctx := context.Background()

	const project = "prj-regq-nested"
	const nested = "registry.registries.repositories"
	seedRegQuota(t, pool, "project", project, "registry.registries", 8)
	seedRegQuota(t, pool, "project", project, "registry.repositories", 32)
	seedRegNestedDefault(t, pool, project, nested, 8)

	reg1 := newReg(project, "reg-one", nil)
	reg2 := newReg(project, "reg-two", nil)
	_, _, err := repo.Insert(ctx, reg1, regIntent(reg1))
	require.NoError(t, err)
	_, _, err = repo.Insert(ctx, reg2, regIntent(reg2))
	require.NoError(t, err)

	// Строка учёта родителя заводится ТОЙ ЖЕ транзакцией, что сам родитель.
	used, ok := regQuotaUsed(t, pool, nested, reg1.ID, nested)
	require.True(t, ok, "строка учёта родителя обязана появиться вместе с родителем")
	assert.Equal(t, int64(0), used)

	// Понижаем предел ровно этого родителя до одного репозитория.
	tag, err := pool.Exec(ctx,
		`UPDATE kacho_registry.project_resource_quotas SET limit_value = 1
		  WHERE carrier_type = $1 AND carrier_id = $2 AND kind = $1`, nested, reg1.ID)
	require.NoError(t, err, "понижение предела — штатное административное действие")
	require.Equal(t, int64(1), tag.RowsAffected())

	require.NoError(t, insertRepoRegistration(ctx, pool, reg1.ID, "app/one"))

	err = insertRepoRegistration(ctx, pool, reg1.ID, "app/two")
	require.Error(t, err, "второй репозиторий в исчерпанном реестре отвергается")
	// Здесь утверждается SQLSTATE, а не sentinel: эта вставка идёт напрямую в
	// базу, минуя классификатор репозитория, поэтому sentinel'у взяться неоткуда.
	// Отображение SQLSTATE → sentinel утверждают пробы выше, идущие через
	// `repo.Insert`; здесь предметом остаётся то, что ОТКАЗАЛ носитель-родитель.
	assert.Equal(t, "KQ001", pgErrCode(t, err),
		"исчерпание родителя приезжает своим SQLSTATE: %v", err)

	// Положительный контроль, доказывающий, что предел на РОДИТЕЛЯ: у соседнего
	// реестра того же проекта место есть.
	require.NoError(t, insertRepoRegistration(ctx, pool, reg2.ID, "app/three"))

	// Второй положительный контроль: проектная ось считает обоих вместе.
	used, ok = regQuotaUsed(t, pool, "project", project, "registry.repositories")
	require.True(t, ok)
	assert.Equal(t, int64(2), used, "проектная ось считает репозитории обоих реестров")
}

// TestQuota_REG_CascadeRefundsWithoutItsParent — удаление реестра каскадом
// снимает его репозитории, и возврат проектного места происходит ПОСЛЕ того, как
// строка родителя исчезла.
//
// Ради этой пробы проект денормализован НА СТРОКУ репозитория: спроси триггер
// возврата проект у реестра — он не нашёл бы ничего, потому что каскадное
// удаление детей идёт уже после удаления родителя. Счётчик проекта остался бы
// завышенным навсегда, и снаружи это неотличимо от исправной работы.
func TestQuota_REG_CascadeRefundsWithoutItsParent(t *testing.T) {
	pool := setupTestDB(t)
	repo := kachopg.NewRegistryRepo(pool)
	ctx := context.Background()

	const project = "prj-regq-cascade"
	const nested = "registry.registries.repositories"
	seedRegQuota(t, pool, "project", project, "registry.registries", 4)
	seedRegQuota(t, pool, "project", project, "registry.repositories", 32)
	seedRegNestedDefault(t, pool, project, nested, 8)

	reg := newReg(project, "reg-cascade", nil)
	_, _, err := repo.Insert(ctx, reg, regIntent(reg))
	require.NoError(t, err)

	require.NoError(t, insertRepoRegistration(ctx, pool, reg.ID, "app/a"))
	require.NoError(t, insertRepoRegistration(ctx, pool, reg.ID, "app/b"))

	used, ok := regQuotaUsed(t, pool, "project", project, "registry.repositories")
	require.True(t, ok)
	require.Equal(t, int64(2), used, "предусловие: оба репозитория списаны")

	require.NoError(t, repo.Delete(ctx, reg.ID, domain.RegisterIntent{}))

	// Возврат ОБОИХ репозиториев состоялся, хотя их родителя уже нет.
	used, ok = regQuotaUsed(t, pool, "project", project, "registry.repositories")
	require.True(t, ok)
	assert.Equal(t, int64(0), used,
		"каскад вернул место проекта: проект прочитан со строки ребёнка, а не у снятого родителя")

	// Строк учёта снятого родителя не остаётся (QV2-32).
	_, ok = regQuotaUsed(t, pool, nested, reg.ID, nested)
	assert.False(t, ok, "строк учёта снятого родителя не остаётся")

	// Положительный контроль: место реестра тоже вернулось.
	used, ok = regQuotaUsed(t, pool, "project", project, "registry.registries")
	require.True(t, ok)
	assert.Equal(t, int64(0), used)
}

// regIntent — интент регистрации для проб учёта.
//
// Строится НАСТОЯЩИМ конструктором домена, а не выдумывается полями: подставной
// интент, принимающий больше настоящего, скрыл бы ровно тот отказ, ради которого
// его подставляют (`testing.md` §«дублёр, принимающий больше настоящего»).
// Предмет этих проб — учёт, но путь до него обязан оставаться живым.
func regIntent(r *domain.Registry) domain.RegisterIntent {
	return domain.RegisterIntentForCreate(r, "user", "usr-quota-probe")
}
