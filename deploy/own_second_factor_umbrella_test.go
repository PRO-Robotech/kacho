// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// own_second_factor_umbrella_test.go — КОПИЯ ЧАРТА СЛУЖБЫ ДОСТУПА В ЗОНТЕ ОБЯЗАНА
// ВЫРАЖАТЬ ДВЕ ВЕЛИЧИНЫ ПОЛОСЫ `own`, ПРИШЕДШИЕ С Ф12: ключ обёртки секретов
// второго фактора и окно свежести правки своих данных (kacho#2714, kacho#1281).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Страж старта службы требует обе ручки ТОЛЬКО под `authn.identity-provider: own`
// (`required_settings.go`, `Lanes: own`): незаданная — отказ старта с именем ручки.
// Копия чарта в зонте не называла ни одной — ни в карте настроек, ни переменной
// окружения, — то есть профиль, переведённый на `own`, ПРОХОДИЛ БЫ РЕНДЕР И ВСЕХ
// СТРАЖЕЙ ПОСАДКИ И НЕ ПОДНЯЛ БЫ СЛУЖБУ. Класс — «возможность объявлена и
// неисполнима»: страж требует того, чего чарт не умеет выразить.
//
// Признак на день заведения (kacho `origin/main` @ `ac33f733`, после посадки
// PR #2710, поднявшего пин службы до `16b5cade`, где обе ручки уже есть):
//
//	git grep -c 'SECOND_FACTOR\|self-service-freshness\|selfServiceFreshness' \
//	  origin/main -- deploy/helm/umbrella/charts/kaname   → 0 (код 1)
//
// ─────────────────────────────────────────────────────────────────────────────
// ОДНОГО ОБЪЕКТА SECRET МАЛО — И ЭТО РАЗЛИЧИЕ С ЧАРТОМ САМОЙ СЛУЖБЫ
//
// В чарте службы секреты подаются КАРТОЙ `secrets`: положил ключ в карту — строка
// переменной родилась сама (`range $k, $v := .Values.secrets`). В зонтичной копии
// такой карты НЕТ: секреты провязаны ПОИМЁННО, блоком `secretKeyRef` на каждую
// переменную. Следствие, которое легко упустить: создать объект Secret в кластере
// НЕДОСТАТОЧНО — без строки переменной в шаблоне процесс его не увидит ни при
// каком содержимом. Поэтому проверка судит ОБЕ половины: и координату секрета в
// значениях, и строку переменной в шаблоне.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ОБЪЯВЛЕНИЯ, А НЕ РЕНДЕР
//
// Тот же довод, что у соседнего login_lane_umbrella_test.go: рендер зонта требует
// скачанных зависимостей, а проверка, умеющая пропуститься, гейтом не является.
// Здесь читается то, что шаблон СОДЕРЖИТ и профиль ОБЪЯВЛЯЕТ, — и это ловит ровно
// тот класс, ради которого проверка заведена: «ключа нет вовсе».
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ПРОВЕРКА НЕ УТВЕРЖДАЕТ
//
// Она не утверждает, что объект Secret в кластере существует и что в нём лежит
// годный материал: это свойство стенда, а не дерева, и его судит подъём. Она
// утверждает, что величине ЕСТЬ ЧЕМ доехать до процесса и что профиль, объявивший
// посадку `own`, её назвал.
package deploy_test

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// secondFactorEnv — переменная, которой перечень ключей обёртки секретов второго
// фактора доезжает до процесса.
const secondFactorEnv = "KANAME_SECOND_FACTOR_ENC_KEY"

// selfServiceFreshnessKey — ключ карты настроек службы, и рядом — имя переменной,
// которой та же величина подаётся окружением.
const (
	selfServiceFreshnessKey   = "self-service-freshness"
	selfServiceFreshnessEnv   = "KANAME_AUTHN__SELF_SERVICE_FRESHNESS"
	selfServiceFreshnessValue = "selfServiceFreshness"
)

// secondFactorStackFacts — что один стенд объявил о двух величинах Ф12.
//
// Факты отделены от их чтения намеренно: судящая функция ниже чистая, поэтому
// инъекция подаёт ей синтетический вход и не трогает ни дерева, ни кластера.
type secondFactorStackFacts struct {
	Stack     string // имя стенда из stacks.txt
	Posture   string // посадка личности, объявленная цепочкой стенда службе
	Freshness bool   // объявлено ли окно свежести
	SecretRef bool   // объявлены ли ОБЕ координаты секрета (имя и ключ)
}

// secondFactorCensus — объём осмотренного. «Находок ноль» обязано быть отличимо
// от «прочитано ноль», поэтому печатаются обе величины.
type secondFactorCensus struct {
	Stacks int // стендов в таблице
	Own    int // из них объявивших посадку `own`
}

func (c secondFactorCensus) String() string {
	return fmt.Sprintf("стендов осмотрено %d · из них на посадке own %d", c.Stacks, c.Own)
}

// judgeSecondFactorDeclarations — НАХОДКИ по перечню стендов.
//
// Стенд на `external` находкой не является ни при каких значениях: страж службы
// обе ручки под ним не читает вовсе, и требовать их значило бы краснеть на
// исправном дереве. Это законный близнец, и он проверяется инъекцией.
func judgeSecondFactorDeclarations(facts []secondFactorStackFacts) ([]string, secondFactorCensus) {
	var (
		findings []string
		census   secondFactorCensus
	)
	for _, f := range facts {
		census.Stacks++
		if f.Posture != "own" {
			continue
		}
		census.Own++
		if !f.Freshness {
			findings = append(findings, fmt.Sprintf(
				"стенд %s на посадке own: `kaname.config.authn.%s` не объявлен — служба откажет в "+
					"старте с именем ручки `authn.%s` (%s). Умолчания нет ни у процесса, ни у чарта: "+
					"перенос прежней величины (15m) объявляет профиль",
				f.Stack, selfServiceFreshnessValue, selfServiceFreshnessKey, selfServiceFreshnessEnv))
		}
		if !f.SecretRef {
			findings = append(findings, fmt.Sprintf(
				"стенд %s на посадке own: координаты секрета `kaname.platform.iam.secondFactor.{encKeySecretName,encKeySecretKey}` "+
					"объявлены не обе — переменная %s родится со ссылкой на пустое имя, и служба откажет в "+
					"старте по `authn.second-factor-encryption-key-hex`. Половина пары ХУЖЕ отсутствия обеих: "+
					"она выглядит настроенной",
				f.Stack, secondFactorEnv))
		}
	}
	return findings, census
}

// TestOwnSecondFactor_UmbrellaSubchartExpressesBothKnobs — копия чарта УМЕЕТ
// выразить обе величины: окно свежести — ключом карты настроек, ключ обёртки —
// строкой переменной со ссылкой на секрет, чья координата берётся из значений.
func TestOwnSecondFactor_UmbrellaSubchartExpressesBothKnobs(t *testing.T) {
	chart := kanameSubchart(t)
	configmap := readChartText(t, filepath.Join(chart, "templates", "configmap.yaml"))
	deployment := readChartText(t, filepath.Join(chart, "templates", "deployment.yaml"))
	values := readChartText(t, filepath.Join(chart, "values.yaml"))

	// ОКНО СВЕЖЕСТИ — под ветвью, а не сквозной подстановкой: незаданное обязано
	// доехать до стража незаданным, иначе профиль объявлял бы посадку, которой не
	// будет.
	freshness := regexp.MustCompile(
		`\{\{-?\s*with\s+\.Values\.config\.authn\.` + selfServiceFreshnessValue + `\s*\}\}`)
	if !freshness.MatchString(configmap) {
		t.Errorf("configmap.yaml: ветви `with .Values.config.authn.%s` нет — профиль на `own` не может "+
			"выразить окно свежести ни одним ключом, и служба откажет в старте (Ф12, kacho#1281)",
			selfServiceFreshnessValue)
	}
	if !strings.Contains(configmap, selfServiceFreshnessKey+":") {
		t.Errorf("configmap.yaml: ключ `authn.%s` не рендерится", selfServiceFreshnessKey)
	}
	// Сквозная подстановка без ветви — отдельная находка: она подала бы стражу
	// пустую строку вместо «не объявлено», и отказ назвал бы не ту причину.
	if regexp.MustCompile(selfServiceFreshnessKey + `:\s*\{\{\s*(default|\.Values)`).MatchString(configmap) {
		t.Errorf("configmap.yaml: `%s` рендерится сквозной подстановкой — незаданная величина доедет "+
			"до стража ПУСТОЙ, а не незаданной", selfServiceFreshnessKey)
	}

	// КЛЮЧ ОБЁРТКИ — строка переменной со ссылкой на секрет. Судятся обе половины
	// разом: имя переменной и то, что её значение берётся из секрета, а не из
	// профиля (профиль отслеживается git).
	envBlock := regexp.MustCompile(
		`(?s)- name: ` + secondFactorEnv + `\s*\n\s*valueFrom:\s*\n\s*secretKeyRef:\s*\n` +
			`\s*name: \{\{\s*\.Values\.platform\.iam\.secondFactor\.encKeySecretName\s*\}\}\s*\n` +
			`\s*key: \{\{\s*\.Values\.platform\.iam\.secondFactor\.encKeySecretKey\s*\}\}`)
	if !envBlock.MatchString(deployment) {
		t.Errorf("deployment.yaml: переменной %s со ссылкой на секрет по координате "+
			"`platform.iam.secondFactor.*` нет. Карты `secrets` в этой копии чарта НЕТ — секреты "+
			"провязаны поимённо, поэтому объекта Secret в кластере недостаточно: без этой строки "+
			"процесс величины не увидит ни при каком его содержимом", secondFactorEnv)
	}
	if strings.Contains(deployment, "- name: "+secondFactorEnv+"\n              value:") {
		t.Errorf("deployment.yaml: %s подаётся ЗНАЧЕНИЕМ, а не ссылкой на секрет — профиль "+
			"отслеживается git, и перечень ключей обёртки уехал бы в историю", secondFactorEnv)
	}

	// Базовый профиль: координата секрета объявлена (обе половины), образец окна
	// свежести и имя его переменной названы читающему.
	for _, must := range []string{
		"secondFactor:", "encKeySecretName: kaname-second-factor-enc-key", "encKeySecretKey:",
		selfServiceFreshnessValue, selfServiceFreshnessEnv,
	} {
		if !strings.Contains(values, must) {
			t.Errorf("values.yaml подчарта: `%s` не назван — оператор, переводящий стенд на `own`, "+
				"узнавал бы имя ручки перезапуском пода", must)
		}
	}
	// Окно свежести умолчанием НЕ объявляется: величина, которую построение
	// подставляет молча, предметом стража быть не может.
	if regexp.MustCompile(`(?m)^\s+` + selfServiceFreshnessValue + `:\s*\S`).MatchString(values) {
		t.Errorf("values.yaml подчарта: `%s` объявлен умолчанием — страж по нему не исполнился бы "+
			"ни разу", selfServiceFreshnessValue)
	}
}

// TestOwnSecondFactor_StacksOnOwnDeclareBothKnobs — каждый стенд, чья цепочка
// объявила службе посадку `own`, называет обе величины; стенд на `external`
// находкой не является.
func TestOwnSecondFactor_StacksOnOwnDeclareBothKnobs(t *testing.T) {
	facts := readSecondFactorStackFacts(t)
	if len(facts) == 0 {
		t.Fatal("таблица стендов пуста: обход беспредметен, и «находок нет» здесь означало бы " +
			"«сверять было не с чем»")
	}

	findings, census := judgeSecondFactorDeclarations(facts)
	t.Logf("перепись: %s", census)
	for _, f := range facts {
		t.Logf("  %s: posture=%s окно=%v секрет=%v", f.Stack, f.Posture, f.Freshness, f.SecretRef)
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// readSecondFactorStackFacts — факты из дерева: цепочка каждого стенда
// накладывается слева направо поверх базовых значений подчарта, ровно как её
// получает helm.
func readSecondFactorStackFacts(t *testing.T) []secondFactorStackFacts {
	t.Helper()
	stacksTbl := deployStacks(t)

	names := make([]string, 0, len(stacksTbl))
	for n := range stacksTbl {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]secondFactorStackFacts, 0, len(names))
	for _, name := range names {
		// Базовые значения подчарта читаются ЗАНОВО на каждый стенд: `mergeValues`
		// правит карту НА МЕСТЕ, и одна общая карта умолчаний протекала бы из
		// стенда в стенд, приписывая одному профилю объявления другого.
		declared := map[string]any{"kaname": readYAML(t, filepath.Join(kanameSubchart(t), "values.yaml"))}
		for _, p := range stacksTbl[name] {
			declared = mergeValues(declared, readYAML(t, filepath.Join(umbrellaDir, p)))
		}
		posture, _ := lookup(declared, "kaname", "config", "authn", "identityProvider")
		_, freshness := lookup(declared, "kaname", "config", "authn", selfServiceFreshnessValue)
		secretName, okName := lookup(declared, "kaname", "platform", "iam", "secondFactor", "encKeySecretName")
		secretKey, okKey := lookup(declared, "kaname", "platform", "iam", "secondFactor", "encKeySecretKey")
		out = append(out, secondFactorStackFacts{
			Stack:     name,
			Posture:   fmt.Sprint(posture),
			Freshness: freshness,
			SecretRef: okName && okKey &&
				strings.TrimSpace(fmt.Sprint(secretName)) != "" &&
				strings.TrimSpace(fmt.Sprint(secretKey)) != "",
		})
	}
	return out
}
