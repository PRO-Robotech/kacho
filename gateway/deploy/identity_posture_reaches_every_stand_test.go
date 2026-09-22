// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_posture_reaches_every_stand_test.go — ПОСАДКА ЛИЧНОСТИ ОБЪЯВЛЯЕТСЯ
// СТЕНДОМ, а не наследуется от умолчания чарта.
//
// # Чего не хватало, пока этого файла не было
//
// Страж старта умеет сказать «не задано». Он НЕ умеет сказать «задано ПРЕЖНЕЕ,
// а прежнего больше нет»: для него `external` — законное значение, и стенд,
// молча унаследовавший его от умолчания подчарта, поднимается как настроенный.
// Сосед по дереву (deploy/helm/umbrella/identity_posture_profiles_test.go)
// спрашивает другое — «согласны ли ПОЛОВИНЫ одного профиля между собой» — и на
// профиле, не объявившем НИ ОДНОЙ половины, молчит намеренно: по его правилу
// такой профиль наследует базовые значения подчартов.
//
// Вместе эти два молчания дают состояние, которого никто не выбирал: посадку
// объявлял ОДИН профиль из одиннадцати, а стендов, которыми развёртывают, —
// семь. Шесть из семи ехали на умолчании.
//
// # Почему умолчание у ЭТОЙ ручки опаснее лишнего объявления
//
// Посадка разводит три места провязки края: полосу личности, ответ «кто я» и
// ретрансляцию глаголов формы. Значение, унаследованное молча, означает не
// «выбрали прежнее», а «не выбирали»: прежний поставщик со стенда снят, войти
// у него нельзя, ответ «кто я» пуст, глаголы формы не смонтированы — и всё это
// БЕЗ отказа старта, потому что значение для стража законно.
//
// Прежнее значение остаётся законным — поставщик развёрнут не везде и ещё
// живёт, — но ТОЛЬКО объявленное явно.
//
// # Единица счёта здесь — СТЕНД, а не файл профиля
//
// Профили накладываются (deploy/stacks.txt), и требовать объявления от каждого
// файла значило бы требовать повтора: накладка образов о посадке не решает
// ничего. Спрашивается сложенная цепочка: объявил ли посадку ХОТЬ ОДИН её слой.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// postureKeyPath — путь ключа посадки в блоке КРАЯ профиля зонта.
var postureKeyPath = []string{"authn", "identityProvider"}

// iamPostureKeyPath — тот же ключ у второй половины стенда, службы прав.
var iamPostureKeyPath = []string{"config", "authn", "identityProvider"}

// iamChartKeyInUmbrella — имя, под которым зонт адресует значения службе прав.
const iamChartKeyInUmbrella = "kaname"

// declaredAt достаёт строковое значение по пути ключей. Отсутствие ключа и
// нестроковое значение — пустая строка: «не объявлено».
func declaredAt(tree map[string]any, keys ...string) string {
	var cur any = tree
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
	s, _ := cur.(string)
	return strings.TrimSpace(s)
}

// standPosture — что объявила ЦЕПОЧКА стенда каждой половине, и каким слоем.
type standPosture struct {
	Stand    string
	EdgeAt   string // слой, объявивший посадку краю; пусто — не объявил никто
	Edge     string
	IAMAt    string
	IAM      string
	Layers   []string
	EdgeSeen bool // цепочка вообще называет блок края
}

// judgeStandPostures — ТЕЛО пробы, вынесенное отдельно, чтобы инъекция звала
// то же, что исполняется на дереве.
func judgeStandPostures(stands []standPosture) (declaring int, findings []string) {
	for _, s := range stands {
		switch {
		case s.EdgeAt == "" && s.IAMAt == "":
			findings = append(findings, fmt.Sprintf(
				"%s (%s): посадку не объявил НИ ОДИН слой цепочки — стенд едет на "+
					"умолчании подчарта, и это состояние «не выбирали», а не «выбрали "+
					"прежнее»: страж старта примет унаследованное значение как законное",
				s.Stand, strings.Join(s.Layers, " + ")))
		case s.EdgeAt == "":
			findings = append(findings, fmt.Sprintf(
				"%s (%s): посадку объявила ТОЛЬКО служба прав (%s=%q) — край унаследует "+
					"умолчание подчарта, и половины одного стенда разойдутся без чьего-либо "+
					"решения", s.Stand, strings.Join(s.Layers, " + "), s.IAMAt, s.IAM))
		case s.IAMAt == "":
			findings = append(findings, fmt.Sprintf(
				"%s (%s): посадку объявил ТОЛЬКО край (%s=%q) — служба прав унаследует "+
					"умолчание подчарта", s.Stand, strings.Join(s.Layers, " + "), s.EdgeAt, s.Edge))
		default:
			declaring++
			if s.Edge != s.IAM {
				findings = append(findings, fmt.Sprintf(
					"%s: половины объявили РАЗНОЕ (край %s=%q · служба прав %s=%q) — "+
						"это расхождение, а не выбор", s.Stand, s.EdgeAt, s.Edge, s.IAMAt, s.IAM))
			}
		}
	}
	sort.Strings(findings)
	return declaring, findings
}

// readStandPostures складывает цепочку каждого стенда ровно так, как её
// складывает helm: слева направо, последний слой выигрывает.
func readStandPostures(t *testing.T) []standPosture {
	t.Helper()
	var out []standPosture
	for stand, chain := range deployableStacks(t) {
		s := standPosture{Stand: stand, Layers: chain}
		for _, layer := range chain {
			tree := umbrellaValues(t, layer)
			if gw, ok := tree[edgeChartKey].(map[string]any); ok {
				s.EdgeSeen = true
				if v := declaredAt(gw, postureKeyPath...); v != "" {
					s.Edge, s.EdgeAt = v, layer
				}
			}
			if iam, ok := tree[iamChartKeyInUmbrella].(map[string]any); ok {
				if v := declaredAt(iam, iamPostureKeyPath...); v != "" {
					s.IAM, s.IAMAt = v, layer
				}
			}
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Stand < out[j].Stand })
	return out
}

// TestIdentityPostureIsDeclaredByEveryStand — КАЖДЫЙ развёртываемый стенд
// называет посадку сам.
func TestIdentityPostureIsDeclaredByEveryStand(t *testing.T) {
	t.Parallel()
	stands := readStandPostures(t)
	if len(stands) == 0 {
		t.Fatal("таблица стендов пуста — обходить нечего, и молчание пробы " +
			"не является утверждением о дереве")
	}

	declaring, findings := judgeStandPostures(stands)
	t.Logf("перепись: стендов осмотрено %d · объявляют посадку ОБЕИМ половинам %d · находок %d",
		len(stands), declaring, len(findings))
	for _, s := range stands {
		t.Logf("  %s [%s]: край %q (%s) · служба прав %q (%s)",
			s.Stand, strings.Join(s.Layers, " + "), s.Edge, s.EdgeAt, s.IAM, s.IAMAt)
	}
	for _, f := range findings {
		t.Error(f)
	}
}

// TestEdgeChartDefaultDoesNotDecideThePostureForTheStand — у ЧАРТА КРАЯ
// умолчания посадки нет.
//
// Вторая половина того же свойства, и без неё первая ничего не стоит: пока
// умолчание в базовом профиле чарта живо, состояние «не объявлено» не наступает
// НИ НА ОДНОМ стенде, и отказ старта, только что возвращённый краю, не
// сработает ни разу — ровно тот случай, когда страж мёртв при живом виде.
func TestEdgeChartDefaultDoesNotDecideThePostureForTheStand(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("values.yaml"))
	if err != nil {
		t.Fatalf("базовый профиль чарта края не прочитан (%v) — предпосылка исчезла", err)
	}
	var tree map[string]any
	if err := yaml.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("базовый профиль чарта края не разобран: %v", err)
	}
	if len(tree) == 0 {
		t.Fatal("базовый профиль чарта края пуст — разбор прочитал не то, " +
			"и «умолчания нет» означало бы «не читали»")
	}
	authn, hasAuthn := tree["authn"].(map[string]any)
	t.Logf("перепись: ключей верхнего уровня в базовом профиле чарта края %d; "+
		"блок authn объявлен: %v", len(tree), hasAuthn)
	if !hasAuthn {
		return
	}
	if v, declared := authn["identityProvider"]; declared {
		t.Errorf("базовый профиль чарта края объявляет посадку (%v) — умолчание решает "+
			"за стенд: состояние «не объявлено» не наступит ни на одном профиле, и отказ "+
			"старта по этой ручке не сработает НИ РАЗУ", v)
	}
}
