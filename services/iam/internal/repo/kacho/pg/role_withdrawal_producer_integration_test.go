// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// role_withdrawal_producer_integration_test.go — ПРОИЗВОДИТЕЛЬ ОТЗЫВА роли
// модуля целиком доводится до настоящего Postgres.
//
// Приёмка `services/iam/docs/engineering/acceptance/role-withdrawal-has-a-producer.md`
// (APPROVED круга 4); сценарии IAM-RW-1-01, -04, -05, -06, -08, -09, -13, -14,
// -24, -29, -30. Задача продукта #1913.
//
// # Почему манифест здесь ЗАГРУЖАЕТСЯ ИЗ ТЕКСТА, а не собирается литералом
//
// Состояний у раздела `roles:` ТРИ, и различает их присутствие самого ключа в
// документе. Литерал структуры этого различия не несёт by construction: поле
// неэкспортируемое, и всякий собранный литерал объявляет раздел неназванным.
// Проба, построенная на литералах, зеленела бы на применителе, не снимающем
// НИКОГДА, — то есть ровно на дефекте, ради которого она написана.
//
// # Что здесь утверждается — ИСХОД, а не вызов
//
// Ни одно утверждение ниже не спрашивает «позвали ли сверку». Каждое читает
// СТРОКИ, оставшиеся в базе, и сверяет их с объявленным.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/services/iam/internal/apps/kacho/moduleroles"
	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
	"github.com/PRO-Robotech/kacho/services/iam/internal/manifest"
)

// withdrawalManifest — документ модуля с ПЕРЕЧИСЛЕННЫМИ ролями. Раздел `roles:`
// объявлен всегда; пустым его делает пустой перечень, а не отсутствие ключа.
func withdrawalManifest(t *testing.T, module string, roleIDs ...string) *manifest.Manifest {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "apiVersion: iam/v1\nmodule: %s\nroles:\n", module)
	if len(roleIDs) == 0 {
		// Пустой перечень записывается ЯВНО: без него ключ `roles:` имеет
		// значение null, и загрузчик прочитал бы раздел объявленным с нулём
		// элементов — то самое, что проба и утверждает, но добытое случайно.
		b.WriteString("  []\n")
	}
	for _, id := range roleIDs {
		owner := strings.SplitN(id, ".", 2)[0]
		fmt.Fprintf(&b, `  - id: %s
    description: "Admin probe role for withdrawal"
    tier:
      tierType: iam.cluster
      tierId: cluster_kacho_root
    rules:
      - module: %s
        resources: [%s]
        classes: ["get"]
`, id, owner, probeResourceOf(owner))
	}
	m, err := manifest.Load([]byte(b.String()))
	require.NoErrorf(t, err, "манифест пробы не загрузился:\n%s", b.String())
	require.Truef(t, m.RolesDeclared(),
		"раздел roles обязан быть ОБЪЯВЛЕН — иначе проба утверждает про другое состояние")
	return m
}

// probeResourceOf — ЖИВОЙ ресурс названного модуля.
//
// Выписан по одному на модуль намеренно: правило роли ссылается на строку
// каталога ключом, и ресурс чужого модуля отвергается раньше, чем проба дойдёт
// до своего предмета, — красное пришло бы от соседа. Перечень мал и меняется
// вместе с каталогом; незнакомый модуль даёт пустую строку, и манифест тогда не
// загрузится, назвав предмет.
func probeResourceOf(module string) string {
	switch module {
	case "vpc":
		return "network"
	case "compute":
		return "instance"
	default:
		return ""
	}
}

// withdrawalManifestWithoutSection — документ БЕЗ ключа `roles:` вовсе. Третье
// состояние раздела, и его надо строить отдельно: длина перечня его не
// отличает.
func withdrawalManifestWithoutSection(t *testing.T, module string) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Load([]byte(fmt.Sprintf("apiVersion: iam/v1\nmodule: %s\n", module)))
	require.NoError(t, err, "манифест без раздела ролей обязан быть законным документом")
	require.False(t, m.RolesDeclared(),
		"раздел roles обязан быть НЕ объявлен — иначе проба утверждает про другое состояние")
	return m
}

// roleLiveness — живость строки роли и её пометка снятия.
func roleLiveness(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id domain.RoleID) (
	live bool, reason, by string,
) {
	t.Helper()
	var r, b *string
	require.NoErrorf(t, pool.QueryRow(ctx,
		`SELECT live, retired_reason, retired_by FROM roles WHERE id = $1`, string(id)).
		Scan(&live, &r, &b), "строки роли %s нет вовсе", id)
	if r != nil {
		reason = *r
	}
	if b != nil {
		by = *b
	}
	return live, reason, by
}

// TestIAMRW1TheProducerRetiresWhatTheManifestStoppedDeclaring — IAM-RW-1-01,
// -05, -09.
//
// Роль, убранная из объявленного раздела, помечается снятой; соседняя роль того
// же модуля и роль ЧУЖОГО модуля не тронуты — оба положительных контроля
// обязательны: без них «не тронута» зеленело бы на применении, не тронувшем
// ничего.
func TestIAMRW1TheProducerRetiresWhatTheManifestStoppedDeclaring(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx, pool, applier := applierOnLiveBase(t)

	const (
		goneID  = "vpc.rwgone.admin"
		stayID  = "vpc.rwstay.admin"
		otherID = "compute.rwother.admin"
	)
	gone := domain.SystemRoleID(domain.RoleName(goneID))
	stay := domain.SystemRoleID(domain.RoleName(stayID))
	other := domain.SystemRoleID(domain.RoleName(otherID))

	rep, err := applier.Apply(ctx, withdrawalManifest(t, "vpc", goneID, stayID),
		moduleroles.BootActorID)
	require.NoError(t, err, "объявленные роли обязаны примениться: %s", rep)
	require.Equal(t, 2, rep.Written, "обе роли обязаны лечь: %s", rep)

	repOther, err := applier.Apply(ctx, withdrawalManifest(t, "compute", otherID),
		moduleroles.BootActorID)
	require.NoError(t, err, "роль чужого модуля обязана лечь: %s", repOther)

	// ── применение, где одной роли больше нет ────────────────────────────────
	rep, err = applier.Apply(ctx, withdrawalManifest(t, "vpc", stayID), moduleroles.BootActorID)
	require.NoError(t, err, "применение обязано пройти, а не отказать: %s", rep)

	assert.Equal(t, 1, rep.Retired, "снятой обязана быть ровно одна роль: %s", rep)
	assert.Equal(t, []string{goneID}, rep.RetiredNames,
		"перепись обязана называть снятое ПОИМЁННО, а не одним счётчиком: %s", rep)

	live, reason, by := roleLiveness(t, ctx, pool, gone)
	assert.False(t, live, "роль, убранная из раздела, обязана быть помечена снятой")
	assert.NotEmpty(t, reason, "причина снятия обязана быть непуста: без неё «отобрали» "+
		"неотличимо от «сломалось»")
	assert.Equal(t, moduleroles.BootActorID, by, "автор снятия обязан быть назван")

	// Положительный контроль номер один: соседняя роль ТОГО ЖЕ модуля жива.
	liveStay, _, _ := roleLiveness(t, ctx, pool, stay)
	assert.True(t, liveStay, "объявленная роль того же модуля снятой быть не должна")

	// Положительный контроль номер два: роль ЧУЖОГО модуля жива.
	liveOther, _, _ := roleLiveness(t, ctx, pool, other)
	assert.True(t, liveOther, "роль чужого модуля этот манифест не сверяет и не снимает")

	// Строка НЕ удалена: форма отзыва — пометка.
	assert.Equal(t, 1, countRoleRows(t, ctx, pool, gone),
		"строка снятой роли обязана ОСТАТЬСЯ: удаление унесло бы ведомости каскадом")
	assert.Equal(t, 0, countRuleRefs(t, ctx, pool, gone),
		"проекция сегментов снятой роли обязана уйти — иначе право продолжает действовать")
}

// TestIAMRW1TheSectionsThreeStatesAreDistinguished — IAM-RW-1-06 и -24.
//
// Раздел не объявлен — не снимается НИ ОДНА роль. Раздел объявлен пустым —
// снимаются ВСЕ роли модуля. Второе есть положительный близнец первого: без него
// первое утверждение зеленело бы на применителе, не снимающем никогда.
func TestIAMRW1TheSectionsThreeStatesAreDistinguished(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx, pool, applier := applierOnLiveBase(t)

	const aID, bID = "vpc.rwthreea.admin", "vpc.rwthreeb.admin"
	a := domain.SystemRoleID(domain.RoleName(aID))
	b := domain.SystemRoleID(domain.RoleName(bID))

	rep, err := applier.Apply(ctx, withdrawalManifest(t, "vpc", aID, bID), moduleroles.BootActorID)
	require.NoError(t, err, "обе роли обязаны лечь: %s", rep)
	require.Equal(t, 2, rep.Written)

	// ── раздел НЕ объявлен: снятий ноль ──────────────────────────────────────
	rep, err = applier.Apply(ctx, withdrawalManifestWithoutSection(t, "vpc"),
		moduleroles.BootActorID)
	require.NoError(t, err, "манифест без раздела ролей обязан примениться: %s", rep)
	assert.Equal(t, 0, rep.Retired,
		"раздел не объявлен — «сверять не с чем», снятий быть не может: %s", rep)
	liveA, _, _ := roleLiveness(t, ctx, pool, a)
	liveB, _, _ := roleLiveness(t, ctx, pool, b)
	assert.True(t, liveA && liveB,
		"манифест, потерявший ключ roles, снял бы ВСЕ роли модуля — от этого и защищает "+
			"различение трёх состояний")

	// ── раздел объявлен ПУСТЫМ: снимаются ВСЕ ────────────────────────────────
	rep, err = applier.Apply(ctx, withdrawalManifest(t, "vpc"), moduleroles.BootActorID)
	require.NoError(t, err, "пустой раздел — законное объявление, а не ошибка: %s", rep)
	assert.Equal(t, 2, rep.Retired,
		"«ролей у модуля нет» обязано снять ВСЕ его роли: %s", rep)
	liveA, _, _ = roleLiveness(t, ctx, pool, a)
	liveB, _, _ = roleLiveness(t, ctx, pool, b)
	assert.False(t, liveA || liveB, "обе роли обязаны быть помечены снятыми")
}

// TestIAMRW1PlatformRolesAreNeverRetired — IAM-RW-1-08.
//
// Платформенные роли (`owner_module IS NULL`) не снимаются НИ ПРИ КАКОМ
// манифесте, в том числе когда манифест НАЗЫВАЕТ их имя. Положительный контроль
// — роль модуля снимается тем же применением.
func TestIAMRW1PlatformRolesAreNeverRetired(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx, pool, applier := applierOnLiveBase(t)

	const roleID = "vpc.rwplatform.admin"
	id := domain.SystemRoleID(domain.RoleName(roleID))

	rep, err := applier.Apply(ctx, withdrawalManifest(t, "vpc", roleID), moduleroles.BootActorID)
	require.NoError(t, err, "роль модуля обязана лечь: %s", rep)

	var platformBefore int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM roles WHERE owner_module IS NULL AND is_system AND live`).
		Scan(&platformBefore))
	require.Positive(t, platformBefore,
		"платформенных ролей ноль — отрицание ниже зеленело бы на пустом множестве")

	rep, err = applier.Apply(ctx, withdrawalManifest(t, "vpc"), moduleroles.BootActorID)
	require.NoError(t, err, "применение обязано пройти: %s", rep)

	// Положительный контроль: роль модуля СНЯТА этим же применением.
	live, _, _ := roleLiveness(t, ctx, pool, id)
	require.False(t, live,
		"роль модуля обязана быть снята — иначе следующее утверждение зеленеет на "+
			"применении, не тронувшем ничего")

	var platformAfter int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM roles WHERE owner_module IS NULL AND is_system AND live`).
		Scan(&platformAfter))
	assert.Equal(t, platformBefore, platformAfter,
		"ни одна платформенная роль не может быть снята манифестом модуля")
	t.Logf("перепись: живых платформенных ролей до %d, после %d", platformBefore, platformAfter)
}

// TestIAMRW1RevivalReturnsTheSameRowAndClearsItsOwnLedger — IAM-RW-1-04, -14,
// -29, -30.
//
// Оживление возвращает ТУ ЖЕ строку с тем же `id`; выдачи целы; ведомость
// отвечает ПОСЛЕДНЕЙ причиной и при оживлении теряет строки СВОЕЙ причины, не
// трогая чужих.
func TestIAMRW1RevivalReturnsTheSameRowAndClearsItsOwnLedger(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx, pool, applier := applierOnLiveBase(t)

	const roleID = "vpc.rwrevive.admin"
	id := domain.SystemRoleID(domain.RoleName(roleID))
	declared := withdrawalManifest(t, "vpc", roleID)

	rep, err := applier.Apply(ctx, declared, moduleroles.BootActorID)
	require.NoError(t, err, "роль обязана лечь: %s", rep)

	// Чужая строка ведомости — причина «снят каталог». Кладётся до снятия, и
	// оживление обязано её ОСТАВИТЬ.
	_, err = pool.Exec(ctx, `
		INSERT INTO role_grant_orphan (role_id, object_type, verb, source, cause, reason)
		VALUES ($1, 'vpc.network', 'get', 'rule_ref', 'catalog_retired', 'чужая причина')`,
		string(id))
	require.NoError(t, err, "фикстура чужой причины не легла")

	// ── первое снятие ────────────────────────────────────────────────────────
	rep, err = applier.Apply(ctx, withdrawalManifest(t, "vpc"), "actor-one")
	require.NoError(t, err, "снятие обязано пройти: %s", rep)
	require.Equal(t, 1, rep.Retired)

	_, reasonOne, byOne := roleLiveness(t, ctx, pool, id)
	require.Equal(t, "actor-one", byOne)

	// ── оживление: ТА ЖЕ строка ──────────────────────────────────────────────
	rep, err = applier.Apply(ctx, declared, moduleroles.BootActorID)
	require.NoError(t, err, "возврат объявления обязан оживить роль: %s", rep)
	live, reason, by := roleLiveness(t, ctx, pool, id)
	assert.True(t, live, "оживление обязано вернуть живость")
	assert.Empty(t, reason, "у оживлённой роли причины снятия быть не может")
	assert.Empty(t, by, "у оживлённой роли автора снятия быть не может")
	assert.Equal(t, 1, countRoleRows(t, ctx, pool, id),
		"строк с этим именем обязана остаться ОДНА: id выводится из имени")

	// IAM-RW-1-30: ведомость СВОЕЙ причины очищена, чужая — цела.
	var own, foreign int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE cause = 'role_retired'),
		        count(*) FILTER (WHERE cause = 'catalog_retired')
		   FROM role_grant_orphan WHERE role_id = $1`, string(id)).Scan(&own, &foreign))
	assert.Equal(t, 0, own, "оживление обязано снять строки СВОЕЙ причины")
	assert.Positive(t, foreign,
		"строки причины «снят каталог» обязаны остаться — без этого близнеца "+
			"«очистили» неотличимо от «снесли ведомость целиком»")

	// ── второе снятие: ведомость отвечает ПОСЛЕДНЕЙ причиной (IAM-RW-1-29) ───
	rep, err = applier.Apply(ctx, withdrawalManifest(t, "vpc"), "actor-two")
	require.NoError(t, err, "второе снятие обязано пройти: %s", rep)

	_, reasonTwo, byTwo := roleLiveness(t, ctx, pool, id)
	assert.Equal(t, "actor-two", byTwo, "пометка обязана отвечать ПОСЛЕДНИМ автором")
	assert.NotEmpty(t, reasonTwo)
	t.Logf("перепись: причина первого снятия %q, второго %q", reasonOne, reasonTwo)

	var lastBy string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT DISTINCT applied_by FROM role_grant_orphan
		  WHERE role_id = $1 AND cause = 'role_retired'`, string(id)).Scan(&lastBy))
	assert.Equal(t, "actor-two", lastBy,
		"ведомость обязана отвечать ПОСЛЕДНЕЙ причиной и последним автором, а не первой")
}
