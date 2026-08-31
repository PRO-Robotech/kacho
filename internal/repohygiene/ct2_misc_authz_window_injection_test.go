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

// ct2WindowFixture — что пишется в синтетическое дерево.
type ct2WindowFixture struct {
	// configDefault — умолчание, объявленное конфигурацией края.
	configDefault string
	// pageBody — тело страницы `gateway/docs/content/getting-started.mdx`.
	pageBody string
	// extraPage — вторая страница, если нужна.
	extraPage string
}

func writeCt2WindowTree(t *testing.T, f ct2WindowFixture) string {
	t.Helper()
	root := t.TempDir()

	mk := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	// Имя ручки стоит ДВАЖДЫ: в комментарии — с чужим числом, в теге — с
	// настоящим. Разбор обязан взять тег; поиск по подстроке взял бы комментарий.
	mk("gateway/internal/config/config.go", `
package config

type Config struct {
	// AuthZCacheTTLSeconds — decision-cache TTL. Прежде здесь стояло 99.
	// Ручка: KACHO_API_GATEWAY_AUTHZ_CACHE_TTL_SECONDS, по умолчанию 99.
	AuthZCacheTTLSeconds int `+"`"+`envconfig:"KACHO_API_GATEWAY_AUTHZ_CACHE_TTL_SECONDS" default:"`+f.configDefault+`"`+"`"+`
}
`)
	mk("gateway/docs/content/getting-started.mdx", f.pageBody)
	if f.extraPage != "" {
		mk("gateway/docs/content/api/operations.mdx", f.extraPage)
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

// Тела страниц: один настоящий дефект и один законный близнец.
const (
	// Дефект, снятый задачей продукта #1645 — дословно то, что стояло на крае.
	ct2PageHalfWindow = `# Начало
:::tip Интервал поллинга
Список ресурсов после успешной мутации имеет смысл перечитывать с небольшим retry:
authz-видимость нового ресурса устанавливается с задержкой (распространение прав до ~2 с).
:::
`
	// Законный близнец: та же страница, называющая свою величину.
	ct2PageNamesKnob = `# Начало
:::tip Интервал поллинга
authz-видимость нового ресурса устанавливается в ограниченном окне; кэш решений края помнит
вердикт ` + "`KACHO_API_GATEWAY_AUTHZ_CACHE_TTL_SECONDS`" + ` секунд (по умолчанию 5).
:::
`
	// Тот же близнец с РАЗОШЕДШЕЙСЯ величиной — вторая половина гейта.
	ct2PageWrongValue = `# Начало
authz-видимость устанавливается в окне; ` + "`KACHO_API_GATEWAY_AUTHZ_CACHE_TTL_SECONDS`" + `
секунд (по умолчанию 30).
:::
`
	// Страница, к окну не относящаяся вовсе, — обязана быть невидима гейту.
	ct2PageUnrelated = `# Операции
Опрашивайте ` + "`Get`" + ` с интервалом 2–5 с. Отменять завершённую операцию нечего.
`
)

// (а) НАСТОЯЩИЙ ДЕФЕКТ: названа половина окна.
func TestCt2WindowInjection_HalfWindowIsAFinding(t *testing.T) {
	root := writeCt2WindowTree(t, ct2WindowFixture{configDefault: "5", pageBody: ct2PageHalfWindow})
	c, findings := ct2WindowRun(t, root)

	if len(findings) != 1 {
		t.Fatalf("половина окна обязана дать РОВНО одну находку, получено %d: %v",
			len(findings), findings)
	}
	for _, want := range []string{"getting-started.mdx", ct2AuthzCacheKnob, "половина окна"} {
		if !strings.Contains(findings[0], want) {
			t.Errorf("находка обязана называть %q, а называет: %s", want, findings[0])
		}
	}
	if c.SpeakingOf != 1 || c.NamingKnob != 0 || c.Agreeing != 0 {
		t.Errorf("перепись обязана показать 1 говорящую, 0 называющих, 0 совпавших; "+
			"получено %d/%d/%d", c.SpeakingOf, c.NamingKnob, c.Agreeing)
	}
}

// (б) ЗАКОННЫЙ БЛИЗНЕЦ обязан молчать; страница вне предмета — быть невидимой.
func TestCt2WindowInjection_LawfulPageIsSilent(t *testing.T) {
	root := writeCt2WindowTree(t, ct2WindowFixture{
		configDefault: "5", pageBody: ct2PageNamesKnob, extraPage: ct2PageUnrelated})
	c, findings := ct2WindowRun(t, root)

	if len(findings) != 0 {
		t.Fatalf("страница, назвавшая свою величину верно, обязана молчать: %v", findings)
	}
	if c.PagesRead != 2 {
		t.Errorf("прочитано обязано быть 2 страницы, прочитано %d", c.PagesRead)
	}
	if len(c.Pages) != 1 || c.Agreeing != 1 {
		t.Errorf("к предмету обязана относиться 1 страница и она обязана совпасть; "+
			"получено относящихся %d, совпавших %d", len(c.Pages), c.Agreeing)
	}
}

// (в) ВТОРАЯ ПОЛОВИНА: величина разошлась с умолчанием. Находка обязана быть
// ОТДЕЛЬНОЙ — слить её с «ручка не названа» значило бы потерять одну из двух.
func TestCt2WindowInjection_DriftedValueIsItsOwnFinding(t *testing.T) {
	root := writeCt2WindowTree(t, ct2WindowFixture{configDefault: "5", pageBody: ct2PageWrongValue})
	c, findings := ct2WindowRun(t, root)

	if len(findings) != 1 {
		t.Fatalf("разошедшаяся величина обязана дать одну находку, получено: %v", findings)
	}
	for _, want := range []string{"названо умолчание 30", "объявляет 5", "config.go"} {
		if !strings.Contains(findings[0], want) {
			t.Errorf("находка обязана называть %q, а называет: %s", want, findings[0])
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
	root := writeCt2WindowTree(t, ct2WindowFixture{configDefault: "5", pageBody: ct2PageNamesKnob})
	c, findings := ct2WindowRun(t, root)
	if c.DefaultValue != "5" {
		t.Fatalf("умолчание обязано быть взято из тега (5), взято %q", c.DefaultValue)
	}
	if len(findings) != 0 {
		t.Fatalf("исправная страница обязана молчать: %v", findings)
	}
}

// (г) ПУСТОЙ ОБХОД отличим от «нарушений нет»: перепись обязана показать нули,
// на которые гейт падает своей проверкой предпосылки.
func TestCt2WindowInjection_EmptyWalkIsDistinguishable(t *testing.T) {
	c, findings := ct2WindowRun(t, t.TempDir())
	if c.PagesRead != 0 || c.DefaultValue != "" || len(c.Pages) != 0 {
		t.Fatalf("на пустом дереве обход обязан быть пуст: страниц %d, умолчание %q, "+
			"относящихся %d", c.PagesRead, c.DefaultValue, len(c.Pages))
	}
	if len(findings) != 0 {
		t.Fatalf("на пустом дереве находок быть не может — их место занимает "+
			"проверка предпосылки: %v", findings)
	}
}
