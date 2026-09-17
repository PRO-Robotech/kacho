// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package deploy_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// login_lane_umbrella_test.go — umbrella-подчарт службы доступа ВЫРАЖАЕТ посадку
// полосы входа паролем (приёмка Ф3, kacho#1269; Ф3-44, Ф3-45) — и вместе с ней
// величины регистрации (Ф4, kacho#2699) и срок кода восстановления (Ф5,
// kacho#2701): у стража службы под `own` они в одном ряду с величинами Ф3.
//
// # Предмет
//
// Служба поднимает слушатель четырёх глаголов формы посадкой `own` и требует при
// этом: адрес слушателя, взаимный TLS на нём (режим `mutual`) и блок величин
// `authn.login` (срок сессии, домен печенья, частота, правило пароля, стоимость
// проверяющего, ёмкость и резерв памяти) — у каждой нет умолчания, незаданная
// есть отказ старта с именем ручки. Собственный чарт службы всё это несёт;
// umbrella-подчарт платформы — не нёс ничего: профиль, переводящий стенд на
// `own`, не мог выразить посадку ВООБЩЕ, и «рендерится» тут не свидетельство
// (рендер судит другую стадию, чем страж старта).
//
// # Что судится — объявления, не рендер
//
// Рендер umbrella требует скачанных зависимостей; проверка по объявлениям читает
// то, что профиль ОБЪЯВЛЯЕТ, и ловит ровно «ключа нет вовсе». Тот же довод, что у
// kaname_listener_knobs_test.go.
//
// # Граница названа вслух
//
// Профилей с посадкой `own` на день заведения — ноль (перепись печатается).
// Величины полосы стоят в боевом профиле на `external` по приёмке Ф3 (Р3, Р11,
// Ф3-42): дословный перенос живёт в профилях обоих чартов, где его видит
// читающий, и перевод на `own` не заводит полосу с нуля. Страж службы читает их
// только под `own`, поэтому каждая запись ниже несёт ПРИЧИНУ — ведомость
// сверяется по ключу, а не по блоку, и истекает с первым профилем на `own`: там
// снятие ручки роняет старт, и причина «страж не читает» перестаёт быть верной.

// loginLaneConfigKeys — ключи блока `authn.login` в настройках службы и их имена
// в значениях чарта (тот же перечень, что у собственного чарта службы).
var loginLaneConfigKeys = []struct{ configKey, valueKey string }{
	{"session-ttl", "sessionTtl"},
	{"cookie-domain", "cookieDomain"},
	{"address-attempts", "addressAttempts"},
	{"address-window", "addressWindow"},
	{"source-attempts", "sourceAttempts"},
	{"source-window", "sourceWindow"},
	{"password-min-length", "passwordMinLength"},
	{"breach-check", "breachCheck"},
	{"breach-check-url", "breachCheckUrl"},
	{"hasher-format", "hasherFormat"},
	{"hasher-memory", "hasherMemory"},
	{"hasher-iterations", "hasherIterations"},
	{"hasher-parallelism", "hasherParallelism"},
	{"verifier-capacity", "verifierCapacity"},
	{"memory-reserve-bytes", "memoryReserveBytes"},
	// Срок кода восстановления доступа (Ф5, kacho#2701) — ручка ТОГО ЖЕ блока
	// `authn.login`: страж службы требует её под `own` наравне с остальными.
	{"recovery-code-ttl", "recoveryCodeTtl"},
}

// registrationConfigKeys — ключи блока `authn.registration` (Ф4, kacho#2699):
// потолок темпа заведения аккаунтов одной личностью и окно счёта. Оба — условие
// старта под `own` (Ф4-18/19), у обоих нет умолчания.
var registrationConfigKeys = []struct{ configKey, valueKey string }{
	{"admissions-per-window", "admissionsPerWindow"},
	{"admission-window", "admissionWindow"},
}

// registrationIntegerKeys — целые блока регистрации; ноль у предела ЗАКОНЕН
// («сверх первого — ни одного»), поэтому ветвь обязана быть по `hasKey`, а не
// по `with`: `with` считает ноль пустым и выбросил бы объявленную величину.
var registrationIntegerKeys = []string{"admissionsPerWindow"}

// loginLaneIntegerKeys — целые, обязанные проходить через `int64`: большое число
// из значений приходит в шаблон плавающим и без приведения рендерится как
// `2.68e+08`, чего разборщик настроек не примет.
var loginLaneIntegerKeys = []string{
	"addressAttempts", "sourceAttempts", "passwordMinLength",
	"hasherMemory", "hasherIterations", "hasherParallelism",
	"verifierCapacity", "memoryReserveBytes",
}

// loginLaneEnv — пять переменных транспорта слушателя формы.
var loginLaneEnv = []string{
	"KANAME_LOGINLANE_SERVER_MTLS_ENABLE",
	"KANAME_LOGINLANE_SERVER_MTLS_CLIENTAUTHMODE",
	"KANAME_LOGINLANE_SERVER_MTLS_CERTFILE",
	"KANAME_LOGINLANE_SERVER_MTLS_KEYFILE",
	"KANAME_LOGINLANE_SERVER_MTLS_CLIENTCAFILES",
}

// loginLaneProdReason — одна причина на все записи боевого профиля, названная у
// каждой (ведомость сверяется по ключу).
const loginLaneProdReason = "величина ПОЛОСЫ ВХОДА ПАРОЛЕМ (Ф3, kacho#1269): страж службы читает её только " +
	"под посадкой `own`, а боевой профиль стоит на `external`. Объявлена по приёмке Ф3 (Р3, Р11, Ф3-42): " +
	"дословный перенос величин живёт в профилях обоих чартов, где его видит читающий, и перевод на `own` " +
	"не заводит полосу с нуля. Запись истекает с первым профилем на `own`: там снятие ручки роняет СТАРТ"

// loginLaneProdLedger — что боевой профиль umbrella объявляет намеренно при
// посадке `external`, и почему у каждой записи.
var loginLaneProdLedger = map[string]string{
	"ports.loginLane":              loginLaneProdReason,
	"mtls.loginLane":               loginLaneProdReason,
	"mtls.loginLaneClientAuthMode": loginLaneProdReason,
}

// registrationProdReason — та же причина для двух величин регистрации (Ф4).
const registrationProdReason = "величина РЕГИСТРАЦИИ НАШЕЙ ПОЛОСОЙ (Ф4, kacho#2699): страж службы читает её " +
	"только под посадкой `own`, а боевой профиль стоит на `external`. Объявлена по приёмке Ф4 (Р5, Ф4-18/19): " +
	"перенос величины справочника (3 за 1 ч) живёт в профиле, где его видит читающий, и перевод на `own` не " +
	"заводит полосу с нуля. Запись истекает с первым профилем на `own`: там снятие ручки роняет СТАРТ"

func init() {
	for _, k := range loginLaneConfigKeys {
		if k.valueKey == "breachCheckUrl" {
			// Боевой профиль объявляет проверку утечек словом `disabled`; адреса при
			// ней не бывает. Профиль с `enabled` заведёт запись вместе с адресом.
			continue
		}
		loginLaneProdLedger["config.authn.login."+k.valueKey] = loginLaneProdReason
	}
	for _, k := range registrationConfigKeys {
		loginLaneProdLedger["config.authn.registration."+k.valueKey] = registrationProdReason
	}
}

// templateBlockBody — тело ПЕРВОГО блока шаблона, открытого строкой, которую
// узнаёт `open`, до закрывающего именно его `end`. Пусто — блока нет.
func templateBlockBody(template string, open *regexp.Regexp) string {
	lines := strings.Split(template, "\n")
	for i, line := range lines {
		if !open.MatchString(line) {
			continue
		}
		depth := 1
		var body []string
		for j := i + 1; j < len(lines); j++ {
			depth += len(templateBlockOpen.FindAllString(lines[j], -1))
			depth -= len(templateBlockClose.FindAllString(lines[j], -1))
			if depth <= 0 {
				return strings.Join(body, "\n")
			}
			body = append(body, lines[j])
		}
		return strings.Join(body, "\n")
	}
	return ""
}

func kanameSubchart(t *testing.T) string {
	t.Helper()
	return filepath.Join(umbrellaDir, "charts", "kaname")
}

// readChartText читает файл подчарта по пути от каталога deploy (соседний
// `readFile` пакета берёт путь от корня репозитория).
func readChartText(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("чтение %s: %v", path, err)
	}
	return string(raw)
}

// TestLoginLane_F3_45_UmbrellaSubchartExpressesTheLane — шаблоны подчарта несут
// адрес слушателя, блок величин, транспорт под своей ручкой и порт на внутреннем
// Service.
func TestLoginLane_F3_45_UmbrellaSubchartExpressesTheLane(t *testing.T) {
	chart := kanameSubchart(t)
	configmap := readChartText(t, filepath.Join(chart, "templates", "configmap.yaml"))
	deployment := readChartText(t, filepath.Join(chart, "templates", "deployment.yaml"))
	service := readChartText(t, filepath.Join(chart, "templates", "service-internal.yaml"))
	values := readChartText(t, filepath.Join(chart, "values.yaml"))

	// Адрес слушателя — из номера порта профиля, только объявленного: умолчания
	// нет ни у процесса, ни у чарта (та же форма, что у REST-фронтов, #2247).
	if !regexp.MustCompile(`(?s)\{\{-?\s*if\s+\.Values\.ports\.loginLane\s*\}\}.*?login-lane-endpoint:\s*\{\{\s*printf "tcp://0\.0\.0\.0:%v" \.Values\.ports\.loginLane`).MatchString(configmap) {
		t.Error("configmap.yaml: `api-server.login-lane-endpoint` не рендерится из `ports.loginLane` под условием объявленности")
	}
	// Блок величин — под `with .Values.config.authn.login`, каждая ветвью. Тело
	// блока берётся ДО ЗАКРЫВАЮЩЕГО ЕГО `end` — глубина считается по действиям
	// шаблона (те же распознаватели, что у kaname_listener_knobs_test.go), а не
	// до первого `end`, который закрывает внутреннюю ветвь.
	block := templateBlockBody(configmap, regexp.MustCompile(`\{\{-?\s*with\s+\.Values\.config\.authn\.login\s*\}\}`))
	if block == "" {
		t.Fatal("configmap.yaml: блока `login:` под `with .Values.config.authn.login` нет — профиль на `own` не может выразить ни одной величины полосы")
	}
	missing := 0
	for _, k := range loginLaneConfigKeys {
		if !strings.Contains(block, k.configKey+":") {
			missing++
			t.Errorf("configmap.yaml: ключ `authn.login.%s` не рендерится", k.configKey)
		}
	}
	for _, ik := range loginLaneIntegerKeys {
		if !regexp.MustCompile(`int64\s+\.` + ik + `\b`).MatchString(block) {
			t.Errorf("configmap.yaml: целое `%s` рендерится без `int64` — большое число уедет как 2.68e+08", ik)
		}
	}
	t.Logf("перепись: ключей блока login объявлено %d · рендерится %d · целых через int64 %d",
		len(loginLaneConfigKeys), len(loginLaneConfigKeys)-missing, len(loginLaneIntegerKeys))

	// Транспорт — под СВОЕЙ полистенной ручкой, пять переменных; режим клиента
	// объявляется профилем (умолчание — единственный годный под `own` `mutual`).
	env := templateBlockBody(deployment, regexp.MustCompile(`\{\{-?\s*if\s*\(\s*dig\s+"loginLane"\s+\.Values\.mtls\.httpListeners\s+\.Values\.mtls\s*\)\s*-?\}\}`))
	if env == "" {
		t.Fatal("deployment.yaml: у слушателя формы нет своей ручки транспорта `dig \"loginLane\" .Values.mtls.httpListeners .Values.mtls` — общая ручка на два слушателя есть класс, ради которого заведён kaname_listener_knobs_test.go")
	}
	for _, name := range loginLaneEnv {
		if !strings.Contains(env, "- name: "+name) {
			t.Errorf("deployment.yaml: переменная %s не эмитится под ручкой loginLane", name)
		}
	}
	if !strings.Contains(env, ".Values.mtls.loginLaneClientAuthMode") {
		t.Error("deployment.yaml: режим проверки клиента слушателя формы не берётся из `mtls.loginLaneClientAuthMode`")
	}
	if !regexp.MustCompile(`(?s)\{\{-?\s*if\s+\.Values\.ports\.loginLane\s*\}\}\s*- name: http-login-lane\s*containerPort:\s*\{\{\s*\.Values\.ports\.loginLane\s*\}\}`).MatchString(deployment) {
		t.Error("deployment.yaml: порт контейнера `http-login-lane` не объявлен под условием `ports.loginLane`")
	}
	if !regexp.MustCompile(`(?s)\{\{-?\s*if\s+\.Values\.ports\.loginLane\s*\}\}\s*- name: http-login-lane`).MatchString(service) {
		t.Error("service-internal.yaml: порт `http-login-lane` не выставлен на внутреннем Service — край не дотянется до слушателя формы")
	}
	if !strings.Contains(service, "targetPort: http-login-lane") {
		t.Error("service-internal.yaml: порт Service не ведёт на `http-login-lane` контейнера")
	}

	// Базовый профиль: слушатель НЕ поднят по умолчанию (умолчания у порта нет), а
	// образец блока величин и ручки транспорта названы читающему.
	for _, must := range []string{"loginLane:", "loginLaneClientAuthMode", "login:", "sessionTtl", "memoryReserveBytes"} {
		if !strings.Contains(values, must) {
			t.Errorf("values.yaml подчарта: образец `%s` для профиля на `own` не назван", must)
		}
	}
	if regexp.MustCompile(`(?m)^\s+loginLane:\s*\d+`).MatchString(values) {
		t.Error("values.yaml подчарта: `ports.loginLane` объявлен умолчанием — слушатель поднимался бы всякой посадкой молча")
	}
}

// TestLoginLane_F3_45_ProductionProfilesDeclareTheLaneWithAReason — каждый стенд
// боевой посадки объявляет ручку транспорта слушателя формы и режим `mutual`
// явно; боевой профиль объявляет адрес и величины полосы, и у каждой записи
// названа причина, почему она стоит на `external`. Профили на `own`
// пересчитываются: у них те же строки — условие старта, и ведомость к ним не
// относится.
func TestLoginLane_F3_45_ProductionProfilesDeclareTheLaneWithAReason(t *testing.T) {
	chart := kanameSubchart(t)
	chartDefaults := readYAML(t, filepath.Join(chart, "values.yaml"))
	stacksTbl := deployStacks(t)
	names := make([]string, 0, len(stacksTbl))
	for n := range stacksTbl {
		names = append(names, n)
	}
	sort.Strings(names)

	production, ownPosture := 0, 0
	for _, name := range names {
		declared := map[string]any{}
		for _, p := range stacksTbl[name] {
			declared = mergeValues(declared, readYAML(t, filepath.Join(umbrellaDir, p)))
		}
		if !productionPosture(chartDefaults, declared, "kaname") {
			continue
		}
		production++
		knob, ok := lookup(declared, "kaname", "mtls", "loginLane")
		if !ok || knob != true {
			t.Errorf("стенд %s: ручка транспорта слушателя формы `kaname.mtls.loginLane` не объявлена `true` — слушатель взаимный по TLS (Р16), и величина, которую построение подставляет само, предметом стража быть не может", name)
		}
		mode, _ := lookup(declared, "kaname", "mtls", "loginLaneClientAuthMode")
		if mode != "mutual" {
			t.Errorf("стенд %s: режим проверки клиента слушателя формы обязан быть объявлен `mutual` явно, получено %v", name, mode)
		}
		if ip, _ := lookup(declared, "kaname", "config", "authn", "identityProvider"); ip == "own" {
			ownPosture++
			// Под `own` строки — условие старта: адрес и весь блок величин.
			if _, ok := lookup(declared, "kaname", "ports", "loginLane"); !ok {
				t.Errorf("стенд %s на посадке own: `kaname.ports.loginLane` не объявлен — отказ старта службы", name)
			}
			for _, k := range loginLaneConfigKeys {
				if k.valueKey == "breachCheckUrl" {
					continue
				}
				if _, ok := lookup(declared, "kaname", "config", "authn", "login", k.valueKey); !ok {
					t.Errorf("стенд %s на посадке own: `kaname.config.authn.login.%s` не объявлен — отказ старта службы", name, k.valueKey)
				}
			}
			for _, k := range registrationConfigKeys {
				if _, ok := lookup(declared, "kaname", "config", "authn", "registration", k.valueKey); !ok {
					t.Errorf("стенд %s на посадке own: `kaname.config.authn.registration.%s` не объявлен — отказ старта службы (Ф4-18)", name, k.valueKey)
				}
			}
		}
	}
	if production == 0 {
		t.Fatal("ни один стенд не объявлен боевой посадкой — вердикт беспредметен")
	}

	// Боевой профиль платформы объявляет адрес и величины на `external` — с
	// причиной у каждой записи ведомости.
	prod := readYAML(t, filepath.Join(umbrellaDir, "values.prod.yaml"))
	declaredKeys := 0
	for key, reason := range loginLaneProdLedger {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("ведомость: запись %s без причины — послабление без предмета", key)
		}
		if _, ok := lookup(prod, append([]string{"kaname"}, strings.Split(key, ".")...)...); !ok {
			t.Errorf("values.prod.yaml: ключ kaname.%s не объявлен, а ведомость называет его намеренно объявленным", key)
			continue
		}
		declaredKeys++
	}
	// Зеркало: ключ полосы, объявленный профилем и не названный в ведомости, —
	// запись без причины.
	if login, ok := lookup(prod, "kaname", "config", "authn", "login"); ok {
		if m, isMap := login.(map[string]any); isMap {
			for k := range m {
				if _, named := loginLaneProdLedger["config.authn.login."+k]; !named {
					t.Errorf("values.prod.yaml объявляет kaname.config.authn.login.%s, а ведомость его не называет — причина не записана", k)
				}
			}
		}
	}
	if reg, ok := lookup(prod, "kaname", "config", "authn", "registration"); ok {
		if m, isMap := reg.(map[string]any); isMap {
			for k := range m {
				if _, named := loginLaneProdLedger["config.authn.registration."+k]; !named {
					t.Errorf("values.prod.yaml объявляет kaname.config.authn.registration.%s, а ведомость его не называет — причина не записана", k)
				}
			}
		}
	}
	t.Logf("перепись: стендов боевой посадки %d · из них на posture own %d · записей ведомости %d · объявлено боевым профилем %d",
		production, ownPosture, len(loginLaneProdLedger), declaredKeys)
}

// TestLoginLane_F4_F5_UmbrellaSubchartExpressesRegistrationAndRecovery —
// подчарт рендерит блок `authn.registration` (Ф4, kacho#2699) и ключ
// `authn.login.recovery-code-ttl` (Ф5, kacho#2701), а базовый профиль называет
// образец обоих читающему.
//
// # Предмет — та же форма, что у полосы Ф3, и тот же класс дефекта
//
// Страж старта службы под `own` требует три величины (Ф4 Р5, Ф4-18/19; Ф5-06/07),
// у которых нет умолчания. Подчарт, знающий только блок `login` Ф3, не может
// подать их ни одним ключом профиля: профиль, переводящий стенд на `own`,
// проходил бы отказ старта по трём именам подряд, и оператору оставалось бы
// угадывать карту `KANAME_AUTHN__REGISTRATION__*` /
// `KANAME_AUTHN__LOGIN__RECOVERY_CODE_TTL` — цикл выкатки на ручку. Собственный
// чарт службы несёт тот же долг (kaname#205); здесь — зонтичный подчарт.
//
// # Ноль у предела темпа законен, и ветвь обязана его пропустить
//
// `admissions-per-window: 0` означает «сверх первого заведения — ни одного»
// (Ф4 Р5), а `with` считает ноль пустым. Целое обязано идти через `hasKey` и
// `int64` — тот же довод, что у целых блока `login`.
func TestLoginLane_F4_F5_UmbrellaSubchartExpressesRegistrationAndRecovery(t *testing.T) {
	chart := kanameSubchart(t)
	configmap := readChartText(t, filepath.Join(chart, "templates", "configmap.yaml"))
	values := readChartText(t, filepath.Join(chart, "values.yaml"))

	login := templateBlockBody(configmap, regexp.MustCompile(`\{\{-?\s*with\s+\.Values\.config\.authn\.login\s*\}\}`))
	if login == "" {
		t.Fatal("configmap.yaml: блока `login:` под `with .Values.config.authn.login` нет")
	}
	if !strings.Contains(login, "recovery-code-ttl:") {
		t.Error("configmap.yaml: ключ `authn.login.recovery-code-ttl` не рендерится — под `own` служба отказывает в старте: срок кода восстановления не задан (Ф5-06)")
	}

	registration := templateBlockBody(configmap, regexp.MustCompile(`\{\{-?\s*with\s+\.Values\.config\.authn\.registration\s*\}\}`))
	if registration == "" {
		t.Fatal("configmap.yaml: блока `registration:` под `with .Values.config.authn.registration` нет — профиль на `own` не может выразить ни потолок темпа, ни окно (Ф4-18)")
	}
	missing := 0
	for _, k := range registrationConfigKeys {
		if !strings.Contains(registration, k.configKey+":") {
			missing++
			t.Errorf("configmap.yaml: ключ `authn.registration.%s` не рендерится", k.configKey)
		}
	}
	for _, ik := range registrationIntegerKeys {
		if !regexp.MustCompile(`hasKey\s+\.\s+"` + ik + `"`).MatchString(registration) {
			t.Errorf("configmap.yaml: целое `%s` не ветвится по `hasKey` — ноль (законная величина) выбрасывался бы как пустое", ik)
		}
		if !regexp.MustCompile(`int64\s+\.` + ik + `\b`).MatchString(registration) {
			t.Errorf("configmap.yaml: целое `%s` рендерится без `int64`", ik)
		}
	}

	// Базовый профиль называет образец обоих блоков читающему — вместе с именами
	// переменных, которыми та же величина подаётся окружением.
	for _, must := range []string{
		"recoveryCodeTtl", "KANAME_AUTHN__LOGIN__RECOVERY_CODE_TTL",
		"registration:", "admissionsPerWindow", "admissionWindow",
		"KANAME_AUTHN__REGISTRATION__ADMISSIONS_PER_WINDOW", "KANAME_AUTHN__REGISTRATION__ADMISSION_WINDOW",
	} {
		if !strings.Contains(values, must) {
			t.Errorf("values.yaml подчарта: образец `%s` для профиля на `own` не назван", must)
		}
	}
	t.Logf("перепись: ключей блока registration объявлено %d · рендерится %d · целых через hasKey/int64 %d · recovery-code-ttl в блоке login %v",
		len(registrationConfigKeys), len(registrationConfigKeys)-missing, len(registrationIntegerKeys), strings.Contains(login, "recovery-code-ttl:"))
}
