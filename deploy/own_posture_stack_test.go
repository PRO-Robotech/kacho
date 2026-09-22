// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_posture_stack_test.go — В ТАБЛИЦЕ СТЕНДОВ ЕСТЬ СТЕК ПОСАДКИ `own`, И ОБЕ
// ЕГО ПОЛОВИНЫ НАЗЫВАЮТ ОДИН И ТОТ ЖЕ СЛУШАТЕЛЬ ПОЛОСЫ (kacho#2716, стадия S2
// приёмки Ф4д §8).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Посадку `own` не объявлял НИ ОДИН профиль зонта и ни один стек (kacho
// `origin/main` @ `ac33f733`):
//
//	git grep -nE 'identityProvider: *"?own' origin/main \
//	  -- 'deploy/helm/umbrella/values*.yaml' gateway/deploy
//	    → 2 строки, обе комментарии; объявлений 0
//	git show origin/main:deploy/stacks.txt | grep -v '^#' | sed '/^$/d'
//	    → 6 стеков, ни одного на `own`
//
// Ручки посадки заводились по отдельности (kacho#2699, #2709, #2714, #2715), но
// СОБРАТЬ их было негде: перевод стенда на `own` было нечем выполнить, и каждая
// ручка проверялась только рендером. От перевода зависят п. 4 предиката
// kacho#1269 и, через него, kacho#2571.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ СУДИТСЯ, ЧЕГО НЕ СУДИТ НИКТО ДРУГОЙ
//
// Соседи покрывают половины по отдельности: identity_posture_profiles_test.go —
// что посадку объявили ОБЕ половины и одинаково; login_lane_umbrella_test.go —
// что стенд на `own` назвал величины полосы; own_lane_memory_budget_test.go —
// что предел памяти покрывает бюджет.
//
// Здесь — СОГЛАСИЕ ДВУХ ПОЛОВИН ОБ ОДНОМ ЧИСЛЕ: порт, на котором внутренняя
// Служба выставляет полосу формы, и порт, на который край ретранслирует четыре
// глагола. Это ровно тот класс, который не виден ни с одной стороны по
// отдельности: обе половины исправны, каждая проверяется своими пробами, а
// вместе они не работают — край стучится в дверь, которой у Службы нет, и
// отвечает 503 на каждом запросе, неотличимо от «служба лежит».
//
// Сверяется порт СЛУЖБЫ, а не слушателя (kacho#2725): край набирает адрес
// Службы, а Служба ведёт на слушатель по имени порта, и её собственный порт
// профиль вправе переопределить (`service.internal.loginLanePort`). Сверка
// с портом слушателя молчала бы ровно в день, когда переопределение появится.
// Выражение порта Службы проба берёт из шаблона не на веру: его сверяет
// TestOwnPostureStack_ServicePortModelIsTheTemplateExpression.
//
// ─────────────────────────────────────────────────────────────────────────────
// СТЕК ОБЯЗАН СУЩЕСТВОВАТЬ, А НЕ «ЕСЛИ ЕСТЬ»
//
// Проверка, судящая стеки на `own` и молчащая, когда их ноль, не поймала бы
// исходное состояние дерева — оно и было нулём. Поэтому отсутствие такого стека
// есть НАХОДКА, а не законный близнец: перевод стенда на `own` объявлен стадией
// приёмки, и стек — его единственный носитель.
package deploy_test

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ownStackFacts — что цепочка одного стенда объявила обеим половинам.
type ownStackFacts struct {
	Stack       string
	IAMPosture  string // kaname.config.authn.identityProvider
	EdgePosture string // api-gateway.authn.identityProvider
	LanePort    string // kaname.ports.loginLane — порт слушателя в поде
	ServicePort string // kaname.service.internal.loginLanePort — переопределение порта Службы
	LaneURL     string // api-gateway.authn.iamLoginLaneUrl
	ServiceName string // kaname.name — из него выводится имя внутреннего Service
	AccessKeys  bool   // объявлены ли все три величины привязки ключей доступа
}

// ownStackCensus — объём осмотренного.
type ownStackCensus struct {
	Stacks          int
	Own             int
	PinNeedsBinding bool // требует ли пин ручек привязки ключей доступа
}

func (c ownStackCensus) String() string {
	return fmt.Sprintf("стендов осмотрено %d · из них на посадке own %d · "+
		"пин требует привязки ключей доступа: %v", c.Stacks, c.Own, c.PinNeedsBinding)
}

// judgeOwnStacks — НАХОДКИ по перечню стендов.
//
// pinNeedsBinding приходит аргументом, а не читается здесь: судящая функция
// чистая, и инъекция подаёт ей синтетический вход, не трогая ни дерева, ни кэша
// модулей.
func judgeOwnStacks(facts []ownStackFacts, pinNeedsBinding bool) ([]string, ownStackCensus) {
	var findings []string
	census := ownStackCensus{PinNeedsBinding: pinNeedsBinding}

	for _, f := range facts {
		census.Stacks++
		if f.IAMPosture != "own" && f.EdgePosture != "own" {
			continue
		}
		census.Own++

		if f.IAMPosture != f.EdgePosture {
			findings = append(findings, fmt.Sprintf(
				"стек %s: половины объявили РАЗНОЕ — служба %q, край %q. Стенд, у которого половины "+
					"решают о личности по-разному, — расхождение, которого никто не решал",
				f.Stack, f.IAMPosture, f.EdgePosture))
			continue
		}
		if strings.TrimSpace(f.LanePort) == "" {
			findings = append(findings, fmt.Sprintf(
				"стек %s на посадке own: `kaname.ports.loginLane` не объявлен — слушатель формы не "+
					"поднимается, и служба откажет в старте с именем ручки", f.Stack))
		}
		raw := strings.TrimSpace(f.LaneURL)
		if raw == "" {
			findings = append(findings, fmt.Sprintf(
				"стек %s на посадке own: `api-gateway.authn.iamLoginLaneUrl` не объявлен — край "+
					"откажет в старте: ретрансляция без цели отвечала бы 503 на каждом запросе всю "+
					"жизнь, неотличимо от «служба лежит»", f.Stack))
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || u.Host == "" {
			findings = append(findings, fmt.Sprintf(
				"стек %s: `iamLoginLaneUrl` = %q не абсолютный адрес со схемой и хостом — край "+
					"отказывает в старте", f.Stack, raw))
			continue
		}
		if u.Scheme != "https" {
			findings = append(findings, fmt.Sprintf(
				"стек %s: `iamLoginLaneUrl` = %q не https — ретранслируемый запрос несёт печенье "+
					"сессии человека, и открытый участок нёс бы его в чистом виде; слушатель полосы "+
					"взаимный по TLS", f.Stack, raw))
		}
		// СОГЛАСИЕ ДВУХ ПОЛОВИН ОБ ОДНОМ ЧИСЛЕ — предмет этой пробы. Край набирает
		// порт СЛУЖБЫ, а не слушателя: Служба ведёт на слушатель по ИМЕНИ порта
		// (`targetPort: http-login-lane`), и потому со слушателем она согласна
		// всегда, а с адресом края — только если её порт и есть порт адреса.
		if port, svcPort := u.Port(), laneServicePort(f); port != svcPort {
			findings = append(findings, fmt.Sprintf(
				"стек %s: край ретранслирует на порт %q, а внутренняя Служба выставляет полосе %q "+
					"(`service.internal.loginLanePort` = %q, по умолчанию `ports.loginLane` = %q) — "+
					"половины называют РАЗНЫЕ двери. Обе исправны по отдельности, и ни одна проба "+
					"половины этого не увидит: край постучится в порт, которого у Службы нет, и "+
					"ответит 503 на каждом запросе",
				f.Stack, port, svcPort, f.ServicePort, f.LanePort))
		}
		if svc := strings.TrimSpace(f.ServiceName); svc != "" {
			if host := u.Hostname(); !strings.HasPrefix(host, svc+"-internal") {
				findings = append(findings, fmt.Sprintf(
					"стек %s: `iamLoginLaneUrl` ведёт на хост %q, а слушатель формы выставлен на "+
						"ВНУТРЕННЕМ Service службы (`%s-internal`) — публичного пути к полосе нет "+
						"by construction", f.Stack, host, svc))
			}
		}
		// Ручки привязки требуются ровно тогда, когда их требует ПИН. Условие
		// вооружается само: поднимут пин — проверка начнёт требовать, и краснота
		// придёт до подъёма стенда, а не отказом пода после.
		if pinNeedsBinding && !f.AccessKeys {
			findings = append(findings, fmt.Sprintf(
				"стек %s на посадке own: пиненная ревизия службы требует привязки ключей доступа "+
					"(`authn.access-keys.rp-id|origins|algorithms`), а профиль её не объявил — "+
					"служба откажет в старте с именем ручки", f.Stack))
		}
	}

	if census.Own == 0 {
		findings = append(findings, fmt.Sprintf(
			"ни один из %d стеков таблицы не объявляет посадку `own` — перевод стенда на свою "+
				"полосу нечем выполнить, и ручки посадки, заведённые по отдельности, собрать негде "+
				"(kacho#2716, стадия S2 приёмки Ф4д §8)", census.Stacks))
	}
	return findings, census
}

// TestOwnPostureStack_ExistsAndBothHalvesNameOneListener — сверка по дереву.
func TestOwnPostureStack_ExistsAndBothHalvesNameOneListener(t *testing.T) {
	pinNeedsBinding := len(accessKeyKnobsOfPin(t, kanameModuleDir(t, ".."))) > 0
	facts := readOwnStackFacts(t)
	if len(facts) == 0 {
		t.Fatal("таблица стендов пуста: обход беспредметен, и «находок нет» здесь означало бы " +
			"«сверять было не с чем»")
	}

	findings, census := judgeOwnStacks(facts, pinNeedsBinding)
	t.Logf("перепись: %s", census)
	for _, f := range facts {
		if f.IAMPosture != "own" && f.EdgePosture != "own" {
			continue
		}
		t.Logf("  %s: служба=%s край=%s слушатель=%s порт Службы=%s адрес=%s привязка=%v",
			f.Stack, f.IAMPosture, f.EdgePosture, f.LanePort, laneServicePort(f), f.LaneURL, f.AccessKeys)
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// readOwnStackFacts — факты из дерева: цепочка каждого стенда накладывается
// слева направо поверх базовых значений подчартов, ровно как её получает helm.
func readOwnStackFacts(t *testing.T) []ownStackFacts {
	t.Helper()
	stacksTbl := deployStacks(t)
	names := make([]string, 0, len(stacksTbl))
	for n := range stacksTbl {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]ownStackFacts, 0, len(names))
	for _, name := range names {
		// Умолчания подчартов читаются ЗАНОВО на каждый стенд: `mergeValues`
		// правит карту на месте, и одна общая карта протекала бы из стенда в
		// стенд, приписывая одному профилю объявления другого.
		declared := map[string]any{
			"kaname":      readYAML(t, filepath.Join(kanameSubchart(t), "values.yaml")),
			"api-gateway": readYAML(t, filepath.Join("..", "gateway", "deploy", "values.yaml")),
		}
		for _, p := range stacksTbl[name] {
			declared = mergeValues(declared, readYAML(t, filepath.Join(umbrellaDir, p)))
		}
		f := ownStackFacts{Stack: name}
		f.IAMPosture = declaredString(lookup(declared, "kaname", "config", "authn", "identityProvider"))
		f.EdgePosture = declaredString(lookup(declared, "api-gateway", "authn", "identityProvider"))
		f.LanePort = declaredString(lookup(declared, "kaname", "ports", "loginLane"))
		f.ServicePort = declaredString(lookup(declared, "kaname", "service", "internal", "loginLanePort"))
		f.LaneURL = declaredString(lookup(declared, "api-gateway", "authn", "iamLoginLaneUrl"))
		f.ServiceName = declaredString(lookup(declared, "kaname", "name"))

		binding, ok := lookup(declared, "kaname", "config", "authn", "accessKeys")
		if m, isMap := binding.(map[string]any); ok && isMap {
			_, rp := m["rpId"]
			_, or := m["origins"]
			_, al := m["algorithms"]
			f.AccessKeys = rp && or && al
		}
		out = append(out, f)
	}
	return out
}

// laneServicePortCondition и laneServicePortExpression — выражения записи
// `http-login-lane` внутреннего Service (charts/kaname/templates/
// service-internal.yaml), которые laneServicePort воспроизводит. Их сверяет с
// шаблоном TestOwnPostureStack_ServicePortModelIsTheTemplateExpression: сменит
// шаблон выражение — модель здесь устареет, и проба скажет это, а не начнёт
// сверять порт, которого Служба не выставляет (kacho#2725).
const (
	laneServicePortCondition  = `.Values.ports.loginLane`
	laneServicePortExpression = `.Values.service.internal.loginLanePort | default .Values.ports.loginLane`
)

// laneServicePort — порт, который внутренний Service выставляет полосе:
//
//	{{- if .Values.ports.loginLane }}
//	  port: {{ .Values.service.internal.loginLanePort | default .Values.ports.loginLane }}
//
// Пустота — по правилам `if` и `default` шаблонов: отсутствие, пустая строка и
// нуль. Без объявленного слушателя записи порта у Службы нет вовсе — "".
func laneServicePort(f ownStackFacts) string {
	if helmEmpty(f.LanePort) {
		return ""
	}
	if !helmEmpty(f.ServicePort) {
		return f.ServicePort
	}
	return f.LanePort
}

// helmEmpty — пусто ли скалярное значение для `if` и `default` шаблона.
// Строка приходит из declaredString: отсутствие уже дало "".
func helmEmpty(s string) bool {
	return s == "" || s == "0" || s == "false"
}

// laneServicePortTemplate — условие записи `http-login-lane` и выражение её
// порта в тексте шаблона внутреннего Service; ok=false, если записи нет.
func laneServicePortTemplate(tmpl string) (condition, expression string, ok bool) {
	m := laneServicePortEntryRe.FindStringSubmatch(tmpl)
	if m == nil {
		return "", "", false
	}
	return strings.TrimSpace(m[1]), strings.TrimSpace(m[2]), true
}

// laneServicePortEntryRe — `{{- if <условие> }}`, затем запись с именем
// `http-login-lane`, затем её `port: {{ <выражение> }}`. Между условием и
// записью — только строки-комментарии и пробелы: иначе условие принадлежало
// бы другой записи.
var laneServicePortEntryRe = regexp.MustCompile(
	`\{\{-?\s*if\s+([^}]+?)\s*-?\}\}[ \t]*\n(?:[ \t]*#[^\n]*\n)*[ \t]*- name:[ \t]*http-login-lane[ \t]*\n[ \t]*port:[ \t]*\{\{-?\s*([^}]+?)\s*-?\}\}`)

// TestOwnPostureStack_ServicePortModelIsTheTemplateExpression — предпосылка
// пробы: шаблон Службы выводит порт полосы тем выражением, которое
// воспроизводит laneServicePort.
func TestOwnPostureStack_ServicePortModelIsTheTemplateExpression(t *testing.T) {
	path := filepath.Join(kanameSubchart(t), "templates", "service-internal.yaml")
	cond, expr, ok := laneServicePortTemplate(readChartText(t, path))
	if !ok {
		t.Fatalf("%s: записи `http-login-lane` под условием `{{- if … }}` не найдено — проба "+
			"сверяет адрес края с портом, выставление которого больше не читается", path)
	}
	t.Logf("перепись: %s · условие записи %q · выражение порта %q", path, cond, expr)
	if cond != laneServicePortCondition || expr != laneServicePortExpression {
		t.Errorf("%s: запись `http-login-lane` выставляется условием %q и портом %q, а проба "+
			"воспроизводит %q и %q — модель порта Службы устарела, и согласие половин "+
			"судится не о той двери (kacho#2725)",
			path, cond, expr, laneServicePortCondition, laneServicePortExpression)
	}
}

// declaredString — значение как строка; отсутствие даёт пустую строку.
//
// Имя НЕ `str`: под признаком сборки `helmcharts` в этом же пакете уже есть
// помощник с таким именем, и обычная сборка тех файлов не читает — пакет
// собирался бы зелёным, а под признаком не собирался бы вовсе.
func declaredString(v any, ok bool) string {
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}
