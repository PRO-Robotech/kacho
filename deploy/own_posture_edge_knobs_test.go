// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_posture_edge_knobs_test.go — СТЕНД, ОБЪЯВИВШИЙ ПОСАДКУ `own`, НЕ ОСТАВЛЯЕТ
// ЖИВЫХ РУЧЕК ЧУЖОГО ПОСТАВЩИКА НА КРАЮ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Накладка посадки объявляла `identityProvider: own` обеим половинам стенда и
// НЕ снимала того, что посадка перестаёт читать. Наблюдаемый исход, снятый
// рендером цепочки `own` (values.prod.yaml + values.own.yaml) на ревизии
// `bec320cf47d`:
//
//	KACHO_API_GATEWAY_IDENTITY_PROVIDER  = "own"
//	KACHO_API_GATEWAY_TOKEN_ISSUERS      = "https://kaname.kacho.local,
//	                                        https://hydra.api.kacho.cloud"
//	KACHO_HYDRA_INTROSPECTION_URL        = "https://…-hydra-admin-tls…:4445/…"
//	KACHO_HYDRA_ADMIN_URL                = "https://…-hydra-admin-tls…:4445"
//	KACHO_API_GATEWAY_KRATOS_PUBLIC_URL  = "http://…-kratos-public…:80"
//
// Первая строка — не мёртвая ручка, а ДЕЙСТВУЮЩАЯ ДВЕРЬ: токен чужого издателя
// проверяется ключом его набора и дальше неотличим от нашего. Остальные три под
// `own` не читает никто — провязка клиента печенья поставщика стоит под
// `identityLane == External` (gateway/cmd/api-gateway/main.go), читателя отзыва
// чужих токенов выбирает ЗАПИСЬ ИЗДАТЕЛЯ, а выход на стороне поставщика снимает
// сессию, которой под `own` не существует.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ГЕЙТ, А НЕ ОДНА ПРАВКА ПРОФИЛЯ
//
// Правка накладки — работа на один раз. Связь «посадка объявлена ⇒ ручки
// поставщика сняты» после неё не держится ничем: стражи старта края эти ручки
// под `own` НЕ ТРЕБУЮТ, но и не запрещают — заданный адрес они судят на форму и
// транспорт и на обеих полосах отвечают одинаково. То есть возврат чужого
// издателя в перечень накладки не дал бы ни одного красного: каждый страж
// честно проверил бы то, что сам объявил.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧИТАЕТ ОБЪЯВЛЕНИЯ, А НЕ РЕНДЕР
//
// Цепочка берётся из единственной таблицы дерева (deploy/stacks.txt), профили
// накладываются тем же правилом, каким их накладывает helm (mergeValues). Ни
// helm, ни кластер не нужны — проба, умеющая пропуститься, гейтом не является.
//
// ─────────────────────────────────────────────────────────────────────────────
// СОСТАВ ЧУЖОГО ВЫВОДИТСЯ, А НЕ ВЫПИСЫВАЕТСЯ
//
// Имена компонентов поставщика берутся из зависимостей зонта по признаку
// репозитория. Выписанный рядом перечень разошёлся бы с Chart.yaml молча, и
// гейт перестал бы видеть переименованный компонент.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЛОЖИТЕЛЬНАЯ СТОРОНА
//
// Проверка, судящая только посадку `own`, зеленела бы на дереве, где ни одной
// такой цепочки нет, и «находок 0» читалось бы как «предмета не осталось».
// Поэтому здесь два утверждения предпосылки: цепочка посадки `own` в дереве
// ЕСТЬ, и рядом есть цепочка `external`, у которой ручки поставщика ЖИВЫ, —
// то есть распознаватель различает две стороны на живом дереве, а не только на
// синтетике соседнего файла.
package deploy_test

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// foreignProviderRepoMark — признак репозитория поставщика чужой службы
// личности среди зависимостей зонта.
const foreignProviderRepoMark = "ory.sh"

// ownPostureValue / externalPostureValue — две посадки, которые объявляет
// профиль. Литералы, а не выведенное: это значения КОНТРАКТА настройки, их
// разбирает `identityposture` на крае, и вывести их из дерева значений
// невозможно by construction.
const (
	ownPostureValue      = "own"
	externalPostureValue = "external"
)

// edgeKnobExpectation — одна ручка края, которую посадка `own` не читает, и то,
// чем она обязана быть объявлена, чтобы шаблон её не эмитил (либо эмитил
// сигнальное значение).
type edgeKnobExpectation struct {
	Path []string // путь в слитых значениях стенда
	Want string   // требуемое значение; "" означает «объявлена пустой»
	Why  string   // почему под `own` читателя нет
}

// ownPostureEdgeKnobs — ручки, снимаемые вместе с посадкой.
//
// `kratosPublicUrl` требует не пустоты, а СИГНАЛЬНОГО значения: шаблон края
// эмитит её всегда и при незаданной величине ВЫВОДИТ адрес из имени выпуска,
// поэтому «не объявить» оставило бы живой адрес чужой службы в объявлении
// процесса.
var ownPostureEdgeKnobs = []edgeKnobExpectation{
	{
		Path: []string{"api-gateway", "hydra", "introspectionUrl"},
		Want: "",
		Why: "интроспекция ЧУЖИХ токенов: читателя выбирает запись издателя " +
			"(VerifiedToken.ReadRevocation), а чужого издателя эта посадка не принимает",
	},
	{
		Path: []string{"api-gateway", "hydra", "adminUrl"},
		Want: "",
		Why: "выход, снимающий сессию НА СТОРОНЕ ПОСТАВЩИКА: под `own` такой сессии " +
			"не существует, носитель браузерной сессии наш",
	},
	{
		Path: []string{"api-gateway", "hydra", "adminCa", "secretName"},
		Want: "",
		Why: "якорь доверия административного хопа: хоп снят вместе с адресом выше, " +
			"а том монтируется из секрета чужого терминатора TLS — на стенде, где " +
			"выдающая половина поставщика выключена, такого секрета нет",
	},
	{
		Path: []string{"api-gateway", "kratosPublicUrl"},
		Want: "disabled",
		Why: "адрес чужой службы личности: его читает клиент печенья сессии, " +
			"провязанный только под `identityLane == External`",
	},
}

// tokenAcceptanceHalves — обе половины стенда, объявляющие приём токенов одним
// и тем же ключом значений. Накладка, снявшая чужого издателя у одной и
// забывшая вторую, оставила бы дверь открытой, и обе половины были бы
// «исправны» каждая по своей пробе.
var tokenAcceptanceHalves = [][]string{
	{"api-gateway", "tokenAcceptance", "issuers"},
	{"registry", "tokenAcceptance", "issuers"},
}

// foreignProviderNames — имена компонентов поставщика, выведенные из зонта.
func foreignProviderNames(t *testing.T) []string {
	t.Helper()

	chart := readYAML(t, filepath.Join(umbrellaDir, "Chart.yaml"))
	deps, _ := chart["dependencies"].([]any)
	if len(deps) == 0 {
		t.Fatalf("в %s не прочитано ни одной зависимости — состав чужого взять неоткуда",
			filepath.Join(umbrellaDir, "Chart.yaml"))
	}

	var out []string
	for _, d := range deps {
		m, ok := d.(map[string]any)
		if !ok {
			continue
		}
		repo, _ := m["repository"].(string)
		if !strings.Contains(repo, foreignProviderRepoMark) {
			continue
		}
		name, _ := m["alias"].(string)
		if strings.TrimSpace(name) == "" {
			name, _ = m["name"].(string)
		}
		if n := strings.TrimSpace(name); n != "" {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		t.Fatalf("среди зависимостей зонта нет ни одной с репозиторием %q — поставщик "+
			"переехал либо признак перестал его узнавать; «чужого не найдено» здесь "+
			"неотличимо от «чужое не прочитано»", foreignProviderRepoMark)
	}
	return out
}

// namesAForeignIssuer — запись перечня издателей называет компонент поставщика.
//
// Судится КАЖДАЯ запись отдельно, а не строка целиком: перечень объявлен одним
// скаляром через запятую, и поиск подстроки по всей строке отвечал бы «да» на
// перечне, где чужого издателя нет, но есть наш с похожим путём.
func namesAForeignIssuer(list string, provider []string) []string {
	var hits []string
	for _, entry := range strings.Split(list, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		low := strings.ToLower(entry)
		for _, p := range provider {
			// Граница имени, а не подстрока: `hydra` обязан быть отдельным
			// сегментом адреса, иначе `hydrargyrum.example` считался бы чужим.
			for _, sep := range []string{"/", ".", ":", "-"} {
				if strings.Contains(low, sep+p+".") || strings.Contains(low, sep+p+"/") ||
					strings.HasSuffix(low, sep+p) || strings.Contains(low, "//"+p+".") {
					hits = append(hits, entry+" ("+p+")")
					goto next
				}
			}
		}
	next:
	}
	return hits
}

// judgeOwnPostureChain — единственный судья. Возвращает находки по одной
// цепочке; пустой возврат означает «под этой посадкой живых ручек поставщика
// нет».
func judgeOwnPostureChain(name, posture string, merged map[string]any, provider []string) []string {
	if posture != ownPostureValue {
		return nil
	}
	var findings []string

	for _, path := range tokenAcceptanceHalves {
		raw, ok := lookup(merged, path...)
		if !ok {
			continue // половины может не быть в стенде вовсе
		}
		list, _ := raw.(string)
		if hits := namesAForeignIssuer(list, provider); len(hits) > 0 {
			findings = append(findings, fmt.Sprintf(
				"стенд %q объявил посадку %q и ПРИНИМАЕТ чужого издателя: %s = %q (записи: %s).\n"+
					"Это не мёртвая ручка, а действующая дверь: токен чужого издателя проверяется "+
					"ключом его набора и дальше неотличим от нашего",
				name, posture, strings.Join(path, "."), list, strings.Join(hits, ", ")))
		}
	}

	for _, knob := range ownPostureEdgeKnobs {
		raw, ok := lookup(merged, knob.Path...)
		got := ""
		if ok && raw != nil {
			got = fmt.Sprintf("%v", raw)
		}
		if ok && got == knob.Want {
			continue
		}
		if !ok && knob.Want == "" {
			continue // ключа нет вовсе — эмитить нечего
		}
		// ОТСУТСТВИЕ ключа и ПУСТОЕ значение — разные состояния, и разными они
		// остаются в тексте находки: первое чинится объявлением, второе —
		// правкой объявленного, и оператор, прочитавший одно вместо другого,
		// правит не то место.
		state := fmt.Sprintf("= %q", got)
		if !ok {
			state = "НЕ ОБЪЯВЛЕНА (шаблон края выведет значение сам)"
		}
		findings = append(findings, fmt.Sprintf(
			"стенд %q объявил посадку %q и оставил живой ручку без читателя: %s %s, ожидалось %q — %s.\n"+
				"Объявление, у которого не осталось читателя, приглашает вернуть снятую ветвь "+
				"обратно «чтобы ручка заработала»",
			name, posture, strings.Join(knob.Path, "."), state, knob.Want, knob.Why))
	}

	return findings
}

// mergedChainValues — значения стенда, слитые тем же правилом, каким их
// накладывает helm.
func mergedChainValues(t *testing.T, chain []string) map[string]any {
	t.Helper()
	merged := map[string]any{}
	for _, profile := range chain {
		merged = mergeValues(merged, readYAML(t, filepath.Join(umbrellaDir, profile)))
	}
	return merged
}

// edgePostureOfChain — посадка, объявленная краю. Не объявившая цепочка
// наследует умолчание подчарта края, и умолчание ЧИТАЕТСЯ, а не пишется здесь
// литералом.
func edgePostureOfChain(t *testing.T, merged map[string]any, chartDefault string) string {
	t.Helper()
	if raw, ok := lookup(merged, "api-gateway", "authn", "identityProvider"); ok {
		if s, _ := raw.(string); strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return chartDefault
}

// edgeChartDefaultPosture — умолчание посадки у подчарта края.
func edgeChartDefaultPosture(t *testing.T) string {
	t.Helper()
	dirs := subchartDirs(t)
	dir, ok := dirs["api-gateway"]
	if !ok {
		t.Fatal("подчарт края не найден среди зависимостей зонта — умолчание посадки взять неоткуда")
	}
	vals := readYAML(t, filepath.Join(dir, "values.yaml"))
	raw, ok := lookup(vals, "authn", "identityProvider")
	if !ok {
		t.Fatalf("в %s нет authn.identityProvider — умолчание посадки исчезло, "+
			"и «цепочка не объявила» стало неотличимо от «объявила external»",
			filepath.Join(dir, "values.yaml"))
	}
	s, _ := raw.(string)
	if strings.TrimSpace(s) == "" {
		t.Fatalf("умолчание посадки в %s пусто", filepath.Join(dir, "values.yaml"))
	}
	return strings.TrimSpace(s)
}

// TestOwnPostureLeavesNoLiveForeignProviderKnobOnTheEdge — главное утверждение.
func TestOwnPostureLeavesNoLiveForeignProviderKnobOnTheEdge(t *testing.T) {
	stacks := deployStacks(t)
	provider := foreignProviderNames(t)
	chartDefault := edgeChartDefaultPosture(t)

	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	var findings []string
	own, external, externalWithLiveKnob := 0, 0, 0
	for _, name := range names {
		merged := mergedChainValues(t, stacks[name])
		posture := edgePostureOfChain(t, merged, chartDefault)
		switch posture {
		case ownPostureValue:
			own++
		case externalPostureValue:
			external++
			// Живая ручка поставщика рядом с посадкой `external` — это
			// ПОЛОЖИТЕЛЬНАЯ сторона предиката: она доказывает, что
			// распознаватель видит предмет на живом дереве, а не только в
			// синтетике.
			if raw, ok := lookup(merged, "api-gateway", "hydra", "adminUrl"); ok {
				if s, _ := raw.(string); strings.TrimSpace(s) != "" {
					externalWithLiveKnob++
				}
			}
		}
		t.Logf("  %-12s посадка=%s профилей=%d", name, posture, len(stacks[name]))
		findings = append(findings, judgeOwnPostureChain(name, posture, merged, provider)...)
	}

	t.Logf("перепись: цепочек осмотрено %d · посадки `own` %d · посадки `external` %d "+
		"(из них с живой ручкой поставщика %d) · имён компонентов поставщика %d (%s) · находок %d",
		len(names), own, external, externalWithLiveKnob, len(provider),
		strings.Join(provider, " "), len(findings))

	// ПРЕДПОСЫЛКИ. Каждая отвечает на вопрос «что означало бы ноль находок».
	if own == 0 {
		t.Fatal("ни одной цепочки посадки `own` — судить нечего, и «находок 0» здесь " +
			"означает «предмета не прочитано», а не «предмета не осталось»")
	}
	if externalWithLiveKnob == 0 {
		t.Fatal("ни одной цепочки `external` с живой ручкой поставщика — распознаватель " +
			"не показан работающим ни на одной стороне живого дерева")
	}

	for _, f := range findings {
		t.Error(f)
	}
}
