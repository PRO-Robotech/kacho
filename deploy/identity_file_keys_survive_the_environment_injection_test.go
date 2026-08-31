// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// identity_file_keys_survive_the_environment_injection_test.go — доказательство,
// что соседний гейт СПОСОБЕН упасть и СПОСОБЕН смолчать.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ИНЪЕКЦИЯ ИДЁТ НАСТОЯЩЕЙ РУЧКОЙ ЧАРТА
//
// Дефект вносится ровно тем механизмом, которым он приезжает в жизни: ключом
// значений `kratos.kratos.config.courier.smtp.connection_uri`, из которого чарт
// поставщика производит переменную. Синтетический рендер доказывал бы, что
// разборщик читает YAML, — а требуется доказать, что гейт видит ПРЕДМЕТ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ТРИ ПРОГОНА, А НЕ ДВА (testing.md §«Гейт на класс», п. 2в)
//
//	контроль          дерево цело — молчат ОБА: и новый гейт, и существующий;
//	инъекция НОВОГО   ключ, перебиваемый переменной, — краснеет ТОЛЬКО новый;
//	инъекция СТАРОГО  снята ручка аргументов почтового процесса — краснеет
//	                  ТОЛЬКО существующий.
//
// Третий прогон обязателен: без него молчание существующего контроля на втором
// прогоне неотличимо от молчания МЁРТВОГО контроля.
//
// Инъекция нового при этом снимает свойство у элемента, чьё старое на месте:
// добавляется ОДНО значение существующей ручки существующего профиля, а не
// заводится новый элемент, нарушающий всё, что от элементов требуется.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЗАКОННЫЙ БЛИЗНЕЦ — БЛИЖНИЙ, А НЕ ПРОИЗВОЛЬНЫЙ
//
// Близнецом служит переменная `SESSION_LIFESPAN_EXTRA`: наш файл объявляет
// `session.lifespan`, поэтому имя отличается от перебивающего ОДНИМ суффиксом.
// Произвольное имя доказывало бы лишь, что гейт не краснеет на чём попало;
// ближнее доказывает, что он ловит СОВПАДЕНИЕ ПУТИ, а не вхождение подстроки.
package deploy_test

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// injectionStack — стек, на котором ведутся все три прогона. `prod` выбран
// затем, что на нём перебиваемых ключей НЕТ и в исходном дереве: значит
// покраснение второго прогона производит ровно инъекция, а не фон.
const injectionStack = "prod"

// scanRenderedStack — то же ядро, которым судит гейт, поднятое на рендере.
func scanRenderedStack(t *testing.T, sets ...string) (declared int, findings []overrideFinding, envFrom []string) {
	t.Helper()
	stacks := deployStacks(t)
	chain, ok := stacks[injectionStack]
	if !ok {
		t.Fatalf("в таблице стеков нет %q — предпосылка инъекции исчезла, а не дефект перестал вноситься",
			injectionStack)
	}
	rendered, err := renderStack(t, chain, sets...)
	if err != nil {
		t.Fatalf("рендер стека %q с инъекцией %v не удался (%v) — вердикта нет:\n%s",
			injectionStack, sets, err, rendered)
	}
	docs := decodeRender(t, rendered)

	bodiesRaw := ourIdentityConfigBodies(docs)
	if len(bodiesRaw) == 0 {
		t.Fatalf("инъекция: в рендере нет нашей карты настроек — обход пуст, вердикт беспредметен")
	}
	bodies := map[string]map[string]any{}
	for name, body := range bodiesRaw {
		var cfg map[string]any
		if err := yaml.Unmarshal([]byte(body), &cfg); err != nil {
			t.Fatalf("инъекция: тело настроек %s не разбирается: %v", name, err)
		}
		bodies[name] = cfg
	}
	subjects := identitySubjects(docs, bodiesRaw)
	if len(subjects) == 0 {
		t.Fatalf("инъекция: процессов-читателей не найдено — обход пуст")
	}
	for _, s := range subjects {
		if s.EnvFrom {
			envFrom = append(envFrom, s.Workload+"/"+s.Container)
		}
	}
	declared, findings = scanIdentityEnvOverrides(injectionStack, bodies, subjects)
	return declared, findings, envFrom
}

// TestFileKeyOverrideInjection_RealKnobRedsAndLegalTwinIsSilent — прогоны 1 и 2
// плюс законный близнец и ось `envFrom`.
func TestFileKeyOverrideInjection_RealKnobRedsAndLegalTwinIsSilent(t *testing.T) {
	// ── прогон 1: КОНТРОЛЬ ────────────────────────────────────────────────
	declared, findings, envFrom := scanRenderedStack(t)
	t.Logf("контроль: ключей объявлено %d, перебиваются %d, процессов с `envFrom` %d",
		declared, len(findings), len(envFrom))
	if declared == 0 {
		t.Fatalf("контроль: наш файл не объявляет ни одного ключа — инъекции нечего перебивать, "+
			"и молчание прогона 2 ничего не доказало бы (осмотрено ключей %d)", declared)
	}
	if len(findings) != 0 {
		t.Fatalf("контроль: на целом дереве найдено %d перебиваемых ключей (%v) — "+
			"фон непуст, поэтому покраснение инъекции не будет доказательством", len(findings), findings)
	}
	if len(envFrom) != 0 {
		t.Fatalf("контроль: на целом дереве %d процессов получают окружение формой `envFrom` (%v) — "+
			"фон непуст", len(envFrom), envFrom)
	}

	// ── прогон 2: ИНЪЕКЦИЯ НОВОГО СВОЙСТВА ────────────────────────────────
	//
	// Настоящая ручка чарта поставщика: объявленный ключ заставляет его
	// положить величину в свой секрет и выставить её переменной на обоих
	// процессах.
	_, injected, _ := scanRenderedStack(t,
		"kratos.kratos.config.courier.smtp.connection_uri=smtp://injected.invalid:25/")
	t.Logf("инъекция ключа: перебиваемых %d", len(injected))
	if len(injected) == 0 {
		t.Fatalf("инъекция настоящей ручкой НЕ дала находки — гейт неспособен упасть на предмете, " +
			"ради которого заведён: ключ `courier.smtp.connection_uri` объявлен нашим файлом, " +
			"а переменная `COURIER_SMTP_CONNECTION_URI` выставлена чартом поставщика")
	}
	var named bool
	for _, f := range injected {
		if f.Key == "courier.smtp.connection_uri" && f.Env == "COURIER_SMTP_CONNECTION_URI" {
			named = true
		}
		if f.Workload == "" || f.Container == "" {
			t.Errorf("находка не называет процесс: %+v — читателю некуда идти", f)
		}
	}
	if !named {
		t.Errorf("находка не называет ни ключ, ни переменную инъекции: %+v — гейт покраснел, "+
			"но диагностика посылает читателя не туда", injected)
	}

	// ── законный близнец: имя, отличающееся ОДНИМ суффиксом ───────────────
	_, twin, _ := scanRenderedStack(t,
		"kratos.deployment.extraEnv[0].name=SESSION_LIFESPAN_EXTRA",
		"kratos.deployment.extraEnv[0].value=ignored")
	t.Logf("законный близнец: перебиваемых %d", len(twin))
	if len(twin) != 0 {
		t.Errorf("гейт краснеет на переменной `SESSION_LIFESPAN_EXTRA`, которая ключа "+
			"`session.lifespan` НЕ перебивает: %+v — он ловит вхождение подстроки, а не "+
			"совпадение пути, и первый же ложный срабат его отключит", twin)
	}

	// ── ось `envFrom`: перепись обязана стать НЕРАЗРЕШИМОЙ, а не пустой ───
	_, _, injectedEnvFrom := scanRenderedStack(t,
		"kratos.deployment.environmentSecretsName=kacho-injected-env")
	t.Logf("инъекция `envFrom`: процессов с непрозрачным окружением %d", len(injectedEnvFrom))
	if len(injectedEnvFrom) == 0 {
		t.Errorf("форма `envFrom` внесена настоящей ручкой чарта и гейтом НЕ замечена — " +
			"перепись объявила бы «ни один ключ не перебивается», не имея возможности это измерить: " +
			"имён ключей чужого секрета в рендере нет by construction")
	}
}

// TestFileKeyOverrideInjection_ExistingControlStillReds — ПРОГОН 3.
//
// Инъекция прогона 2 не должна была ронять ничего, кроме нового гейта. Чтобы
// это молчание не оказалось молчанием мёртвого контроля, существующий контроль
// роняется СВОЕЙ инъекцией: у почтового процесса снимается ручка аргументов, и
// он перестаёт читать наш файл.
//
// Существующий контроль — deploy/identity_courier_reads_what_it_mounts_test.go;
// здесь воспроизводится ровно его предмет (множества файлов настроек у двух
// процессов совпадают), а не пересказывается его текст.
func TestFileKeyOverrideInjection_ExistingControlStillReds(t *testing.T) {
	configArgsOf := func(sets ...string) map[string][]string {
		stacks := deployStacks(t)
		rendered, err := renderStack(t, stacks[injectionStack], sets...)
		if err != nil {
			t.Fatalf("рендер %q (%v): %v\n%s", injectionStack, sets, err, rendered)
		}
		out := map[string][]string{}
		for _, d := range decodeRender(t, rendered) {
			kind := str(d, "kind")
			if kind != "Deployment" && kind != "StatefulSet" {
				continue
			}
			name := str(submap(d, "metadata"), "name")
			if !strings.Contains(name, "kratos") || strings.Contains(name, "pg-") {
				continue
			}
			pod := submap(submap(submap(d, "spec"), "template"), "spec")
			for _, c := range slice(pod, "containers") {
				cm, _ := c.(map[string]any)
				args := slice(cm, "args")
				var configs []string
				for i, a := range args {
					s, _ := a.(string)
					if s != "--config" || i+1 >= len(args) {
						continue
					}
					v, _ := args[i+1].(string)
					configs = append(configs, v)
				}
				if len(configs) > 0 {
					out[name+"/"+str(cm, "name")] = configs
				}
			}
		}
		return out
	}

	// ── контроль: оба процесса читают ОДНО И ТО ЖЕ ────────────────────────
	base := configArgsOf()
	t.Logf("контроль: процессов с файлами настроек %d", len(base))
	if len(base) < 2 {
		t.Fatalf("контроль: процессов с файлами настроек %d (<2) — предмет существующего "+
			"контроля исчез, и его инъекция ничего не доказала бы: %v", len(base), base)
	}
	var sets [][]string
	for _, v := range base {
		sets = append(sets, v)
	}
	for i := 1; i < len(sets); i++ {
		if strings.Join(sets[i], "|") != strings.Join(sets[0], "|") {
			t.Fatalf("контроль: множества файлов настроек РАЗОШЛИСЬ уже на целом дереве (%v) — "+
				"фон непуст, инъекция ничего не докажет", base)
		}
	}

	// ── инъекция СТАРОГО свойства: у почтового процесса снята своя ручка ──
	//
	// `null` очищает список: чарт поставщика аргументы почтовому процессу НЕ
	// наследует, поэтому наш файл перестаёт им читаться.
	broken := configArgsOf("kratos.statefulSet.extraArgs=null")
	t.Logf("инъекция старого: процессов с файлами настроек %d", len(broken))
	diverged := false
	for name, v := range broken {
		if strings.Join(v, "|") != strings.Join(sets[0], "|") {
			diverged = true
			t.Logf("разошёлся: %s читает %v вместо %v", name, v, sets[0])
		}
	}
	if !diverged {
		t.Errorf("снятие `statefulSet.extraArgs` НЕ развело множества файлов настроек (%v) — "+
			"существующий контроль неспособен упасть, и его молчание при инъекции нового "+
			"гейта ничего не доказывает", broken)
	}

	// ── и НОВЫЙ гейт на этой инъекции молчит: предмет у них разный ────────
	_, findings, _ := scanRenderedStack(t, "kratos.statefulSet.extraArgs=null")
	if len(findings) != 0 {
		t.Errorf("новый гейт покраснел на инъекции ЧУЖОГО предмета (%v) — красное приходит "+
			"от соседа, и вердикты перестают быть прослеживаемыми", findings)
	}
	t.Logf("инъекция старого: новый гейт молчит (перебиваемых %d) — предметы разведены", len(findings))
}
