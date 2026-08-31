// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/internal/repohygiene"
)

// Доказательство того, что гейт отображения имени СПОСОБЕН упасть — и что
// падает он на существе, а не на форме.
//
// ФИКСТУРА ПРИВЯЗАНА К ДЕРЕВУ: обе стороны берутся из закоммиченных файлов,
// дефект в них ВОЗВРАЩАЕТСЯ, и каждая проба сперва утверждает, что предмет её
// правки в дереве ЕСТЬ. Синтетика доказывала бы свойство вчерашнего дерева.
//
// ПРОГОНОВ ТРИ (`testing.md` §«Гейт на класс», п. 2в): без третьего молчание на
// контроле неотличимо от молчания мёртвого.

func hostMappingSources(t *testing.T) (cfg, wf string) {
	t.Helper()
	root := repoRootFor(t)
	c, err := os.ReadFile(filepath.Join(root, "ui-future", "e2e", "playwright.config.ts")) // #nosec G304
	require.NoError(t, err)
	w, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "console-e2e.yml")) // #nosec G304
	require.NoError(t, err)
	return string(c), string(w)
}

// Прогон 1 — КОНТРОЛЬ: дерево как есть, гейт молчит, обе половины видны.
func TestCHM_Run1_ControlIsSilentAndBothLanesSeen(t *testing.T) {
	t.Parallel()
	cfg, wf := hostMappingSources(t)

	f, cen := repohygiene.AuditConsoleHostMapping(cfg, wf)
	require.Empty(t, f, "гейт покраснел на дереве как есть — контроль опровергнут")
	require.True(t, cen.BrowserLane && cen.RequestLane, "на контроле обе половины обязаны быть видны: %s", cen)
	require.True(t, cen.SelftestWired, "на контроле самопроверка обязана быть провязана: %s", cen)
}

// Прогон 2 — СНЯТА НОВАЯ половина (путь запроса). Это ровно то состояние, в
// котором дерево было до #1750: браузер отображён, путь запроса — нет.
func TestCHM_Run2_DroppingTheRequestLaneIsAFinding(t *testing.T) {
	t.Parallel()
	cfg, wf := hostMappingSources(t)

	const call = "installHostMapping(host, HOST_IP)"
	require.Containsf(t, cfg, call,
		"фикстура беспредметна: вызова %q в объявлении проб нет — форма сменилась, "+
			"и проба доказывала бы свойство вчерашнего дерева", call)

	f, cen := repohygiene.AuditConsoleHostMapping(strings.Replace(cfg, call, "null", 1), wf)
	require.NotEmpty(t, f, "снятая половина пути запроса не дала находки — гейт не способен упасть на предмете #1750")
	require.Len(t, f, 1, "снятие ОДНОЙ половины дало %d находок — инъекция роняет не только проверяемое: %v", len(f), f)
	require.Contains(t, f[0], "половина ПУТИ ЗАПРОСА — нет")
	require.False(t, cen.RequestLane, "перепись не заметила снятой половины: %s", cen)
	require.True(t, cen.BrowserLane, "снята не та половина — инъекция задела соседнюю: %s", cen)
}

// Прогон 3 — СНЯТА СТАРАЯ половина (браузер). Обязателен: без него молчание
// существующего контроля неотличимо от молчания мёртвого.
func TestCHM_Run3_DroppingTheBrowserLaneIsAFinding(t *testing.T) {
	t.Parallel()
	cfg, wf := hostMappingSources(t)

	const flag = "`--host-resolver-rules=${rules.join(\",\")}`"
	require.Containsf(t, cfg, flag, "фикстура беспредметна: флага резолвера браузера в объявлении нет")

	f, cen := repohygiene.AuditConsoleHostMapping(strings.Replace(cfg, flag, "``", 1), wf)
	require.NotEmpty(t, f, "снятая половина браузера не дала находки — существующий контроль мёртв")
	require.Len(t, f, 1, "снятие ОДНОЙ половины дало %d находок — инъекция роняет не только проверяемое: %v", len(f), f)
	require.Contains(t, f[0], "половина БРАУЗЕРА — нет")
	require.False(t, cen.BrowserLane, "перепись не заметила снятой половины: %s", cen)
	require.True(t, cen.RequestLane, "снята не та половина — инъекция задела соседнюю: %s", cen)
}

// Прогон 4 — снята ПРОВЯЗКА самопроверки. Половины на месте, а доказывать их
// поведение некому: самопроверка, которую никто не зовёт, не мешает ничему.
func TestCHM_Run4_UnwiringTheSelftestIsAFinding(t *testing.T) {
	t.Parallel()
	cfg, wf := hostMappingSources(t)

	require.Containsf(t, wf, repohygiene.HostMappingSelftest,
		"фикстура беспредметна: конвейер не зовёт %s", repohygiene.HostMappingSelftest)

	f, _ := repohygiene.AuditConsoleHostMapping(cfg,
		strings.ReplaceAll(wf, repohygiene.HostMappingSelftest, "scripts/nothing.ts"))
	require.Len(t, f, 1, "снятие провязки дало %d находок вместо одной: %v", len(f), f)
	require.Contains(t, f[0], "конвейер не зовёт")
}

// Законный близнец: ПРОЗА о половинах производителем не является. Обе они
// подробно объяснены в шапке того же файла, и гейт по подстроке остался бы
// зелёным на снятом вызове — краснея при этом на собственном объяснении.
func TestCHM_ProseAboutALaneIsNotTheLane(t *testing.T) {
	t.Parallel()

	const onlyProse = `
/**
 * installHostMapping закрывает путь запроса, а --host-resolver-rules — браузер.
 */
const args = ["--host-resolver-rules=MAP a b"];
// installHostMapping(host, ip) — так это выглядело бы, если бы вызывалось.
const ip = process.env.KACHO_CONSOLE_HOST_IP;
void ip;
`
	f, cen := repohygiene.AuditConsoleHostMapping(onlyProse, "scripts/host-mapping-selftest.ts")
	require.False(t, cen.RequestLane,
		"вызов найден в КОММЕНТАРИИ и в шапке: распознаватель судит текст, а не код, "+
			"и остался бы зелёным на снятом вызове")
	require.True(t, cen.BrowserLane, "половина браузера в коде есть и обязана быть видна")
	require.NotEmpty(t, f, "половина пути запроса отсутствует — обязана быть находка")
}
