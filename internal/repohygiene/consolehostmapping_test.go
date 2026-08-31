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

// Отображение имени стенда закрывает ОБЕ половины прогона проб консоли.
//
// # Предмет (задача `PRO-Robotech/kacho#1750`)
//
// Шапка ручки утверждала, что «снят САМ ПРЕДМЕТ: адрес задаётся браузеру
// отображением, поэтому его резолвер в разрешении имени не участвует вовсе».
// Для браузера это верно; для ПУТИ ЗАПРОСА — нет. `page.request.*`
// (`APIRequestContext`) исполняется в процессе Node, а `--host-resolver-rules`
// — флаг Chromium, и о втором клиенте он не знает by construction. Заявление
// было шире сделанного.
//
// # Почему это не видно ни одним прогоном ранера
//
// На ранере имя стоит в `/etc/hosts` — обе половины разрешают его, каждая своим
// способом, и разрыва нет. Проявляется он ровно там, ради чего ручка заведена:
// на стенде, где имя разрешается ТОЛЬКО отображением, каждая проба, зовущая
// `registerAndSignIn`, умирает на `getaddrinfo ENOTFOUND`, то есть ни одна не
// доходит до продукта, и «не выполнилось» подаётся как красное — тот самый
// класс, из которого выведены #935 и #985.
//
// # Что судит ЭТОТ гейт, а что — самопроверка рядом
//
// Гейт судит ПРОВЯЗКУ: обе половины объявлены, обе берут одну ручку, и
// конвейер зовёт самопроверку. ПОВЕДЕНИЕ половин он не судит вовсе — это
// предмет `ui-future/e2e/scripts/host-mapping-selftest.ts`, который поднимает
// настоящий сервер и проверяет доставку. Разделение намеренное: провязку видно
// в дереве, доставку — только исполнением.
//
// Слово ищется в КОДЕ, а не в тексте: обе половины подробно объяснены прозой
// в той же шапке, и гейт по подстроке краснел бы на собственном объяснении
// (`testing.md` §«Гейт на класс», п. 4).
func TestConsoleProbeHostMappingCoversBothLanes(t *testing.T) {
	t.Parallel()

	root := repoRootFor(t)

	cfgPath := filepath.Join(root, "ui-future", "e2e", "playwright.config.ts")
	cfg, err := os.ReadFile(cfgPath) // #nosec G304 -- путь собран из корня репозитория
	require.NoError(t, err, "чтение объявления проб %s", cfgPath)

	wfPath := filepath.Join(root, ".github", "workflows", "console-e2e.yml")
	wf, err := os.ReadFile(wfPath) // #nosec G304 -- путь собран из корня репозитория
	require.NoError(t, err, "чтение объявления конвейера %s", wfPath)

	findings, cen := repohygiene.AuditConsoleHostMapping(string(cfg), string(wf))

	// Предпосылки: ноль здесь означает «ноль прочитанного», а не «ноль находок».
	require.NotZero(t, cen.WorkflowLines,
		"объявление конвейера прочитано пустым — гейт объявил бы находку, ничего не осмотрев")
	require.Truef(t, cen.BrowserLane || cen.RequestLane,
		"в объявлении проб не найдено НИ ОДНОЙ половины отображения: сменилась форма "+
			"записи, и гейт сверял бы пустое с пустым. %s", cen.String())

	// Самопроверка обязана СУЩЕСТВОВАТЬ, иначе провязка ведёт в пустоту — та же
	// форма без содержания, что и провязанный, но отсутствующий хук.
	selftest := filepath.Join(root, "ui-future", "e2e", repohygiene.HostMappingSelftest)
	require.FileExistsf(t, selftest,
		"конвейер зовёт %s, а файла нет: провязка в пустоту неотличима от работающей "+
			"проверки, пока не спросишь конвейер", repohygiene.HostMappingSelftest)

	t.Log(cen.String())

	require.Emptyf(t, findings, "отображение имени стенда неполно:\n  %s\n\n%s",
		strings.Join(findings, "\n  "), cen.String())
}
