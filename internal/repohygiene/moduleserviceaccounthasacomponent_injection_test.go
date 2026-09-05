// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene_test

// Доказательство того, что гейт
// `TestSeededModuleServiceAccountNamesAComponentOfTheTree` СПОСОБЕН упасть —
// и падает ровно на предмете (задача продукта #1829).
//
// Инъекция ведётся по СИНТЕТИЧЕСКОМУ посеву, а не по дереву: фикстура,
// привязанная к живой строке, истекла бы вместе с ней — то есть ровно тогда,
// когда предмет починен (`testing.md` §«Чтение вердикта» п. 5).
//
// Осей пять, и по каждой утверждаются ОБЕ стороны. Первая воспроизводит
// исторический случай дословно: учётка снятого компонента сети.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var componentsFixture = map[string]bool{"vpc": true, "gateway": true, "iam": true}

const (
	saVPC = `INSERT INTO kacho_iam.service_accounts (id, account_id, name, description, created_at, enabled, labels) ` +
		`VALUES ('sva0000000000000vpc', 'acc0', 'kacho-vpc', 'Module SA: kacho-vpc (SEC-C least-priv)', now(), true, '{}')`
	saOperator = `INSERT INTO kacho_iam.service_accounts (id, account_id, name, description, created_at, enabled, labels) ` +
		`VALUES ('sva000000000000oper', 'acc0', 'kacho-vpc-operator', 'Module SA: kacho-vpc-operator (SEC-C least-priv)', now(), true, '{}')`
	saGateway = `INSERT INTO kacho_iam.service_accounts (id, account_id, name, description, created_at, enabled, labels) ` +
		`VALUES ('sva00000000000gw001', 'acc0', 'kacho-api-gateway', 'Module SA: kacho-api-gateway (SEC-C identity-only)', now(), true, '{}')`
	saBootstrap = `INSERT INTO kacho_iam.service_accounts (id, account_id, name, description, created_at, enabled, labels) ` +
		`VALUES ('sva00000000000boot1', 'acc0', 'kacho-bootstrap-admin', 'Bootstrap admin ServiceAccount (#58)', now(), true, '{}')`
	saVPCUnmarked = `INSERT INTO kacho_iam.service_accounts (id, account_id, name, description, created_at, enabled, labels) ` +
		`VALUES ('sva0000000000000vpc', 'acc0', 'kacho-vpc', 'least-priv account', now(), true, '{}')`
	saRetireByID   = `DELETE FROM kacho_iam.service_accounts WHERE id = 'sva000000000000oper'`
	saRetireByName = `DELETE FROM kacho_iam.service_accounts WHERE name IN ('kacho-vpc-operator')`
	saUnknownForm  = `UPDATE kacho_iam.service_accounts SET enabled = false WHERE name LIKE 'kacho-%'`
	saTableDDL     = `CREATE TABLE kacho_iam.service_accounts (id text NOT NULL, name text NOT NULL)`
)

func judgeSeed(t *testing.T, files ...[2]string) (findings, unknown []string, alive int) {
	t.Helper()
	ordered := make([]string, 0, len(files))
	bodies := map[string]string{}
	for _, f := range files {
		ordered = append(ordered, f[0])
		bodies[f[0]] = f[1] + ";"
	}
	live, unknownForms, _ := foldSeededServiceAccounts(ordered, bodies)
	return serviceAccountFindings(live, componentsFixture), unknownForms, len(live)
}

func TestModuleSAGateFindsAnAccountWithoutAComponent(t *testing.T) {
	findings, unknown, alive := judgeSeed(t, [2]string{"0001_initial.sql", saOperator})
	require.Empty(t, unknown)
	require.Equal(t, 1, alive)
	require.Len(t, findings, 1, "модульная учётка снятого компонента обязана быть находкой")
	require.Contains(t, findings[0], "kacho-vpc-operator", "находка обязана назвать учётку")
	require.Contains(t, findings[0], "0001_initial.sql", "находка обязана назвать файл")
}

func TestModuleSAGateIsSilentOnLawfulAccounts(t *testing.T) {
	// Законные близнецы: компонент есть прямо; компонент есть последним токеном;
	// учётка не модульная и компонентом не названа.
	findings, unknown, alive := judgeSeed(t,
		[2]string{"0001_initial.sql", saVPC + ";\n" + saGateway + ";\n" + saBootstrap})
	require.Empty(t, unknown)
	require.Equal(t, 3, alive, "все три строки обязаны попасть в свод, а не быть пропущенными")
	require.Empty(t, findings, "у каждой из трёх исход законный")
}

func TestModuleSAGateFindsAnAccountThatDroppedTheMarker(t *testing.T) {
	// Блиндаж признака: снять `Module SA:` и тем уйти из популяции нельзя.
	findings, unknown, _ := judgeSeed(t, [2]string{"0001_initial.sql", saVPCUnmarked})
	require.Empty(t, unknown)
	require.Len(t, findings, 1)
	require.Contains(t, findings[0], "модульной себя не объявляет")

	// Обратная сторона: та же строка С признаком молчит.
	back, _, _ := judgeSeed(t, [2]string{"0001_initial.sql", saVPC})
	require.Empty(t, back)
}

func TestModuleSAGateNamesAFormItDoesNotKnow(t *testing.T) {
	_, unknown, _ := judgeSeed(t, [2]string{"20260101000000_x.sql", saUnknownForm})
	require.Len(t, unknown, 1, "незнакомая форма обязана быть находкой, а не молчанием")
	require.Contains(t, unknown[0], "20260101000000_x.sql")

	// Обратная сторона: объявление таблицы посевом не является.
	_, unknownDDL, alive := judgeSeed(t, [2]string{"0001_initial.sql", saTableDDL})
	require.Empty(t, unknownDDL)
	require.Zero(t, alive)
}

func TestModuleSAGateFollowsRetirementInBothForms(t *testing.T) {
	// Так и закрывается находка: поздняя миграция снимает строку.
	byID, unknownID, aliveID := judgeSeed(t,
		[2]string{"0001_initial.sql", saOperator},
		[2]string{"20260101000000_retire.sql", saRetireByID},
	)
	require.Empty(t, unknownID)
	require.Zero(t, aliveID)
	require.Empty(t, byID, "снятая строка судиться не должна")

	byName, unknownName, aliveName := judgeSeed(t,
		[2]string{"0001_initial.sql", saOperator},
		[2]string{"20260101000000_retire.sql", saRetireByName},
	)
	require.Empty(t, unknownName)
	require.Zero(t, aliveName)
	require.Empty(t, byName)

	// Обратная сторона: снятие ЧУЖОЙ строки находку не гасит.
	other, _, aliveOther := judgeSeed(t,
		[2]string{"0001_initial.sql", saOperator},
		[2]string{"20260101000000_retire.sql", `DELETE FROM kacho_iam.service_accounts WHERE id = 'sva0000000000000vpc'`},
	)
	require.Equal(t, 1, aliveOther)
	require.Len(t, other, 1)
}

func TestModuleSAGateDerivesComponentsFromTheTreeInBothDirections(t *testing.T) {
	got := componentsOfTree([]string{
		"services/vpc/cmd/vpc/main.go",
		"services/geo/internal/x.go",
		"gateway/cmd/api-gateway/main.go",
		"docs/architecture/x.md",                     // не компонент
		"tools/declaredbreak/cmd/adjudicate/main.go", // глубже трёх сегментов — не компонент
	})
	require.True(t, got["vpc"], "каталог сервиса обязан давать компонент")
	require.True(t, got["geo"], "каталог сервиса без точки входа — тоже компонент")
	require.True(t, got["gateway"], "каталог верхнего уровня со своей точкой входа — компонент")
	require.False(t, got["docs"], "каталог без сервиса и без точки входа компонентом не является")
	require.False(t, got["tools"], "вложенная точка входа компонента верхнего уровня не заводит")

	// Разрешение имени: обе стороны той пары, ради которой правило и написано.
	_, okGateway := namesAComponent("kacho-api-gateway", componentsFixture)
	require.True(t, okGateway, "последний токен имени разрешается в компонент")
	rest, okOperator := namesAComponent("kacho-vpc-operator", componentsFixture)
	require.False(t, okOperator,
		"нормализация НЕ смеет разрешать %q: иначе исторический хвост прошёл бы молча", rest)
}

func TestModuleSAGateRefusesAnEmptySweep(t *testing.T) {
	alive, unknown, stmts := foldSeededServiceAccounts(nil, map[string]string{})
	require.Empty(t, alive)
	require.Empty(t, unknown)
	require.Zero(t, stmts)
	require.Empty(t, serviceAccountFindings(alive, componentsFixture),
		"на пустом своде судья находок не даёт — поэтому «ноль находок» обязан отсекаться "+
			"предпосылкой гейта, а не читаться как зелёное")
	require.True(t, strings.HasPrefix(saTableDDL, "CREATE"))
}
