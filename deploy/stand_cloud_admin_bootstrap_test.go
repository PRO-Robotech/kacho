// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// stand_cloud_admin_bootstrap_test.go — ПОДЪЁМ СТЕНДА own ЗАВОДИТ АДМИНИСТРАТОРА
// ОБЛАКА ПУТЁМ ПРОДУКТА, А УДОСТОВЕРЕНИЕ ЛЕЖИТ ВНЕ ДЕРЕВА (kacho#2878).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Единственный вход стенда own заводился регистрацией — это владелец своего
// аккаунта, а не администратор облака, и раздел «Система» консоли посмотреть
// было нечем. Шаг подъёма (`bootstrap-cloud-admin`) заводит человека тем же
// путём, что проходит человек (регистрация на внешнем слушателе края, код из
// письма приёмника стенда, подтверждение), а право администратора облака
// выдаёт сама служба доступа посевом бутстрапа по адресу из
// KANAME_BOOTSTRAP_ROOT_EMAIL. Адрес и пароль — ключи секрета стенда; в дереве
// нет ни того, ни другого, а профиль называет только ИМЯ секрета.
//
// Здесь судится провязка, видимая без кластера:
//
//  1. own-up зовёт шаг после применения умбреллы и готовности края и раньше
//     гейтов готовности и посадки — до применения человека некуда заводить, а
//     после гейтов стенд объявлялся бы готовым без входа администратора;
//  2. цепочка own объявляет секрет бутстрапа (`kaname.platform.iam.
//     bootstrapRootAdmin.secretName`), и у этого секрета есть строка в
//     ведомости производителей stack-secrets.sh — на kind его чеканит рецепт,
//     на площадке заводит оператор;
//  3. скрипт шага не печатает величин секрета: в его исполнимых строках нет
//     вывода переменных, в которые они читаются.
//
// Что человек входит и раздел отвечает данными, а владелец аккаунта на тех же
// путях получает отказ, — вопрос поднятого стенда (нога own, вывод — в задаче).
package deploy_test

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const (
	cloudAdminStep       = "bootstrap-cloud-admin"
	cloudAdminScript     = "scripts/bootstrap-cloud-admin.sh"
	cloudAdminStack      = "own"
	cloudAdminSecretPath = "kaname.platform.iam.bootstrapRootAdmin.secretName"
)

var cloudAdminStepCall = regexp.MustCompile(`\$\(MAKE\)[^;]*[ \t]` + cloudAdminStep + `([ \t;\\]|$)`)

func TestOwnUpBootstrapsTheCloudAdministratorBetweenApplyAndTheGates(t *testing.T) {
	raw, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatalf("deploy/Makefile не прочитан: %v", err)
	}
	targets := parseRecipeTargets(string(raw))
	if _, ok := targets[cloudAdminStep]; !ok {
		t.Fatalf("цели %s в Makefile нет — шага подъёма, заводящего администратора облака, не существует", cloudAdminStep)
	}
	recipe := targets[cloudAdminStep].recipe
	if !strings.Contains(strings.Join(recipe, "\n"), cloudAdminScript) {
		t.Errorf("рецепт %s не зовёт %s", cloudAdminStep, cloudAdminScript)
	}
	pos := map[string]int{}
	for i, line := range targets["own-up"].recipe {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		mark := func(k string, ok bool) {
			if _, seen := pos[k]; ok && !seen {
				pos[k] = i
			}
		}
		mark("apply", callSite(umbrellaApply, line) && strings.Contains(line, "helm upgrade"))
		mark("edge", strings.Contains(line, "wait-edge-ready"))
		mark("step", callSite(cloudAdminStepCall, line))
		mark("rollout", strings.Contains(line, "assert-rollout-ready"))
		mark("posture", strings.Contains(line, "assert-production-posture"))
	}
	t.Logf("own-up: строк рецепта %d; позиции %v", len(targets["own-up"].recipe), pos)
	for _, k := range []string{"apply", "edge", "rollout", "posture"} {
		if _, ok := pos[k]; !ok {
			t.Fatalf("предпосылка пробы: в рецепте own-up не найден шаг %q — распознаватель разошёлся с рецептом", k)
		}
	}
	step, ok := pos["step"]
	if !ok {
		t.Fatalf("own-up не зовёт $(MAKE) … %s — стенд поднимается без администратора облака", cloudAdminStep)
	}
	if step < pos["apply"] || step < pos["edge"] {
		t.Errorf("own-up зовёт %s раньше применения умбреллы или готовности края — человека некуда заводить", cloudAdminStep)
	}
	if step > pos["rollout"] || step > pos["posture"] {
		t.Errorf("own-up зовёт %s после гейтов готовности и посадки — стенд объявлялся бы готовым без входа администратора", cloudAdminStep)
	}
}

func TestOwnStackDeclaresTheBootstrapSecretAndItHasAProducer(t *testing.T) {
	chain, ok := deployStacks(t)[cloudAdminStack]
	if !ok {
		t.Fatalf("стека %s в таблице нет", cloudAdminStack)
	}
	merged := readYAML(t, umbrellaDir+"/values.yaml")
	for _, p := range chain {
		merged = mergeValues(merged, readYAML(t, umbrellaDir+"/"+p))
	}
	v, _ := lookup(merged, strings.Split(cloudAdminSecretPath, ".")...)
	name := yamlScalarOf(v)
	t.Logf("стек %s (%s): %s = %q", cloudAdminStack, strings.Join(chain, " + "), cloudAdminSecretPath, name)
	if name == "" {
		t.Fatalf("цепочка %s не объявляет секрет бутстрапа (%s пуст) — служба доступа не знает, кому выдать право администратора облака",
			cloudAdminStack, cloudAdminSecretPath)
	}
	ledger, err := os.ReadFile("scripts/stack-secrets.sh")
	if err != nil {
		t.Fatalf("scripts/stack-secrets.sh не прочитан: %v", err)
	}
	arm := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(name) + `\)`)
	if n := len(arm.FindAllIndex(ledger, -1)); n < 2 {
		t.Errorf("у секрета %s в stack-secrets.sh ветвей %d, ждали две — строку ведомости производителей "+
			"(кто заводит на площадке) и рецепт для kind", name, n)
	}
}

func TestCloudAdminScriptPrintsNoSecretValue(t *testing.T) {
	raw, err := os.ReadFile(cloudAdminScript)
	if err != nil {
		t.Fatalf("%s не прочитан: %v — шага нет", cloudAdminScript, err)
	}
	reads := 0
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(trimmed, "KACHO_CLOUD_ADMIN_EMAIL=") || strings.Contains(trimmed, "KACHO_CLOUD_ADMIN_PASSWORD=") {
			reads++
		}
		for _, v := range []string{"$KACHO_CLOUD_ADMIN_EMAIL", "${KACHO_CLOUD_ADMIN_EMAIL", "$KACHO_CLOUD_ADMIN_PASSWORD", "${KACHO_CLOUD_ADMIN_PASSWORD"} {
			if strings.Contains(trimmed, v) && (strings.Contains(trimmed, "echo") || strings.Contains(trimmed, "printf") || strings.Contains(trimmed, "log ")) {
				t.Errorf("%s печатает величину секрета: %s", cloudAdminScript, trimmed)
			}
		}
	}
	t.Logf("%s: строк чтения величин секрета %d", cloudAdminScript, reads)
	if reads == 0 {
		t.Errorf("%s не читает величины секрета в переменные KACHO_CLOUD_ADMIN_* — проверять печать нечего, "+
			"распознаватель разошёлся со скриптом", cloudAdminScript)
	}
}

func yamlScalarOf(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}
