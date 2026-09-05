// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package softopengate

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/PRO-Robotech/kacho/pkg/gitenv"
)

// declaredFilterRoots — все фильтры сужения страницы. Перечислены ЦЕЛИКОМ, а не
// образцом: утверждение «у остальных так же» проверяется перечислением, иначе это
// догадка. iam и registry входят, хотя мягкого прохода у них нет вовсе — именно это
// и подтверждается их нулём.
//
// Перечень ВЫПИСАН, а не выведен, и
// именно поэтому рядом стоит TestFilterRootsCoverEveryPageFilterInTheTree: он
// сверяет этот перечень с деревом. Без сверки перечень полон ровно на тот день,
// когда его писали, — а новый сервис со своим фильтром страницы попадал бы в
// слепую зону молча (проверено инъекцией: подложенный сервис с непрослеживаемым
// мягким проходом гейт не увидел и остался зелёным на «6 root(s)»).
var declaredFilterRoots = []string{
	// Общий сужатель — единственный дом мягкого прохода у четырёх сервисов. Пока он
	// жил в четырёх копиях, каждая несла свою ветку; теперь ветка одна, и гейт обязан
	// смотреть туда, где она есть, а не туда, где она была. Без этой строки перепись
	// «прочитано веток мягкого прохода» падала бы в ноль — что гейт и сделал, отказав
	// на собственной предпосылке вместо того, чтобы промолчать зелёным.
	"pkg/listnarrow",
	"services/compute/internal/authzfilter",
	"services/nlb/internal/authzfilter",
	"services/storage/internal/authzfilter",
	"services/vpc/internal/authzfilter",
	"services/iam/internal/authzfilter",
	"services/registry/internal/handler",
}

// pageFilterDirGlob — форма, по которой фильтр сужения страницы опознаётся В
// ДЕРЕВЕ. Это предпосылка сверки: если ей перестанет соответствовать хоть один
// каталог, сверка скажет об этом, а не промолчит.
const pageFilterDirGlob = "services/*/internal/authzfilter"

// declaredNonFilterRoots — корни перечня, которые под форму выше НЕ подпадают, и
// причина у каждого. Запись, у которой не осталось предмета, — находка: иначе
// освобождение переживёт то, что освобождало.
var declaredNonFilterRoots = map[string]string{
	"services/registry/internal/handler": "у registry фильтр страницы живёт в обработчике, отдельного пакета нет",
	"pkg/listnarrow": "" +
		"общий сужатель списков: живёт в фундаменте, а не под services/, поэтому под " +
		"форму каталога сервиса не подпадает. Мягкий проход теперь принимается здесь — " +
		"один раз на все сервисы, — и именно поэтому корень обязан быть в перечне.",
}

func filterRoots(t *testing.T) []string {
	t.Helper()
	repo := repoRoot(t)
	roots := declaredFilterRoots
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		p := filepath.Join(repo, r)
		st, err := os.Stat(p)
		require.NoError(t, err, "%s не найден — гейт нацелен не на то дерево, и его «чисто» ничего не значит", r)
		require.True(t, st.IsDir(), "%s не директория", r)
		out = append(out, p)
	}
	return out
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)
	for dir := wd; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		if filepath.Dir(dir) == dir {
			t.Fatal("go.mod не найден вверх по дереву")
		}
	}
}

// trackedPageFilterDirs — каталоги фильтра страницы, ВЫВЕДЕННЫЕ ИЗ ИНДЕКСА git.
//
// Индекс, а не диск: гейт судит о том, что уедет в чистый клон и в CI, и не
// должен зависеть от локального мусора рядом.
func trackedPageFilterDirs(t *testing.T, repo string) []string {
	t.Helper()
	out, err := gitenv.Command(repo, "ls-files", "-z", "--", pageFilterDirGlob+"/*.go").Output()
	require.NoError(t, err, "git ls-files сорвался — предпосылку сверки не на чем проверить")

	seen := map[string]bool{}
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" {
			continue
		}
		seen[path.Dir(filepath.ToSlash(rel))] = true
	}
	dirs := make([]string, 0, len(seen))
	for d := range seen {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	return dirs
}

// TestFilterRootsCoverEveryPageFilterInTheTree — перечень обхода сверяется с
// ДЕРЕВОМ, а не остаётся утверждением того дня, когда его писали.
//
// # Предмет
//
// Гейт мягкого прохода судит только те каталоги, которые ему назвали. Пока
// перечень пишется руками, «шесть корней» — это не факт о дереве, а память
// автора: сервис, заведший СВОЙ фильтр страницы после, в обход не попадает, и
// непрослеживаемый мягкий проход в нём остаётся невидим. Гейт при этом зелёный и
// печатает честную перепись — по ней и не отличить «нечего находить» от «мы туда
// не смотрели». Ровно та форма без содержания, которую сам гейт и ловит, только
// уровнем выше: не ветка не может упасть, а обход не доходит.
//
// # Что это стоит
//
// Мягкий проход отдаёт вызывающему НЕСУЖЕННУЮ страницу. Радиус пропуска здесь —
// не стиль, а видимость чужих строк.
//
// # Предпосылка проверяется
//
// Сверка обоснована тем, что фильтр страницы в этом дереве опознаётся формой
// каталога. Перестанет — выведенное множество опустеет, и тест скажет об этом
// вместо того, чтобы молча согласиться с перечнем.
//
// # Доказано инъекцией в обе стороны
//
// Подложенный каталог седьмого сервиса той же формы даёт находку с именем
// каталога; чистое дерево — молчание.
func TestFilterRootsCoverEveryPageFilterInTheTree(t *testing.T) {
	repo := repoRoot(t)
	derived := trackedPageFilterDirs(t, repo)

	declared := map[string]bool{}
	for _, r := range declaredFilterRoots {
		declared[r] = true
	}

	t.Logf("перепись: выведено из индекса по форме %q — %d каталог(ов) %v; "+
		"объявлено корней — %d (из них не подпадающих под форму — %d)",
		pageFilterDirGlob, len(derived), derived, len(declaredFilterRoots), len(declaredNonFilterRoots))

	// Предпосылка: форма всё ещё опознаёт хоть что-то. Пустое выведенное
	// множество — это «мы не посмотрели», а не «сверять нечего».
	require.NotEmpty(t, derived,
		"по форме %q не найдено НИ ОДНОГО каталога: фильтры страницы переименованы или "+
			"переехали. Сверка перечня с деревом потеряла предмет, и «перечень полон» "+
			"больше ничем не подтверждается", pageFilterDirGlob)

	var findings []string
	for _, d := range derived {
		if !declared[d] {
			findings = append(findings, d+": каталог фильтра страницы есть в дереве, но гейт "+
				"мягкого прохода его НЕ обходит — непрослеживаемый мягкий проход в нём остался бы "+
				"невидимым, а гейт зелёным. Добавь каталог в declaredFilterRoots")
		}
	}

	// Обратная сторона: объявленный корень, подпадающий под форму, обязан в
	// дереве быть. Иначе перечень описывает то, чего нет.
	inDerived := map[string]bool{}
	for _, d := range derived {
		inDerived[d] = true
	}
	for _, r := range declaredFilterRoots {
		if _, excused := declaredNonFilterRoots[r]; excused {
			continue
		}
		if ok, _ := path.Match(pageFilterDirGlob, r); ok && !inDerived[r] {
			findings = append(findings, r+": объявлен корнем, но в индексе под этой формой "+
				"каталога нет — запись пережила свой предмет")
		}
	}

	// Освобождение живёт, пока у него есть предмет.
	for r, why := range declaredNonFilterRoots {
		if !declared[r] {
			findings = append(findings, r+": освобождение ("+why+") не относится ни к одному "+
				"объявленному корню — удали запись")
			continue
		}
		if ok, _ := path.Match(pageFilterDirGlob, r); ok {
			findings = append(findings, r+": освобождение ("+why+") больше не нужно — корень "+
				"подпадает под общую форму. Удали запись, иначе она станет тихой слепой зоной")
		}
	}

	assert.Empty(t, findings, strings.Join(findings, "\n"))
}

// TestSoftOpenPassesAreObservable — предмет гейта на реальном дереве.
func TestSoftOpenPassesAreObservable(t *testing.T) {
	rep, err := Run(filterRoots(t))
	require.NoError(t, err)
	t.Log(rep.Census())

	// Перепись — отдельное утверждение: «ноль находок» обязано быть отличимо от
	// «ноль прочитанного».
	assert.Greater(t, rep.Files, 0, "гейт не прочитал ни одного файла")
	assert.Greater(t, rep.SwitchReads, 0, "ни одна ветка не читает ручку мягкого прохода")
	assert.Greater(t, rep.SoftPasses, 0, "гейт не осудил ни одного мягкого прохода")

	assert.Empty(t, rep.PremiseNotes, "предпосылка гейта перестала держаться")
	assert.Empty(t, rep.Findings, strings.Join(rep.Findings, "\n"))
}

// TestGateIsRedOnAnUnobservableSoftPass — ИНЪЕКЦИЯ №1: возвращаем дефект, гейт обязан
// покраснеть И НАЗВАТЬ КООРДИНАТУ.
func TestGateIsRedOnAnUnobservableSoftPass(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "filter.go", `package p

type cfg struct{ FailOpen bool }
type F struct{ cfg cfg }

func (f *F) handleErr(ids []string, err error) ([]string, error) {
	if f.cfg.FailOpen {
		// Мягкий проход: страница уходит целиком. Считаем и пишем предупреждение.
		// Комментарий описывает и счётчик, и запись — но их здесь НЕТ.
		return ids, nil
	}
	return nil, err
}
`)
	rep, err := Run([]string{dir})
	require.NoError(t, err)
	require.Len(t, rep.Findings, 1, rep.Census())
	assert.Contains(t, rep.Findings[0], "filter.go:7:2", "находка обязана назвать координату — файл, строку и колонку")
	assert.Contains(t, rep.Findings[0], "handleErr")
	assert.Contains(t, rep.Findings[0], "neither logged nor counted")
	assert.False(t, rep.OK())
}

// TestGateIsSilentOnALegitimateRefusal — ИНЪЕКЦИЯ №2 (обратная сторона): ЗАКОННАЯ
// конструкция ТОЙ ЖЕ ФОРМЫ — загрузочный страж, читающий ту же ручку и ОТКАЗЫВАЮЩИЙ,
// — обязана пройти молча. Без этой пары гейт ловил бы форму, а не существо, и первый
// же ложный срабат его бы отключил: он краснел бы ровно на тех стражах, которые и
// делают мягкий проход переживаемым.
func TestGateIsSilentOnALegitimateRefusal(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "validate.go", `package p

import "fmt"

type conf struct{ ListFilterFailOpen bool }

func requireListFilter(c conf, scopeFiltered []string) error {
	if c.ListFilterFailOpen {
		return fmt.Errorf("production requires fail-open=false (%d scope-filtered RPC)", len(scopeFiltered))
	}
	return nil
}
`)
	rep, err := Run([]string{dir})
	require.NoError(t, err)
	assert.Empty(t, rep.Findings, "отказ — не проход; гейт обязан молчать")
	assert.Equal(t, 1, rep.SwitchReads, "ветка увидена")
	assert.Equal(t, 0, rep.SoftPasses)
	assert.Equal(t, 1, rep.Refusals, "и классифицирована как отказ")
	assert.True(t, rep.OK())
}

// TestGateIsSilentOnAnObservableSoftPass — вторая законная конструкция той же формы:
// мягкий проход, который НАЗВАН и ПОСЧИТАН, в том числе когда наблюдаемость живёт в
// делегате. Иначе гейт требовал бы писать всё в одной ветке.
func TestGateIsSilentOnAnObservableSoftPass(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "filter.go", `package p

import (
	"log/slog"
	"sync/atomic"
)

type cfg struct{ FailOpen bool }
type F struct {
	cfg    cfg
	logger *slog.Logger
	passes atomic.Uint64
}

func (f *F) handleErr(ids []string, err error) ([]string, error) {
	if f.cfg.FailOpen {
		return f.openPass(ids, err)
	}
	return nil, err
}

func (f *F) openPass(ids []string, err error) ([]string, error) {
	total := f.passes.Add(1)
	f.logger.Warn("unfiltered page returned", "error", err, "total", total)
	return ids, nil
}
`)
	rep, err := Run([]string{dir})
	require.NoError(t, err)
	assert.Empty(t, rep.Findings, "наблюдаемый проход — не находка")
	assert.Equal(t, 1, rep.SoftPasses, "и он именно проход, а не отказ")
	assert.True(t, rep.OK())
}

// TestGateRefusesToVouchForAnEmptyWalk — гейт несёт проверку СВОЕЙ предпосылки: «ноль
// находок» на дереве, где нечего было находить, обязано читаться как непроверенное.
func TestGateRefusesToVouchForAnEmptyWalk(t *testing.T) {
	t.Run("nothing_parsed", func(t *testing.T) {
		rep, err := Run([]string{t.TempDir()})
		require.NoError(t, err)
		assert.Empty(t, rep.Findings)
		assert.False(t, rep.OK(), "пустой обход не бывает чистым результатом")
		require.NotEmpty(t, rep.PremiseNotes)
		assert.Contains(t, rep.PremiseNotes[0], "examined nothing")
	})
	t.Run("knob_renamed_away", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "filter.go", `package p

type cfg struct{ SoftPassOnPeerError bool }
type F struct{ cfg cfg }

func (f *F) handleErr(ids []string, err error) ([]string, error) {
	if f.cfg.SoftPassOnPeerError {
		return ids, nil
	}
	return nil, err
}
`)
		rep, err := Run([]string{dir})
		require.NoError(t, err)
		assert.Empty(t, rep.Findings)
		assert.False(t, rep.OK(), "переименованная ручка делает прогон бездоказательным")
		require.NotEmpty(t, rep.PremiseNotes)
		assert.Contains(t, rep.PremiseNotes[0], "premise no longer holds")
	})
}

// TestGateReadsCodeNotComments — запись и счётчик, УПОМЯНУТЫЕ в комментарии, но не
// вызванные, не спасают ветку. Комментарий рядом с такой веткой — обычное дело именно
// потому, что он объясняет удалённый вызов.
func TestGateReadsCodeNotComments(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "filter.go", `package p

type cfg struct{ FailOpen bool }
type F struct{ cfg cfg }

func (f *F) handleErr(ids []string, err error) ([]string, error) {
	if f.cfg.FailOpen {
		// f.logger.Warn("unfiltered page returned", "error", err)
		// f.passes.Add(1)
		return ids, nil
	}
	return nil, err
}
`)
	rep, err := Run([]string{dir})
	require.NoError(t, err)
	require.Len(t, rep.Findings, 1, "закомментированные вызовы — не вызовы")
	assert.Contains(t, rep.Findings[0], "neither logged nor counted")
}

func write(t *testing.T, dir, name, src string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600))
}
