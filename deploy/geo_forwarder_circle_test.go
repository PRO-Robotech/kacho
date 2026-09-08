// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// geo_forwarder_circle_test.go — круг отправителей чужой личности у geo обязан
// покрывать ФАКТИЧЕСКИХ отправителей, а не «наверное, только шлюз».
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Сервис, принимающий переданную личность, сужает круг законных отправителей
// списком SAN'ов. Отправитель ВНЕ круга не «ходит под своим сертификатом» — его
// форвардленная личность СНИМАЕТСЯ (grpcsrv.UnaryTrustedPrincipalExtract кладёт
// operations.WithoutPrincipal), и принимающая сторона видит вызов БЕЗ
// принципала. Дальше per-RPC Check отвечает fail-closed.
//
// У geo круг состоял из одного шлюза, тогда как справочник размещения читают на
// пути запроса пять сервисов — и каждый форвардит личность инициатора. Пока
// чтения справочника стояли на полосе освобождения, проверка не выполнялась и
// расхождение не проявлялось НИЧЕМ. Как только чтения вернулись под отношение
// `viewer`, каждый peer-вызов стал получать отказ, а потребитель разворачивал
// его в «зоны не существует» (полоса peer-validate неразличима снаружи by
// design) — то есть симптом называл НЕ ТУ причину.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ГЕЙТ, А НЕ ВНИМАНИЕ
//
// Соседний deploy/tests/helm/trusted-forwarder-profiles-test.sh проверяет, что
// круг СУЖЕН (непуст). Это верное требование, и оно НЕ ловит этот класс: список
// из одного шлюза сужен идеально и при этом неверен. Ошибку такого рода нельзя
// увидеть в диффе — она видна только сопоставлением списка с графом вызовов.
//
// ПЕРЕЧЕНЬ ОТПРАВИТЕЛЕЙ ВЫВОДИТСЯ ИЗ ДЕРЕВА, А НЕ ВЫПИСЫВАЕТСЯ ЗДЕСЬ: рукописная
// копия разошлась бы с кодом молча, и гейт стал бы печатью. Появится шестой
// потребитель справочника — он попадёт в перечень сам и потребует записи в
// круге; исчезнет потребитель — требование снимется само.
//
// СЛОВАРЬ SAN'ов ТОЖЕ НЕ ПИШЕТСЯ ЗДЕСЬ. Он берётся из круга kaname — того
// единственного списка в дереве, чей состав выводит рендером peer-чартов
// deploy/tests/helm/iam-trusted-forwarder-test.sh. iam зовут все, поэтому его
// круг — надмножество, и второй копии словаря не заводится. Это существенно:
// у storage пространство имён в SPIFFE-идентификаторе своё, и запись «по
// образцу соседа» не совпала бы с предъявляемым сертификатом — отказ был бы
// молчаливым.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// geoClientCtor — конструкторы клиентов справочника размещения. Признак того,
// что сервис ЗВОНИТ в geo, а не просто импортирует его типы: импорт стабов есть
// и у тех, кто лишь перекладывает ответ.
var geoClientCtor = regexp.MustCompile(`New(Zone|Region)ServiceClient\(`)

// geoSenders — сервисы, строящие клиента geo в не-тестовом коде, выведенные из
// дерева. Ключ — каталог сервиса (`services/<svc>/`), значение — файлы-улики.
func geoSenders(t *testing.T) map[string][]string {
	t.Helper()
	files := trackedFiles(t, "services/*/internal/**/*.go")
	if len(files) == 0 {
		t.Fatal("обход не прочитал НИ ОДНОГО файла сервисов — «ноль отправителей» здесь " +
			"означало бы «ноль прочитанного», а не «никто не звонит в geo»")
	}
	senders := map[string][]string{}
	scanned := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		scanned++
		parts := strings.Split(f, string(filepath.Separator))
		if len(parts) < 2 {
			continue
		}
		svc := parts[1]
		if svc == "geo" { // geo не звонит сам себе
			continue
		}
		b, err := os.ReadFile(filepath.Join(repoRoot, f))
		if err != nil {
			t.Fatalf("прочитать %s: %v", f, err)
		}
		if geoClientCtor.Match(b) {
			senders[svc] = append(senders[svc], f)
		}
	}
	t.Logf("перепись: не-тестовых файлов сервисов осмотрено %d; строят клиента geo — сервисов %d",
		scanned, len(senders))
	if len(senders) == 0 {
		t.Fatal("ни один сервис не строит клиента geo — предпосылка гейта исчезла: " +
			"либо ребро снято (тогда снимается и гейт), либо признак перестал ловить конструктор")
	}
	return senders
}

// sanBlock — строки `- spiffe://…` из именованного блока списка YAML.
func sanBlock(body, key string) []string {
	var out []string
	in := false
	for _, ln := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, key+":") {
			in = true
			continue
		}
		if !in {
			continue
		}
		if strings.HasPrefix(trimmed, "- spiffe://") {
			out = append(out, strings.TrimPrefix(trimmed, "- "))
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		break // блок кончился на первой не-записи
	}
	return out
}

// spiffeSANByService — словарь «сервис → SAN, который он ПРЕДЪЯВЛЯЕТ», взятый из
// круга kaname (единственный список дерева, чей состав выводится рендером
// peer-чартов). Ключ — часть имени после `sa/kacho-`.
func spiffeSANByService(t *testing.T) map[string]string {
	t.Helper()
	const rel = "deploy/helm/umbrella/charts/kaname/values.yaml"
	sans := sanBlock(readFile(t, rel), "trustedForwarderSANs")
	if len(sans) == 0 {
		t.Fatalf("в %s не прочитано ни одного SAN — словарь пуст, и любое утверждение "+
			"о покрытии круга geo было бы вакуумным", rel)
	}
	byService := map[string]string{}
	for _, san := range sans {
		i := strings.LastIndex(san, "/sa/kacho-")
		if i < 0 {
			continue
		}
		byService[strings.TrimPrefix(san[i:], "/sa/kacho-")] = san
	}
	t.Logf("перепись: словарь SAN прочитан из круга iam — записей %d, распознано сервисов %d",
		len(sans), len(byService))
	return byService
}

// geoForwarderDeclarations — профили, ОБЪЯВЛЯЮЩИЕ круг geo, и объявленный состав.
// Читается объявление, а не рендер: контракт — то, что профиль называет сам (та
// же причина, что у соседних posture_parity_test.go и dbtls_declaration_test.go).
func geoForwarderDeclarations(t *testing.T) map[string][]string {
	t.Helper()
	const knob = "KACHO_GEO_AUTHZ_TRUSTED_FORWARDER_SANS:"
	files := trackedFiles(t, "deploy/helm/umbrella/values*.yaml")
	if len(files) == 0 {
		t.Fatal("не прочитано ни одного профиля umbrella — обход пуст")
	}
	out := map[string][]string{}
	for _, f := range files {
		for _, ln := range strings.Split(readFile(t, f), "\n") {
			trimmed := strings.TrimSpace(ln)
			if !strings.HasPrefix(trimmed, knob) {
				continue
			}
			val := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, knob)), `"'`)
			var sans []string
			for _, s := range strings.Split(val, ",") {
				if s = strings.TrimSpace(s); s != "" {
					sans = append(sans, s)
				}
			}
			out[f] = sans
		}
	}
	t.Logf("перепись: профилей umbrella прочитано %d; объявляют круг geo — %d", len(files), len(out))
	if len(out) == 0 {
		t.Fatalf("круг geo не объявлен НИ ОДНИМ профилем: либо ручка переехала (тогда этот "+
			"гейт устарел и молчит вхолостую), либо профили перестали её нести — а пустой "+
			"круг по контракту corelib значит «доверять любому проверенному пиру». Ручка: %s", knob)
	}
	return out
}

// TestGeoTrustedForwarderCircleCoversItsActualSenders — каждый сервис, который
// РЕАЛЬНО зовёт справочник размещения, обязан стоять в круге отправителей geo в
// КАЖДОМ профиле, этот круг объявляющем.
//
// Гейт судит ПОКРЫТИЕ, а не равенство: запись сверх графа — предмет отдельного
// требования (пинить круг по фактическим отправителям), а недостача означает
// молчаливый отказ на живом пути, и ловится она здесь.
func TestGeoTrustedForwarderCircleCoversItsActualSenders(t *testing.T) {
	senders := geoSenders(t)
	sanByService := spiffeSANByService(t)
	profiles := geoForwarderDeclarations(t)

	// Шлюз в графе клиентов не появляется — он проксирует REST, а не строит
	// типизированного клиента. Его право форвардить личность и есть его роль,
	// поэтому он требуется отдельно и всегда.
	const gatewaySAN = "spiffe://kacho.cloud/ns/kacho/sa/kacho-api-gateway"

	required := map[string]string{"api-gateway": gatewaySAN}
	var unknown []string
	for svc := range senders {
		san, ok := sanByService[svc]
		if !ok {
			unknown = append(unknown, svc)
			continue
		}
		required[svc] = san
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		t.Fatalf("сервис зовёт geo, но его SAN не найден в круге iam: %v.\n"+
			"Словарь SAN берётся из круга iam намеренно (он надмножество, и его состав "+
			"выводится рендером чартов). Сервис, которого там нет, — либо новый потребитель, "+
			"которого забыли завести в iam, либо переехавший SAN: и то и другое чинится ТАМ, "+
			"а не второй копией словаря здесь", unknown)
	}

	var svcNames []string
	for svc := range required {
		svcNames = append(svcNames, svc)
	}
	sort.Strings(svcNames)
	t.Logf("перепись: круг geo обязан покрывать %d отправител(я/ей): %s",
		len(required), strings.Join(svcNames, ", "))

	var profileNames []string
	for f := range profiles {
		profileNames = append(profileNames, f)
	}
	sort.Strings(profileNames)

	var findings []string
	for _, f := range profileNames {
		declared := map[string]bool{}
		for _, san := range profiles[f] {
			declared[san] = true
		}
		for _, svc := range svcNames {
			if !declared[required[svc]] {
				findings = append(findings, fmt.Sprintf(
					"%s: круг geo не несёт отправителя %s (%s) — его форвардленная личность "+
						"будет снята, и чтение справочника ответит как неаутентифицированное",
					f, svc, required[svc]))
			}
		}
	}
	if len(findings) > 0 {
		t.Fatalf("круг отправителей geo у́же графа его вызовов:\n  %s", strings.Join(findings, "\n  "))
	}
	t.Logf("сверено: профилей %d × отправителей %d = %d утверждени(й), недостач нет",
		len(profileNames), len(svcNames), len(profileNames)*len(svcNames))
}
