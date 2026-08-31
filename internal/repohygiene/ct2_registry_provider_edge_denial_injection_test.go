// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// Доказательство способности гейта упасть И СМОЛЧАТЬ.
//
// Вход подаётся анализатору напрямую, а не через дерево: дерево починено, и
// «зелено на чистом дереве» доказывает ровно половину — ту, которая не может
// обнаружить поломку. Дефект подаётся ДОСЛОВНО тем текстом, который стоял в
// провайдере до #1646, а не синтетикой «похожего вида».

// edgeInjectionVerbs — контракт registry в объёме, нужном для суждения.
var edgeInjectionVerbs = map[string]bool{
	"GetRepository": true, "CreateRepository": true, "RenameRepository": true,
}

// wrapProviderSource — минимальный файл провайдера с одним описанием схемы.
func wrapProviderSource(body string) string {
	return "package provider\n\n// registryv1 — ссылка на контракт, по ней файл попадает в корпус.\n" +
		"func describe() string {\n\treturn " + body + "\n}\n"
}

func scanEdgeClaimOne(t *testing.T, source string) ([]EdgeClaimFinding, EdgeClaimCensus) {
	t.Helper()
	findings, census, err := ScanProviderEdgeClaims(map[string]string{"registry_repository_resource.go": source}, edgeInjectionVerbs)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	return findings, census
}

// (а) НАСТОЯЩИЙ дефект — текст, стоявший в дереве до #1646.
func TestEdgeDenialInjection_HistoricalTextIsFound(t *testing.T) {
	findings, census := scanEdgeClaimOne(t, wrapProviderSource(
		`"Имя репозитория. Может содержать косые черты " +`+"\n\t\t"+
			`"(`+"`team/service`"+`). Переименования у края нет: изменение пересоздаёт " +`+"\n\t\t"+
			`"репозиторий вместе со всем его содержимым."`))

	if len(findings) != 1 {
		t.Fatalf("исторический дефект не найден: находок %d, %s", len(findings), census)
	}
	got := findings[0].String()
	for _, want := range []string{"registry_repository_resource.go:", "Rename", "переименован"} {
		if !strings.Contains(got, want) {
			t.Errorf("находка не называет %q — читателя посылают искать не там: %s", want, got)
		}
	}
	if findings[0].Affirmative {
		t.Error("отрицание существующего глагола классифицировано как утверждение")
	}
}

// (б) ЗАКОННЫЙ БЛИЗНЕЦ — отрицание, чей предмет глаголом контракта НЕ является.
// Текст живой, он стоит в дереве и обязан остаться: переноса между реестрами в
// контракте нет вовсе (D-5), и утверждение истинно.
func TestEdgeDenialInjection_TrueDenialOfANonVerbIsSilent(t *testing.T) {
	findings, census := scanEdgeClaimOne(t, wrapProviderSource(
		`"Реестр-владелец. Перенос репозитория между реестрами " +`+"\n\t\t"+
			`"краем не поддержан — изменение пересоздаёт ресурс."`))

	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на ИСТИННОМ отрицании — первый же ложный срабат его отключит: %v", findings)
	}
	if census.Claims == 0 {
		t.Error("утверждение о крае не опознано вовсе: молчание здесь означало бы «не читал», " +
			"а не «прочитал и согласен»")
	}
}

// (в) ЗАКОННЫЙ БЛИЗНЕЦ — утверждение о глаголе, который в контракте ЕСТЬ.
func TestEdgeDenialInjection_TrueAffirmationIsSilent(t *testing.T) {
	findings, _ := scanEdgeClaimOne(t, wrapProviderSource(
		`"Переименование у края есть, провайдер им не пользуется намеренно."`))
	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на истинном утверждении: %v", findings)
	}
}

// (г) ОБРАТНАЯ СТОРОНА — провайдер утверждает глагол, которого в контракте нет.
// Утверждение о возможности стареет тише отрицания: оно не мешает работать, пока
// клиент по нему не пойдёт.
func TestEdgeDenialInjection_AffirmationOfAnAbsentVerbIsFound(t *testing.T) {
	findings, _, err := ScanProviderEdgeClaims(
		map[string]string{"registry_repository_resource.go": wrapProviderSource(
			`"Переименование у края есть — зовите его вместо пересоздания."`)},
		map[string]bool{"GetRepository": true}) // контракт БЕЗ Rename*
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("утверждение о несуществующем глаголе не найдено: находок %d", len(findings))
	}
	if !findings[0].Affirmative {
		t.Error("классифицировано как отрицание, хотя это утверждение")
	}
}

// (д) ГРАНИЦА ПРЕДЛОЖЕНИЯ — отрицание в одной фразе, глагол в другой. Находки
// быть не должно: её никто не писал.
func TestEdgeDenialInjection_MarkerAndNounInDifferentSentencesAreSilent(t *testing.T) {
	findings, census := scanEdgeClaimOne(t, wrapProviderSource(
		`"Полудиапазона у края нет. Переименование делают отдельным вызовом."`))
	if len(findings) != 0 {
		t.Fatalf("отрицание встретилось с глаголом из ЧУЖОЙ фразы: %v", findings)
	}
	if census.Sentences < 2 {
		t.Errorf("предложения не разделены (прочитано %d) — граница не проверена", census.Sentences)
	}
}

// (е) СВЁРТКА КОНКАТЕНАЦИИ — утверждение разорвано ровно по границе литералов.
// Без свёртки ни один литерал маркера целиком не несёт, и гейт молчал бы на
// настоящем дефекте.
func TestEdgeDenialInjection_ClaimSplitAcrossLiteralsIsFound(t *testing.T) {
	findings, _ := scanEdgeClaimOne(t, wrapProviderSource(
		`"Имя репозитория. Переименования у края " +`+"\n\t\t"+`"нет: изменение пересоздаёт ресурс."`))
	if len(findings) != 1 {
		t.Fatalf("разорванное по литералам утверждение не найдено: находок %d", len(findings))
	}
}

// (ж) ПРЕДПОСЫЛКА — глагол ушёл из контракта. Отрицающая половина гейта стала бы
// вакуумной МОЛЧА, поэтому предпосылка обязана заявить о себе сама.
func TestEdgeDenialInjection_PremiseFailsWhenTheVerbLeavesTheContract(t *testing.T) {
	if _, ok := EdgeClaimPremiseHolds(edgeInjectionVerbs); !ok {
		t.Fatal("предпосылка не держится на живом контракте — гейт красен на исправном дереве")
	}
	missing, ok := EdgeClaimPremiseHolds(map[string]bool{"GetRepository": true})
	if ok {
		t.Fatal("контракт без Rename* не уронил предпосылку: половина гейта тихо стала вакуумной")
	}
	if !strings.Contains(missing, "Rename") {
		t.Errorf("отказ предпосылки не называет глагол: %q", missing)
	}
}

// (з) ПУСТОЙ КОРПУС — перепись обязана показать ноль, а не выглядеть как чистое дерево.
func TestEdgeDenialInjection_EmptyCorpusIsVisibleInTheCensus(t *testing.T) {
	findings, census, err := ScanProviderEdgeClaims(map[string]string{}, edgeInjectionVerbs)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("находки на пустом корпусе: %v", findings)
	}
	if census.Files != 0 || census.Sentences != 0 {
		t.Errorf("перепись пустого корпуса непуста: %s", census)
	}
}
