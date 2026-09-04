// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

func roleOpStateOptions(t *testing.T) RoleOperationResponseStateOptions {
	t.Helper()
	return RoleOperationResponseStateOptions{
		Root:             repoRoot(t),
		ServiceRoot:      "services/iam",
		DomainPkg:        "domain",
		RoleType:         "Role",
		TransferFunc:     "Transfer",
		ProjectionMethod: "WithoutComputedState",
	}
}

// TestRoleOperationResponseCarriesNoComputedState — вердикт о НАСТОЯЩЕМ дереве.
//
// Способность падать доказывает не этот прогон, а инъекция
// (`roleoperationresponsestate_injection_test.go`): здесь только вердикт.
func TestRoleOperationResponseCarriesNoComputedState(t *testing.T) {
	var log strings.Builder
	findings, census, err := AuditRoleOperationResponseState(roleOpStateOptions(t), &log)
	if err != nil {
		t.Fatalf("анализатор не отработал: %v", err)
	}
	t.Log(strings.TrimSpace(log.String()))

	// Премиса: прочитано то, что заведомо есть. Без неё «ноль находок»
	// достигалось бы пустым обходом.
	if census.Files < 200 {
		t.Fatalf("файлов Go прочитано %d — обход пуст, вердикт беспредметен", census.Files)
	}
	if census.Funcs < 1000 {
		t.Fatalf("функций разобрано %d — разбор перестал видеть объявления", census.Funcs)
	}
	if census.AnypbFuncs < 20 {
		t.Fatalf("функций, возвращающих ответ операции, %d — признак разошёлся с деревом",
			census.AnypbFuncs)
	}
	// Переводчиков роли ДВА: один у пути мутации, один у резолвера операций.
	// Меньше — признак разошёлся с деревом; больше — завёлся третий, и он тоже
	// обязан звать проекцию (это и проверяет вердикт ниже).
	if census.RoleTranslators < 2 {
		t.Fatalf("переводчиков роли найдено %d, ожидалось не меньше 2 — признак разошёлся "+
			"с деревом", census.RoleTranslators)
	}

	if len(findings) == 0 {
		return
	}
	lines := make([]string, 0, len(findings))
	for _, f := range findings {
		lines = append(lines, "  "+f.String())
	}
	t.Errorf("ответ операции над ролью может понести ВЫЧИСЛЕННОЕ состояние "+
		"(переводчиков роли %d, зовут проекцию %d):\n%s\n\n"+
		"Контракт обещает арендатору, что нулевые health и lifecycle в ответе операции "+
		"означают «этим ответом не вычислено», а не «роль здорова и объявлена». Состояние, "+
		"посчитанное на пути мутации, относится к ДРУГОМУ снимку проекции — публиковать его "+
		"значит отвечать про состояние, которого у роли не было ни до, ни после. Снятие — "+
		"звать domain.Role.WithoutComputedState() в переводчике.",
		census.RoleTranslators, census.ProjectionCalled, strings.Join(lines, "\n"))
}
