// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// newman_enum_contract_test.go — гейт: ни один REST-запрос регрессионных
// suite'ов не кладёт в тело имя значения перечисления, которого в контракте нет.
//
// ЗАЧЕМ ОТДЕЛЬНО ОТ ГЕЙТА КЛЮЧЕЙ. Соседний гейт
// (`newman_body_contract_test.go`) сверяет ИМЕНА КЛЮЧЕЙ. Значение он не смотрит,
// и не должен: ключ, которого нет в сообщении, край отбрасывает по конвенции
// маски обновления. А вот ЗНАЧЕНИЕ перечисления вне словаря край теперь
// отвергает (`strict_enum.go`) — и это ровно тот сорт изменения, который
// зеленеет в unit-тестах и краснеет на живом прогоне, если в корпусе кейсов
// нашлось тело, слать которое считалось законным.
//
// Поэтому здесь измеряется цена строгости ДО прогона: сколько тел корпуса она
// затрагивает и какие именно. Ноль находок обязан быть отличим от нуля
// прочитанного — счётчики печатаются всегда.
//
// ЧТО НЕ ПРОВЕРЯЕТСЯ (и почему это не дыра). Значение, собранное из
// `{{переменной}}` Postman, статически неизвестно: подставит его прогон. Такие
// считаются отдельно и печатаются числом — «не проверено» не растворяется в
// «проверено и чисто».

// enumTemplateSentinel — чем заменяется `{{var}}` перед разбором. Наличие метки
// в строке однозначно означает «значение подставит прогон».
//
// Метка ОБЯЗАНА оставаться печатаемой: JSON запрещает сырые управляющие символы
// внутри строкового литерала, поэтому подстановка `\x00` роняла разбор КАЖДОГО
// тела с переменной — а таких большинство. Гейт при этом не краснел: он просто
// не смотрел на них, и «проверено 456 тел» читалось как исчерпывающий обход при
// том, что обойдено было меньшинство. Ровно тот класс, который этот файл ловит,
// только в самом гейте.
const enumTemplateSentinel = "__KACHO_TEMPLATE__"

var enumVarPlaceholder = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// enumValueWaiver — поимённое разрешение слать значение перечисления вне
// словаря.
//
// Правило, которое оно ослабляет: «значение перечисления обязано быть в
// словаре». Ослаблять его можно ровно для проб, чей ПРЕДМЕТ — отказ на таком
// значении: без мусора в теле такая проба перестала бы воспроизводить то, что
// проверяет.
//
// Ключ — идентификатор кейса плюс путь до поля. Переехавшая на другое поле проба
// ключ меняет и возвращается в вердикт. Список печатается КАЖДЫЙ прогон и
// проверяется на устаревание: разрешение, которому больше нечего разрешать,
// роняет гейт — иначе оно тихо копило бы право, под которым въедет чужая ошибка.
var enumValueWaiver = map[string]string{
	"NLB-CR-VAL-INVALID-AFFINITY sessionAffinity":                 "проба и есть отказ на значении вне словаря: край обязан ответить INVALID_ARGUMENT вместо того, чтобы принять 200 за настройку, которой не сделал",
	"LST-CR-VAL-UNSUPPORTED-PROTOCOL protocol":                    "проба и есть отказ на протоколе вне словаря (HTTP при TCP/UDP): без значения вне словаря она перестала бы воспроизводить проверяемое",
	"SG-URL-VAL-DIRECTION-UNKNOWN additionRuleSpecs[0].direction": "проба и есть отказ на направлении вне словаря: без значения вне словаря она перестала бы воспроизводить проверяемое. Прежняя редакция этого обоснования добавляла, что утверждение пробы принимает и 200, и 400, «то есть отказаться она не может» — с 2026-07-31 это неверно: утверждение сведено к одному исходу (400 + код 3), предмет разрешения остался прежним",
	"LST-CR-VAL-IPV-UNKNOWN protocol":                             "проба и есть отказ на значении вне словаря. Её прежний предмет — семейство адресов — снят с контракта (reserved), и она была ошибочно ретайрена под обоснование «край такое выбрасывает», верное лишь до появления самой этой проверки. Перенаправлена на живое обязательное перечисление того же запроса: без значения вне словаря чёрного ящика у этого контроля не осталось бы вовсе",
}

// enumFinding — одно значение перечисления вне словаря, с адресом в коллекции.
type enumFinding struct {
	File   string
	Line   int
	Case   string
	CaseID string
	Method string
	Path   string
	FQN    string
	// Field — путь до поля внутри тела (`ruleSpecs[0].direction`).
	Field string
	Value string
}

func (f enumFinding) String() string {
	return fmt.Sprintf("%s:%d [%s] %s %s -> %s — %s: %q is not a value of that enum",
		f.File, f.Line, f.Case, f.Method, f.Path, f.FQN, f.Field, f.Value)
}

func enumWaiverKey(caseID, field string) string { return caseID + " " + field }

func TestNewmanCollectionsSendNoUnknownEnumValues(t *testing.T) {
	root := repoRoot(t)
	// Обе площадки: suite шлюза живёт в gateway/, а не в services/ — одна маска
	// её не покрывает, и её тела остались бы неизмеренными.
	var files []string
	for _, pat := range []string{
		filepath.Join(root, "services", "*", "tests", "newman", "collections", "*.json"),
		filepath.Join(root, "gateway", "tests", "newman", "collections", "*.json"),
	} {
		got, err := filepath.Glob(pat)
		if err != nil {
			t.Fatalf("glob %s: %v", pat, err)
		}
		files = append(files, got...)
	}
	if len(files) == 0 {
		t.Fatal("no newman collections found: gate has nothing to check, which is a failure, not a pass")
	}
	sort.Strings(files)

	var findings []enumFinding
	requests, bodies, unparsed, enumValues, templated := 0, 0, 0, 0, 0

	for _, file := range files {
		reqs, err := parseNewmanCollection(file)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		rel := mustRel(root, file)
		for _, r := range reqs {
			requests++
			if r.bodyRaw == "" {
				continue
			}
			obj, ok := decodeSentinelJSONObject(r.bodyRaw)
			if !ok {
				// Неразбираемое тело (или тело-не-объект) — предмет соседнего
				// гейта, он его и докладывает. Дублировать вердикт здесь
				// незачем, но СЧИТАТЬ обязательно: необойдённое тело не имеет
				// права выглядеть как обойдённое и чистое.
				unparsed++
				continue
			}
			bodies++
			b, ok := resolveHTTPBinding(r.method, r.path)
			if !ok {
				continue // маршрута нет — контракта запроса не существует (соседний гейт)
			}
			md, ok := bodyMessage(b)
			if !ok {
				continue
			}
			var found bodyRefusals
			walkEnumValueNames(md, obj, "", &found)
			for _, ev := range found.enums {
				field, value := ev.Path, ev.Value
				enumValues++
				if strings.Contains(value, enumTemplateSentinel) {
					templated++
					continue
				}
				findings = append(findings, enumFinding{
					File: rel, Line: r.line, Case: r.name, CaseID: r.caseID,
					Method: r.method, Path: r.path, FQN: b.fqn,
					Field: field, Value: value,
				})
			}
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})

	var waived, unwaived []enumFinding
	used := make(map[string]bool, len(enumValueWaiver))
	for _, f := range findings {
		key := enumWaiverKey(f.CaseID, f.Field)
		if _, ok := enumValueWaiver[key]; ok {
			used[key] = true
			waived = append(waived, f)
			continue
		}
		unwaived = append(unwaived, f)
	}

	t.Logf("scanned %d collections, %d requests, %d walked JSON bodies, %d bodies NOT walked (unparseable or non-object); "+
		"%d enum value(s) outside their dictionary (%d of them templated and therefore unverifiable here), "+
		"%d waived by name, %d not",
		len(files), requests, bodies, unparsed, enumValues, templated, len(waived), len(unwaived))

	var w strings.Builder
	fmt.Fprintf(&w, "%d probe(s) deliberately send an enum value outside its dictionary, waived by name:\n", len(waived))
	for _, f := range waived {
		fmt.Fprintf(&w, "  %s\n      reason: %s\n", f, enumValueWaiver[enumWaiverKey(f.CaseID, f.Field)])
	}
	t.Log(w.String())

	var stale []string
	for key := range enumValueWaiver {
		if !used[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)

	if len(unwaived) == 0 && len(stale) == 0 {
		return
	}
	var b strings.Builder
	if len(unwaived) > 0 {
		fmt.Fprintf(&b, "%d request(s) send an enum value the contract does not define — "+
			"the edge now answers INVALID_ARGUMENT, so either the fixture is wrong or the enum is:\n", len(unwaived))
		for _, f := range unwaived {
			fmt.Fprintf(&b, "  %s\n      waiver key: %q\n", f, enumWaiverKey(f.CaseID, f.Field))
		}
	}
	if len(stale) > 0 {
		fmt.Fprintf(&b, "%d waiver(s) in enumValueWaiver match nothing — delete them, "+
			"an unused permission is one the next mistake inherits:\n", len(stale))
		for _, key := range stale {
			fmt.Fprintf(&b, "  %q\n", key)
		}
	}
	t.Fatal(b.String())
}

// decodeSentinelJSONObject разбирает тело, подставив вместо `{{var}}` метку,
// по которой потом видно, что значение статически неизвестно.
func decodeSentinelJSONObject(raw string) (map[string]any, bool) {
	substituted := enumVarPlaceholder.ReplaceAllString(raw, enumTemplateSentinel)
	var v any
	if err := json.Unmarshal([]byte(substituted), &v); err != nil {
		return nil, false
	}
	obj, ok := v.(map[string]any)
	return obj, ok
}
