// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// Самопроверка гейта «шаблон чарта не прокладывает административной дороги к
// поставщику» — инъекция в обе стороны на входе той же формы, что в дереве.
//
// Фикстуры здесь СИНТЕТИЧЕСКИЕ и намеренно не опираются ни на одну живую строку
// дерева. Самопроверка, опёртая на существующую находку, исчезает вместе с ней —
// то есть ровно тогда, когда цель гейта достигнута.

func TestDeploymentExecutablePart_InjectionBothWays(t *testing.T) {
	const surface = "/admin/trust/grants/jwt-bearer/issuers"

	// (а) ВНЕСЁННЫЙ ДЕФЕКТ: рабочая нагрузка обращается к поверхности из своей
	//     команды. Обязан быть виден в исполняемой части.
	defect := strings.Join([]string{
		"apiVersion: batch/v1",
		"kind: Job",
		"spec:",
		"  template:",
		"    spec:",
		"      containers:",
		"        - command: [\"sh\", \"-c\"]",
		"          args:",
		`            - curl -sS -X POST "$ADMIN/admin/trust/grants/jwt-bearer/issuers" -d @-`,
	}, "\n")
	if !strings.Contains(deploymentExecutablePart(defect), surface) {
		t.Error("обращение к поверхности в команде контейнера не видно в исполняемой части — " +
			"гейт пропустил бы ровно тот дефект, ради которого написан")
	}

	// (б) ЗАКОННЫЙ БЛИЗНЕЦ: тот же путь в комментарии `#`, объясняющем, зачем
	//     соседний контейнер знает адрес. Такой комментарий в дереве есть.
	//     Молчит — иначе гейт краснел бы на собственном объяснении, и его сняли
	//     бы первым же ложным срабатыванием.
	hashComment := "            # In-cluster Hydra ADMIN endpoint (POST /admin/clients + jwt-bearer\n" +
		"            # trust grants " + surface + ")\n" +
		"            - name: HYDRA_ADMIN_URL\n"
	if strings.Contains(deploymentExecutablePart(hashComment), surface) {
		t.Error("путь в комментарии `#` прочитан как обращение — комментарий, объясняющий " +
			"назначение адреса, законен и обязан молчать")
	}

	// (в) ВТОРОЙ ЗАКОННЫЙ БЛИЗНЕЦ: тот же путь в блочном комментарии helm
	//     `{{/* … */}}`, которым шаблоны снабжают шапкой. Молчит по той же причине.
	helmBlock := "{{/*\n  Регистрация издателей идёт через `POST " + surface + "`.\n*/}}\n" +
		"apiVersion: v1\n"
	if strings.Contains(deploymentExecutablePart(helmBlock), surface) {
		t.Error("путь в блочном комментарии helm прочитан как обращение")
	}

	// (г) ТРЕТИЙ ЗАКОННЫЙ БЛИЗНЕЦ: не-административная поверхность. `/oauth2/token`
	//     обслуживает стандартную выдачу токена, её законно называют профили,
	//     кейсы и документация; попади она в предикат — гейт получил бы
	//     популяцию ложных находок и был бы отключён.
	for _, s := range adminSurfaces() {
		if s.Path == "/oauth2/token" {
			t.Error("`/oauth2/token` попала в административные поверхности — предикат стал " +
				"слишком широким")
		}
		if !strings.HasPrefix(s.Path, "/admin/") {
			t.Errorf("поверхность %q не административная, а отобрана как таковая", s.Path)
		}
	}
}

// Предпосылка гейта: словарь поверхностей ОБЩИЙ, а не копия. Проверяется тем,
// что отбор административных путей выводится из него и непуст.
func TestAdminSurfaces_DerivedFromTheSharedDictionary(t *testing.T) {
	admin := adminSurfaces()
	if len(admin) == 0 {
		t.Fatal("административных поверхностей ноль — отбор перестал узнавать словарь, " +
			"и гейт объявил бы дерево чистым, ничего не осмотрев")
	}
	if len(admin) >= len(ProviderSurfaces) {
		t.Errorf("отобрано %d из %d — отбор ничего не сузил, значит предикат `/admin/` "+
			"перестал действовать", len(admin), len(ProviderSurfaces))
	}
	// Положительный контроль: конкретный административный путь обязан попасть.
	found := false
	for _, s := range admin {
		if s.Path == "/admin/clients" {
			found = true
		}
	}
	if !found {
		t.Error("`/admin/clients` не попала в отбор — предикат стал слишком узким")
	}
	t.Logf("перепись: поверхностей в словаре — %d, из них административных — %d",
		len(ProviderSurfaces), len(admin))
}
