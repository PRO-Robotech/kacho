// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// excludingbuildtag_test.go — признак сборки, способный ВЫВЕСТИ файл из обычной
// сборки, не вправе оставлять пакет несобираемым.
//
// # Предмет
//
// Есть два разных вида признака сборки, и опасности у них разные.
//
// Признак ВКЛЮЧАЮЩИЙ (`//go:build integration`) держит файл вне обычной сборки:
// пока тег никто не передаёт, содержимое файла не компилируется ни разу. Это
// класс задачи #489, и его держит отдельный гейт — здесь он не повторяется.
//
// Признак ИСКЛЮЧАЮЩИЙ (`//go:build … || !short`) — противоположность: файл стоит
// в обычной сборке, и потому выглядит проверенным каждым прогоном. Но существует
// тег, передача которого его оттуда УБИРАЕТ. Если такой файл определяет символ,
// которым пользуются файлы, этим тегом не задетые, то передача тега ломает
// СБОРКУ ПАКЕТА — а значит отменяет прогон ВСЕХ его проб, включая те, что
// признака не несут. Обычная сборка при этом зелёная и остаётся зелёной, потому
// что она этот тег не передаёт.
//
// # Почему это класс «послабление, чей предикат снятия не читает свой предмет»
//
// `!short` читается как «не включать в короткий прогон». Обещание не исполняется:
// `short` — флаг `go test`, а не признак сборки, и как признак его не передаёт
// никто. То есть выражение эквивалентно отсутствию выражения. Зато обещание
// исполнимо в обратную сторону: тот, кто прочтёт его буквально и позовёт
// `-tags=short`, получит не «пропущенную фикстуру», а несобирающийся пакет.
//
// # Где наблюдалось
//
// `services/vpc/internal/repo/quota_fixture_integration_test.go` (задача #493):
// `//go:build integration || !short` над фикстурой учёта, чью функцию зовёт
// TestMain пакета — файл без признака вовсе. `go vet -tags=short` на этом пакете
// отвечал `undefined: seedFixtureQuotas`, `go vet` без тега — молчал. Настоящее
// исключение короткого прогона делает `testing.Short()` в TestMain, то есть
// признак сборки был вторым местом об одном предмете, из которых работает одно.
//
// # Почему предикат — компилятор, а не текст
//
// «Символ определён здесь и позван оттуда» точным разбором текста не берётся:
// имя может быть полем, ключом литерала, методом. Нужен вывод типов, то есть
// настоящий компилятор. Гейт зовёт `go vet` с тем самым тегом — ту же команду,
// что стоит в признаке задачи.
//
// Перед этим он зовёт `go vet` БЕЗ тега: пакет, не собирающийся и так, — предмет
// обычной сборки, а не этого гейта. Без контроля в эту сторону гейт приписывал бы
// признаку сборки чужую поломку.
//
// # Почему перечень признаков инструмента спрашивается, а не выписывается
//
// `//go:build !windows` тоже «исключающий», но его пара обычно выбирается по ИМЕНИ
// файла (`foo_windows.go`), а не тегом, — и сборка с `-tags=windows` на linux
// нашла бы отсутствующее определение, дав находку на совершенно законном коде.
// Поэтому имена платформ берутся у самого инструмента (`go tool dist list`), а не
// переписываются сюда: выписанный перечень устаревает молча, а спрошенный — нет.
// Руками названы только те признаки, которые инструмент выставляет НЕ по
// платформе и потому в этом перечне отсутствуют.
//
// # Перепись и пустая ведомость
//
// «Ноль находок» обязано отличаться от «ноль прочитанного», поэтому гейт печатает
// объём осмотренного по каждой оси. Пустая ведомость исключающих признаков —
// ЦЕЛЬ, а не поломка: на ней гейт проходит, называя перепись. Способность судьи
// упасть доказывается отдельно и на настоящем входе —
// excludingbuildtag_injection_test.go.
package repohygiene

import (
	"bufio"
	"fmt"
	"go/build/constraint"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// excludingTagFinding — пакет, который перестаёт собираться, стоит передать тег,
// выводящий из сборки один из его файлов.
type excludingTagFinding struct {
	Pkg    string // rel-путь каталога пакета
	File   string // rel-путь файла, чей признак его исключает
	Tag    string // тег, передача которого исключает файл
	Output string // что сказал компилятор — находка без этого не действие
}

func (f excludingTagFinding) String() string {
	return fmt.Sprintf(
		"%s не собирается под -tags=%s, а этот тег выводит из сборки %s "+
			"(его признак: `//go:build …`, исключающий тег %q):\n%s",
		f.Pkg, f.Tag, f.File, f.Tag, f.Output)
}

// excludingTagCensus — объём осмотренного. У гейта, зовущего внешнюю команду,
// исходов три, а не два: кроме «нашёл» и «не нашёл» есть «не звал ни разу».
type excludingTagCensus struct {
	GoFilesRead           int
	FilesWithConstraint   int
	FilesInDefaultBuild   int
	ExcludableFiles       int
	ExcludingTags         []string
	SkippedPlatformTags   []string
	PackagesChecked       int
	PackagesAlreadyBroken int
	VetRuns               int
}

func (c excludingTagCensus) String() string {
	return fmt.Sprintf(
		"перепись: файлов .go прочитано %d · с признаком сборки %d · из них в обычной "+
			"сборке %d · исключаемых %d · исключающих тегов %d (%s) · отброшено как "+
			"признаки платформы %d (%s) · пакетов проверено %d · из них не собирались и "+
			"без тега %d · вызовов сборки %d",
		c.GoFilesRead, c.FilesWithConstraint, c.FilesInDefaultBuild, c.ExcludableFiles,
		len(c.ExcludingTags), strings.Join(c.ExcludingTags, ", "),
		len(c.SkippedPlatformTags), strings.Join(c.SkippedPlatformTags, ", "),
		c.PackagesChecked, c.PackagesAlreadyBroken, c.VetRuns)
}

// toolchainSetTags — признаки, которые инструмент выставляет САМ и не по имени
// платформы, поэтому в `go tool dist list` их нет.
//
// Перечень короткий намеренно: всё, что зависит от платформы, спрашивается, а не
// пишется (см. шапку). Здесь только то, что спросить негде. Цена промаха названа
// и невелика: незнакомый признак инструмента дал бы один лишний вызов сборки,
// который прошёл бы, — а незнакомый ПОЛЬЗОВАТЕЛЬСКИЙ признак и есть предмет
// гейта, и умолчание «незнакомый — пользовательский» для него верное.
var toolchainSetTags = map[string]bool{
	"ignore": true, "cgo": true, "race": true, "msan": true, "asan": true,
	"gc": true, "gccgo": true, "unix": true, "purego": true, "boringcrypto": true,
}

// platformTagsFromToolchain — имена GOOS и GOARCH, названные самим инструментом.
func platformTagsFromToolchain() (map[string]bool, error) {
	out, err := exec.Command("go", "tool", "dist", "list").Output()
	if err != nil {
		return nil, fmt.Errorf("go tool dist list: %w — перечень платформ взять неоткуда, "+
			"а выписанный сюда устаревал бы молча", err)
	}
	set := make(map[string]bool, 128)
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		goos, goarch, ok := strings.Cut(line, "/")
		if !ok {
			continue
		}
		set[goos] = true
		set[goarch] = true
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("go tool dist list не назвал ни одной платформы — " +
			"вердикт об исключающих тегах на этом был бы недействителен")
	}
	return set, nil
}

// headerBuildConstraint читает признак сборки ФАЙЛА так же, как читает его
// инструмент: только из шапки, до объявления пакета.
//
// Ниже объявления пакета `//go:build` признаком не является — это разговор о
// признаке. Судья, читающий такую строку как признак, звал бы сборку с тегом,
// которого в дереве нет, и получал бы «ноль находок» на непрочитанном.
func headerBuildConstraint(path string) (constraint.Expr, bool, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "package ") {
			return nil, false, nil
		}
		if !constraint.IsGoBuild(line) {
			continue
		}
		expr, err := constraint.Parse(line)
		if err != nil {
			return nil, false, fmt.Errorf("%s: разбор %q: %w", path, line, err)
		}
		return expr, true, nil
	}
	return nil, false, sc.Err()
}

// constraintTags — все теги, названные выражением.
func constraintTags(expr constraint.Expr) []string {
	seen := map[string]struct{}{}
	var walk func(constraint.Expr)
	walk = func(e constraint.Expr) {
		switch v := e.(type) {
		case *constraint.TagExpr:
			seen[v.Tag] = struct{}{}
		case *constraint.NotExpr:
			walk(v.X)
		case *constraint.AndExpr:
			walk(v.X)
			walk(v.Y)
		case *constraint.OrExpr:
			walk(v.X)
			walk(v.Y)
		}
	}
	walk(expr)
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// inDefaultBuild — файл попадает в сборку, когда не передано ни одного тега.
func inDefaultBuild(expr constraint.Expr) bool {
	return expr.Eval(func(string) bool { return false })
}

// excludedByTag — передача ОДНОГО этого тега выводит файл из сборки.
func excludedByTag(expr constraint.Expr, tag string) bool {
	return !expr.Eval(func(t string) bool { return t == tag })
}

// vetWithTags зовёт настоящий компилятор на пакете. Возвращает вывод и признак
// успеха: собралось / не собралось.
func vetWithTags(root, pkgRel, tag string) (string, bool) {
	args := []string{"vet"}
	if tag != "" {
		args = append(args, "-tags="+tag)
	}
	args = append(args, "./"+filepath.ToSlash(pkgRel)+"/")
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err == nil
}

// excludableFile — файл, стоящий в обычной сборке, и тег, который его оттуда
// выводит.
type excludableFile struct {
	relPath string
	pkgRel  string
	tag     string
}

// auditExcludingBuildTags — судья. Тот же и для дерева, и для инъекции.
func auditExcludingBuildTags(root string) ([]excludingTagFinding, excludingTagCensus, error) {
	var census excludingTagCensus

	files, err := treecorpus.UnderWithSuffix(root, ".go")
	if err != nil {
		return nil, census, err
	}
	census.GoFilesRead = len(files)

	var candidates []excludableFile
	skippedPlatform := map[string]struct{}{}
	tagsSeen := map[string]struct{}{}

	var platform map[string]bool
	for _, abs := range files {
		expr, ok, err := headerBuildConstraint(abs)
		if err != nil {
			return nil, census, err
		}
		if !ok {
			continue
		}
		census.FilesWithConstraint++
		if !inDefaultBuild(expr) {
			continue
		}
		census.FilesInDefaultBuild++

		rel, err := filepath.Rel(root, abs)
		if err != nil {
			return nil, census, fmt.Errorf("rel для %s: %w", abs, err)
		}
		rel = filepath.ToSlash(rel)

		excludable := false
		for _, tag := range constraintTags(expr) {
			if !excludedByTag(expr, tag) {
				continue
			}
			// Перечень платформ спрашивается ЛЕНИВО: на дереве без исключающих
			// признаков подпроцесса не будет вовсе.
			if platform == nil {
				platform, err = platformTagsFromToolchain()
				if err != nil {
					return nil, census, err
				}
			}
			if platform[tag] || toolchainSetTags[tag] || strings.HasPrefix(tag, "go1.") {
				skippedPlatform[tag] = struct{}{}
				continue
			}
			excludable = true
			tagsSeen[tag] = struct{}{}
			candidates = append(candidates, excludableFile{
				relPath: rel,
				pkgRel:  filepath.ToSlash(filepath.Dir(rel)),
				tag:     tag,
			})
		}
		if excludable {
			census.ExcludableFiles++
		}
	}
	census.ExcludingTags = sortedTagSet(tagsSeen)
	census.SkippedPlatformTags = sortedTagSet(skippedPlatform)

	// Группировка по пакету: контроль «а собирается ли он вообще» стоит одного
	// вызова на пакет, а не на файл.
	baselineOK := map[string]bool{}
	var findings []excludingTagFinding
	for _, c := range candidates {
		ok, known := baselineOK[c.pkgRel]
		if !known {
			out, built := vetWithTags(root, c.pkgRel, "")
			census.VetRuns++
			census.PackagesChecked++
			if !built {
				census.PackagesAlreadyBroken++
				_ = out
			}
			baselineOK[c.pkgRel] = built
			ok = built
		}
		if !ok {
			continue
		}
		out, built := vetWithTags(root, c.pkgRel, c.tag)
		census.VetRuns++
		if built {
			continue
		}
		findings = append(findings, excludingTagFinding{
			Pkg:    c.pkgRel,
			File:   c.relPath,
			Tag:    c.tag,
			Output: out,
		})
	}
	return findings, census, nil
}

func sortedTagSet(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestExcludingBuildTagLeavesThePackageBuildable — свойство дерева.
//
// Пустая ведомость исключающих признаков — законный и желаемый исход: гейт
// проходит, называя перепись. Красным он становится там, где такой признак есть
// и ломает сборку пакета.
func TestExcludingBuildTagLeavesThePackageBuildable(t *testing.T) {
	root := repoRoot(t)

	findings, census, err := auditExcludingBuildTags(root)
	if err != nil {
		t.Fatalf("судья сорвался: %v", err)
	}
	t.Log(census.String())

	if census.GoFilesRead == 0 {
		t.Fatalf("прочитано ноль файлов .go — вердикт недействителен: %s", census)
	}

	for _, f := range findings {
		t.Errorf("%s", f)
	}
	if len(findings) > 0 {
		t.Fatalf("исключающий признак сборки оставляет пакет несобираемым: находок %d. "+
			"Исходов три: снять признак (файл и так в каждой сборке) · перенести символ в "+
			"файл без признака · вывести из сборки ВСЕХ его пользователей тем же признаком. "+
			"Маркер отложенной работы исходом не является", len(findings))
	}
}
