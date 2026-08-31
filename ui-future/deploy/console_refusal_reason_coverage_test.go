// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// console_refusal_reason_coverage_test.go — ПРИЗНАК, КОТОРЫЙ СЕРВИС ПРОИЗВОДИТ,
// ОБЯЗАН БЫТЬ РАЗОБРАН КОНСОЛЬЮ.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ (#1736)
//
// Клиент различает полосы отказа МАШИННО — по `reason`-токену в
// `google.rpc.ErrorInfo` (`api-conventions.md` §By-lane code-split), — а не
// разбором прозы. Производители это исполняют: каждый из них объявляет токен и
// пишет рядом, ЧТО вызывающему делать в этой полосе. Двое говорят прямо, что
// признак заведён ради потребителя, который «обязан отличить эту полосу машинно,
// а не догадкой по прозе».
//
// Потребителя не было. Консоль ключевалась на ДВУХ токенах из пятнадцати, а
// полосу отказа в правах решала разбором английской фразы `permission denied` —
// в том же файле, где соседний разбор прямо оговаривает, почему так нельзя.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕМ ЭТО ПЛОХО ИМЕННО ТИХО
//
// Тон сообщений — часть контракта и меняется ОСОЗНАННО. Любая такая правка на
// полосе, ключуемой прозой, молча возвращает арендатору внутреннее имя проверки
// вместо объяснения — то есть ровно то состояние, ради устранения которого
// объяснение и написано. Ни одно утверждение о содержимом экрана при этом не
// краснеет: строка есть, она просто другая.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ТРЕБУЕТ ГЕЙТ — РАВЕНСТВО ДВУХ МНОЖЕСТВ, А НЕ ВКЛЮЧЕНИЕ
//
//	произведено ⊆ объявлено   — токен без вердикта консоли есть полоса, о которой
//	                            арендатору показывают, что придётся;
//	объявлено ⊆ произведено   — вердикт без производителя есть послабление,
//	                            пережившее свой предмет (самоистечение).
//
// Вердикт — РЕШЕНИЕ, а не перевод: `passthrough` («текст сервера уже называет
// следующий шаг») столь же законен, как `explain`. Гейт не судит, КАКОЙ вердикт
// верен, — он требует, чтобы вердикт БЫЛ. Иначе новый токен доезжает до
// арендатора необъяснённым, и заметить это может только тот, кто в него упрётся.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПЕРЕПИСЬ ИДЁТ ПО МЕХАНИЗМУ, А НЕ ПО ИМЕНИ ПЕРЕМЕННОЙ
//
// Предикат вида «переменная называется `reason*`» меряет СОГЛАШЕНИЕ ОБ ИМЕНАХ, а
// не предмет: он назвал ЧЕТЫРЕ токена в одном сервисе там, где дерево несёт
// ВОСЕМНАДЦАТЬ в семи. Поэтому здесь распознаются ТРИ законные формы записи, и
// у каждой своя проба в инъекции (`testing.md` §«Гейт на класс», п.7):
//
//	A  Reason: "TOKEN"            — литерал прямо в составном литерале ErrorInfo
//	B  Reason{token: "TOKEN"}     — закрытый словарь полос (`pkg/errors/reason.go`)
//	C  <ident>Reason<ident> = "…" — константа в файле, который строит ErrorInfo
//
// Форма, распознавателю неизвестная, не даёт ни красного, ни зелёного — она
// молчит, и всё записанное в ней оказывается вне наблюдения.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПОТОК ИСКЛЮЧЁН — И ПОЧЕМУ ЭТО НЕ ВЕДОМОСТЬ
//
// У потока ДРУГОЙ потребитель: `stateUnavailable.reason` читает хаб подписки
// (`ui-future/shared/src/lib/subscription/hub.ts`), а не разбор отказа запроса.
// Объявить его токены здесь значило бы завести второе место об одном предмете.
//
// Исключение задано ПУТЁМ ПРОИЗВОДИТЕЛЯ (сервер потока и его край), а не
// перечнем токенов: перечень принял бы под себя и токен запросной полосы,
// случайно названный похоже, — а путь этого сделать не может. Исключённое
// печатается поимённо: «ноль находок» обязано быть отличимо от «ноль
// прочитанного».
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТОТ ГЕЙТ НЕ ВИДИТ — ОХВАТ СЧИТАЕТСЯ ПО ТОКЕНАМ, А НЕ ПО ОТКАЗАМ
//
// Равенство двух множеств выше — утверждение о ТОКЕНАХ: каждый признак, который
// дерево производит, консоль разбирает, и наоборот. Отсюда НЕ следует, что
// каждый отказ несёт признак. Отказ, не несущий его вовсе, в перепись не
// попадает ни одной стороной: он не производит токена, поэтому гейт остаётся
// зелёным, ничего о нём не сказав.
//
// Величина названа числом, чтобы «гейт зелёный» не читалось как «покрытие
// полное». Ревизия — `kacho@5d58b4aa5`; единица счёта — ТОЧКА ЧЕКАНКИ, то есть
// вызов `status.Error`/`Errorf`/`New`/`Newf` с кодом, отличным от OK; обход —
// типизированный разбор 358 пакетов и 1445 не-тестовых файлов:
//
//	903  точки чеканки отказа во всём прод-дереве
//	 15  из них прикрепляют ErrorInfo в той же функции. Это ПОТОЛОК, а не факт:
//	     одна из пятнадцати — запасная ветка Internal в Reason.Errf, признака не
//	     несущая, поэтому несущих НЕ БОЛЬШЕ четырнадцати
//	621  точек лежат на запросной полосе (`**/apps/kacho/api/**`), голых из них 618
//	545  из этих 618 действенны арендатору — без Internal, Canceled,
//	     DeadlineExceeded и Unimplemented, где признак не полагается
//
// Признак прикрепляется НЕ в точке чеканки, а на уровне ПОЛОСЫ — общим
// классификатором сервиса либо перехватчиком (учёт, отказ в правах, полосы
// резолва). Поэтому пятнадцать точек покрывают много больше пятнадцати отказов;
// а 545 — это отказы, у которых полосы нет вовсе, и клиенту остаётся проза.
//
// Отсюда следует, ЧТО ИМЕННО НЕЛЬЗЯ утверждать по зелени этого гейта:
// «отказы объяснимы машинно» — нельзя; «объявленный токен объяснён» — можно.
// Расширение охвата с токенов на отказы требует словаря полос шире нынешнего
// (он ЗАКРЫТ пятью, и закрыт намеренно), то есть решения о контракте, а не
// правки этого файла. Предмет заведён задачей продукта #1756.
package deploy_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// consoleRefusalDictRel — словарь вердиктов консоли относительно корня дерева.
var consoleRefusalDictRel = filepath.Join(
	"ui-future", "shared", "src", "lib", "error-presentation.ts")

// Три законные формы записи токена. Каждая доказана своей пробой в инъекции.
var (
	reasonFormDirect = regexp.MustCompile(`Reason:\s*"([A-Z][A-Z0-9_]+)"`)
	reasonFormTyped  = regexp.MustCompile(`token:\s*"([A-Z][A-Z0-9_]+)"`)
	reasonFormConst  = regexp.MustCompile(`[A-Za-z_]*[Rr]eason[A-Za-z]*\s*=\s*"([A-Z][A-Z0-9_]+)"`)
)

// consoleVerdictEntry — запись словаря консоли: `  TOKEN: {` на двух пробелах.
var consoleVerdictEntry = regexp.MustCompile(`(?m)^  ([A-Z][A-Z0-9_]+):`)

// consoleDictBlock вырезает объявление словаря целиком.
var consoleDictBlock = regexp.MustCompile(`(?s)const REFUSALS: Record<string, RefusalVerdict> = \{(.*?)\n\};`)

// offConsoleProducerPaths — пути, чей потребитель НЕ КОНСОЛЬ.
//
// Исключение задано ПУТЁМ ПРОИЗВОДИТЕЛЯ, а не именем токена: перечень имён
// принял бы под себя и токен запросной полосы, случайно названный похоже, — а
// путь этого сделать не может.
//
// У каждой записи назван СВОЙ потребитель, и это не украшение: «потребитель
// другой» — единственное основание исключения, поэтому запись без названного
// потребителя была бы послаблением без предмета. Потребители печатаются
// переписью поимённо.
//
// Прежде перечень звался «полосой потока» и нёс два пути с одним потребителем.
// Оснований у исключения оказалось два, а не одно: у полосы отзыва края отказ до
// браузера не доезжает ВООБЩЕ — глагол внутренний (ban #6), и его читает Go
// соседней реплики. Объявить такому токену вердикт консоли значило бы завести
// запись, которая не сработает ни при каком входе.
var offConsoleProducerPaths = []struct {
	prefix   string
	consumer string
}{
	{filepath.Join("pkg", "subscription") + string(filepath.Separator),
		"хаб подписки браузера"},
	{filepath.Join("gateway", "internal", "subscriptionstream") + string(filepath.Separator),
		"хаб подписки браузера"},
	{filepath.Join("pkg", "subjectchange") + string(filepath.Separator),
		"читатель отзыва края (Go): глагол внутренний, до браузера отказ не доезжает"},
}

// offConsoleConsumer — чей это токен, если не консоли. Пустая строка означает
// запросную полосу, которую консоль обязана разобрать.
//
// Функция чистая намеренно: доказательство подаёт ей путь строкой, а не трогает
// дерево.
func offConsoleConsumer(rel string) string {
	for _, p := range offConsoleProducerPaths {
		if strings.HasPrefix(rel, p.prefix) {
			return p.consumer
		}
	}
	return ""
}

// censusSkipPaths — не производители: гейты и утилиты держат СВОИ копии токенов,
// чтобы судить о них; засчитав их, перепись удостоверяла бы саму себя.
var censusSkipPaths = []string{
	filepath.Join("internal", "repohygiene") + string(filepath.Separator),
	string(filepath.Separator) + "tools" + string(filepath.Separator),
}

type producedReason struct {
	token string
	where string
	// offConsole — потребитель токена, если это НЕ консоль; пусто у запросной
	// полосы.
	offConsole string
}

// collectProducedReasons собирает токены всех трёх форм ПО СОСТАВУ ИНДЕКСА, а не
// обходом диска: вердикт обязан быть свойством коммита, а под деревом на машине
// сборки лежит и то, чего в репозитории нет. `UnderWithSuffix` к тому же
// отказывается на пустом корпусе — «ноль находок» остаётся отличимо от «ноль
// прочитанного».
func collectProducedReasons(t *testing.T, root string) (found []producedReason, filesRead int) {
	t.Helper()
	files, err := treecorpus.UnderWithSuffix(root, ".go")
	if err != nil {
		t.Fatalf("состав дерева Go под %s: %v — без индекса вердикт недействителен", root, err)
	}
	for _, abs := range files {
		if strings.HasSuffix(abs, "_test.go") {
			continue
		}
		rel, rerr := filepath.Rel(root, abs)
		if rerr != nil {
			continue
		}
		relSlashed := string(filepath.Separator) + rel
		skip := false
		for _, sp := range censusSkipPaths {
			if strings.Contains(relSlashed, sp) {
				skip = true
			}
		}
		if skip {
			continue
		}
		body, rerr := os.ReadFile(abs) // #nosec G304 -- путь пришёл из индекса git этого дерева
		if rerr != nil {
			t.Fatalf("%s: %v", rel, rerr)
		}
		filesRead++

		offConsole := offConsoleConsumer(rel)
		for _, tok := range scanGoSource(string(body)) {
			found = append(found, producedReason{token: tok, where: rel, offConsole: offConsole})
		}
	}
	return found, filesRead
}

// scanGoSource — ТРИ законные формы записи токена в одном тексте.
//
// Функция чистая намеренно: доказательство подаёт ей вход СТРОКОЙ. Инъекция,
// трогающая дерево, испортила бы чужую рабочую копию, а инъекция на копии
// разбора говорила бы о копии, а не о том, что исполняется.
func scanGoSource(text string) []string {
	var out []string
	for _, m := range reasonFormDirect.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	for _, m := range reasonFormTyped.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	// Форма C значима лишь там, где файл действительно строит ErrorInfo: иначе
	// под неё попала бы любая константа с похожим именем.
	if strings.Contains(text, "errdetails.ErrorInfo") {
		for _, m := range reasonFormConst.FindAllStringSubmatch(text, -1) {
			out = append(out, m[1])
		}
	}
	return out
}

// parseVerdictDict — вердикты из текста словаря; ok=false, когда объявления нет.
func parseVerdictDict(text string) (map[string]bool, bool) {
	block := consoleDictBlock.FindStringSubmatch(text)
	if block == nil {
		return nil, false
	}
	out := map[string]bool{}
	for _, m := range consoleVerdictEntry.FindAllStringSubmatch(block[1], -1) {
		out[m[1]] = true
	}
	return out, true
}

// judgeCoverage — РАВЕНСТВО двух множеств, а не включение: слева полоса, о
// которой арендатору показывают, что придётся; справа послабление, пережившее
// свой предмет.
func judgeCoverage(rest map[string][]string, declared map[string]bool) (missing, orphan []string) {
	for tok := range rest {
		if !declared[tok] {
			missing = append(missing, tok)
		}
	}
	for tok := range declared {
		if rest[tok] == nil {
			orphan = append(orphan, tok)
		}
	}
	sort.Strings(missing)
	sort.Strings(orphan)
	return missing, orphan
}

// declaredVerdicts читает словарь консоли.
func declaredVerdicts(t *testing.T, root string) map[string]bool {
	t.Helper()
	path := filepath.Join(root, consoleRefusalDictRel)
	body, err := os.ReadFile(path) // #nosec G304 -- координата словаря объявлена выше
	if err != nil {
		t.Fatalf("словарь вердиктов консоли не читается (%s): %v", consoleRefusalDictRel, err)
	}
	out, ok := parseVerdictDict(string(body))
	if !ok {
		t.Fatalf("в %s не найдено объявление `const REFUSALS: Record<string, RefusalVerdict>` — "+
			"словарь переименован либо ещё не заведён; перепись беспредметна", consoleRefusalDictRel)
	}
	return out
}

func TestConsoleDeclaresEveryProducedRefusalReason(t *testing.T) {
	root := repoRootFromTest(t)

	produced, filesRead := collectProducedReasons(t, root)

	// ПРОВЕРКА СОБСТВЕННОЙ ПРЕДПОСЫЛКИ: пустой обход — не «ноль находок».
	if filesRead == 0 {
		t.Fatalf("прочитано ноль файлов Go под %s — обход беспредметен, вердикт недействителен", root)
	}
	if len(produced) == 0 {
		t.Fatalf("прочитано файлов Go %d, а токенов отказа не найдено НИ ОДНОГО — "+
			"распознаватель перестал видеть предмет; вердикт недействителен", filesRead)
	}

	rest := map[string][]string{}
	offConsole := map[string][]string{}
	byConsumer := map[string][]string{}
	for _, p := range produced {
		dst := rest
		if p.offConsole != "" {
			dst = offConsole
			if !containsWhere(byConsumer[p.offConsole], p.token) {
				byConsumer[p.offConsole] = append(byConsumer[p.offConsole], p.token)
			}
		}
		if !containsWhere(dst[p.token], p.where) {
			dst[p.token] = append(dst[p.token], p.where)
		}
	}

	declared := declaredVerdicts(t, root)

	// Перепись печатается ВСЕГДА: «ноль находок» обязано быть отличимо от
	// «ноль прочитанного».
	t.Logf("перепись: файлов Go прочитано %d · токенов запросной полосы %d · "+
		"токенов вне консоли %d (исключены по пути производителя) · вердиктов консоли %d",
		filesRead, len(rest), len(offConsole), len(declared))
	// Исключённое печатается ПО ПОТРЕБИТЕЛЯМ, а не одним списком: основание у
	// исключения одно на потребителя, и «ноль находок» обязано быть отличимо не
	// только от «ноль прочитанного», но и от «основание перестало действовать».
	for _, consumer := range sortedTokens(byConsumer) {
		sort.Strings(byConsumer[consumer])
		t.Logf("вне консоли, потребитель — %s: %s",
			consumer, strings.Join(byConsumer[consumer], " · "))
	}

	missing, orphan := judgeCoverage(rest, declared)
	for i, tok := range missing {
		missing[i] = tok + " ← " + strings.Join(rest[tok], ", ")
	}
	if len(missing) > 0 {
		t.Errorf("произведено, но консолью НЕ РАЗОБРАНО — %d из %d:\n\t%s\n\n"+
			"Каждый такой токен доезжает до арендатора необъяснённым: сервис назвал полосу "+
			"машинно, консоль полосы не знает и показывает прозу производителя. Вердикт "+
			"обязан быть у КАЖДОГО — `passthrough` («текст сервера уже называет следующий "+
			"шаг») столь же законен, как `explain`; отсутствие вердикта решением не является.",
			len(missing), len(rest), strings.Join(missing, "\n\t"))
	}

	// САМОИСТЕЧЕНИЕ: вердикт, которому нечего разбирать, — находка.
	if len(orphan) > 0 {
		t.Errorf("консоль объявляет вердикт по токену, которого НИКТО НЕ ПРОИЗВОДИТ — %d:\n\t%s\n\n"+
			"Такая запись пережила свой предмет: она выглядит работающей и не сработает "+
			"никогда. Снимите её вместе с производителем либо верните производителя.",
			len(orphan), strings.Join(orphan, "\n\t"))
	}
}

func containsWhere(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func sortedTokens(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
