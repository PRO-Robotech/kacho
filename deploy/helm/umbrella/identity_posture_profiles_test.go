// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_posture_profiles_test.go — ДВЕ ПОЛОВИНЫ одного стенда решают о личности
// одинаково (задача #1125, подфаза Ф4д эпика #896; kacho#2818).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Посадку читали ДВА процесса: служба доступа и край. Профиль, объявивший её
// одной половине и забывший вторую, давал стенд, у которого половины решают о
// личности по-разному, и НИКТО ЭТОГО НЕ РЕШАЛ.
//
// С kaname#363 у службы посадка одна — своя полоса, — и ключа посадки у неё
// нет (kacho#2818): её подчарт отказывает в рендере профилю, который его всё
// ещё объявляет. Разница половин теперь выражается одним способом — край
// объявил не `own`, — и проба спрашивает ровно это, плюс то, что ни один
// профиль не объявляет снятого ключа службы.
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

// kanameLanding — посадка службы доступа на любом стенде: одна, своя полоса
// (kaname#363). Ключа посадки у её подчарта нет (kacho#2818).
const kanameLanding = "own"

// postureDeclaration — что профиль объявил каждой половине.
type postureDeclaration struct {
	Profile string
	IAM     string // config.authn.identityProvider у подчарта службы — снятый ключ
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

// Профиль, объявивший посадку краю, объявляет посадку службы (`own`); снятого
// ключа службы не объявляет ни один профиль.
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

// Базовый профиль КРАЯ объявляет посадку, и она равна посадке службы; базовый
// профиль службы снятого ключа не несёт.
//
// Это «умолчание живёт в профиле, а не в коде»: у края умолчания нет by
// construction, поэтому его базовый профиль обязан назвать значение.
func TestIdentityPostureIsDeclaredByBothSubchartDefaults(t *testing.T) {
	iam := readNested(t, filepath.Join(iamChartDir, "values.yaml"),
		"config", "authn", "identityProvider")
	edge := readNested(t, filepath.Join("..", "..", "..", "gateway", "deploy", "values.yaml"),
		"authn", "identityProvider")

	t.Logf("перепись: базовых профилей подчартов 2 · объявляют посадку %d (служба — снятым ключом %d)",
		boolToInt(iam != "")+boolToInt(edge != ""), boolToInt(iam != ""))

	if iam != "" {
		t.Errorf("базовый профиль службы доступа объявляет снятый ключ посадки (%q) — служба его "+
			"отвергает при любом значении (kaname#363), и её подчарт откажет в рендере", iam)
	}
	if edge == "" {
		t.Error("базовый профиль края посадку не объявляет — умолчания нет ни у него, ни у процесса, " +
			"и всякий стенд, не назвавший её сам, не поднимется")
	}
	if edge != "" && edge != kanameLanding {
		t.Errorf("базовый профиль края объявляет посадку %q, а у службы она одна — %q: "+
			"половины разошлись, и этого никто не решал", edge, kanameLanding)
	}
}

// Законный близнец: профиль, посадку НЕ объявляющий вовсе, находкой не
// считается — он наследует базовое значение края, а оно согласовано с посадкой
// службы проверкой выше. Без этого случая проба краснела бы на каждом узком профиле.
func TestAProfileDeclaringNeitherHalfIsNotAFinding(t *testing.T) {
	decls := readPostureDeclarations(t)
	silent := 0
	for _, d := range decls {
		if len(d.halves()) == 0 {
			silent++
		}
	}
	t.Logf("перепись: профилей %d · не объявляют ни одной половины %d (наследуют базовые значения подчартов)",
		len(decls), silent)
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
		if len(d.halves()) == 0 {
			continue // профиль посадку не объявляет — наследует базовое значение края
		}
		declaring++
		if d.IAM != "" {
			findings = append(findings, d.Profile+": объявлен снятый ключ посадки службы "+
				"(kaname.config.authn.identityProvider="+d.IAM+") — служба отвергает его при любом "+
				"значении (kaname#363), и её подчарт откажет в рендере")
		}
		if d.Edge != "" && d.Edge != kanameLanding {
			findings = append(findings, d.Profile+": половины объявили РАЗНОЕ (gateway="+d.Edge+
				", у службы посадка одна — "+kanameLanding+") — это расхождение, а не выбор")
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
		{Profile: "values.synthetic.yaml", Edge: "external"},
	})
	if len(findings) != 1 || !strings.Contains(findings[0], "РАЗНОЕ") {
		t.Fatalf("расхождение половин не найдено: %v", findings)
	}
}

// Дефект: профиль всё ещё объявляет снятый ключ посадки службы. Обязано
// находиться при любом значении — служба его отвергает.
func TestInjection_OnlyOneHalfDeclaringIsFound(t *testing.T) {
	for _, v := range []string{"own", "external"} {
		_, findings := judgePostureDeclarations([]postureDeclaration{
			{Profile: "values.synthetic.yaml", IAM: v, Edge: "own"},
		})
		if len(findings) != 1 || !strings.Contains(findings[0], "снятый ключ посадки службы") {
			t.Fatalf("снятый ключ службы (%s) не найден: %v", v, findings)
		}
	}
}

// Законный близнец: обе половины объявили ОДНО И ТО ЖЕ — проба молчит.
func TestInjection_HalvesInAgreementAreSilent(t *testing.T) {
	declaring, findings := judgePostureDeclarations([]postureDeclaration{
		{Profile: "values.synthetic.yaml", Edge: "own"},
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
