// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package pg_test

// selector_lock_order_inversion_integration_test.go — ХАРАКТЕРИЗУЮЩИЙ ЗАМОК на
// цену, которой оплачен общий замок стража живости (задача продукта #1985).
//
// # Что здесь закрепляется — сегодняшний исход, а не желаемый
//
// Страж живости берёт на строке каталога `FOR KEY SHARE` — тот же замок, который
// взял бы внешний ключ. Этим закрыт перекос записи. Но ПОРЯДОК взятия замков у
// двух сторон разный, и это цена, а не дефект стража:
//
//	применитель   строка каталога → строки селекторов
//	правка роли   строки селекторов (снятие прежних) → строка каталога (страж)
//
// Инверсия даёт взаимную блокировку, которую Postgres обнаруживает и отвергает
// одну из сторон (`40P01`). Отвергается СТОРОНА АРЕНДАТОРА: применитель доходит
// до конца, правка правил роли получает отказ — то есть цену платит не тот, кто
// инверсию создаёт.
//
// # Почему это записано пробой, а не только словами в миграции
//
// Утверждение «взаимная блокировка представима» — утверждение о поведении, и
// проверяется оно опытом, а не чтением. Записанное только прозой, оно пережило бы
// свой предмет: следующий, кто выровняет порядок взятия замков на стороне писателя
// роли, не узнает, что снял её, — а эта проба покраснеет и будет переписана под
// решение, как переписан замок #1942.
//
// # Чего проба НЕ утверждает
//
// Она НЕ утверждает, что взаимная блокировка приемлема, и не является доводом
// оставить всё как есть. Размен назван в миграции дословно: громкий отказ,
// который повторяется, против тихого неверного состояния, которое не сходится
// само. Предмет выравнивания порядка — свой, и он у писателя роли, а не у стража.

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSelectorLockOrderInversionIsStillPayable — сегодня инверсия ДАЁТ взаимную
// блокировку, и одна из сторон отвергается с `40P01`.
func TestSelectorLockOrderInversionIsStillPayable(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: требует Postgres")
	}
	ctx, pool := catalogPool(t)
	applier := applierOver(t, pool)

	const doomed = applierProbeModule + ".lockordergone"
	_, err := applier.Apply(ctx, probeManifest(probeResource("lockordergone", "get")))
	require.NoError(t, err)

	role := catalogRole(t, ctx, pool, "lockorder1985")
	require.NoError(t, upsertSelector(ctx, pool, role, "fp-lockorder", []string{doomed}),
		"ПОЛОЖИТЕЛЬНЫЙ КОНТРОЛЬ: живой тип обязан записаться")

	// ── СТОРОНА A: строка каталога помечена снятой (замок на строке каталога) ──
	txA, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = txA.Rollback(ctx) }()
	_, err = txA.Exec(ctx, skewRetireVerbsDirect, applierProbeModule, "lockordergone")
	require.NoError(t, err)
	_, err = txA.Exec(ctx, skewRetireDirect, doomed)
	require.NoError(t, err)

	// ── СТОРОНА B: правка правил роли — снятие прежних селекторов, затем запись ─
	//
	// Дословно порядок `ReplaceRuleSelectors`: сперва `DELETE … WHERE role_id`,
	// затем запись через `ON CONFLICT DO UPDATE`. Замок на строке селектора взят
	// РАНЬШЕ замка на строке каталога — это и есть инверсия.
	bres := make(chan error, 1)
	go func() {
		txB, berr := pool.Begin(ctx)
		if berr != nil {
			bres <- berr
			return
		}
		defer func() { _ = txB.Rollback(ctx) }()
		if _, berr = txB.Exec(ctx,
			`DELETE FROM kacho_iam.role_rule_selectors WHERE role_id = $1`, string(role)); berr != nil {
			bres <- berr
			return
		}
		if _, berr = txB.Exec(ctx, `
			INSERT INTO kacho_iam.role_rule_selectors
			  (role_id, rule_fp, arm, object_types, resource_names, match_labels, created_at, updated_at)
			VALUES ($1, 'fp-lockorder', 'anchor', $2, '{}', '{}'::jsonb, now(), now())
			ON CONFLICT (role_id, rule_fp) DO UPDATE
			   SET object_types = EXCLUDED.object_types, updated_at = now()`,
			string(role), []string{doomed}); berr != nil {
			bres <- berr
			return
		}
		bres <- txB.Commit(ctx)
	}()

	// B обязана встать на страже живости: строка каталога под замком A.
	select {
	case berr := <-bres:
		t.Fatalf("правка роли НЕ встала на страже живости (%v) — предпосылка пробы "+
			"нарушена, и всё ниже вакуумно", berr)
	case <-time.After(time.Second):
	}

	// ── A идёт за строками селекторов, которые держит B ───────────────────────
	_, aerr := txA.Exec(ctx,
		`DELETE FROM kacho_iam.role_rule_selectors WHERE role_id = $1 AND rule_fp = 'fp-lockorder'`,
		string(role))
	berr := <-bres

	t.Logf("исход инверсии: сторона применителя %v · сторона правки роли %v", aerr, berr)

	deadlocked := (aerr != nil && strings.Contains(aerr.Error(), "40P01")) ||
		(berr != nil && strings.Contains(berr.Error(), "40P01"))
	require.Truef(t, deadlocked,
		"инверсия порядка взятия замков БОЛЬШЕ не даёт взаимной блокировки: "+
			"применитель %v, правка роли %v.\n"+
			"Если порядок выровнен на стороне писателя роли — цена, названная в "+
			"миграции 20260903181000, оплачена, и эту пробу надо переписать под "+
			"решение, а не ослабить.", aerr, berr)
	require.Falsef(t, aerr != nil && berr != nil,
		"отвергнуты ОБЕ стороны — Postgres отвергает одну: применитель %v, правка %v",
		aerr, berr)
}
