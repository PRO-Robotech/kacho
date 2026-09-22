// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_posture_profiles_test.go — ДВЕ ПОЛОВИНЫ одного стенда объявляют одну
// и ту же посадку личности (задача #1125, подфаза Ф4д эпика #896).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Посадку читают ДВА процесса: служба прав и край. Половины разводят по ней
// РАЗНЫЕ требования — служба прав три адреса поставщика, край один. Профиль,
// объявивший посадку одной половине и забывший второй, даёт стенд, у которого
// половины решают о личности по-разному, и НИКТО ЭТОГО НЕ РЕШАЛ: расхождение
// возникает побочным эффектом правки.
//
// Проверяется именно РАЗНИЦА, а не каждая половина отдельно. Проба каждой
// половины требует знать, какой посадка должна быть, — а это и есть спорный
// вопрос профиля. Сравнение половин спрашивает другое: «решал ли кто-нибудь,
// что они различаются». На это ответ есть всегда.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧИТАЕТ ОБЪЯВЛЕНИЯ, А НЕ РЕНДЕР
//
// Тот же приём, что у декларативной пробы формы токена на крае: разбираются
// файлы значений, а не отрендеренные шаблоны. Рендер зонта требует загруженных
// зависимостей и сети; проба, умеющая пропускаться, гейтом не является.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПЕРЕПИСЬ ПЕЧАТАЕТ ОБЕ ВЕЛИЧИНЫ
//
// «профилей N · объявляют посадку M» — одно число скрывает ровно тот случай,
// ради которого проба заведена: профиль, где посадку не объявила ни одна
// половина, при одном числе неотличим от профиля, где её объявили обе.
package umbrella_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// postureDeclaration — что профиль объявил каждой половине.
type postureDeclaration struct {
	Profile string
	IAM     string // config.authn.identityProvider у подчарта службы прав
	Edge    string // authn.identityProvider у подчарта края
}

// halves возвращает объявленные половины и их число.
func (d postureDeclaration) halves() []string {
	var out []string
	if d.IAM != "" {
		out = append(out, "iam="+d.IAM)
	}
	if d.Edge != "" {
		out = append(out, "gateway="+d.Edge)
	}
	return out
}

// Профиль, объявивший посадку хотя бы одной половине, обязан объявить её ОБЕИМ,
// и объявить ОДИНАКОВО.
func TestIdentityPostureHalvesOfAProfileAgree(t *testing.T) {
	decls := readPostureDeclarations(t)
	if len(decls) == 0 {
		t.Fatal("обход пуст: файлов значений зонтичного чарта не найдено — проба судила бы о непрочитанном")
	}

	declaring, findings := judgePostureDeclarations(decls)

	t.Logf("перепись: профилей осмотрено %d · объявляют посадку %d · расхождений %d",
		len(decls), declaring, len(findings))
	for _, d := range decls {
		if h := d.halves(); len(h) > 0 {
			t.Logf("  %s: %s", d.Profile, strings.Join(h, " "))
		}
	}

	for _, f := range findings {
		t.Error(f)
	}
}

// НИ ОДНО умолчание подчарта не решает посадку ЗА стенд.
//
// # Что здесь утверждалось раньше и почему перевёрнуто
//
// Стояло обратное: базовые профили ОБОИХ подчартов обязаны объявлять посадку —
// «умолчание живёт в профиле, а не в коде». Умолчание, живущее в базовом
// профиле чарта, от умолчания в коде отличается только адресом: решает оно всё
// равно за стенд. Измерено: развёртываемых стендов семь, посадку объявлял ОДИН,
// шесть ехали на унаследованном значении.
//
// Страж старта умеет сказать «не задано»; сказать «задано ПРЕЖНЕЕ, а прежнего
// больше нет» он не умеет — `external` для него законное значение. Поэтому
// умолчание снято у края, а посадку объявляет КАЖДЫЙ стенд
// (gateway/deploy/identity_posture_reaches_every_stand_test.go).
//
// # ОСТАТОК, названный и с предикатом
//
// Копия чарта службы прав, вендоренная в этот зонт, умолчание ещё несёт. Её
// СОБСТВЕННЫЙ чарт в дереве службы умолчания не объявляет (`identityProvider:
// ""`), то есть расхождение здесь — у копии, и сводит копию с оригиналом
// владелец чарта, а не эта полоса. Запись ниже истекает сама: как только копия
// перестаёт объявлять посадку, запись становится находкой.
type postureDefaultRemainder struct {
	Chart string
	Path  string
	Why   string
	Until string
}

var postureDefaultRemainders = []postureDefaultRemainder{
	{
		Chart: "kaname",
		Path:  "charts/kaname/values.yaml",
		Why: "вендоренная копия чарта службы прав несёт умолчание `external`, которого " +
			"НЕТ в её собственном чарте (deploy/values.yaml службы объявляет пустое " +
			"значение). Сводит копию с оригиналом владелец чарта: правка здесь меняла бы " +
			"боевую посадку второй половины стенда из полосы, которая о ней не спрашивала",
		Until: "копия перестаёт объявлять config.authn.identityProvider — тогда запись " +
			"становится находкой сама, и её место снять",
	},
}

func TestNoSubchartDefaultDecidesThePostureForTheStand(t *testing.T) {
	iam := readNested(t, filepath.Join(iamChartDir, "values.yaml"),
		"config", "authn", "identityProvider")
	edge := readNested(t, filepath.Join("..", "..", "..", "gateway", "deploy", "values.yaml"),
		"authn", "identityProvider")

	declaring := boolToInt(iam != "") + boolToInt(edge != "")
	t.Logf("перепись: базовых профилей подчартов 2 · объявляют посадку %d · "+
		"названных остатков %d", declaring, len(postureDefaultRemainders))

	// Ведомость остатков судится САМА: запись без обоснования или без предиката
	// снятия — послабление, которое не умеет истечь.
	for _, r := range postureDefaultRemainders {
		if strings.TrimSpace(r.Why) == "" || strings.TrimSpace(r.Until) == "" {
			t.Errorf("остаток %s: запись без обоснования либо без предиката снятия", r.Chart)
		}
	}

	if edge != "" {
		t.Errorf("базовый профиль КРАЯ объявляет посадку (%q) — умолчание решает за стенд: "+
			"состояние «не объявлено» не наступит ни на одном профиле, и возвращённый краю "+
			"отказ старта не сработает НИ РАЗУ", edge)
	}

	// Вторая половина ведомости: запись, которой нечего исключать, — находка.
	for _, r := range postureDefaultRemainders {
		if r.Chart == "kaname" && iam == "" {
			t.Errorf("остаток %s пережил свой предмет: копия чарта посадку больше не "+
				"объявляет, а запись о ней осталась — её место снять, а не унаследовать", r.Chart)
		}
	}
}

// Законный близнец: профиль, посадку НЕ объявляющий вовсе, находкой ЭТОЙ пробы
// не считается — она судит согласие половин ОДНОГО профиля, а не полноту
// объявлений стенда. Полноту судит гейт стендов, складывающий цепочку `-f`;
// без этого случая проба краснела бы на каждом узком профиле.
func TestAProfileDeclaringNeitherHalfIsNotAFinding(t *testing.T) {
	decls := readPostureDeclarations(t)
	silent := 0
	for _, d := range decls {
		if len(d.halves()) == 0 {
			silent++
		}
	}
	t.Logf("перепись: профилей %d · не объявляют ни одной половины %d (накладки; "+
		"посадку им объявляет слой НИЖЕ по цепочке — это судит гейт стендов, "+
		"а умолчания у чарта края больше нет)", len(decls), silent)
	if len(decls) == 0 {
		t.Fatal("обход пуст — законный близнец не на чем проверить")
	}
}

// readPostureDeclarations разбирает файлы значений зонтичного чарта.
func readPostureDeclarations(t *testing.T) []postureDeclaration {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("каталог зонтичного чарта не прочитан: %v", err)
	}
	var out []postureDeclaration
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "values") || !strings.HasSuffix(name, ".yaml") {
			continue
		}
		out = append(out, postureDeclaration{
			Profile: name,
			IAM:     readNested(t, name, "kaname", "config", "authn", "identityProvider"),
			Edge:    readNested(t, name, "api-gateway", "authn", "identityProvider"),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Profile < out[j].Profile })
	return out
}

// readNested достаёт строковое значение по пути ключей. Отсутствие ключа —
// пустая строка: «не объявлено» отличимо от «объявлено пустым» тем, что пустое
// значение процессом отвергается отдельно.
func readNested(t *testing.T, path string, keys ...string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s не прочитан: %v", path, err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		// Профиль с шаблонными вставками разбору не поддаётся — это граница
		// предиката, и она называется вслух, а не проглатывается.
		t.Logf("  %s: YAML не разобран (%v) — профиль вне охвата этой пробы", path, err)
		return ""
	}
	var cur any = doc
	for _, k := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur, ok = m[k]
		if !ok {
			return ""
		}
	}
	s, ok := cur.(string)
	if !ok {
		return ""
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// judgePostureDeclarations — ТЕЛО пробы, вынесенное отдельно, чтобы инъекция
// звала то же, что исполняется на дереве. Своя копия предиката в инъекции
// разошлась бы с настоящей пробой молча.
func judgePostureDeclarations(decls []postureDeclaration) (declaring int, findings []string) {
	for _, d := range decls {
		h := d.halves()
		if len(h) == 0 {
			continue // профиль посадку не объявляет — наследует базовые значения подчартов
		}
		declaring++
		if len(h) == 1 {
			findings = append(findings, d.Profile+": посадку объявила ТОЛЬКО одна половина ("+
				strings.Join(h, ", ")+") — вторая унаследует базовое значение подчарта, и половины "+
				"одного стенда разойдутся без чьего-либо решения")
			continue
		}
		if d.IAM != d.Edge {
			findings = append(findings, d.Profile+": половины объявили РАЗНОЕ ("+
				strings.Join(h, ", ")+") — это расхождение, а не выбор")
		}
	}
	return declaring, findings
}

// ─────────────────────────────────────────────────────────────────────────────
// ИНЪЕКЦИЯ в обе стороны: проба обязана падать на расхождении и молчать на
// согласии. Без неё «расхождений 0» на дереве, где ни один профиль посадку не
// переопределяет, было бы неотличимо от пробы, не умеющей падать вовсе.

// Дефект: половины объявили РАЗНОЕ. Обязано находиться.
func TestInjection_HalvesDeclaringDifferentPosturesAreFound(t *testing.T) {
	_, findings := judgePostureDeclarations([]postureDeclaration{
		{Profile: "values.synthetic.yaml", IAM: "own", Edge: "external"},
	})
	if len(findings) != 1 || !strings.Contains(findings[0], "РАЗНОЕ") {
		t.Fatalf("расхождение половин не найдено: %v", findings)
	}
}

// Дефект: посадку объявила ТОЛЬКО одна половина. Обязано находиться — вторая
// молча унаследует базовое значение, и стенд разъедется.
func TestInjection_OnlyOneHalfDeclaringIsFound(t *testing.T) {
	_, findings := judgePostureDeclarations([]postureDeclaration{
		{Profile: "values.synthetic.yaml", IAM: "own"},
	})
	if len(findings) != 1 || !strings.Contains(findings[0], "ТОЛЬКО одна половина") {
		t.Fatalf("односторонняя декларация не найдена: %v", findings)
	}
}

// Законный близнец: обе половины объявили ОДНО И ТО ЖЕ — проба молчит.
func TestInjection_HalvesInAgreementAreSilent(t *testing.T) {
	declaring, findings := judgePostureDeclarations([]postureDeclaration{
		{Profile: "values.synthetic.yaml", IAM: "own", Edge: "own"},
	})
	if len(findings) != 0 {
		t.Fatalf("согласие половин объявлено находкой: %v", findings)
	}
	if declaring != 1 {
		t.Fatalf("перепись не засчитала объявивший профиль: declaring=%d", declaring)
	}
}

// Законный близнец второго рода: профиль, не объявивший НИ ОДНОЙ половины,
// находкой не является и в число объявивших не входит — иначе одно число
// скрыло бы ровно тот случай, ради которого перепись печатает два.
func TestInjection_AProfileDeclaringNothingIsSilentAndNotCounted(t *testing.T) {
	declaring, findings := judgePostureDeclarations([]postureDeclaration{
		{Profile: "values.synthetic.yaml"},
	})
	if len(findings) != 0 || declaring != 0 {
		t.Fatalf("профиль без объявлений: находок %v, объявивших %d", findings, declaring)
	}
}
