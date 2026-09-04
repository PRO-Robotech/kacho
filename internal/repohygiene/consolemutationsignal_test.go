// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// consolemutationsignal_test.go — гейт: мутирующее действие консоли проходит
// через единый механизм сигнала об исходе.
//
// # Предмет
//
// Решение владельца 2026-08-15: «каждое действие сигнализирует о выполнении или
// фейле», форма сообщений единая. Наблюдение, из которого выведено: на форме
// создания региона нажатие «создать» не показывало ничего, при том что край
// отвечал отказом.
//
// Механизм — `ui-future/shared/src/lib/use-signalled-mutation.ts`: он разбирает
// ответ, опрашивает операцию и сообщает про ВСЕ ТРИ исхода одной формой. Исходов
// три, а не два, потому что мутации Kachō асинхронные: `done=true` вправе нести
// отказ, и «создана», выданная по факту приёма, была бы ложью.
//
// # Почему гейт, а не проба на каждую форму
//
// Проба на форму держит ту форму, которая есть. Заведут новую — она не покрыта
// ничем, и молчание вернётся тем же способом, каким пришло. Свойство «каждое»
// — свойство ДЕРЕВА, и требовать его надо от кода, которого ещё нет.
//
// # Что гейт держит
//
//	ПРИЗНАК    вызов мутирующего метода клиента края (`api.create` / `api.update`
//	           / `api.delete` / `api.post` / `api.action`) в исполняемой части
//	           не-тестового исходника консоли.
//	ТРЕБОВАНИЕ файл с таким вызовом ссылается на единый механизм — либо стоит в
//	           ведомости незаведённых (ниже) с назначенной задачей.
//	ВЕДОМОСТЬ  самоистекает: запись, чей файл больше не мутирует ИЛИ уже
//	           заведён на механизм, — НАХОДКА. Иначе ведомость переживёт свой
//	           предмет и станет описанием несуществующего долга.
//	ПЕРЕПИСЬ   файлов прочитано · мест мутации найдено · файлов заведено ·
//	           длина ведомости. «Ноль находок» обязано быть отличимо от «ноль
//	           прочитанного».
//
// # Чего гейт НЕ держит, и это названо, а не умолчано
//
//  1. Мутацию, доезжающую до края через ДОМЕННУЮ обёртку (`saKeysApi.issue`,
//     `instancesApi.attachDisk`, `clusterApi.grantAdmin` — тонкие однострочники
//     в `src/api/`). Признак ловит прямой вызов клиента; обёртка прячет его от
//     разбора по имени. Расширять признак списком имён обёрток — значит
//     завести второй словарь, который разойдётся с первым молча. Это записано
//     задачей, а не выдано за покрытое.
//  2. Что механизм провязан ПРАВИЛЬНО (верный глагол, верное подлежащее). Это
//     держат пробы самого механизма (`mutation-signal.test.ts`,
//     `use-signalled-mutation.test.tsx`), а не обход дерева.
//  3. Что сигнал ВИДЕН. Очередь тостов — модульный синглтон, и без
//     примонтированного показа сигнал оседает молча; ровно так и выглядела
//     находка владельца. Это держит отдельная проба
//     `shared/src/components/molecules/Toaster/Toaster.mounted.test.ts`.
//
// # Способность упасть
//
// Доказана инъекцией в обе стороны — `consolemutationsignal_injection_test.go`:
// мутация без механизма краснеет с координатой, законный близнец (тот же вызов
// в файле с механизмом) молчит, устаревшая запись ведомости краснеет отдельно.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// consoleUIRoot — корень консоли относительно корня репозитория.
const consoleUIRoot = "ui-future"

// consoleMutationMechanism — единый механизм. Файл, который его упоминает,
// считается заведённым: сузить до «зовёт хук» нельзя, потому что часть мест
// пользуется его чистой частью (`mutationFailureText`) для отказа собственной
// проверки — это тот же сигнал той же формы.
var consoleMutationMechanismMarkers = []string{
	"use-signalled-mutation",
	"useSignalledMutation",
	"mutation-signal",
}

// consoleMutationCall — признак мутирующего действия: вызов мутирующего метода
// клиента края. `api.get`/`api.list` сюда не входят — они ничего не меняют.
//
// Проверяется по МАСКЕ (см. consoleProbeScan), поэтому то же сочетание внутри
// строкового литерала или комментария находкой не станет: гейт, краснеющий на
// собственном объяснении, снимут как непонятный.
var consoleMutationCallRe = regexp.MustCompile(`\bapi\.(create|update|delete|post|action)\s*[(<]`)

// consoleMutationTransportDir — каталог транспорта: сам клиент края и тонкие
// доменные однострочники над ним. Они переносят запрос, а не решают, сообщать ли
// о его исходе; решает вызывающий. Исключение по РОЛИ каталога, а не по списку
// имён, поэтому новый однострочник не требует правки гейта.
const consoleMutationTransportDir = "/src/api/"

// consoleMutationLedger — места, которые ещё не заведены на механизм.
//
// Это НЕ прощение и не «список известного красного»: каждая запись — предмет
// задачи #483, и её единственное назначение — сделать долг СЧЁТНЫМ. Ведомость
// самоистекает (см. TestConsoleMutationLedgerHasSubject): запись, которой нечего
// исключать, роняет гейт.
//
// Замер на ревизии заведения: мест действия 42, из них заведено 3, в ведомости
// 39. Предикат повторяемый — см. шапку теста.
//
// Самоистечение уже сработало и сняло 4 записи: диалог формы ресурса
// (`ResourceFormDialog` у compute · nlb · registry · storage) не рендерился ни в
// одном модуле и снят как мёртвый де-форком консоли (#405, коммиты 48c29ca2 и
// 9f957e99). Мутации не переехали — их не стало вместе с кодом, который их звал.
// Ровно тот исход, ради которого ведомость обязана истекать: долг закрылся не
// работой по нему, и заметил это гейт, а не читатель.
var consoleMutationLedger = []string{
	"iam/src/components/organisms/iam/AccessBindingCreateForm/AccessBindingCreateForm.tsx",
	"iam/src/pages/iam/AccessPage/AccessPage.tsx",
	"nlb/src/components/organisms/TargetsManager/TargetsManager.tsx",
	"shared/src/components/organisms/CidrTableSection/CidrTableSection.tsx",
	"shared/src/components/organisms/InlineAddressPoolCreateForm/InlineAddressPoolCreateForm.tsx",
	"shared/src/components/organisms/InlineAddressPoolEditForm/InlineAddressPoolEditForm.tsx",
	"shared/src/components/organisms/InlineNetworkInterfaceCreateForm/InlineNetworkInterfaceCreateForm.tsx",
	"shared/src/components/organisms/InlineNetworkInterfaceEditForm/InlineNetworkInterfaceEditForm.tsx",
	"shared/src/components/organisms/InlineResourceCreateForm/InlineResourceCreateForm.tsx",
	"shared/src/components/organisms/InlineResourceEditForm/InlineResourceEditForm.tsx",
	"shared/src/components/organisms/InlineSecurityGroupEditForm/InlineSecurityGroupEditForm.tsx",
	"shared/src/components/organisms/InlineSubnetCreateForm/InlineSubnetCreateForm.tsx",
	"shared/src/components/organisms/InlineSubnetEditForm/InlineSubnetEditForm.tsx",
	"shared/src/components/organisms/ResourceDetailPage/ResourceDetailPage.tsx",
	"shared/src/components/organisms/RoutesPanel/RoutesPanel.tsx",
	"shared/src/components/organisms/SgRulesPanel/SgRulesPanel.tsx",
	"shared/src/components/organisms/iam/IamCommon/IamCommon.tsx",
	"shared/src/pages/InstanceDetailPage.tsx",
	"shared/src/pages/system/LimitsPage.tsx",
}

// consoleMutationSite — место мутации: координата и текст вызова.
type consoleMutationSite struct {
	File string // путь от корня консоли
	Line int
	Text string
}

func (s consoleMutationSite) String() string {
	return consoleUIRoot + "/" + s.File + ":" + strconv.Itoa(s.Line) + " " + s.Text
}

// consoleMutationCensus — объём осмотренного.
type consoleMutationCensus struct {
	Files     int // не-тестовых исходников консоли прочитано
	Sites     int // мест мутации найдено
	ActFiles  int // файлов-мест действия (без транспорта)
	Wired     int // из них заведено на механизм
	Ledgered  int // из них в ведомости
	Transport int // файлов транспорта пропущено
}

// consoleMutationScan обходит дерево консоли и раскладывает файлы по трём
// корзинам: заведено · в ведомости · находка.
func consoleMutationScan(t *testing.T) (findings []consoleMutationSite, stale []string, c consoleMutationCensus) {
	t.Helper()

	root := repoRoot(t)
	uiRoot := filepath.Join(root, consoleUIRoot)
	if _, err := os.Stat(uiRoot); err != nil {
		t.Fatalf("предпосылка гейта не выполняется: каталога консоли %s нет (%v)", consoleUIRoot, err)
	}

	ledger := make(map[string]bool, len(consoleMutationLedger))
	for _, p := range consoleMutationLedger {
		ledger[p] = true
	}
	// Файлы, ради которых ведомость заведена, — чтобы поймать записи без предмета.
	ledgerHit := make(map[string]bool, len(consoleMutationLedger))

	// Состав дерева берётся у ИНДЕКСА, а не с диска. Под `ui-future/` на всякой
	// машине, где собирали фронтенд, лежат `node_modules` и сборочные каталоги;
	// обход диска сделал бы вердикт свойством рабочего каталога, а не коммита.
	// Требование и его причина — `pkg/treecorpus`.
	tracked, err := treecorpus.UnderWithSuffix(uiRoot, ".ts", ".tsx")
	if err != nil {
		t.Fatalf("состав дерева консоли взять неоткуда: %v", err)
	}

	for _, path := range tracked {
		name := filepath.Base(path)
		if strings.Contains(name, ".test.") || strings.HasSuffix(name, ".d.ts") {
			continue
		}
		rel, rerr := filepath.Rel(uiRoot, path)
		if rerr != nil {
			t.Fatalf("относительный путь для %s: %v", path, rerr)
		}
		rel = filepath.ToSlash(rel)
		// Только исходники приложений: `/src/` отсекает конфиги и скрипты сборки.
		if !strings.Contains("/"+rel, "/src/") {
			continue
		}
		// Сквозные пробы — отдельная семья со своим вердиктом; мутируют они стенд,
		// а не показывают его пользователю.
		if strings.HasPrefix(rel, "e2e/") {
			continue
		}

		src, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("чтение %s: %v", rel, rerr)
		}
		c.Files++

		// Разбор — по маске: вызов внутри строки или комментария не считается.
		mask, _ := consoleProbeScan(string(src))
		locs := consoleMutationCallRe.FindAllIndex(mask, -1)
		if len(locs) == 0 {
			continue
		}
		c.Sites += len(locs)

		if strings.Contains("/"+rel, consoleMutationTransportDir) {
			c.Transport++
			continue
		}
		c.ActFiles++

		text := string(src)
		wired := false
		for _, m := range consoleMutationMechanismMarkers {
			if strings.Contains(text, m) {
				wired = true
				break
			}
		}
		if wired {
			c.Wired++
			if ledger[rel] {
				// Заведён — и всё ещё числится долгом. Запись пережила предмет.
				ledgerHit[rel] = true
				stale = append(stale, rel+" — заведён на механизм, но остался в ведомости")
			}
			continue
		}
		if ledger[rel] {
			c.Ledgered++
			ledgerHit[rel] = true
			continue
		}
		for _, loc := range locs {
			findings = append(findings, consoleMutationSite{
				File: rel,
				Line: 1 + strings.Count(string(src[:loc[0]]), "\n"),
				Text: strings.TrimSpace(string(mask[loc[0]:loc[1]])),
			})
		}
	}

	// Записи ведомости, которым нечего исключать: файла нет либо он больше не мутирует.
	for _, p := range consoleMutationLedger {
		if !ledgerHit[p] {
			stale = append(stale, p+" — мутации в файле нет (снят или переписан); запись без предмета")
		}
	}
	sort.Strings(stale)
	return findings, stale, c
}

func TestConsoleMutationGoesThroughTheSignalMechanism(t *testing.T) {
	findings, _, c := consoleMutationScan(t)

	t.Logf("осмотрено: исходников %d · мест мутации %d · файлов-действий %d "+
		"(заведено %d, в ведомости %d) · транспорта пропущено %d · ведомость %d",
		c.Files, c.Sites, c.ActFiles, c.Wired, c.Ledgered, c.Transport, len(consoleMutationLedger))

	// Предпосылка: разбор обязан НАХОДИТЬ предмет. Ноль мест при непустом дереве —
	// поломка разбора, а не чистота консоли, и молчаливый успех тут был бы худшим
	// из исходов: гейт объявил бы зелёным дерево, которого не прочитал.
	if c.Files == 0 {
		t.Fatalf("предпосылка не выполняется: не прочитано ни одного исходника консоли")
	}
	if c.Sites == 0 {
		t.Fatalf("предпосылка не выполняется: прочитано %d исходников, мест мутации найдено 0 — "+
			"признак %q перестал ловить предмет", c.Files, consoleMutationCallRe)
	}
	if c.Wired == 0 {
		t.Fatalf("предпосылка не выполняется: ни один файл не заведён на механизм — "+
			"либо механизм переименован, либо маркеры %v устарели", consoleMutationMechanismMarkers)
	}

	if len(findings) > 0 {
		var b strings.Builder
		b.WriteString("мутирующее действие не проходит через единый механизм сигнала об исходе.\n")
		b.WriteString("Каждое действие обязано сообщать о выполнении или об отказе — решение владельца 2026-08-15.\n")
		b.WriteString("Заведите место на shared/src/lib/use-signalled-mutation.ts либо внесите его в\n")
		b.WriteString("consoleMutationLedger с задачей, если оно закрывается отдельным изменением.\n\n")
		for _, f := range findings {
			b.WriteString("  " + f.String() + "\n")
		}
		t.Fatal(b.String())
	}
}

// TestConsoleMutationLedgerHasSubject — ведомость истекает сама.
//
// Отдельным тестом, а не веткой предыдущего: у них разные предметы, и слитые в
// один они дали бы один вердикт на два разных вопроса. Здесь спрашивается не
// «все ли заведены», а «не описывает ли ведомость несуществующий долг».
func TestConsoleMutationLedgerHasSubject(t *testing.T) {
	_, stale, c := consoleMutationScan(t)

	t.Logf("ведомость: записей %d · из них с живым предметом %d", len(consoleMutationLedger), c.Ledgered)

	// Пустая ведомость — ЦЕЛЬ, а не поломка: падение на ней толкало бы держать
	// запись ради зелёного (`testing.md` §«Проба не имеет права падать на
	// достижении своей цели»).
	if len(consoleMutationLedger) == 0 {
		t.Log("ведомость пуста — все мутирующие места заведены на механизм")
		return
	}

	if len(stale) > 0 {
		var b strings.Builder
		b.WriteString("запись ведомости пережила свой предмет — снимите её:\n\n")
		for _, s := range stale {
			b.WriteString("  " + s + "\n")
		}
		t.Fatal(b.String())
	}

	// Дубли делают счёт долга неверным в меньшую сторону — то есть в ту, где это
	// выглядит как прогресс.
	seen := make(map[string]bool, len(consoleMutationLedger))
	for _, p := range consoleMutationLedger {
		if seen[p] {
			t.Errorf("запись ведомости продублирована: %s", p)
		}
		seen[p] = true
	}
	if !sort.StringsAreSorted(consoleMutationLedger) {
		t.Error("ведомость не отсортирована — в таком виде её правки конфликтуют без нужды")
	}
}
