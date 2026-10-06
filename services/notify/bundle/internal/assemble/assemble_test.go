// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// assemble_test.go — отказы шага сборки и сверка в обе стороны (короткие
// пробы). Дерево — синтетическое во временном каталоге: копия каталога
// шаблонов notify из рабочего дерева; каждая порча меняет один факт против
// близнеца — того же дерева без порчи.
package assemble

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/treecorpus"
)

const treeTemplates = "services/notify/notifications"

// repoRoot — корень репозитория от каталога пакета.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../../../..")
	if err != nil {
		t.Fatalf("ФИКСТУРА: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("ФИКСТУРА: %s без go.mod", root)
	}
	return root
}

// syntheticTree — временное дерево с копией каталога шаблонов notify. Состав
// копии — состав индекса (treecorpus), а не диска: проба судит коммит, а не
// рабочий каталог прогоняющего.
func syntheticTree(t *testing.T) string {
	t.Helper()
	src := filepath.Join(repoRoot(t), treeTemplates)
	files, err := treecorpus.Under(src)
	if err != nil {
		t.Fatalf("ФИКСТУРА: состав %s по индексу: %v", treeTemplates, err)
	}
	dst := t.TempDir()
	for _, p := range files {
		rel, err := filepath.Rel(src, p)
		if err != nil {
			t.Fatalf("ФИКСТУРА: %v", err)
		}
		to := filepath.Join(dst, treeTemplates, rel)
		if err := os.MkdirAll(filepath.Dir(to), 0o750); err != nil {
			t.Fatalf("ФИКСТУРА: %v", err)
		}
		data, err := os.ReadFile(p) // #nosec G304 -- путь из индекса дерева
		if err != nil {
			t.Fatalf("ФИКСТУРА: %v", err)
		}
		if err := os.WriteFile(to, data, 0o600); err != nil {
			t.Fatalf("ФИКСТУРА: %v", err)
		}
	}
	return dst
}

func treeSources() []Source {
	return []Source{{Namespace: "notify-probe", Dir: treeTemplates}}
}

func TestAssemble_WorkingTreeIsDeterministic(t *testing.T) {
	root := repoRoot(t)
	a, err := Assemble(root, Sources())
	if err != nil {
		t.Fatalf("сборка рабочего дерева: %v", err)
	}
	b, err := Assemble(root, Sources())
	if err != nil {
		t.Fatalf("повторная сборка: %v", err)
	}
	if a.Templates == 0 || len(a.Files) == 0 {
		t.Fatalf("сборка пуста: шаблонов %d, файлов %d", a.Templates, len(a.Files))
	}
	if len(a.Files) != len(b.Files) {
		t.Fatalf("две сборки одного дерева: файлов %d и %d", len(a.Files), len(b.Files))
	}
	for i := range a.Files {
		if a.Files[i].Path != b.Files[i].Path || string(a.Files[i].Data) != string(b.Files[i].Data) {
			t.Errorf("две сборки одного дерева расходятся в %s", a.Files[i].Path)
		}
	}
	perDir := map[string]int{}
	for _, f := range a.Files {
		perDir[strings.SplitN(f.Path, "/", 2)[0]]++
	}
	for _, d := range outputDirs {
		if perDir[d] == 0 {
			t.Errorf("каталог вывода %s пуст", d)
		}
	}
	t.Logf("сборка рабочего дерева: источников %d · шаблонов %d · файлов %d (%v)", a.Sources, a.Templates, len(a.Files), perDir)
}

// TestAssemble_WorkingTreeBuildIsFresh — NTF1-G17 на рабочем дереве той же
// функцией сверки, что `make -C services/notify bundle-check`: записанная
// сборка равна пересборке. Красный — шаблон правлен без `bundle`.
func TestAssemble_WorkingTreeBuildIsFresh(t *testing.T) {
	root := repoRoot(t)
	out, err := Assemble(root, Sources())
	if err != nil {
		t.Fatalf("сборка рабочего дерева: %v", err)
	}
	bundleDir := filepath.Join(root, "services/notify/bundle")
	findings, inspected, err := Check(bundleDir, out)
	if err != nil {
		t.Fatalf("сверка: %v", err)
	}
	t.Logf("NTF1-G17 рабочее дерево: файлов сборки ожидается %d · осмотрено %d · расхождений %d",
		len(out.Files), inspected, len(findings))
	if inspected == 0 {
		t.Fatalf("осмотрено 0 файлов сборки — пустой обход не вердикт")
	}
	for _, f := range findings {
		t.Errorf("сборка устарела — make -C services/notify bundle: %s", f)
	}
	// Встраивается то, что лежит в КОММИТЕ: файл сборки вне индекса есть у
	// прогоняющего и нет в образе.
	tracked := map[string]bool{}
	for _, d := range outputDirs {
		files, err := treecorpus.Under(filepath.Join(bundleDir, d))
		if err != nil {
			t.Fatalf("состав %s по индексу: %v", d, err)
		}
		for _, p := range files {
			rel, err := filepath.Rel(bundleDir, p)
			if err != nil {
				t.Fatalf("%v", err)
			}
			tracked[filepath.ToSlash(rel)] = true
		}
	}
	for _, f := range out.Files {
		if !tracked[f.Path] {
			t.Errorf("файл сборки %s не в индексе — git add services/notify/bundle", f.Path)
		}
		delete(tracked, f.Path)
	}
	for p := range tracked {
		t.Errorf("в индексе файл сборки %s, которого пересборка не выводит", p)
	}
}

func TestAssemble_RefusesWhatItCannotBuild(t *testing.T) {
	if _, err := Assemble(syntheticTree(t), treeSources()); err != nil {
		t.Fatalf("близнец: синтетическое дерево без порчи не собралось: %v", err)
	}
	cases := []struct {
		name    string
		spoil   func(t *testing.T, root string)
		sources []Source
		reason  string
	}{
		{"источник без шаблонов", func(t *testing.T, root string) {
			if err := os.RemoveAll(filepath.Join(root, treeTemplates)); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(root, treeTemplates), 0o750); err != nil {
				t.Fatal(err)
			}
		}, treeSources(), "шаблонов 0"},
		{"шаблон без ревизии", func(t *testing.T, root string) {
			removeWithPrefix(t, root, "revision.")
		}, treeSources(), "нет ревизии"},
		{"пространство дважды", func(*testing.T, string) {}, append(treeSources(), Source{Namespace: "notify-probe", Dir: treeTemplates}), "дважды"},
		{"таблица пуста", func(*testing.T, string) {}, nil, "таблица источников пуста"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := syntheticTree(t)
			tc.spoil(t, root)
			out, err := Assemble(root, tc.sources)
			if err == nil {
				t.Fatalf("сборка принята: файлов %d", len(out.Files))
			}
			if !strings.Contains(err.Error(), tc.reason) {
				t.Errorf("отказ %q не называет %q", err, tc.reason)
			}
		})
	}
}

// TestAssemble_CopiesWhatTheValidatorReads — файл на «.» в каталоге шаблона
// валидатор пропускает, и в сборку он не идёт; близнец — дерево без него.
func TestAssemble_CopiesWhatTheValidatorReads(t *testing.T) {
	root := syntheticTree(t)
	twin, err := Assemble(root, treeSources())
	if err != nil {
		t.Fatalf("ФИКСТУРА: %v", err)
	}
	dirs, err := os.ReadDir(filepath.Join(root, treeTemplates))
	if err != nil || len(dirs) == 0 {
		t.Fatalf("ФИКСТУРА: каталогов шаблонов %d, %v", len(dirs), err)
	}
	if err := os.WriteFile(filepath.Join(root, treeTemplates, dirs[0].Name(), ".editor.swp"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Assemble(root, treeSources())
	if err != nil {
		t.Fatalf("сборка с файлом на «.»: %v", err)
	}
	if len(got.Files) != len(twin.Files) {
		t.Errorf("файлов сборки %d, у близнеца %d — файл на «.» попал в сборку", len(got.Files), len(twin.Files))
	}
}

// removeWithPrefix удаляет единственный файл дерева, чьё имя начинается с
// prefix; не один — ФИКСТУРА.
func removeWithPrefix(t *testing.T, root, prefix string) {
	t.Helper()
	var hit []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasPrefix(d.Name(), prefix) {
			hit = append(hit, p)
		}
		return err
	})
	if len(hit) != 1 {
		t.Fatalf("ФИКСТУРА: файлов %q* в дереве %d, ждали 1", prefix, len(hit))
	}
	if err := os.Remove(hit[0]); err != nil {
		t.Fatal(err)
	}
}

func TestCheck_JudgesEveryFileBothWays(t *testing.T) {
	out, err := Assemble(syntheticTree(t), treeSources())
	if err != nil {
		t.Fatalf("ФИКСТУРА: %v", err)
	}
	fresh := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		if err := Write(dir, out); err != nil {
			t.Fatalf("ФИКСТУРА: %v", err)
		}
		return dir
	}
	twin := fresh(t)
	got, inspected, err := Check(twin, out)
	if err != nil || len(got) != 0 || inspected != len(out.Files) {
		t.Fatalf("близнец: находок %v, осмотрено %d из %d, %v", got, inspected, len(out.Files), err)
	}
	first := out.Files[0]
	cases := []struct {
		name  string
		spoil func(t *testing.T, dir string)
		path  string
		owner string
		why   string
	}{
		{"файл изменён", func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, first.Path), append(append([]byte{}, first.Data...), '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
		}, first.Path, first.Owner, "отличается"},
		{"файла нет", func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, first.Path)); err != nil {
				t.Fatal(err)
			}
		}, first.Path, first.Owner, "нет в сборке"},
		{"лишний эталон", func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, GoldenDir, "notify-probe", "gone.ru.eml"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, GoldenDir + "/notify-probe/gone.ru.eml", "notify-probe/gone", "лишний"},
		{"лишний шаблон каталога", func(t *testing.T, dir string) {
			p := filepath.Join(dir, CatalogDir, "kaname", "invite", "x.yaml")
			if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, CatalogDir + "/kaname/invite/x.yaml", "kaname/invite", "лишний"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := fresh(t)
			tc.spoil(t, dir)
			got, _, err := Check(dir, out)
			if err != nil {
				t.Fatalf("сверка: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("находок %d (%v), ждали 1", len(got), got)
			}
			f := got[0]
			if f.Path != tc.path || f.Owner != tc.owner || !strings.Contains(f.Reason, tc.why) {
				t.Errorf("находка %q, ждали путь %s, шаблон %s, причину %q", f, tc.path, tc.owner, tc.why)
			}
		})
	}
}

func TestWrite_DropsFilesOfARemovedTemplate(t *testing.T) {
	out, err := Assemble(syntheticTree(t), treeSources())
	if err != nil {
		t.Fatalf("ФИКСТУРА: %v", err)
	}
	dir := t.TempDir()
	stale := filepath.Join(dir, PreviewDir, "notify-probe", "gone.ru.html")
	if err := os.MkdirAll(filepath.Dir(stale), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "bundle.go")
	if err := os.WriteFile(keep, []byte("package bundle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(dir, out); err != nil {
		t.Fatalf("запись: %v", err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Errorf("файл снятого шаблона %s пережил запись сборки", stale)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("запись сборки тронула файл вне каталогов вывода: %v", err)
	}
	if got, _, err := Check(dir, out); err != nil || len(got) != 0 {
		t.Errorf("после записи сверка: %v, %v", got, err)
	}
}
