// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Инъекция для гейта окна authz-видимости — В ОБЕ СТОРОНЫ.
//
// Гейт, чью способность падать не доказали, неотличим от вечно-зелёного.
// Поэтому здесь: (а) НАСТОЯЩИЙ дефект — страница, называющая половину окна, —
// обязан дать находку с координатой; (б) законный близнец обязан молчать;
// (в) разошедшаяся величина обязана краснеть ОТДЕЛЬНО от неназванной ручки —
// это две разные половины, и слить их значило бы потерять одну;
// (г) пустой обход отличим от «нарушений нет».

// ct2WindowFixture — что пишется в синтетическое дерево ОДНОГО владельца.
type ct2WindowFixture struct {
	// owner — индекс в ct2WindowOwners: 0 — край (умолчание числом), 1 —
	// registry (умолчание ДЛИТЕЛЬНОСТЬЮ). Обе формы обязаны разбираться.
	owner int
	// configDefault — умолчание, объявленное конфигурацией владельца.
	configDefault string
	// pageBody — тело первой страницы владельца.
	pageBody string
	// extraPage — вторая страница, если нужна.
	extraPage string
	// noConfig — файла конфигурации нет вовсе.
	noConfig bool
}

func writeCt2WindowTree(t *testing.T, fixtures ...ct2WindowFixture) string {
	t.Helper()
	root := t.TempDir()

	mk := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	for _, f := range fixtures {
		o := ct2WindowOwners[f.owner]
		if !f.noConfig {
			// Имя ручки стоит ДВАЖДЫ: в комментарии — с чужим числом, в теге — с
			// настоящим. Разбор обязан взять тег; поиск по подстроке взял бы
			// комментарий.
			mk(o.ConfigFile, `
package config

type Config struct {
	// Прежде здесь стояло 99. Ручка: `+o.Knob+`, по умолчанию 99.
	AuthZCacheTTL int `+"`"+`envconfig:"`+o.Knob+`" default:"`+f.configDefault+`"`+"`"+`
}
`)
		}
		mk(o.DocsDir+"getting-started.mdx", f.pageBody)
		if f.extraPage != "" {
			mk(o.DocsDir+"api/operations.mdx", f.extraPage)
		}
	}
	return root
}

func ct2WindowRun(t *testing.T, root string) (ct2WindowCensus, []string) {
	t.Helper()
	c, err := collectAuthzWindow(mustSyntheticTree(t, root))
	if err != nil {
		t.Fatalf("обход синтетического дерева: %v", err)
	}
	return c, authzWindowFindings(c)
}

// ct2WindowPageBody собирает тело страницы под ручку конкретного владельца.
func ct2WindowPageBody(knob, stated string) string {
	if knob == "" {
		return "# Начало\n:::tip\nauthz-видимость нового ресурса устанавливается с задержкой\n" +
			"(распространение прав до ~2 с).\n:::\n"
	}
	return "# Начало\n:::tip\nauthz-видимость нового ресурса устанавливается в ограниченном окне;\n" +
		"вердикт живёт `" + knob + "` секунд (по умолчанию " + stated + ").\n:::\n"
}

// Страница, к окну не относящаяся вовсе, — обязана быть невидима гейту.
const ct2PageUnrelated = `# Операции
Опрашивайте ` + "`Get`" + ` с интервалом 2–5 с. Отменять завершённую операцию нечего.
`

// (а) НАСТОЯЩИЙ ДЕФЕКТ: названа половина окна — у КАЖДОГО владельца.
func TestCt2WindowInjection_HalfWindowIsAFinding(t *testing.T) {
	for idx, o := range ct2WindowOwners {
		t.Run(o.Name, func(t *testing.T) {
			root := writeCt2WindowTree(t, ct2WindowFixture{
				owner: idx, configDefault: "5", pageBody: ct2WindowPageBody("", "")})
			c, findings := ct2WindowRun(t, root)

			// Владельцы, которых фикстура не заводила, дают свою находку об
			// отсутствии умолчания; предмет пробы — находка о половине окна.
			var half []string
			for _, f := range findings {
				if strings.Contains(f, "половина окна") {
					half = append(half, f)
				}
			}
			if len(half) != 1 {
				t.Fatalf("половина окна обязана дать РОВНО одну находку, получено %d: %v",
					len(half), findings)
			}
			for _, want := range []string{"getting-started.mdx", o.Knob} {
				if !strings.Contains(half[0], want) {
					t.Errorf("находка обязана называть %q, а называет: %s", want, half[0])
				}
			}
			if c.NamingKnob != 0 || c.Agreeing != 0 {
				t.Errorf("перепись обязана дать 0 называющих и 0 совпавших; получено %d/%d",
					c.NamingKnob, c.Agreeing)
			}
		})
	}
}

// (б) ЗАКОННЫЙ БЛИЗНЕЦ обязан молчать; страница вне предмета — быть невидимой.
// Проверяется на ОБОИХ владельцах сразу: умолчание одного объявлено числом,
// другого — ДЛИТЕЛЬНОСТЬЮ, и обе формы обязаны привестись к секундам.
func TestCt2WindowInjection_LawfulPagesAreSilent(t *testing.T) {
	root := writeCt2WindowTree(t,
		ct2WindowFixture{owner: 0, configDefault: "5",
			pageBody:  ct2WindowPageBody(ct2WindowOwners[0].Knob, "5"),
			extraPage: ct2PageUnrelated},
		ct2WindowFixture{owner: 1, configDefault: "2s",
			pageBody: ct2WindowPageBody(ct2WindowOwners[1].Knob, "2")},
	)
	c, findings := ct2WindowRun(t, root)

	if len(findings) != 0 {
		t.Fatalf("страницы, назвавшие свои величины верно, обязаны молчать: %v", findings)
	}
	if c.Defaults["gateway"] != "5" || c.Defaults["registry"] != "2" {
		t.Fatalf("умолчание обязано приводиться к секундам обеими формами, получено %v",
			c.Defaults)
	}
	if c.PagesRead != 3 || len(c.Pages) != 2 || c.Agreeing != 2 {
		t.Errorf("прочитано 3 страницы, к предмету 2, совпало 2; получено %d/%d/%d",
			c.PagesRead, len(c.Pages), c.Agreeing)
	}
}

// (в) ВТОРАЯ ПОЛОВИНА: величина разошлась с умолчанием. Находка обязана быть
// ОТДЕЛЬНОЙ и называть ВЛАДЕЛЬЦА — иначе на двух владельцах читатель не поймёт,
// чью конфигурацию открывать.
func TestCt2WindowInjection_DriftedValueIsItsOwnFinding(t *testing.T) {
	root := writeCt2WindowTree(t,
		ct2WindowFixture{owner: 1, configDefault: "2s",
			pageBody: ct2WindowPageBody(ct2WindowOwners[1].Knob, "30")})
	c, findings := ct2WindowRun(t, root)

	var drift []string
	for _, f := range findings {
		if strings.Contains(f, "названо умолчание") {
			drift = append(drift, f)
		}
	}
	if len(drift) != 1 {
		t.Fatalf("разошедшаяся величина обязана дать одну находку, получено: %v", findings)
	}
	for _, want := range []string{"названо умолчание 30", "объявляет 2", "registry", "config.go"} {
		if !strings.Contains(drift[0], want) {
			t.Errorf("находка обязана называть %q, а называет: %s", want, drift[0])
		}
	}
	if c.NamingKnob != 1 || c.Agreeing != 0 {
		t.Errorf("ручка названа (1) и величина не совпала (0); получено %d/%d",
			c.NamingKnob, c.Agreeing)
	}
}

// (в2) УМОЛЧАНИЕ БЕРЁТСЯ ИЗ ТЕГА, а не из комментария рядом: в синтетике
// комментарий называет 99, тег — 5. Поиск по подстроке взял бы 99 и объявил бы
// исправную страницу разошедшейся.
func TestCt2WindowInjection_DefaultComesFromTheTagNotTheComment(t *testing.T) {
	root := writeCt2WindowTree(t, ct2WindowFixture{owner: 0, configDefault: "5",
		pageBody: ct2WindowPageBody(ct2WindowOwners[0].Knob, "5")})
	c, _ := ct2WindowRun(t, root)
	if c.Defaults["gateway"] != "5" {
		t.Fatalf("умолчание обязано быть взято из тега (5), взято %q", c.Defaults["gateway"])
	}
}

// (г) ВЛАДЕЛЕЦ БЕЗ КОНФИГУРАЦИИ — слепая зона, названная находкой: без умолчания
// сверять названное не с чем, и молчание тут было бы ложным «всё в порядке».
func TestCt2WindowInjection_OwnerWithoutADefaultIsAFinding(t *testing.T) {
	root := writeCt2WindowTree(t, ct2WindowFixture{owner: 0, noConfig: true,
		pageBody: ct2WindowPageBody(ct2WindowOwners[0].Knob, "5")})
	_, findings := ct2WindowRun(t, root)
	var blind []string
	for _, f := range findings {
		if strings.Contains(f, "не выведено") {
			blind = append(blind, f)
		}
	}
	if len(blind) != len(ct2WindowOwners) {
		t.Fatalf("владелец без умолчания обязан быть назван, получено: %v", findings)
	}
}

// (д) ПУСТОЙ ОБХОД отличим от «нарушений нет»: перепись показывает нули, на
// которые гейт падает своей проверкой предпосылки, а находки называют КАЖДОГО
// владельца, чьё умолчание не выведено.
func TestCt2WindowInjection_EmptyWalkIsDistinguishable(t *testing.T) {
	c, findings := ct2WindowRun(t, t.TempDir())
	if c.PagesRead != 0 || len(c.Pages) != 0 {
		t.Fatalf("на пустом дереве обход обязан быть пуст: страниц %d, относящихся %d",
			c.PagesRead, len(c.Pages))
	}
	if len(findings) != len(ct2WindowOwners) {
		t.Fatalf("на пустом дереве обязана быть находка на каждого владельца (%d), "+
			"получено %d: %v", len(ct2WindowOwners), len(findings), findings)
	}
}
