// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// responsecodeformparity_test.go — «какие коды шаг объявляет приемлемым исходом»
// разбирается ОДИНАКОВО генератором и гейтом дерева (`PRO-Robotech/kacho#1278`).
//
// # Предмет
//
// Один предмет разбирают ДВА механизма:
//
//   - `_accepted_http_codes` в `scripts/gen.py` каждого набора — решает,
//     оборачивать ли шаг ограниченным ретраем (`retry_until_authorized`);
//   - разбор `acceptedResponseCodes` в `nestedquotasuitereclaim_test.go` — решает,
//     создал ли шаг ребёнка под вложенным потолком.
//
// Пока они расходятся, фраза «шаг утверждает отказ» означает у них РАЗНОЕ. Это
// уже стоило разбора: два шага, писавшие код ответа с переносом строки, генератор
// считал утверждающими отказ, а гейт — успешным созданием, и гейт объявил утечку
// там, где её нет. Расхождение было и в обратную сторону: гейт читал `to.equal(`,
// генератор — нет, и потому 724 вхождения этой формы были ему невидимы; из-за
// этого четыре шага, УТВЕРЖДАЮЩИЕ 403, оборачивались ретраем ПО 403 — ровно то,
// что `testing.md` запрещает («НЕ оборачивать: negatives, cross-account deny»).
//
// # Чем это закрыто
//
// Свести две реализации в одну нельзя — языки разные. Свести можно ОЖИДАНИЕ:
// `testdata/response_code_assertion_forms.json` объявляет по каждой форме
// утверждения набор кодов, и этим перечнем судятся ОБЕ стороны. Тогда
// расхождение нельзя внести молча: краснеет та сторона, которая разошлась, и
// краснеет она с именем формы.
//
// Проверка читает ИСХОД обеих сторон — фактический результат разбора, — а не
// текст их регулярок: гейт, сверяющий тексты, доказывал бы совпадение написания,
// а не поведения.
package repohygiene_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// responseCodeForm — одна форма утверждения и объявленный для неё набор исходов.
type responseCodeForm struct {
	ID       string   `json:"id"`
	Why      string   `json:"why"`
	Lines    []string `json:"lines"`
	Accepted []int    `json:"accepted"`
}

type responseCodeCorpus struct {
	Forms []responseCodeForm `json:"forms"`
}

const responseCodeCorpusRel = "internal/repohygiene/testdata/response_code_assertion_forms.json"

func loadResponseCodeCorpus(t *testing.T, root string) responseCodeCorpus {
	t.Helper()

	b, err := os.ReadFile(filepath.Join(root, responseCodeCorpusRel))
	require.NoErrorf(t, err,
		"общий источник ожиданий %s не прочитан: без него обе стороны судятся сами "+
			"собой, и расхождение снова становится невносимым молча", responseCodeCorpusRel)

	var c responseCodeCorpus
	require.NoError(t, json.Unmarshal(b, &c), "разбор %s", responseCodeCorpusRel)

	// Предпосылка: пустой корпус сделал бы обе проверки ниже тождественно
	// истинными — «ноль находок» стало бы «ноль прочитанного».
	require.NotEmpty(t, c.Forms, "корпус форм пуст — судить нечем")
	seen := map[string]bool{}
	for _, f := range c.Forms {
		require.NotEmptyf(t, f.ID, "у формы нет имени — находка не приведёт к предмету")
		require.Falsef(t, seen[f.ID], "форма %q объявлена дважды", f.ID)
		seen[f.ID] = true
		require.NotEmptyf(t, f.Lines, "форма %q не несёт ни одной строки", f.ID)
		require.NotEmptyf(t, f.Why, "форма %q не объясняет, что именно она стережёт", f.ID)
	}
	return c
}

func sortedCodes(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for c := range m {
		out = append(out, c)
	}
	sort.Ints(out)
	return out
}

// TestResponseCodeFormCorpusIsHonoredByTheTreeGate — СТОРОНА GO.
func TestResponseCodeFormCorpusIsHonoredByTheTreeGate(t *testing.T) {
	t.Parallel()

	root := repoRootFor(t)
	corpus := loadResponseCodeCorpus(t, root)

	var mismatch []string
	for _, f := range corpus.Forms {
		got := sortedCodes(acceptedResponseCodes(strings.Join(f.Lines, "\n")))
		want := append([]int(nil), f.Accepted...)
		sort.Ints(want)
		if len(got) == 0 && len(want) == 0 {
			continue
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			mismatch = append(mismatch, fmt.Sprintf(
				"форма %q: разбор гейта дал %v, корпус объявляет %v\n      зачем форма: %s",
				f.ID, got, want, f.Why))
		}
	}

	t.Logf("перепись: форм в корпусе %d; расхождений %d", len(corpus.Forms), len(mismatch))
	require.Emptyf(t, mismatch,
		"разбор гейта дерева разошёлся с общим источником ожиданий (%s).\n%s",
		responseCodeCorpusRel, strings.Join(mismatch, "\n"))
}

// pythonForFormParity — интерпретатор, которым исполняются генераторы.
//
// Отсутствие — ОТКАЗ, а не пропуск: генерация предшествует каждому сквозному
// прогону, поэтому дерево без python3 сквозные пробы не собирает вовсе. Проверка,
// молча пропустившаяся в такой среде, отчиталась бы зелёным о свойстве, которого
// не проверяла. (Одноимённый помощник есть в пакете `repohygiene`; этот файл живёт
// во ВНЕШНЕМ тестовом пакете и до него не дотягивается.)
func pythonForFormParity(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("python3")
	if err != nil {
		t.Fatalf("python3 не найден (%v): генераторы сквозных проб не исполнимы, "+
			"а значит их сторона НЕ ПРОВЕРЕНА — это не «ноль находок»", err)
	}
	return p
}

// generatorsWithAcceptedCodes — наборы, чей генератор НЕСЁТ предикат приемлемых
// исходов. Перечень ВЫВОДИТСЯ из дерева, а не выписывается: выписанный разошёлся
// бы с деревом молча, и новый набор остался бы вне проверки.
//
// Набор без предиката (сегодня — geo) исключением НЕ объявляется: его там просто
// нет, и перепись это НАЗЫВАЕТ. Заведут — попадёт под проверку сам.
func generatorsWithAcceptedCodes(t *testing.T, root string) (with, without []string) {
	t.Helper()

	out, err := gitenv.Command(root, "ls-files", "*/tests/newman/scripts/gen.py").Output()
	require.NoError(t, err, "git ls-files: без переписи «ноль находок» неотличимо "+
		"от «ноль прочитанного»")

	for _, rel := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if rel == "" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304 -- путь из индекса git
		require.NoErrorf(t, err, "чтение %s", rel)
		if generatorCarriesAcceptedCodes(string(b)) {
			with = append(with, rel)
			continue
		}
		without = append(without, rel)
	}
	sort.Strings(with)
	sort.Strings(without)
	return with, without
}

// acceptedCodesDriver — исполняется python3 и печатает JSON «имя формы -> коды»,
// полученные ФУНКЦИЕЙ ГЕНЕРАТОРА. Читается исход, а не текст регулярки.
const acceptedCodesDriver = `
import importlib.util, json, sys

gen_path, corpus_path = sys.argv[1], sys.argv[2]
spec = importlib.util.spec_from_file_location("gen_under_test", gen_path)
mod = importlib.util.module_from_spec(spec)
sys.argv = [sys.argv[0]]
sys.modules["gen_under_test"] = mod
spec.loader.exec_module(mod)

with open(corpus_path, encoding="utf-8") as fh:
    corpus = json.load(fh)

out = {}
for form in corpus["forms"]:
    out[form["id"]] = sorted(mod._accepted_http_codes("\n".join(form["lines"])))
sys.stdout.write(json.dumps(out))
`

// TestResponseCodeFormCorpusIsHonoredByEveryNewmanGenerator — СТОРОНА PYTHON,
// и она проверяется у КАЖДОГО генератора, а не у того, где вспомнили.
func TestResponseCodeFormCorpusIsHonoredByEveryNewmanGenerator(t *testing.T) {
	t.Parallel()

	root := repoRootFor(t)
	corpus := loadResponseCodeCorpus(t, root)
	python := pythonForFormParity(t)

	with, without := generatorsWithAcceptedCodes(t, root)
	require.NotEmptyf(t, with,
		"ни один генератор не несёт `_accepted_http_codes` — проверка беспредметна, "+
			"и её молчание неотличимо от согласия")

	driver := filepath.Join(t.TempDir(), "accepted_codes_driver.py")
	require.NoError(t, os.WriteFile(driver, []byte(acceptedCodesDriver), 0o600))
	corpusAbs := filepath.Join(root, responseCodeCorpusRel)

	var findings []string
	checked := 0
	for _, rel := range with {
		genAbs := filepath.Join(root, rel)
		cmd := exec.Command(python, driver, genAbs, corpusAbs) // #nosec G204 -- пути из индекса git
		// Генератор исполняется из каталога своего набора: так его зовут и
		// прогонщик, и CI. Чужой рабочий каталог сделал бы вердикт свойством места
		// запуска.
		cmd.Dir = filepath.Dir(filepath.Dir(genAbs))
		outBytes, err := cmd.Output()
		if err != nil {
			var stderr string
			if ee, ok := err.(*exec.ExitError); ok {
				stderr = strings.TrimSpace(string(ee.Stderr))
			}
			findings = append(findings, fmt.Sprintf(
				"%s: генератор не исполнился (%v) — его сторона НЕ ПРОВЕРЕНА, и это не "+
					"«ноль находок»\n      %s", rel, err, stderr))
			continue
		}
		var got map[string][]int
		require.NoErrorf(t, json.Unmarshal(outBytes, &got), "%s: разбор вывода драйвера", rel)
		checked++

		for _, f := range corpus.Forms {
			want := append([]int(nil), f.Accepted...)
			sort.Ints(want)
			have := got[f.ID]
			sort.Ints(have)
			if len(have) == 0 && len(want) == 0 {
				continue
			}
			if fmt.Sprint(have) != fmt.Sprint(want) {
				findings = append(findings, fmt.Sprintf(
					"%s\n      форма %q: генератор дал %v, корпус объявляет %v\n      зачем форма: %s",
					rel, f.ID, have, want, f.Why))
			}
		}
	}

	// Имя набора: под `services/<svc>/` — сам сервис, иначе первый сегмент
	// (`gateway`). Слепое взятие второго сегмента звало бы край «tests» —
	// координата, не приводящая к предмету, ничем не лучше её отсутствия.
	names := func(paths []string) string {
		var s []string
		for _, p := range paths {
			seg := strings.Split(p, "/")
			name := seg[0]
			if name == "services" && len(seg) > 1 {
				name = seg[1]
			}
			s = append(s, name)
		}
		if len(s) == 0 {
			return "нет"
		}
		return strings.Join(s, ", ")
	}
	t.Logf("перепись: генераторов в дереве %d; несут предикат %d (%s); без предиката %d (%s); "+
		"форм в корпусе %d; генераторов проверено %d; расхождений %d",
		len(with)+len(without), len(with), names(with), len(without), names(without),
		len(corpus.Forms), checked, len(findings))

	require.Equalf(t, len(with), checked,
		"проверено %d генераторов из %d — непроверенный генератор это «не выполнилось», "+
			"а не согласие", checked, len(with))
	require.Emptyf(t, findings,
		"генератор разошёлся с общим источником ожиданий (%s). Пока стороны читают "+
			"утверждение о коде по-разному, «шаг утверждает отказ» означает у них разное.\n%s",
		responseCodeCorpusRel, strings.Join(findings, "\n"))
}

// generatorCarriesAcceptedCodes — несёт ли генератор помощник `_accepted_http_codes`,
// В ЛЮБОЙ из двух законных форм записи.
//
// ПОЧЕМУ ФОРМ ДВЕ. До задачи #1367 помощник объявлял КАЖДЫЙ генератор своей
// копией; после сведения он живёт в общем слое (`tests/newman/kacholib/gen_shared.py`),
// и генератор получает его импортом. Обе формы законны и обе дают модулю
// атрибут `_accepted_http_codes` — драйвер ниже зовёт его у модуля и о способе
// появления имени не знает by construction.
//
// РАСПОЗНАВАТЕЛЬ, ЗНАЮЩИЙ ОДНУ ФОРМУ, НЕ ДАЁТ НИ КРАСНОГО, НИ ЗЕЛЁНОГО — он даёт
// НЕВИДИМОСТЬ: генератор со второй формой уезжает в `without`, то есть в
// «предмета нет», и корпус форм перестаёт проверяться ровно там, где он
// проверялся. Здесь это наблюдалось сразу у всех восьми наборов: перечень `with`
// стал пуст, и гейт справедливо отказал по своей же предпосылке.
func generatorCarriesAcceptedCodes(src string) bool {
	if strings.Contains(src, "def _accepted_http_codes") {
		return true // форма 1: собственное объявление набора
	}
	// Форма 2: имя приходит из общего слоя. Судится ИМПОРТ, а не упоминание:
	// имя помощника встречается в комментариях и в текстах сообщений, и
	// проверка по подстроке краснела бы на собственном объяснении.
	from := strings.Index(src, "from gen_shared import (")
	if from < 0 {
		return false
	}
	end := strings.Index(src[from:], ")")
	if end < 0 {
		return false
	}
	for _, line := range strings.Split(src[from:from+end], "\n") {
		if strings.TrimSpace(line) == "_accepted_http_codes," {
			return true
		}
	}
	return false
}
