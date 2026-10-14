// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build helmcharts

// packaged_subchart_tree_test.go — РЕНДЕР УМБРЕЛЛЫ СУДИТ ДЕРЕВО ЧАРТА, А НЕ
// ОТСТАВШИЙ АРХИВ (kacho#3028, P5).
//
// Сабчарты, которые умбрелла берёт из дерева (`repository: file://…`), helm
// рендерит не из дерева, а из архива `charts/<имя>-<версия>.tgz`, который
// упаковывает `make -C deploy helm-deps`. Архив, упакованный до правки шаблона,
// рендерит прежний шаблон: инъекция в дерево (Local → Cluster у службы раздачи,
// строка X-Forwarded-For, снятая в одной полосе) рендерным гейтом не видна, и
// он зеленеет на том, чего в дереве уже нет, — либо краснеет на том, что в
// дереве уже исправлено. Поэтому рендерные гейты адреса клиента начинают с
// предпосылки: каждый упакованный сабчарт дерева совпадает с деревом
// побайтово по шаблонам и values.yaml. Расхождение — не находка о продукте, а
// несозданное условие: вердикта у гейта нет, пока архив не переупакован.
package deploy_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"gopkg.in/yaml.v3"
)

// judgePackagedAgainstTree — расхождения архива сабчарта `name` с его деревом.
// Чистая функция. Судятся шаблоны (`templates/**`) и `values.yaml` — то, из
// чего helm рендерит: файл дерева, которого нет в архиве, файл архива, которого
// нет в дереве, и файл с иным содержимым. Chart.yaml и Chart.lock не судятся:
// упаковка их переписывает. checked — число сверенных файлов (знаменатель).
func judgePackagedAgainstTree(tgz []byte, name string, tree fs.FS) (findings []string, checked int, err error) {
	judged := func(p string) bool { return p == "values.yaml" || strings.HasPrefix(p, "templates/") }
	packed := map[string][]byte{}
	zr, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		return nil, 0, fmt.Errorf("архив не gzip: %w", err)
	}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("архив не читается: %w", err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		rel, ok := strings.CutPrefix(h.Name, name+"/")
		if !ok || !judged(rel) {
			continue
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			return nil, 0, fmt.Errorf("%s в архиве не читается: %w", h.Name, err)
		}
		packed[rel] = b
	}
	inTree := map[string]bool{}
	walkErr := fs.WalkDir(tree, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !judged(p) {
			return nil
		}
		inTree[p] = true
		b, err := fs.ReadFile(tree, p)
		if err != nil {
			return err
		}
		checked++
		got, ok := packed[p]
		switch {
		case !ok:
			findings = append(findings, fmt.Sprintf("%s: файла дерева %s в архиве нет", name, p))
		case !bytes.Equal(got, b):
			findings = append(findings, fmt.Sprintf("%s: %s в архиве отличается от дерева", name, p))
		}
		return nil
	})
	if walkErr != nil {
		return nil, 0, fmt.Errorf("дерево %s не обходится: %w", name, walkErr)
	}
	for p := range packed {
		if !inTree[p] {
			findings = append(findings, fmt.Sprintf("%s: файла архива %s в дереве нет", name, p))
		}
	}
	sort.Strings(findings)
	if checked == 0 {
		findings = append(findings, fmt.Sprintf("%s: в дереве нет ни шаблонов, ни values.yaml — сверять нечего", name))
	}
	return findings, checked, nil
}

// packagedTreeSubchart — сабчарт умбреллы, взятый из дерева и упакованный.
type packagedTreeSubchart struct {
	Name, Tree, Archive string
}

// umbrellaTreeSubcharts — сабчарты Chart.yaml умбреллы с `repository: file://…`
// и их архивы в charts/. Сабчарт из дерева без архива — отказ: умбрелла без
// него не рендерится тем, что в дереве.
func umbrellaTreeSubcharts() ([]packagedTreeSubchart, error) {
	raw, err := os.ReadFile(filepath.Join(umbrellaDir, "Chart.yaml"))
	if err != nil {
		return nil, err
	}
	var chart struct {
		Dependencies []struct {
			Name       string `yaml:"name"`
			Repository string `yaml:"repository"`
		} `yaml:"dependencies"`
	}
	if err := yaml.Unmarshal(raw, &chart); err != nil {
		return nil, fmt.Errorf("Chart.yaml умбреллы не разбирается: %w", err)
	}
	var out []packagedTreeSubchart
	seen := map[string]bool{}
	for _, d := range chart.Dependencies {
		rel, ok := strings.CutPrefix(d.Repository, "file://")
		if !ok || seen[d.Name] {
			continue
		}
		seen[d.Name] = true
		arch, _ := filepath.Glob(filepath.Join(umbrellaDir, "charts", d.Name+"-*.tgz"))
		if len(arch) != 1 {
			return nil, fmt.Errorf("сабчарт %s из дерева (%s): архивов в charts/ %d, ожидался один — "+
				"`make -C deploy helm-deps`", d.Name, rel, len(arch))
		}
		out = append(out, packagedTreeSubchart{
			Name: d.Name, Tree: filepath.Join(umbrellaDir, filepath.FromSlash(path.Clean(rel))), Archive: arch[0],
		})
	}
	return out, nil
}

var (
	packagedTreeOnce sync.Once
	packagedTreeErr  error
	packagedTreeSeen int
)

// requireUmbrellaPackagedFromTree — предпосылка рендерных гейтов: архивы
// сабчартов из дерева совпадают с деревом. Иначе вердикта нет.
func requireUmbrellaPackagedFromTree(t *testing.T) {
	t.Helper()
	packagedTreeOnce.Do(func() {
		subs, err := umbrellaTreeSubcharts()
		if err != nil {
			packagedTreeErr = err
			return
		}
		var all []string
		for _, s := range subs {
			tgz, err := os.ReadFile(s.Archive)
			if err != nil {
				packagedTreeErr = err
				return
			}
			f, n, err := judgePackagedAgainstTree(tgz, s.Name, os.DirFS(s.Tree))
			if err != nil {
				packagedTreeErr = err
				return
			}
			packagedTreeSeen += n
			all = append(all, f...)
		}
		if len(subs) == 0 {
			all = append(all, "у умбреллы нет ни одного сабчарта из дерева — сверять нечего")
		}
		if len(all) != 0 {
			packagedTreeErr = fmt.Errorf("%s", strings.Join(all, "\n"))
		}
	})
	if packagedTreeErr != nil {
		t.Fatalf("архивы сабчартов отстали от дерева — рендер судил бы прежний шаблон, условие не создано "+
			"(`make -C deploy helm-deps`):\n%v", packagedTreeErr)
	}
}

func TestUmbrellaRendersTheTreeNotAStaleArchive(t *testing.T) {
	requireUmbrellaPackagedFromTree(t)
	t.Logf("перепись: файлов шаблонов и values сверено %d", packagedTreeSeen)
}

// Судья способен упасть и смолчать: архив упаковывается из НАСТОЯЩЕГО дерева
// чарта консоли; близнец — архив как есть — молчит, каждая инъекция меняет
// один факт.
func TestPackagedAgainstTreeJudgement_CanFailAndStaysSilent(t *testing.T) {
	const name = "uif"
	subs, err := umbrellaTreeSubcharts()
	if err != nil {
		t.Fatal(err)
	}
	var tree string
	for _, s := range subs {
		if s.Name == name {
			tree = s.Tree
		}
	}
	if tree == "" {
		t.Fatalf("сабчарта %s из дерева у умбреллы нет — предпосылка инъекций", name)
	}
	const target = "templates/service-public.yaml"
	pack := func(t *testing.T, edit func(map[string][]byte)) []byte {
		t.Helper()
		files := map[string][]byte{}
		err := fs.WalkDir(os.DirFS(tree), ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := os.ReadFile(filepath.Join(tree, p))
			files[p] = b
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := files[target]; !ok {
			t.Fatalf("%s нет в дереве — предпосылка инъекций", target)
		}
		if edit != nil {
			edit(files)
		}
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		tw := tar.NewWriter(zw)
		names := make([]string, 0, len(files))
		for p := range files {
			names = append(names, p)
		}
		sort.Strings(names)
		for _, p := range names {
			if err := tw.WriteHeader(&tar.Header{Name: name + "/" + p, Mode: 0o644, Size: int64(len(files[p])),
				Typeflag: tar.TypeReg}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write(files[p]); err != nil {
				t.Fatal(err)
			}
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	cases := []struct {
		name    string
		edit    func(map[string][]byte)
		mustSay string
	}{
		{name: "законный близнец — молчит"},
		{
			name: "архив упакован до правки шаблона",
			edit: func(f map[string][]byte) {
				f[target] = bytes.Replace(f[target], []byte("externalTrafficPolicy: Local"),
					[]byte("externalTrafficPolicy: Cluster"), 1)
			},
			mustSay: target + " в архиве отличается от дерева",
		},
		{
			name:    "шаблон дерева в архив не попал",
			edit:    func(f map[string][]byte) { delete(f, target) },
			mustSay: "файла дерева " + target + " в архиве нет",
		},
		{
			name:    "в архиве шаблон, снятый с дерева",
			edit:    func(f map[string][]byte) { f["templates/retired.yaml"] = []byte("kind: Service\n") },
			mustSay: "файла архива templates/retired.yaml в дереве нет",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, n, err := judgePackagedAgainstTree(pack(t, c.edit), name, os.DirFS(tree))
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				t.Fatal("ни одного файла не сверено")
			}
			if c.mustSay == "" {
				if len(got) != 0 {
					t.Fatalf("законный близнец не молчит: %v", got)
				}
				return
			}
			if !strings.Contains(strings.Join(got, "\n"), c.mustSay) {
				t.Fatalf("ни одна находка не называет %q: %v", c.mustSay, got)
			}
		})
	}
	t.Run("нулевой обход", func(t *testing.T) {
		got, _, err := judgePackagedAgainstTree(pack(t, nil), name, os.DirFS(t.TempDir()))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(got, "\n"), "сверять нечего") {
			t.Fatalf("пустое дерево не названо: %v", got)
		}
	})
}
