// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// bundle_g17_test.go — пробы полосы N9 (kacho#2915): сборка шаблонов notify.
//
// Две пробы, обе судят наблюдаемое, а не раскладку реализации:
//
//   - TestBundle_NTF1G17_StaleBuildIsRedAndRebuildIsGreen — NTF1-G17 и DoD S3
//     п.13 через цели `make -C services/notify bundle` и `bundle-check`, на
//     КОПИИ рабочего дерева во временном каталоге: шаг `bundle` пишет в дерево,
//     а общая рабочая копия не трогается. Близнец — дерево как есть (сборка
//     свежая): `bundle-check` зелёный, повторный `bundle` не меняет ни байта.
//     Отрицание — ровно один изменённый факт: текст абзаца `body.ru.yaml`
//     шаблона `probe-hello` пробы-источника `notify-probe` (набор ревизии тот
//     же, поэтому `revision.yaml` не устаревает и краснеть может только
//     сборка). Тогда `bundle-check` красный с именем шаблона и источника; после
//     `bundle` зелёный, исходников шаблонов шаг не правит.
//   - TestBundle_NTF1G17_ProdBuildServesTheTreeTemplate — первая
//     прод-реализация порта `deliver.Build`: пакет
//     `services/notify/bundle` отдаёт конструктором сборку, по которой шаблон
//     дерева находится как `<пространство ленты>/<имя>` (Р6), с классом и
//     ревизией из `revision.yaml`. Конструктор ищется по ТИПУ, а не по имени:
//     экспортируемая функция пакета без параметров с результатом `T` либо
//     `(T, error)`, где `T` присваивается `deliver.Build`, — программа-проба
//     собирается оверлеем (`go run -overlay`) поверх дерева, дерево не
//     правится. Таких функций ровно одна: две — два пути к сборке.
//
// Порядок проверок несущий (change-graph §2): сперва вся фикстура — копия,
// шаблон дерева, работающий `make`, одно-фактная дельта, законный близнец
// программы-пробы на синтетической сборке в оверлее, — и только потом вопрос
// к испытуемому. Сломанная фикстура останавливает пробу текстом «ФИКСТУРА»,
// а не выдаёт себя за отсутствие сборки.
//
// Пробы долгие (копия дерева, сборка Go-программ), поэтому под -short не
// исполняются.
package bundle_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/PRO-Robotech/corelib/notify/spec"
)

const (
	modulePath = "github.com/PRO-Robotech/kacho"

	// Пространство ленты пробы-источника — модуль её записи в таблице модулей
	// чарта notify (deploy/helm/notify/templates/_sources.tpl, `notifyProbe`).
	probeNamespace = "notify-probe"
	probeTemplate  = "probe-hello"

	notifyDir      = "services/notify"
	probeTplDir    = "services/notify/notifications/probe-hello"
	bundlePkgDir   = "services/notify/bundle"
	deliverPkgPath = modulePath + "/services/notify/internal/deliver"
	bundlePkgPath  = modulePath + "/services/notify/bundle"

	// Один изменённый факт отрицания G17: текст абзаца тела, не входящий в
	// набор ревизии (Р7).
	injectFile = probeTplDir + "/body.ru.yaml"
	injectFrom = "цепочка доставки установки исправна."
	injectTo   = "цепочка доставки установки исправна (правка NTF1-G17)."

	stepTimeout = 10 * time.Minute
)

// ── общее ──────────────────────────────────────────────────────────────────

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatalf("ФИКСТУРА: корень репозитория: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("ФИКСТУРА: %s без go.mod — проба не исполнялась", root)
	}
	return root
}

// runResult — исход одного запуска внешней команды.
type runResult struct {
	cmd    string
	code   int
	output string
}

func (r runResult) String() string {
	return fmt.Sprintf("`%s` → код %d\n%s", r.cmd, r.code, r.output)
}

// run исполняет команду в dir с LC_ALL=C (тексты make — на одном языке).
// Не запустившаяся команда — ФИКСТУРА: «не спросили» не выдаётся за «нет».
func run(t *testing.T, dir string, env []string, name string, args ...string) runResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), stepTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(append(os.Environ(), "LC_ALL=C", "LANG=C"), env...)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	res := runResult{cmd: name + " " + strings.Join(args, " "), output: buf.String()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr) && ctx.Err() == nil:
		res.code = exitErr.ExitCode()
	default:
		t.Fatalf("НЕ ВЫПОЛНИЛОСЬ: %s не исполнилась (%v; срок %s исчерпан: %v)\n%s",
			res.cmd, err, stepTimeout, ctx.Err() != nil, buf.String())
	}
	return res
}

// probeTemplateFromTree — шаблон probe-hello настоящим загрузчиком spec из
// каталога шаблонов дерева root; отказ — ФИКСТУРА.
func probeTemplateFromTree(t *testing.T, root string) spec.Template {
	t.Helper()
	cat, census, err := spec.Load(filepath.Join(root, "services/notify/notifications"))
	if err != nil {
		t.Fatalf("ФИКСТУРА: загрузчик spec отверг каталог шаблонов %s: %v", root, err)
	}
	if census.Templates == 0 {
		t.Fatalf("ФИКСТУРА: в каталоге шаблонов notify прочитано 0 шаблонов — судить нечего")
	}
	for _, tpl := range cat.Templates {
		if tpl.Name == probeTemplate {
			if !tpl.HasRevision || tpl.Revision.Number < 1 {
				t.Fatalf("ФИКСТУРА: у шаблона %s нет ревизии из revision.yaml", probeTemplate)
			}
			return tpl
		}
	}
	t.Fatalf("ФИКСТУРА: шаблона %s в каталоге нет (шаблонов %d)", probeTemplate, census.Templates)
	return spec.Template{}
}

// ── G17: цели bundle и bundle-check ────────────────────────────────────────

// copyTree копирует в dst файлы рабочего дерева root: отслеживаемые и
// неотслеживаемые неигнорируемые (`git ls-files -co --exclude-standard`).
// Копия настоящая, не жёсткие ссылки: шаг `bundle` пишет в неё.
func copyTree(t *testing.T, root, dst string) int {
	t.Helper()
	res := run(t, root, nil, "git", "ls-files", "-co", "--exclude-standard", "-z")
	if res.code != 0 {
		t.Fatalf("ФИКСТУРА: перечень файлов дерева: %s", res)
	}
	n := 0
	for _, rel := range strings.Split(res.output, "\x00") {
		if rel == "" {
			continue
		}
		src := filepath.Join(root, rel)
		info, err := os.Lstat(src)
		if err != nil {
			// Удалённый в рабочем дереве, но ещё отслеживаемый файл.
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			t.Fatalf("ФИКСТУРА: %s: %v", rel, err)
		}
		to := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			t.Fatalf("ФИКСТУРА: %v", err)
		}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(src)
			if err != nil {
				t.Fatalf("ФИКСТУРА: %v", err)
			}
			if err := os.Symlink(target, to); err != nil {
				t.Fatalf("ФИКСТУРА: %v", err)
			}
		case info.Mode().IsRegular():
			copyFile(t, src, to, info.Mode().Perm())
		default:
			continue
		}
		n++
	}
	return n
}

func copyFile(t *testing.T, src, dst string, perm fs.FileMode) {
	t.Helper()
	in, err := os.Open(src) // #nosec G304 -- путь из перечня файлов дерева
	if err != nil {
		t.Fatalf("ФИКСТУРА: %v", err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm) // #nosec G304
	if err != nil {
		t.Fatalf("ФИКСТУРА: %v", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("ФИКСТУРА: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("ФИКСТУРА: %v", err)
	}
}

// snapshot — путь от корня копии → sha256 содержимого обычного файла.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(p) // #nosec G304 -- обход копии дерева
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		rel, _ := filepath.Rel(root, p)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatalf("ФИКСТУРА: обход копии: %v", err)
	}
	return out
}

// delta — пути, различающиеся между двумя снимками (изменены, добавлены,
// удалены), упорядоченно.
func delta(a, b map[string]string) []string {
	var out []string
	for p, h := range a {
		if b[p] != h {
			out = append(out, p)
		}
	}
	for p := range b {
		if _, ok := a[p]; !ok {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// targetAbsent — make ответил, что у цели нет рецепта: испытуемого нет вовсе.
// Три формы ответа make: правила нет; имя цели совпало с каталогом либо
// файлом (`services/notify/bundle` — каталог), и make считает её
// «актуальной» либо «делать нечего». Цель без .PHONY и рецепта сюда же.
func targetAbsent(r runResult, target string) bool {
	return strings.Contains(r.output, "No rule to make target '"+target+"'") ||
		strings.Contains(r.output, "Nothing to be done for '"+target+"'") ||
		strings.Contains(r.output, "'"+target+"' is up to date")
}

// g17Fixture — копия дерева и вычисленная одно-фактная дельта отрицания.
type g17Fixture struct {
	cp, svc      string
	injectPath   string
	orig, inject []byte
}

// newG17Fixture копирует дерево в новый временный каталог и проверяет
// фикстуру целиком: шаблон, make, одно-фактность правки (дельта вычисляется
// снимками, а не объявляется; набор ревизии правкой не меняется).
func newG17Fixture(t *testing.T, root string) *g17Fixture {
	t.Helper()
	cp := t.TempDir()
	files := copyTree(t, root, cp)
	if files == 0 {
		t.Fatalf("ФИКСТУРА: в копию дерева попало 0 файлов")
	}
	twinTpl := probeTemplateFromTree(t, cp)
	svc := filepath.Join(cp, notifyDir)
	if r := run(t, svc, nil, "make", "-n", "build"); r.code != 0 {
		t.Fatalf("ФИКСТУРА: make в копии не разбирает Makefile службы: %s", r)
	}
	f := &g17Fixture{cp: cp, svc: svc, injectPath: filepath.Join(cp, injectFile)}
	orig, err := os.ReadFile(f.injectPath) // #nosec G304
	if err != nil {
		t.Fatalf("ФИКСТУРА: %v", err)
	}
	if strings.Count(string(orig), injectFrom) != 1 {
		t.Fatalf("ФИКСТУРА: в %s строка %q встречается не ровно один раз — инъекция не одно-фактна",
			injectFile, injectFrom)
	}
	f.orig, f.inject = orig, []byte(strings.Replace(string(orig), injectFrom, injectTo, 1))
	twinSnap := snapshot(t, cp)
	f.write(t, f.inject)
	injTpl := probeTemplateFromTree(t, cp)
	if spec.SetFingerprint(spec.SetOf(injTpl)) != spec.SetFingerprint(spec.SetOf(twinTpl)) {
		t.Fatalf("ФИКСТУРА: правка тела сменила набор ревизии — краснеть мог бы revision.yaml, а не сборка")
	}
	if d := delta(twinSnap, snapshot(t, cp)); len(d) != 1 || d[0] != injectFile {
		t.Fatalf("ФИКСТУРА: отрицание отличается от близнеца не одним фактом: %v", d)
	}
	f.write(t, f.orig)
	if d := delta(twinSnap, snapshot(t, cp)); len(d) != 0 {
		t.Fatalf("ФИКСТУРА: близнец не восстановлен: %v", d)
	}
	t.Logf("NTF1-G17 фикстура: файлов в копии %d · шаблон %s/%s ревизии %d · дельта отрицания — 1 файл (%s)",
		files, probeNamespace, probeTemplate, twinTpl.Revision.Number, injectFile)
	return f
}

func (f *g17Fixture) write(t *testing.T, data []byte) {
	t.Helper()
	if err := os.WriteFile(f.injectPath, data, 0o600); err != nil {
		t.Fatalf("ФИКСТУРА: %v", err)
	}
}

// g17Scenario — NTF1-G17 над копией: находки (пусто — сошлось). Первая
// находка «КРАСНЫЙ: цели нет» останавливает сценарий: спрашивать дальше
// некого.
func g17Scenario(t *testing.T, f *g17Fixture) []string {
	t.Helper()
	var bad []string
	for _, target := range []string{"bundle", "bundle-check"} {
		r := run(t, f.svc, nil, "make", "-n", target)
		if targetAbsent(r, target) {
			return append(bad, fmt.Sprintf("КРАСНЫЙ: у services/notify/Makefile нет цели %q с рецептом — "+
				"сборки шаблонов нет: %s", target, r))
		}
		if r.code != 0 {
			return append(bad, fmt.Sprintf("`make -n %s` — код %d: %s", target, r.code, r))
		}
	}

	// Близнец: сборка свежая.
	twinSnap := snapshot(t, f.cp)
	if r := run(t, f.svc, nil, "make", "bundle-check"); r.code != 0 {
		bad = append(bad, fmt.Sprintf("близнец: на дереве как есть bundle-check обязан быть зелёным: %s", r))
	}
	if r := run(t, f.svc, nil, "make", "bundle"); r.code != 0 {
		return append(bad, fmt.Sprintf("близнец: bundle на дереве как есть: %s", r))
	}
	if d := delta(twinSnap, snapshot(t, f.cp)); len(d) != 0 {
		bad = append(bad, fmt.Sprintf("близнец: bundle на свежей сборке изменил файлы — "+
			"сборка в дереве не равна пересборке: %v", d))
	}

	// Отрицание: шаблон изменён, bundle не выполнен.
	f.write(t, f.inject)
	stale := run(t, f.svc, nil, "make", "bundle-check")
	if stale.code == 0 {
		bad = append(bad, fmt.Sprintf("шаблон %s изменён без bundle, а bundle-check зелёный: %s", probeTemplate, stale))
	}
	for _, name := range []string{probeTemplate, probeNamespace} {
		if stale.code != 0 && !strings.Contains(stale.output, name) {
			bad = append(bad, fmt.Sprintf("красный bundle-check не называет %q (шаблон и источник обязательны): %s",
				name, stale))
		}
	}

	// После bundle — зелёный; пишет шаг только в services/notify и не в
	// исходники шаблонов.
	before := snapshot(t, f.cp)
	if r := run(t, f.svc, nil, "make", "bundle"); r.code != 0 {
		return append(bad, fmt.Sprintf("bundle после правки шаблона: %s", r))
	}
	written := delta(before, snapshot(t, f.cp))
	if len(written) == 0 {
		bad = append(bad, "bundle после правки шаблона не изменил ни одного файла — сборка не зависит от содержимого шаблона")
	}
	for _, p := range written {
		if !strings.HasPrefix(p, notifyDir+"/") || strings.Contains(p, "/notifications/") {
			bad = append(bad, fmt.Sprintf("bundle записал %s — вне services/notify либо в исходник шаблона", p))
		}
	}
	if r := run(t, f.svc, nil, "make", "bundle-check"); r.code != 0 {
		bad = append(bad, fmt.Sprintf("после bundle bundle-check обязан быть зелёным: %s", r))
	}
	t.Logf("NTF1-G17: bundle переписал %d файлов: %v", len(written), written)
	return bad
}

// controlMakefile — законная реализация целей bundle и bundle-check в копии
// (ведомость: отпечаток каждого файла каталога шаблонов с пространством),
// либо — при dead — bundle-check, не проверяющий ничего. Нужна, чтобы
// сценарий был доказан в обе стороны ДО вопроса к испытуемому.
func controlMakefile(dead bool) string {
	check := `	@cd notifications && find . -type f | LC_ALL=C sort | xargs sha256sum | sed 's|^|notify-probe |' > ../ctl.ledger.new
	@if ! cmp -s ctl.ledger ctl.ledger.new; then diff ctl.ledger ctl.ledger.new; rm -f ctl.ledger.new; echo "сборка устарела"; exit 1; fi
	@rm -f ctl.ledger.new
`
	if dead {
		check = "\t@true\n"
	}
	return `
.PHONY: bundle bundle-check
bundle:
	@cd notifications && find . -type f | LC_ALL=C sort | xargs sha256sum | sed 's|^|notify-probe |' > ../ctl.ledger
bundle-check:
` + check
}

func installControl(t *testing.T, f *g17Fixture, dead bool) {
	t.Helper()
	mk := filepath.Join(f.svc, "Makefile")
	data, err := os.ReadFile(mk) // #nosec G304
	if err != nil {
		t.Fatalf("ФИКСТУРА: %v", err)
	}
	if err := os.WriteFile(mk, append(data, controlMakefile(dead)...), 0o600); err != nil {
		t.Fatalf("ФИКСТУРА: %v", err)
	}
	if r := run(t, f.svc, nil, "make", "bundle"); r.code != 0 {
		t.Fatalf("ФИКСТУРА: контрольная сборка: %s", r)
	}
}

func TestBundle_NTF1G17_StaleBuildIsRedAndRebuildIsGreen(t *testing.T) {
	if testing.Short() {
		t.Skip("NTF1-G17: копия дерева и сборка — вне -short")
	}
	root := repoRoot(t)
	for _, tool := range []string{"make", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("ФИКСТУРА: %s не найден: %v", tool, err)
		}
	}

	// ── фикстура: сценарий доказан в обе стороны на контрольных целях ──────
	lawful := newG17Fixture(t, root)
	installControl(t, lawful, false)
	if bad := g17Scenario(t, lawful); len(bad) != 0 {
		t.Fatalf("ФИКСТУРА: сценарий G17 краснеет на законных контрольных целях — проба не годна:\n%s",
			strings.Join(bad, "\n"))
	}
	dead := newG17Fixture(t, root)
	installControl(t, dead, true)
	if bad := g17Scenario(t, dead); len(bad) == 0 {
		t.Fatalf("ФИКСТУРА: сценарий G17 зелёный на bundle-check, который ничего не проверяет — проба не годна")
	}
	t.Logf("NTF1-G17 фикстура: законные контрольные цели — сошлось; мёртвый bundle-check — красный")

	// ── испытуемый: Makefile службы как есть ──────────────────────────────
	subject := newG17Fixture(t, root)
	for _, b := range g17Scenario(t, subject) {
		t.Errorf("NTF1-G17 %s", b)
	}
}

// ── порт deliver.Build: прод-реализация ────────────────────────────────────

// buildCandidates — экспортируемые функции не-тестовых файлов пакета dir
// без параметров с результатом T либо (T, error). Пакета нет — nil, false.
func buildCandidates(t *testing.T, dir string) (names []string, twoResults map[string]bool, pkgFiles int) {
	t.Helper()
	twoResults = map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("ФИКСТУРА: %v", err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("разбор %s: %v", n, err)
		}
		pkgFiles++
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() || fn.Type.TypeParams != nil {
				continue
			}
			if fn.Type.Params != nil && fn.Type.Params.NumFields() > 0 {
				continue
			}
			res := fn.Type.Results
			if res == nil {
				continue
			}
			switch res.NumFields() {
			case 1:
				names = append(names, fn.Name.Name)
			case 2:
				if id, ok := res.List[len(res.List)-1].Type.(*ast.Ident); ok && id.Name == "error" {
					names = append(names, fn.Name.Name)
					twoResults[fn.Name.Name] = true
				}
			}
		}
	}
	sort.Strings(names)
	return names, twoResults, pkgFiles
}

// lookup — один вопрос к сборке и её ответ.
type lookup struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Found     bool   `json:"found"`
	TplName   string `json:"tplName"`
	Class     string `json:"class"`
	Revision  int    `json:"revision"`
	HasRev    bool   `json:"hasRev"`
}

// queries — положительный вопрос и два отрицания, каждое меняет один факт
// против положительного: имя шаблона; пространство (шаблон ищется как
// `<пространство ленты>/<имя>`, Р6).
var queries = [][2]string{
	{probeNamespace, probeTemplate},
	{probeNamespace, probeTemplate + "-absent"},
	{"kaname", probeTemplate},
}

// probeProgram — программа, зовущая конструктор pkgPath.fn, присваивающая
// результат deliver.Build и печатающая ответы на queries JSON-строкой.
// Не присваивается — программа не компилируется: кандидат не сборка.
func probeProgram(pkgPath, fn string, withErr bool) string {
	call := "v := b." + fn + "()"
	if withErr {
		call = "v, err := b." + fn + "()\n\tif err != nil {\n\t\tfmt.Fprintln(os.Stderr, \"CONSTRUCTOR-ERROR:\", err)\n\t\tos.Exit(3)\n\t}"
	}
	var q strings.Builder
	for _, x := range queries {
		fmt.Fprintf(&q, "\t\t{%q, %q},\n", x[0], x[1])
	}
	return `package main

import (
	"encoding/json"
	"fmt"
	"os"

	b "` + pkgPath + `"
	"` + deliverPkgPath + `"
)

type lookup struct {
	Namespace string ` + "`json:\"namespace\"`" + `
	Name      string ` + "`json:\"name\"`" + `
	Found     bool   ` + "`json:\"found\"`" + `
	TplName   string ` + "`json:\"tplName\"`" + `
	Class     string ` + "`json:\"class\"`" + `
	Revision  int    ` + "`json:\"revision\"`" + `
	HasRev    bool   ` + "`json:\"hasRev\"`" + `
}

func main() {
	` + call + `
	var build deliver.Build = v
	qs := [][2]string{
` + q.String() + `	}
	out := []lookup{}
	for _, x := range qs {
		l := lookup{Namespace: x[0], Name: x[1]}
		if tpl, ok := build.Template(x[0], x[1]); ok && tpl != nil {
			l.Found, l.TplName, l.Class = true, tpl.Name, string(tpl.Class)
			l.Revision, l.HasRev = tpl.Revision.Number, tpl.HasRevision
		}
		out = append(out, l)
	}
	data, err := json.Marshal(out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(4)
	}
	fmt.Println("LOOKUPS:" + string(data))
}
`
}

// controlPackage — законный близнец испытуемого в оверлее: сборка из одного
// шаблона дерева, прочитанного настоящим загрузчиком spec. Нужен, чтобы
// программа-проба, оверлей и разбор ответа были доказаны ДО вопроса к
// испытуемому.
func controlPackage(root string) string {
	return `package n9control

import (
	"github.com/PRO-Robotech/corelib/notify/spec"
)

type Build struct{ m map[string]*spec.Template }

func (b *Build) Template(namespace, name string) (*spec.Template, bool) {
	t, ok := b.m[namespace+"/"+name]
	return t, ok
}

func New() (*Build, error) {
	cat, _, err := spec.Load(` + fmt.Sprintf("%q", filepath.Join(root, "services/notify/notifications")) + `)
	if err != nil {
		return nil, err
	}
	b := &Build{m: map[string]*spec.Template{}}
	for i := range cat.Templates {
		b.m["` + probeNamespace + `/"+cat.Templates[i].Name] = &cat.Templates[i]
	}
	return b, nil
}
`
}

// runProbe собирает и исполняет программу-пробу оверлеем поверх root.
// Возвращает ответы и признак «не компилируется» (кандидат не сборка).
func runProbe(t *testing.T, root string, files map[string]string) ([]lookup, runResult, bool) {
	t.Helper()
	dir := t.TempDir()
	repl := map[string]string{}
	i := 0
	for rel, src := range files {
		p := filepath.Join(dir, fmt.Sprintf("f%d.go", i))
		i++
		if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
			t.Fatalf("ФИКСТУРА: оверлей: %v", err)
		}
		repl[filepath.Join(root, filepath.FromSlash(rel))] = p
	}
	body, _ := json.Marshal(map[string]any{"Replace": repl})
	ov := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(ov, body, 0o600); err != nil {
		t.Fatalf("ФИКСТУРА: оверлей: %v", err)
	}
	r := run(t, root, nil, "go", "run", "-overlay", ov, "./"+bundlePkgDir+"/n9probe")
	if r.code != 0 {
		// Отказ компиляции программы-пробы — кандидат не присваивается порту.
		notBuild := strings.Contains(r.output, "n9probe") && !strings.Contains(r.output, "CONSTRUCTOR-ERROR:")
		return nil, r, notBuild
	}
	var got []lookup
	for _, line := range strings.Split(r.output, "\n") {
		if rest, ok := strings.CutPrefix(line, "LOOKUPS:"); ok {
			if err := json.Unmarshal([]byte(rest), &got); err != nil {
				t.Fatalf("ФИКСТУРА: ответ программы-пробы не разбирается: %v\n%s", err, r)
			}
		}
	}
	if len(got) != len(queries) {
		t.Fatalf("ФИКСТУРА: программа-проба ответила на %d вопросов из %d: %s", len(got), len(queries), r)
	}
	return got, r, false
}

// judgeLookups — исход вопросов против шаблона дерева want; пустой — сошлось.
func judgeLookups(got []lookup, want spec.Template) []string {
	var bad []string
	pos := got[0]
	if !pos.Found {
		bad = append(bad, fmt.Sprintf("Template(%q, %q) — не найден", pos.Namespace, pos.Name))
	} else {
		if pos.TplName != want.Name {
			bad = append(bad, fmt.Sprintf("имя %q, в дереве %q", pos.TplName, want.Name))
		}
		if pos.Class != string(want.Class) {
			bad = append(bad, fmt.Sprintf("класс %q, в дереве %q", pos.Class, want.Class))
		}
		if !pos.HasRev || pos.Revision != want.Revision.Number {
			bad = append(bad, fmt.Sprintf("ревизия %d (есть: %v), в revision.yaml %d",
				pos.Revision, pos.HasRev, want.Revision.Number))
		}
	}
	for _, neg := range got[1:] {
		if neg.Found {
			bad = append(bad, fmt.Sprintf("Template(%q, %q) найден — шаблона с таким пространством и именем в дереве нет",
				neg.Namespace, neg.Name))
		}
	}
	return bad
}

func TestBundle_NTF1G17_ProdBuildServesTheTreeTemplate(t *testing.T) {
	if testing.Short() {
		t.Skip("N9: сборка программы-пробы — вне -short")
	}
	root := repoRoot(t)
	want := probeTemplateFromTree(t, root)

	// ── фикстура: порт объявлен, программа-проба доказана на близнеце ──────
	deliverSrc, err := os.ReadFile(filepath.Join(root, "services/notify/internal/deliver/deliver.go")) // #nosec G304
	if err != nil {
		t.Fatalf("ФИКСТУРА: %v", err)
	}
	if !strings.Contains(string(deliverSrc), "type Build interface") {
		t.Fatalf("ФИКСТУРА: порта deliver.Build в deliver.go нет — вопрос к сборке не задать")
	}
	ctlPath := bundlePkgPath + "/n9control"
	ctl, r, notBuild := runProbe(t, root, map[string]string{
		bundlePkgDir + "/n9control/control.go": controlPackage(root),
		bundlePkgDir + "/n9probe/main.go":      probeProgram(ctlPath, "New", true),
	})
	if ctl == nil {
		t.Fatalf("ФИКСТУРА: программа-проба на законном близнеце не исполнилась (не компилируется: %v): %s", notBuild, r)
	}
	if bad := judgeLookups(ctl, want); len(bad) != 0 {
		t.Fatalf("ФИКСТУРА: законный близнец не сошёлся — проба не годна: %v", bad)
	}
	// Отрицание близнеца: сборка, которая не находит ничего, обязана краснеть.
	empty := make([]lookup, len(ctl))
	copy(empty, ctl)
	empty[0].Found = false
	if len(judgeLookups(empty, want)) == 0 {
		t.Fatalf("ФИКСТУРА: суд ответов не краснеет на пустой сборке")
	}
	t.Logf("N9 фикстура: близнец сошёлся на %d вопросах (%s/%s ревизии %d)",
		len(ctl), probeNamespace, probeTemplate, want.Revision.Number)

	// ── испытуемый ────────────────────────────────────────────────────────
	names, withErr, pkgFiles := buildCandidates(t, filepath.Join(root, bundlePkgDir))
	if pkgFiles == 0 {
		t.Fatalf("N9 КРАСНЫЙ: в %s нет ни одного не-тестового файла Go — прод-реализации порта deliver.Build нет", bundlePkgDir)
	}
	var builds []string
	for _, fn := range names {
		got, r, notBuild := runProbe(t, root, map[string]string{
			bundlePkgDir + "/n9probe/main.go": probeProgram(bundlePkgPath, fn, withErr[fn]),
		})
		if got == nil {
			if notBuild {
				continue
			}
			t.Errorf("N9: конструктор bundle.%s не исполнился: %s", fn, r)
			builds = append(builds, fn)
			continue
		}
		builds = append(builds, fn)
		if bad := judgeLookups(got, want); len(bad) != 0 {
			t.Errorf("N9: сборка bundle.%s не отдаёт шаблон дерева: %v", fn, bad)
		}
	}
	switch len(builds) {
	case 0:
		t.Fatalf("N9 КРАСНЫЙ: в пакете %s (не-тестовых файлов %d, функций-кандидатов %d: %v) нет конструктора, "+
			"чей результат присваивается deliver.Build", bundlePkgDir, pkgFiles, len(names), names)
	case 1:
		t.Logf("N9: прод-сборка — bundle.%s", builds[0])
	default:
		t.Errorf("N9: конструкторов сборки %d (%v) — путь к сборке обязан быть один", len(builds), builds)
	}
}
