// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"regexp"
	"strings"
)

// Судящая часть гейта «отображение имени стенда закрывает ОБЕ половины прогона».
// Вынесена из пробы, чтобы инъекция гоняла ЕЁ, а не свою копию логики.

// HostMappingCensus — объём осмотренного. Половины названы ПОРОЗНЬ намеренно:
// одно суммарное число («отображение объявлено») скрыло бы ровно тот случай,
// ради которого гейт заведён, — половина стоит, половина нет.
type HostMappingCensus struct {
	// EnvReads — сколько раз конфигурация читает ручку отображения.
	EnvReads int
	// BrowserLane — поставлена ли браузерная половина (флаг резолвера Chromium).
	BrowserLane bool
	// RequestLane — поставлена ли половина пути запроса (резолвер Node).
	RequestLane bool
	// SelftestWired — зовёт ли конвейер самопроверку отображения.
	SelftestWired bool
	// WorkflowLines — прочитано строк объявления конвейера: ноль означает
	// «ноль прочитанного», а не «не зовёт».
	WorkflowLines int
}

func (c HostMappingCensus) String() string {
	return fmt.Sprintf(
		"осмотрено: чтений ручки отображения %d; половина браузера %v, половина пути "+
			"запроса %v; самопроверка провязана %v (строк объявления конвейера %d)",
		c.EnvReads, c.BrowserLane, c.RequestLane, c.SelftestWired, c.WorkflowLines)
}

const (
	// HostMappingEnv — ручка, задающая отображение имени стенда.
	HostMappingEnv = "KACHO_CONSOLE_HOST_IP"
	// HostMappingSelftest — самопроверка обеих половин.
	HostMappingSelftest = "scripts/host-mapping-selftest.ts"
)

var (
	// Половина браузера: флаг резолвера Chromium.
	hmBrowserRe = regexp.MustCompile(`--host-resolver-rules`)
	// Половина пути запроса: установка резолвера Node.
	//
	// Ищется ВЫЗОВ, а не слово: имя стоит и в шапке, объясняющей эту самую
	// половину, и гейт по подстроке краснел бы на собственном объяснении
	// (`testing.md` §«Гейт на класс», п. 4).
	hmRequestRe = regexp.MustCompile(`\binstallHostMapping\s*\(`)
	hmEnvRe     = regexp.MustCompile(`process\.env\.` + HostMappingEnv + `\b`)
)

// hmStripComments — снять построчные комментарии и блочные шапки TypeScript.
//
// Без этого «поставлено» неотличимо от «объяснено»: обе половины этого файла
// подробно описаны прозой, и распознаватель по слову нашёл бы их в тексте
// объяснения даже после того, как вызов сняли.
func hmStripComments(src string) string {
	var b strings.Builder
	inBlock := false
	for _, line := range strings.Split(src, "\n") {
		t := line
		for {
			if inBlock {
				if i := strings.Index(t, "*/"); i >= 0 {
					t = t[i+2:]
					inBlock = false
					continue
				}
				t = ""
				break
			}
			if i := strings.Index(t, "/*"); i >= 0 {
				rest := t[i+2:]
				t = t[:i]
				if j := strings.Index(rest, "*/"); j >= 0 {
					t += rest[j+2:]
					continue
				}
				inBlock = true
				break
			}
			break
		}
		if i := strings.Index(t, "//"); i >= 0 {
			t = t[:i]
		}
		b.WriteString(t)
		b.WriteString("\n")
	}
	return b.String()
}

// AuditConsoleHostMapping — сверка обеих половин и провязки самопроверки.
//
// ПРЕДМЕТ (#1750). Флаг `--host-resolver-rules` — флаг Chromium: он закрывает
// запросы БРАУЗЕРА и о пути запроса Node (`page.request.*`, `APIRequestContext`)
// не знает by construction. Пока имя стоит в `/etc/hosts` ранера, разрыв не
// виден никогда — обе половины разрешают имя, каждая своим способом. Проявляется
// он ровно там, ради чего ручка заведена: на стенде, где имя разрешается ТОЛЬКО
// отображением, КАЖДАЯ проба умирает на `ENOTFOUND`, и «не выполнилось»
// подаётся как красное.
func AuditConsoleHostMapping(configSrc, workflowSrc string) ([]string, HostMappingCensus) {
	code := hmStripComments(configSrc)
	cen := HostMappingCensus{
		EnvReads:      len(hmEnvRe.FindAllString(code, -1)),
		BrowserLane:   hmBrowserRe.MatchString(code),
		RequestLane:   hmRequestRe.MatchString(code),
		WorkflowLines: len(strings.Split(workflowSrc, "\n")),
		SelftestWired: strings.Contains(workflowSrc, HostMappingSelftest),
	}

	var findings []string
	if cen.BrowserLane && !cen.RequestLane {
		findings = append(findings, fmt.Sprintf(
			"половина БРАУЗЕРА поставлена (`--host-resolver-rules`), а половина ПУТИ "+
				"ЗАПРОСА — нет (`installHostMapping` не вызывается). На ранере имя стоит "+
				"в /etc/hosts, поэтому разрыв не виден никогда; на стенде, где имя "+
				"разрешается только отображением, КАЖДАЯ проба умрёт на ENOTFOUND, и "+
				"«не выполнилось» будет подано как красное (#1750)"))
	}
	if cen.RequestLane && !cen.BrowserLane {
		findings = append(findings, fmt.Sprintf(
			"половина ПУТИ ЗАПРОСА поставлена, а половина БРАУЗЕРА — нет "+
				"(`--host-resolver-rules` не задаётся): `page.goto` уйдёт к резолверу "+
				"браузера, и пробы не дойдут до продукта (#935)"))
	}
	if (cen.BrowserLane || cen.RequestLane) && cen.EnvReads == 0 {
		findings = append(findings, fmt.Sprintf(
			"отображение ставится, но ручка %s не читается: значение взято откуда-то "+
				"ещё, и половины разойдутся молча", HostMappingEnv))
	}
	if !cen.SelftestWired {
		findings = append(findings, fmt.Sprintf(
			"конвейер не зовёт %s: самопроверка, которую никто не гоняет, доказывает "+
				"безупречно и не мешает ничему", HostMappingSelftest))
	}
	return findings, cen
}
