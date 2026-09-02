// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// roles_test.go — раздел `roles` (приёмка §2.6, §2.6а; сценарии MOD-MR-10 …
// MOD-MR-15).
//
// Раздел АВТОРСКИЙ: аннотации о ролях не говорят ничего. Зато говорят
// МИГРАЦИИ — 51 системная роль объявлена применёнными, а применённую миграцию
// не правят (ban #5). Поэтому манифест объявляет роли уровня аккаунта и
// проекта, а системную отвергает ЯВНО.
//
// Право роли пишется ключом `classes`, и это ЕДИНСТВЕННАЯ его форма. Имена
// ключей совпадают с полями `domain.Rule` всюду, кроме этого ОДНОГО названного
// расхождения; объявленность расхождения и тождественность перевода утверждает
// проба перевода (`ruletranslation_internal_test.go`, MOD-RC-06 … MOD-RC-08),
// заменившая прежний изоморфизм ПО ИМЕНАМ — см. её шапку, там сказано, почему
// замена, а не ослабление.
package manifest_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/services/iam/internal/manifest"
)

// ── MOD-MR-10 ───────────────────────────────────────────────────────────────

// TestMODMR10RolesSectionLoadsWhole — положительный контроль полосы `roles`:
// фикстура читается целиком, и значения ключей доступны вызывающему.
//
// # Здесь же стояло РАВЕНСТВО имён ключей выдачи и полей `domain.Rule` — снято
//
// Утверждение снято ВМЕСТЕ СО СВОИМ ПРЕДМЕТОМ: право роли пишется ключом
// `classes`, у которого поля-тёзки в домене нет и не будет, поэтому равенство
// имён стало ложным по букве. Ослабить его до членства было бы неверно —
// ослабленное перестало бы ловить вторую беду, ради которой писалось («поле,
// выразимое в продукте, невыразимо в манифесте»). Обе половины утверждает
// теперь проба перевода (`ruletranslation_internal_test.go`): тотальность,
// непотерянность и ОБЪЯВЛЕННОСТЬ расхождения словарём, который самоистекает.
//
// Положительный контроль полосы к изоморфизму отношения не имел и остаётся:
// проба, снятая вместе с чужим предметом, унесла бы своё.
func TestMODMR10RolesSectionLoadsWhole(t *testing.T) {
	m, err := manifest.Load([]byte(mustReadResourcesFixture(t)))
	if err != nil {
		t.Fatalf("раздел roles отвергнут: %v", err)
	}
	if len(m.Roles) != 2 {
		t.Fatalf("ролей прочитано %d, в фикстуре две", len(m.Roles))
	}
	if m.Roles[0].Tier == nil || m.Roles[0].Tier.TierType != "iam.project" {
		t.Errorf("ярус роли прочитан неверно: %+v", m.Roles[0].Tier)
	}
	if len(m.Roles[0].Rules) != 1 || m.Roles[0].Rules[0].Module != "vpc" {
		t.Errorf("выдача роли прочитана неверно: %+v", m.Roles[0].Rules)
	}
	if got := m.Roles[0].Rules[0].Classes; len(got) == 0 {
		t.Errorf("право роли прочитано пустым: %+v", m.Roles[0].Rules[0])
	}
	t.Logf("перепись: ролей %d · правил у первой %d", len(m.Roles), len(m.Roles[0].Rules))
}

// ── MOD-MR-11 ───────────────────────────────────────────────────────────────

// TestMODMR11RoleIDOfAForeignModuleIsRefused — манифест объявляет роли СВОЕГО
// модуля; чужая роль здесь была бы объявлением за чужой домен.
func TestMODMR11RoleIDOfAForeignModuleIsRefused(t *testing.T) {
	base := "apiVersion: iam/v1\nmodule: vpc\nroles:\n" +
		"  - id: %s\n    name: Наблюдатель\n    description: Читает.\n" +
		"    tier: {tierType: iam.project, tierId: prj000000000000000}\n" +
		"    rules:\n      - {module: vpc, resources: [network], classes: [get]}\n"

	_, err := manifest.Load([]byte(strings.Replace(base, "%s", "compute.viewer", 1)))
	if err == nil {
		t.Fatalf("роль чужого модуля принята")
	}
	if !errors.Is(err, manifest.ErrRoleForeignModule) {
		t.Errorf("отказ не отнесён к своей причине: %v", err)
	}
	for _, want := range []string{"roles[0].id", "compute", "vpc"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("отказ не называет %q: %v", want, err)
		}
	}

	if _, err := manifest.Load([]byte(strings.Replace(base, "%s", "vpc.viewer", 1))); err != nil {
		t.Fatalf("парный положительный отвергнут: %v", err)
	}
}

// ── MOD-MR-12 ───────────────────────────────────────────────────────────────

// TestMODMR12ResourceWildcardIsAlwaysRefusedAndVerbWildcardIsNot — асимметрия
// ИЗМЕРЕНА, а не предположена: подстановка ресурса системна by construction, а
// подстановка глагола в несистемной роли законна и значит «все глаголы типа».
//
// Проверять надо ОБЕ стороны: односторонняя проба зеленела бы на загрузчике,
// отвергающем всякую подстановку.
func TestMODMR12ResourceWildcardIsAlwaysRefusedAndVerbWildcardIsNot(t *testing.T) {
	base := "apiVersion: iam/v1\nmodule: vpc\nroles:\n" +
		"  - id: vpc.viewer\n    name: Наблюдатель\n    description: Читает.\n" +
		"    tier: {tierType: iam.project, tierId: prj000000000000000}\n" +
		"    rules:\n      - {module: vpc, resources: [%s], classes: [%s]}\n"

	wildcardResources := strings.Replace(strings.Replace(base, "%s", `"*"`, 1), "%s", "get", 1)
	_, err := manifest.Load([]byte(wildcardResources))
	if err == nil {
		t.Fatalf("подстановка ресурса в несистемной роли принята")
	}
	if !errors.Is(err, manifest.ErrRoleRuleInvalid) {
		t.Errorf("отказ не отнесён к своей причине: %v", err)
	}
	// Текст домена — часть контракта и воспроизводится ДОСЛОВНО.
	if !strings.Contains(err.Error(), "Illegal argument resources (wildcard '*' is system-only)") {
		t.Errorf("отказ не несёт дословного текста домена: %v", err)
	}

	// Парный положительный, измеренный и АСИММЕТРИЧНЫЙ: подстановка глагола
	// единственным элементом законна.
	wildcardVerbs := strings.Replace(strings.Replace(base, "%s", "network", 1), "%s", `"*"`, 1)
	if _, err := manifest.Load([]byte(wildcardVerbs)); err != nil {
		t.Fatalf("подстановка глагола в несистемной роли отвергнута: %v", err)
	}
}

// ── MOD-MR-13 ───────────────────────────────────────────────────────────────

// TestMODMR13ResourceNamesAndMatchLabelsAreMutuallyExclusive — действительный
// взаимоисключающий инвариант `domain.Rule` со своим стабильным текстом.
func TestMODMR13ResourceNamesAndMatchLabelsAreMutuallyExclusive(t *testing.T) {
	base := "apiVersion: iam/v1\nmodule: vpc\nroles:\n" +
		"  - id: vpc.viewer\n    name: Наблюдатель\n    description: Читает.\n" +
		"    tier: {tierType: iam.project, tierId: prj000000000000000}\n" +
		"    rules:\n      - module: vpc\n        resources: [network]\n        classes: [get]\n%s"

	both := "        resourceNames: [net-abc]\n        matchLabels: {env: prod}\n"
	_, err := manifest.Load([]byte(strings.Replace(base, "%s", both, 1)))
	if err == nil {
		t.Fatalf("resourceNames и matchLabels приняты вместе")
	}
	if !errors.Is(err, manifest.ErrRoleRuleInvalid) {
		t.Errorf("отказ не отнесён к своей причине: %v", err)
	}
	if !strings.Contains(err.Error(),
		"Illegal argument: resourceNames and matchLabels are mutually exclusive") {
		t.Errorf("отказ не несёт дословного текста домена: %v", err)
	}

	// Парные положительные: ровно одно из двух.
	for _, only := range []string{
		"        resourceNames: [net-abc]\n",
		"        matchLabels: {env: prod}\n",
	} {
		if _, err := manifest.Load([]byte(strings.Replace(base, "%s", only, 1))); err != nil {
			t.Fatalf("ровно один селектор отвергнут (%q): %v", strings.TrimSpace(only), err)
		}
	}
}

// ── MOD-MR-14 ───────────────────────────────────────────────────────────────

// TestMODMR14SystemRoleIsRefusedExplicitly — системность НЕ отдельный признак, а
// СЛЕДСТВИЕ яруса: контракт говорит дословно, что `is_system` выводится из
// `tier_type == iam.cluster`. Поэтому отказ по ярусу и есть отказ системной
// роли, а не его приближение.
//
// Исход 2 запрета «принято-и-проигнорировано»: приняв, мы вернули бы
// вызывающему успех и уверенность, что его роль заведена, тогда как заводит её
// миграция, которой в этом изменении нет.
func TestMODMR14SystemRoleIsRefusedExplicitly(t *testing.T) {
	base := "apiVersion: iam/v1\nmodule: vpc\nroles:\n" +
		"  - id: vpc.admin\n    name: Администратор\n    description: Может всё.\n" +
		"    tier: {tierType: %s, tierId: cluster_kacho_root}\n" +
		"    rules:\n      - {module: vpc, resources: [network], classes: [get]}\n"

	_, err := manifest.Load([]byte(strings.Replace(base, "%s", "iam.cluster", 1)))
	if err == nil {
		t.Fatalf("системная роль в манифесте принята")
	}
	if !errors.Is(err, manifest.ErrSystemRoleNotAuthorable) {
		t.Errorf("отказ не отнесён к своей причине: %v", err)
	}
	for _, want := range []string{
		"roles[0].tier.tierType", "iam.cluster", "iam.account", "iam.project", "миграц",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("отказ не называет %q: %v", want, err)
		}
	}

	if _, err := manifest.Load([]byte(strings.Replace(base, "%s", "iam.project", 1))); err != nil {
		t.Fatalf("парный положительный отвергнут: %v", err)
	}
}

// ── MOD-MR-15 ───────────────────────────────────────────────────────────────

// TestMODMR15RoleIDOfABindingIsResolvedByTheRolesSection — послабление #1088
// ИСТЕКЛО: перечень ролей приезжает из разобранного документа, а не подаётся
// перечнем сбоку.
//
// Перепись обязана сообщать НЕНУЛЕВОЕ число сверенных ссылок и не содержать
// строки «раздел roles не описан»: ноль сверенных читался бы как «сверили и не
// нашли расхождений».
func TestMODMR15RoleIDOfABindingIsResolvedByTheRolesSection(t *testing.T) {
	doc := mustReadResourcesFixture(t)

	m, err := manifest.Load([]byte(doc))
	if err != nil {
		t.Fatalf("фикстура отвергнута: %v", err)
	}
	census := m.Linkage()
	t.Logf("перепись связности: %s", census)
	if census.RoleRefsChecked == 0 {
		t.Errorf("сверено ноль ссылок на роль при описанном разделе: %s", census)
	}
	if !census.RolesDeclared {
		t.Errorf("раздел roles описан, а перепись считает его необъявленным: %s", census)
	}
	if strings.Contains(census.String(), "не описан") {
		t.Errorf("перепись всё ещё объясняет ноль сверенных отсутствием раздела: %s", census)
	}

	broken := replaceOnce(t, doc, "roleId: vpc.internalConsumer", "roleId: vpc.nosuchRole")
	_, err = manifest.Load([]byte(broken))
	if err == nil {
		t.Fatalf("выдача на роль, которой манифест не объявляет, принята")
	}
	if !errors.Is(err, manifest.ErrRoleNotDeclared) {
		t.Errorf("отказ не отнесён к своей причине: %v", err)
	}
	for _, want := range []string{"vpc.nosuchRole", "seed.accessBindings[0].roleId"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("отказ не называет %q: %v", want, err)
		}
	}
}
