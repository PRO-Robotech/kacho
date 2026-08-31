// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// Доказательство способности гейта упасть И СМОЛЧАТЬ — прогоняется ЗАНОВО после
// переустройства анализатора (многодоменный резолв, токены действий, корпус `.tf`,
// снятие переноса строк). Совпадение переписей этого не заменяет: гейт, потерявший
// способность краснеть, на чистом дереве выглядит точно так же.
//
// Вход подаётся анализатору напрямую: дерево починено, и «зелено на чистом дереве»
// доказывает ровно ту половину, которая не может обнаружить поломку. Дефект
// подаётся ДОСЛОВНО текстом, стоявшим в провайдере до #1646.

// Контракты-фикстуры. Различие между ними — несущее: `Move` объявлен у
// балансировщика и не объявлен у registry, ровно как в дереве.
var (
	edgeFxRegistry = EdgeContract{Domain: "registry",
		RPCs:    map[string]bool{"GetRepository": true, "RenameRepository": true},
		Actions: map[string]bool{":rename": true}}
	edgeFxNLB = EdgeContract{Domain: "loadbalancer",
		RPCs:    map[string]bool{"Move": true, "UpdateTargetGroup": true},
		Actions: map[string]bool{":move": true},
		// Пара «служба/метод» здесь несущая: `AddTargets` объявлен У ГРУППЫ ЦЕЛЕЙ и
		// НЕ объявлен у слушателя — ровно как в дереве. На ней доказывается, что
		// решает объект, а не глагол.
		Methods: map[string]bool{
			"TargetGroupService/AddTargets": true,
			"TargetGroupService/Update":     true,
			"ListenerService/Update":        true,
		}}
	edgeFxCompute = EdgeContract{Domain: "compute",
		RPCs:    map[string]bool{"Start": true, "Stop": true},
		Actions: map[string]bool{":start": true, ":stop": true}}
)

func edgeFxContracts() map[string]EdgeContract {
	return map[string]EdgeContract{
		"registry": edgeFxRegistry, "loadbalancer": edgeFxNLB, "compute": edgeFxCompute}
}

// wrapProviderSource — минимальный файл провайдера с одним описанием схемы.
func wrapProviderSource(body string) string {
	return "package provider\n\nfunc describe() string {\n\treturn " + body + "\n}\n"
}

func scanEdgeClaimOne(t *testing.T, src EdgeSource) ([]EdgeClaimFinding, EdgeClaimCensus) {
	t.Helper()
	findings, _, census, err := ScanProviderEdgeClaims([]EdgeSource{src}, edgeFxContracts())
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	return findings, census
}

func goSource(domain, body string) EdgeSource {
	return EdgeSource{Path: "registry_repository_resource.go", Kind: "go",
		Domain: domain, Text: wrapProviderSource(body)}
}

// (а) НАСТОЯЩИЙ дефект — текст, стоявший в дереве до #1646.
func TestEdgeDenialInjection_HistoricalTextIsFound(t *testing.T) {
	findings, census := scanEdgeClaimOne(t, goSource("registry",
		`"Имя репозитория. Может содержать косые черты " +`+"\n\t\t"+
			`"(`+"`team/service`"+`). Переименования у края нет: изменение пересоздаёт " +`+"\n\t\t"+
			`"репозиторий вместе со всем его содержимым."`))

	if len(findings) != 1 {
		t.Fatalf("исторический дефект не найден: находок %d, %s", len(findings), census)
	}
	got := findings[0].String()
	for _, want := range []string{"registry_repository_resource.go:", "переименован", "RenameRepository", "registry"} {
		if !strings.Contains(got, want) {
			t.Errorf("находка не называет %q — читателя посылают искать не там: %s", want, got)
		}
	}
	if findings[0].Affirmative {
		t.Error("отрицание существующего глагола классифицировано как утверждение")
	}
	if census.Resolved == 0 {
		t.Error("утверждение не отмечено резолвящимся — перепись обманывает в свою пользу")
	}
}

// (б) ДОМЕН РЕШАЕТ. Та же фраза — находка там, где глагол есть, и молчание там,
// где его нет. Без этой пары гейт, резолвящий по всему дереву контрактов сразу,
// объявил бы находкой шесть исправных утверждений дерева.
func TestEdgeDenialInjection_DomainDecidesTheSameSentence(t *testing.T) {
	const sentence = `"Сеть группы неизменяема: операции переноса между сетями у края не существует."`

	quiet, _ := scanEdgeClaimOne(t, goSource("registry", sentence))
	if len(quiet) != 0 {
		t.Errorf("домен без Move* краснеет на ИСТИННОМ отрицании: %v", quiet)
	}

	loud, _ := scanEdgeClaimOne(t, goSource("loadbalancer", sentence))
	if len(loud) != 1 {
		t.Fatalf("домен, где Move объявлен, не дал находки: находок %d", len(loud))
	}
	if !strings.Contains(loud[0].String(), "loadbalancer") {
		t.Errorf("находка не называет домен: %s", loud[0].String())
	}
}

// (в) ЗАКОННЫЙ БЛИЗНЕЦ — отрицание, чей предмет в словаре не значится.
//
// # Пример пробы ЗАМЕНЁН вместе с предметом гейта
//
// Здесь стояло «…а не обновлением цели, которого у края нет» — и оно перестало быть
// законным близнецом: «обновление» есть РОДОВОЕ имя действия, и такое утверждение
// теперь находка третьего вида (см. ниже, полоса «родовое имя»). Оставить прежний
// пример значило бы держать пробу, утверждающую молчание там, где гейт обязан
// говорить. Взят живой из дерева близнец того же класса: предмет назван не
// действием, а ЗНАЧЕНИЕМ, и резолвить его не с чем ни при какой форме.
func TestEdgeDenialInjection_ClaimOutsideTheDictionaryIsCountedNotJudged(t *testing.T) {
	findings, census := scanEdgeClaimOne(t, goSource("loadbalancer",
		`"Границы задаются вместе: полудиапазона у края нет."`))
	if len(findings) != 0 {
		t.Fatalf("гейт судит предмет, который словарь не резолвит: %v", findings)
	}
	if census.Claims != 1 {
		t.Errorf("утверждение не опознано вовсе (claims=%d): молчание означало бы «не читал», "+
			"а не «прочитал и не смог сверить»", census.Claims)
	}
	if census.Resolved != 0 {
		t.Errorf("нерезолвящееся утверждение засчитано как проверенное (resolved=%d)", census.Resolutions)
	}
}

// (г) ЗАКОННЫЙ БЛИЗНЕЦ — утверждение о глаголе, который в контракте ЕСТЬ.
func TestEdgeDenialInjection_TrueAffirmationIsSilent(t *testing.T) {
	findings, _ := scanEdgeClaimOne(t, goSource("registry",
		`"Переименование у края есть, провайдер им не пользуется намеренно."`))
	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на истинном утверждении: %v", findings)
	}
}

// (д) ОБРАТНАЯ СТОРОНА — утверждение о глаголе, которого нет. Стареет тише
// отрицания: не мешает работать, пока клиент по нему не пойдёт.
func TestEdgeDenialInjection_AffirmationOfAnAbsentVerbIsFound(t *testing.T) {
	findings, _ := scanEdgeClaimOne(t, goSource("registry",
		`"Перенос между реестрами у края есть — зовите его вместо пересоздания."`))
	if len(findings) != 1 {
		t.Fatalf("утверждение о несуществующем глаголе не найдено: находок %d", len(findings))
	}
	if !findings[0].Affirmative {
		t.Error("классифицировано как отрицание, хотя это утверждение")
	}
}

// (е) ТОКЕН ДЕЙСТВИЯ — второй способ назвать предмет, морфологии не требующий.
// Пара: существующий токен молчит, отсутствующий краснеет.
func TestEdgeDenialInjection_ActionTokenIsResolvedBothWays(t *testing.T) {
	ok, census := scanEdgeClaimOne(t, goSource("compute",
		"\"У края есть действия `:start` и `:stop` — провайдер их не выражает.\""))
	if len(ok) != 0 {
		t.Fatalf("существующие токены дали находку: %v", ok)
	}
	if census.Resolutions != 2 {
		t.Errorf("сверок токенов %d вместо двух — часть предмета не осмотрена", census.Resolutions)
	}

	bad, _ := scanEdgeClaimOne(t, goSource("compute",
		"\"У края есть действие `:hibernate` — зовите его.\""))
	if len(bad) != 1 {
		t.Fatalf("токен, которого в контракте нет, не найден: находок %d", len(bad))
	}
	if !strings.Contains(bad[0].String(), ":hibernate") {
		t.Errorf("находка не называет токен: %s", bad[0].String())
	}
}

// (е2) ТОКЕН БЕЗ МАРКЕРА — назвать действие края значит утверждать, что оно есть.
// Требуй здесь маркера — и находка осталась бы вне наблюдения: «у контракта есть»
// в закрытый набор форм не входит и входить не должно (гонка за прозой бесконечна).
func TestEdgeDenialInjection_TokenWithoutAnyMarkerIsStillAClaim(t *testing.T) {
	findings, census := scanEdgeClaimOne(t, goSource("compute",
		"\"У контракта есть `:hibernate` — и он отвечает 501.\""))
	if len(findings) != 1 {
		t.Fatalf("токен без маркера не сужден: находок %d, %s", len(findings), census)
	}
	if census.Claims != 1 {
		t.Errorf("предложение с токеном не опознано утверждением (claims=%d)", census.Claims)
	}
}

// (е3) ДЕФИС В ТОКЕНЕ — законная форма записи, и обе стороны обязаны её знать.
// Пара намеренная: слева контракт токен НЕСЁТ, справа не несёт. Первая редакция
// знала дефис только в прозе и объявила несуществующими четыре токена, которые
// контракт объявляет, — то есть ложные находки от собственной асимметрии.
func TestEdgeDenialInjection_HyphenatedTokenIsKnownOnBothSides(t *testing.T) {
	contracts := map[string]EdgeContract{"vpc": {Domain: "vpc",
		RPCs:    map[string]bool{"AddCidrBlocks": true},
		Actions: map[string]bool{":add-cidr-blocks": true}}}

	quiet, _, _, err := ScanProviderEdgeClaims([]EdgeSource{{
		Path: "vpc_cidr_group_resource.go", Kind: "go", Domain: "vpc",
		Text: wrapProviderSource("\"Изменяется действиями края (`:add-cidr-blocks`).\"")}}, contracts)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(quiet) != 0 {
		t.Fatalf("существующий дефисный токен объявлен несуществующим: %v", quiet)
	}

	loud, _, _, err := ScanProviderEdgeClaims([]EdgeSource{{
		Path: "vpc_route_table_resource.go", Kind: "go", Domain: "vpc",
		Text: wrapProviderSource("\"Изменяется действиями края (`:add-routes`).\"")}}, contracts)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(loud) != 1 {
		t.Fatalf("несуществующий дефисный токен не найден: находок %d", len(loud))
	}
	if !strings.Contains(loud[0].String(), ":add-routes") {
		t.Errorf("находка не называет токен: %s", loud[0].String())
	}
}

// (ж) ПЕРЕНОС СТРОКИ ВНУТРИ АБЗАЦА снимается. Проза переносится по ширине, и
// утверждение свободно разрывается посередине; деление по строкам теряло бы его
// молча — предмет и маркер оказывались бы в разных единицах суждения.
func TestEdgeDenialInjection_ClaimWrappedAcrossLinesIsFound(t *testing.T) {
	findings, _ := scanEdgeClaimOne(t, EdgeSource{
		Path: "vpc_security_group_resource.go", Kind: "go", Domain: "loadbalancer",
		Text: "package provider\n\n" +
			"// Сеть у группы обязательна и неизменяема, операции переноса группы между\n" +
			"// сетями у края не существует.\nfunc f() {}\n"})
	if len(findings) != 1 {
		t.Fatalf("утверждение, разорванное переносом строки, не найдено: находок %d", len(findings))
	}
}

// (з) ГРАНИЦА АБЗАЦА при этом сохраняется: отрицание из одного абзаца не встречает
// глагол из другого.
func TestEdgeDenialInjection_MarkerAndNounInDifferentSentencesAreSilent(t *testing.T) {
	findings, census := scanEdgeClaimOne(t, goSource("loadbalancer",
		`"Полудиапазона у края нет. Перенос делают отдельным вызовом."`))
	if len(findings) != 0 {
		t.Fatalf("отрицание встретилось с глаголом из ЧУЖОЙ фразы: %v", findings)
	}
	if census.Sentences < 2 {
		t.Errorf("предложения не разделены (прочитано %d) — граница не проверена", census.Sentences)
	}
}

// (и) СВЁРТКА КОНКАТЕНАЦИИ — утверждение разорвано по границе литералов.
func TestEdgeDenialInjection_ClaimSplitAcrossLiteralsIsFound(t *testing.T) {
	findings, _ := scanEdgeClaimOne(t, goSource("registry",
		`"Имя репозитория. Переименования у края " +`+"\n\t\t"+`"нет: изменение пересоздаёт ресурс."`))
	if len(findings) != 1 {
		t.Fatalf("разорванное по литералам утверждение не найдено: находок %d", len(findings))
	}
}

// (к) ФОРМА `.tf` — отдельный вид записи предмета, и доказывается отдельно.
// Модули несут утверждения о крае и до расширения корпуса не судились ничем.
func TestEdgeDenialInjection_ModuleFormIsJudged(t *testing.T) {
	quiet, census := scanEdgeClaimOne(t, EdgeSource{
		Path: "terraform/modules/registry-space/variables.tf", Kind: "tf", Domain: "registry",
		Text: "variable \"region_id\" {\n  description = \"Регион реестра. Неизменяем: перенос\n" +
			"    между регионами краем не поддержан.\"\n}\n"})
	if len(quiet) != 0 {
		t.Fatalf("истинное отрицание в модуле дало находку: %v", quiet)
	}
	if census.Claims != 1 || census.Resolved != 1 {
		t.Fatalf("утверждение модуля не осмотрено (claims=%d resolved=%d): форма `.tf` вне наблюдения",
			census.Claims, census.Resolutions)
	}

	loud, _ := scanEdgeClaimOne(t, EdgeSource{
		Path: "terraform/modules/nlb-service/variables.tf", Kind: "tf", Domain: "loadbalancer",
		Text: "# Перенос между балансировщиками краем не поддержан.\n"})
	if len(loud) != 1 {
		t.Fatalf("ложное отрицание в модуле не найдено: находок %d", len(loud))
	}
	if !strings.Contains(loud[0].String(), "variables.tf:") {
		t.Errorf("находка не называет координату файла модуля: %s", loud[0].String())
	}
}

// (л) ФАЙЛ БЕЗ ДОМЕНА — общая оболочка стабов не импортирует, резолвить не с чем.
// Утверждение обязано быть СОСЧИТАНО и НЕ объявлено проверенным.
func TestEdgeDenialInjection_FileWithoutADomainIsCountedNotJudged(t *testing.T) {
	findings, census := scanEdgeClaimOne(t, goSource("",
		`"Снятия у края нет: глагол переводит ресурс с одного значения на другое."`))
	if len(findings) != 0 {
		t.Fatalf("файл без домена судится вслепую: %v", findings)
	}
	if census.Claims != 1 {
		t.Errorf("утверждение не сосчитано (claims=%d) — оно выглядит проверенным", census.Claims)
	}
	if census.Resolved != 0 {
		t.Errorf("утверждение без домена объявлено проверенным (resolved=%d)", census.Resolutions)
	}
}

// (м) ПРЕДПОСЫЛКА — глагол ушёл из контрактов. Отрицающая половина стала бы
// вакуумной МОЛЧА, поэтому предпосылка обязана заявить о себе сама.
func TestEdgeDenialInjection_PremiseFailsWhenTheVerbLeavesTheContracts(t *testing.T) {
	if _, ok := EdgeClaimPremiseHolds(edgeFxContracts()); !ok {
		t.Fatal("предпосылка не держится на живых контрактах — гейт красен на исправном дереве")
	}
	missing, ok := EdgeClaimPremiseHolds(map[string]EdgeContract{
		"registry": {Domain: "registry", RPCs: map[string]bool{"GetRepository": true}}})
	if ok {
		t.Fatal("контракты без Rename*/Move* не уронили предпосылку: половина гейта тихо стала вакуумной")
	}
	for _, want := range []string{"Rename", "Move"} {
		if !strings.Contains(missing, want) {
			t.Errorf("отказ предпосылки не называет %q: %q", want, missing)
		}
	}
}

// (н) ПУСТОЙ КОРПУС — перепись обязана показать ноль, а не выглядеть как чистое дерево.
func TestEdgeDenialInjection_EmptyCorpusIsVisibleInTheCensus(t *testing.T) {
	findings, _, census, err := ScanProviderEdgeClaims(nil, edgeFxContracts())
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("находки на пустом корпусе: %v", findings)
	}
	if census.Files != 0 || census.Sentences != 0 || census.Claims != 0 {
		t.Errorf("перепись пустого корпуса непуста: %s", census)
	}
}

// ---------------------------------------------------------------------------
// Полоса «СЛУЖБА С МЕТОДОМ» — третий способ назвать предмет (#1728).
//
// Доказывается ПО КАЖДОЙ форме отдельно и в обе стороны. Голого имени RPC такой
// полосы не бывает намеренно: имя объекта не несёт (`rpc Update` — в шести доменах
// из шести), поэтому резолв по нему отвечал бы «есть» на любой глагол.
// ---------------------------------------------------------------------------

// (ж1) ЛОЖНОЕ ОТРИЦАНИЕ квалифицированного метода — находка.
func TestEdgeDenialInjection_QualifiedMethodDeniedButDeclaredIsFound(t *testing.T) {
	findings, census := scanEdgeClaimOne(t, goSource("loadbalancer",
		"\"Добавления целей у края нет: метода `TargetGroupService/AddTargets` контракт не объявляет.\""))
	if len(findings) != 1 {
		t.Fatalf("ложное отрицание объявленного метода не найдено: находок %d, %s", len(findings), census)
	}
	got := findings[0].String()
	for _, want := range []string{"TargetGroupService/AddTargets", "loadbalancer"} {
		if !strings.Contains(got, want) {
			t.Errorf("находка не называет %q — читателя посылают искать не там: %s", want, got)
		}
	}
	if findings[0].Affirmative || findings[0].Unnamed {
		t.Errorf("вид находки не тот: отрицание объявленного — не утверждение и не безымянность: %s", got)
	}
}

// (ж2) УТВЕРЖДЕНИЕ метода, которого нет, — обратная сторона той же полосы.
func TestEdgeDenialInjection_QualifiedMethodAffirmedButAbsentIsFound(t *testing.T) {
	findings, _ := scanEdgeClaimOne(t, goSource("loadbalancer",
		"\"У края есть `TargetGroupService/UpdateTarget` — зовите его вместо пары снятие+добавление.\""))
	if len(findings) != 1 {
		t.Fatalf("утверждение о необъявленном методе не найдено: находок %d", len(findings))
	}
	if !findings[0].Affirmative {
		t.Errorf("классифицировано как отрицание, хотя это утверждение: %s", findings[0].String())
	}
}

// (ж3) ЗАКОННЫЙ БЛИЗНЕЦ — отрицание метода, которого и вправду нет. Это ровно та
// форма, которой переписаны пять живых утверждений дерева.
func TestEdgeDenialInjection_QualifiedMethodDeniedAndAbsentIsSilent(t *testing.T) {
	findings, census := scanEdgeClaimOne(t, goSource("loadbalancer",
		"\"Смена веса выражается снятием и добавлением, а не изменением цели: метода "+
			"`TargetGroupService/UpdateTarget` у края нет.\""))
	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на ИСТИННОМ отрицании: %v", findings)
	}
	if census.Resolved != 1 {
		t.Errorf("истинное отрицание не засчитано резолвящимся (resolved=%d) — тогда починка "+
			"пяти живых утверждений не была бы видна переписью", census.Resolved)
	}
}

// (ж4) ЗАКОННЫЙ БЛИЗНЕЦ — утверждение о методе, который объявлен.
func TestEdgeDenialInjection_QualifiedMethodAffirmedAndDeclaredIsSilent(t *testing.T) {
	findings, census := scanEdgeClaimOne(t, goSource("loadbalancer",
		"\"Цели правит `TargetGroupService/AddTargets`, а группу — `TargetGroupService/Update`.\""))
	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на истинном упоминании объявленных методов: %v", findings)
	}
	if census.Resolutions != 2 {
		t.Errorf("сверок %d, а методов названо два — перепись объявляет осмотренным не то, "+
			"что осмотрено", census.Resolutions)
	}
}

// (ж5) РЕШАЕТ ОБЪЕКТ, А НЕ ГЛАГОЛ — прямой ответ на измеренную ловушку.
//
// Один и тот же глагол `AddTargets` объявлен у группы целей и НЕ объявлен у
// слушателя. Отрицание молчит там, где метода нет, и краснеет там, где он есть, —
// при ОДНОМ И ТОМ ЖЕ глаголе и ОДНОМ И ТОМ ЖЕ домене. Резолв по голому имени RPC
// такой пары различить не может by construction.
func TestEdgeDenialInjection_ServiceNotVerbDecidesTheVerdict(t *testing.T) {
	quiet, _ := scanEdgeClaimOne(t, goSource("loadbalancer",
		"\"У слушателя целей не бывает: метода `ListenerService/AddTargets` у края нет.\""))
	if len(quiet) != 0 {
		t.Errorf("отрицание метода, которого у ЭТОЙ службы нет, объявлено находкой: %v", quiet)
	}

	loud, _ := scanEdgeClaimOne(t, goSource("loadbalancer",
		"\"У группы целей добавления нет: метода `TargetGroupService/AddTargets` у края нет.\""))
	if len(loud) != 1 {
		t.Fatalf("отрицание метода, объявленного У ЭТОЙ службы, не дало находки: находок %d", len(loud))
	}
}

// ---------------------------------------------------------------------------
// Полоса «РОДОВОЕ ИМЯ ДЕЙСТВИЯ» — требование ФОРМЫ (#1728).
//
// Утверждение, чей предмет назван родовым именем и не назван токеном, не сверяется
// с контрактом ни в одну сторону — и выглядит проверенным ровно так же, как
// проверенное.
// ---------------------------------------------------------------------------

// (з1) РОДОВОЕ ИМЯ БЕЗ ТОКЕНА — находка. Дефект подан ДОСЛОВНО текстом, стоявшим в
// дереве до этой правки.
func TestEdgeDenialInjection_GenericActionNounWithoutATokenIsFound(t *testing.T) {
	findings, census := scanEdgeClaimOne(t, goSource("loadbalancer",
		`"Смена веса выражается снятием и добавлением, а не обновлением цели, которого у края нет."`))
	if len(findings) != 1 {
		t.Fatalf("родовое имя без токена не найдено: находок %d, %s", len(findings), census)
	}
	if !findings[0].Unnamed {
		t.Errorf("вид находки не тот — это не расхождение с контрактом, а безымянность: %s",
			findings[0].String())
	}
	got := findings[0].String()
	for _, want := range []string{"обновлен", "loadbalancer", "TargetGroupService/AddTargets"} {
		if !strings.Contains(got, want) {
			t.Errorf("находка не называет %q: без имени предмета и без образца формы "+
				"её нечем закрыть: %s", want, got)
		}
	}
	if census.Resolved != 0 {
		t.Errorf("безымянное утверждение засчитано проверенным (resolved=%d)", census.Resolved)
	}
}

// (з2) ЗАКОННЫЙ БЛИЗНЕЦ — то же родовое имя, но предмет назван токеном. Форма
// исполнена, и гейт молчит.
func TestEdgeDenialInjection_GenericActionNounNamedByATokenIsSilent(t *testing.T) {
	findings, census := scanEdgeClaimOne(t, goSource("loadbalancer",
		"\"Обновления цели у края нет: метода `TargetGroupService/UpdateTarget` контракт не объявляет.\""))
	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на утверждении, ФОРМУ которого он и требует: %v", findings)
	}
	if census.Resolved != 1 {
		t.Errorf("названное токеном утверждение не засчитано резолвящимся (resolved=%d)", census.Resolved)
	}
}

// (з3) ЗАКОННЫЙ БЛИЗНЕЦ — родовое имя ВНЕ окна: оно стоит ПОСЛЕ маркера и предметом
// утверждения не является.
//
// Пример живой, из дерева: предмет здесь — «привязка», а «удаление» отстоит на
// девять слов и в другую сторону. Предикат по всему предложению объявил бы это
// находкой, и первый же ложный срабат снял бы полосу целиком.
func TestEdgeDenialInjection_GenericNounOutsideTheWindowIsSilent(t *testing.T) {
	findings, census := scanEdgeClaimOne(t, goSource("loadbalancer",
		`"Отозванные строки в обход ВКЛЮЧЕНЫ: они существуют, и «наш идентификатор среди них» `+
			`означает, что привязка у края есть, — то самое расхождение с чтением, о котором `+
			`вызывающему сообщают отдельно, а не удаление."`))
	if len(findings) != 0 {
		t.Fatalf("родовое имя из соседнего придаточного объявлено предметом утверждения: %v", findings)
	}
	if census.Claims != 1 {
		t.Errorf("утверждение не опознано вовсе (claims=%d): молчание тогда означает "+
			"«не читал», а не «прочитал и не смог сверить»", census.Claims)
	}
}

// (з4) ЗАКОННЫЙ БЛИЗНЕЦ — родовое имя есть, но утверждение уже резолвится СЛОВАРЁМ.
// Полоса включается только там, где не сработала ни одна другая.
func TestEdgeDenialInjection_GenericNounIsSilentWhenTheDictionaryResolved(t *testing.T) {
	findings, census := scanEdgeClaimOne(t, goSource("registry",
		`"Изменение пересоздаёт ресурс: операции переноса между реестрами у края не существует."`))
	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на утверждении, которое СВЕРЕНО словарём: %v", findings)
	}
	if census.Resolved != 1 {
		t.Errorf("утверждение не отмечено резолвящимся (resolved=%d)", census.Resolved)
	}
}

// (з5) КОНТРОЛЬ КОРПУСА — родовое имя без маркера края утверждением о крае не
// является вовсе. Без этой пробы полоса могла бы расширить корпус молча.
func TestEdgeDenialInjection_GenericNounWithoutAMarkerIsNotAClaim(t *testing.T) {
	findings, census := scanEdgeClaimOne(t, goSource("loadbalancer",
		`"Обновление цели провайдер выражает снятием и добавлением."`))
	if len(findings) != 0 {
		t.Fatalf("предложение без маркера края объявлено находкой: %v", findings)
	}
	if census.Claims != 0 {
		t.Errorf("предложение без маркера и без токена засчитано утверждением о крае "+
			"(claims=%d) — корпус расширен молча", census.Claims)
	}
}
