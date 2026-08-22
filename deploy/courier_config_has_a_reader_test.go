// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

import (
	"strings"
	"testing"
)

// TestCourierSectionIsDeclaredOnlyWhereItsProcessReadsIt — почтовая полоса
// объявляется ТОЛЬКО там, где её читает почтовый процесс.
//
// # Предмет
//
// Наша конфигурация службы личности объявляла `courier.smtp`, а письма шлёт
// ОТДЕЛЬНЫЙ процесс, работающий на файле подчарта поставщика: указание на наш
// файл приезжает из ручки основного рабочего объекта, почтовый берёт свою, и
// там его нет. Значения не участвовали ни в одной отправке.
//
// Со стороны это выглядело как «почта настроена нами» — то есть
// «принято-и-проигнорировано» на уровне настроек. Решение (#943): раздел снят;
// действующие значения остаются в слое поставщика, где читатель у них есть.
//
// # Что утверждается
//
// Пара: наша конфигурация раздела не объявляет — И объявление не переехало
// молча в значения чарта. Проверка одной половины зеленела бы на дереве, где
// раздел снят из шаблона, но значения остались висеть без читателя.
func TestCourierSectionIsDeclaredOnlyWhereItsProcessReadsIt(t *testing.T) {
	const tpl = "helm/umbrella/charts/kacho-iam/templates/_kratos-identity.tpl"

	body := readTracked(t, "deploy/"+tpl)
	lines := strings.Split(body, "\n")

	var declared int
	for _, l := range lines {
		s := strings.TrimSpace(l)
		if strings.HasPrefix(s, "#") {
			continue // объяснение, ПОЧЕМУ раздела нет, разделом не является
		}
		if s == "courier:" || strings.HasPrefix(s, "courier.smtp") {
			declared++
		}
	}
	t.Logf("перепись: строк конфигурации %d; объявлений почтовой полосы %d", len(lines), declared)

	if len(lines) < 50 {
		t.Fatal("шаблон конфигурации прочитан не целиком — «ноль объявлений» здесь означало бы «ноль прочитанного»")
	}
	if declared != 0 {
		t.Errorf("наша конфигурация объявляет почтовую полосу (%d), а её процесс читает файл поставщика — "+
			"объявление без читателя выглядит как «почта настроена нами»; либо снять, либо дать процессу "+
			"читать наш файл ВМЕСТЕ с прогоном полосы восстановления доступа", declared)
	}

	// Вторая половина пары: значения не остались висеть без читателя.
	for _, rel := range []string{
		"deploy/helm/umbrella/values.yaml",
		"deploy/helm/umbrella/charts/kacho-iam/values.yaml",
	} {
		for _, l := range strings.Split(readTracked(t, rel), "\n") {
			s := strings.TrimSpace(l)
			if strings.HasPrefix(s, "#") {
				continue
			}
			if s == "smtp:" {
				t.Errorf("%s объявляет значения почты, которых не читает ни один шаблон — "+
					"они пережили свой раздел", rel)
			}
		}
	}
}
