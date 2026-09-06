// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// clusteranchorliteral_injection_test.go — доказательство, что гейт написания
// якоря СПОСОБЕН упасть, и падает ТОЛЬКО на своём предмете.
//
// Прогонов ТРИ, а не два (`testing.md` §«Гейт на класс», п. 2в):
//
//	контроль            — всё цело, молчат обе проверки;
//	инъекция нового     — краснеет ТОЛЬКО перепись литералов;
//	инъекция соседнего  — краснеет ТОЛЬКО согласие объявлений.
//
// Без третьего молчание соседней проверки неотличимо от молчания мёртвой.
//
// Рядом с каждой инъекцией стоит ЗАКОННЫЙ БЛИЗНЕЦ, отличающийся ОДНИМ фактом:
// то же написание, названное комментарием, взятое константой либо стоящее
// внутри фразы. Без него гейт ловил бы форму, и первое же законное срабатывание
// его отключило бы.

import (
	"strings"
	"testing"
)

// anchorWorld — синтетическое дерево: два объявления (модуля два) плюс
// законные способы назвать якорь.
func anchorWorld() map[string]string {
	return map[string]string{
		"pkg/authz/catalogderive/derive.go": `package catalogderive

const ClusterSingletonID = "cluster_kacho_root"
`,
		"services/iam/internal/domain/constants.go": `package domain

const ClusterSingletonID = "cluster_kacho_root"
`,
		// ЗАКОННЫЙ БЛИЗНЕЦ: обращение через константу, упоминание в комментарии
		// и написание ВНУТРИ фразы. Все три обязаны молчать.
		"services/iam/internal/apps/legit.go": `package apps

import "github.com/PRO-Robotech/kaname/internal/domain"

// Якорь кластера называется cluster_kacho_root и стоит здесь в ПРОЗЕ:
// комментарий — не строковый литерал, и запрет на слово краснел бы на
// исправном дереве.
func Object() string { return "cluster:" + domain.ClusterSingletonID }

// Description — написание ВНУТРИ фразы: формы «взять константу» у такого
// литерала нет, и требовать её значило бы требовать невозможного.
const Description = "Идентификатор объекта-якоря: cluster_kacho_root для кластера"
`,
	}
}

func anchorRun(t *testing.T, world map[string]string) ([]AnchorDeclaration, []AnchorFinding, AnchorCensus) {
	t.Helper()
	decls, findings, census, err := FindClusterAnchorLiterals(world)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	return decls, findings, census
}

// TestClusterAnchorInjection_ControlIsSilent — контроль: целое дерево молчит, и
// объём осмотренного при этом НЕ ноль.
//
// Проба, молчащая на пустом обходе, молчала бы и на сломанном дереве.
func TestClusterAnchorInjection_ControlIsSilent(t *testing.T) {
	decls, findings, census := anchorRun(t, anchorWorld())

	if len(findings) != 0 {
		t.Fatalf("контроль дал находки: %+v", findings)
	}
	if census.Declarations != 2 {
		t.Fatalf("объявлений %d, ожидалось 2 — контроль беспредметен", census.Declarations)
	}
	if census.Files == 0 || census.Literals == 0 {
		t.Fatalf("осмотрено файлов %d, литералов %d — молчание на пустом обходе ничего не значит",
			census.Files, census.Literals)
	}
	if census.Embedded != 1 {
		t.Fatalf("литералов с написанием внутри фразы %d, ожидался 1 — "+
			"законный близнец не осмотрен, и его молчание не доказано", census.Embedded)
	}
	if decls[0].Value != decls[1].Value {
		t.Fatalf("объявления контроля расходятся: %q против %q", decls[0].Value, decls[1].Value)
	}
}

// TestClusterAnchorInjection_BareAnchorLiteralIsFound — инъекция НОВОГО
// свойства: написание, повторённое литералом, краснеет и называет координату.
func TestClusterAnchorInjection_BareAnchorLiteralIsFound(t *testing.T) {
	world := anchorWorld()
	world["gateway/internal/middleware/authz.go"] = `package middleware

func anchor() string { return "cluster_kacho_root" }
`
	decls, findings, _ := anchorRun(t, world)

	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась одна: %+v", len(findings), findings)
	}
	f := findings[0]
	if f.File != "gateway/internal/middleware/authz.go" {
		t.Errorf("находка не называет координату: %+v", f)
	}
	if f.Line != 3 {
		t.Errorf("находка называет строку %d, ожидалась 3 — координата неверна", f.Line)
	}
	if f.Kind != "якорь" {
		t.Errorf("вид находки %q, ожидался «якорь»: читателю надо знать, какую из "+
			"двух форм он видит", f.Kind)
	}
	// Инъекция обязана ронять ТОЛЬКО проверяемое: согласие объявлений цело.
	if decls[0].Value != decls[1].Value {
		t.Errorf("инъекция литерала развела объявления — краснеет соседняя проверка, " +
			"и её молчание перестало что-либо значить")
	}
}

// TestClusterAnchorInjection_ObjectFormLiteralIsFound — та же инъекция во
// ВТОРОЙ форме: объект модели прав целиком.
//
// Отличие от предыдущей — ОДИН факт: форма литерала.
func TestClusterAnchorInjection_ObjectFormLiteralIsFound(t *testing.T) {
	world := anchorWorld()
	world["services/iam/internal/apps/bypass.go"] = `package apps

const object = "cluster:cluster_kacho_root"
`
	_, findings, _ := anchorRun(t, world)

	if len(findings) != 1 {
		t.Fatalf("находок %d, ожидалась одна: %+v", len(findings), findings)
	}
	if findings[0].Kind != "объект" {
		t.Errorf("вид находки %q, ожидался «объект»", findings[0].Kind)
	}
	if !strings.Contains(findings[0].Literal, ClusterAnchorObjectPrefix) {
		t.Errorf("находка не несёт формы объекта: %q", findings[0].Literal)
	}
}

// TestClusterAnchorInjection_DeclarationsDisagree — инъекция СОСЕДНЕГО
// свойства: объявления разошлись.
//
// Краснеть обязана ТОЛЬКО проверка согласия; перепись литералов при этом
// молчит — иначе её молчание в предыдущих пробах ничего не доказывало бы.
func TestClusterAnchorInjection_DeclarationsDisagree(t *testing.T) {
	world := anchorWorld()
	world["services/iam/internal/domain/constants.go"] = `package domain

const ClusterSingletonID = "cluster_root"
`
	decls, findings, census := anchorRun(t, world)

	if census.Declarations != 2 {
		t.Fatalf("объявлений %d, ожидалось 2", census.Declarations)
	}
	if decls[0].Value == decls[1].Value {
		t.Fatalf("объявления не разошлись — инъекция не сработала")
	}
	// Перепись литералов молчит: законный близнец собирает объект сложением, а
	// не литералом, и от смены написания это не меняется.
	if len(findings) != 0 {
		t.Errorf("перепись литералов покраснела на расхождении объявлений: %+v.\n"+
			"Инъекция обязана ронять только своё — иначе два свойства неразличимы", findings)
	}
}

// TestClusterAnchorInjection_NoDeclarationIsRefusal — предпосылка гейта: без
// объявлений он ОТКАЗЫВАЕТ, а не молчит.
//
// Молчание здесь было бы худшим из исходов: «литералов мимо объявления нет»
// тривиально верно на дереве, где объявления нет вовсе.
func TestClusterAnchorInjection_NoDeclarationIsRefusal(t *testing.T) {
	world := map[string]string{
		"services/iam/internal/apps/bypass.go": `package apps

const object = "cluster:cluster_kacho_root"
`,
	}
	_, _, census, err := FindClusterAnchorLiterals(world)
	if err == nil {
		t.Fatal("гейт промолчал без единого объявления — его вердикт беспредметен, " +
			"а «находок ноль» здесь тривиально верно")
	}
	if !strings.Contains(err.Error(), ClusterAnchorConstName) {
		t.Errorf("отказ не называет, чего не хватает: %v", err)
	}
	if census.Declarations != 0 {
		t.Errorf("перепись объявлений %d, ожидался ноль", census.Declarations)
	}
}
