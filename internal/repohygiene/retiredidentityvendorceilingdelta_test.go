// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Пробы СУДА ПО ДОБАВЛЕННОМУ (#3002). Гейт потолка судил итог: прирост в одном
// месте прощался убылью в другом, и правка «+1 −6» проходила с итогом −5.
// Предмет — суждение о добавленной привязке: всякая привязка изменения, которой
// на базе нет ни на прежнем, ни на новом месте, краснит дерево, сколько бы
// привязок ни сняла та же правка.
//
// Мир — тот же синтетический репозиторий, что у проб базы ([vendorProbeWorld]),
// и суд — тот же путь гейта по дереву ([vendorVerdictAt]).

// vendorSixRemovableWorld — мир с третьим файлом `deploy/c.yaml` на шесть
// привязок, вершина линии `100`, и ветка `lane` от неё. Шесть строк — та убыль,
// которую правка задачи противопоставляла одной добавленной.
func vendorSixRemovableWorld(t *testing.T, lane string) string {
	t.Helper()
	root, _ := vendorProbeWorld(t)
	vendorGit(t, root, "switch", "--quiet", "100")
	vendorWriteAt(t, root, "deploy/c.yaml", []byte(vendorProbeFile("c", 6)))
	vendorGit(t, root, "add", "-A")
	vendorGit(t, root, "commit", "--quiet", "-m", "шесть снимаемых строк")
	vendorGit(t, root, "switch", "--quiet", "-c", lane)
	return root
}

// TestRetiredVendorCeiling_AddedLineIsRedEvenWhenRemovalIsLarger — ПРЕДИКАТ 1
// задачи #3002: одна правка добавляет одну привязку и снимает шесть. Итог ниже
// базы на пять, а добавленная строка — новая привязка: гейт краснеет и называет
// её координатой.
func TestRetiredVendorCeiling_AddedLineIsRedEvenWhenRemovalIsLarger(t *testing.T) {
	t.Parallel()
	root := vendorSixRemovableWorld(t, "9101")

	vendorWriteAt(t, root, "deploy/c.yaml", []byte(vendorOwnLine+"\n"))
	vendorWriteAt(t, root, "deploy/a.yaml", []byte(vendorProbeFile("a", 2)+vendorProbeLine+" # added\n"))
	vendorGit(t, root, "commit", "--quiet", "-am", "прирост 1 · убыль 6")

	v := vendorProbeVerdict(t, root)
	d := v.Deltas[vendorTreePlatform]
	if len(d.Removed) != 6 || d.Base-d.Head != 5 {
		t.Fatalf("фикстура: убыль %d · %d → %d, ждали убыль 6 и итог −5 — условие не создано",
			len(d.Removed), d.Base, d.Head)
	}
	if len(v.Findings) != 1 || v.Findings[0].Tree != vendorTreePlatform {
		t.Fatalf("добавленная привязка обязана краснить дерево платформы, сколько бы ни сняла "+
			"та же правка (прирост %d · убыль %d · итог %+d): находок %d %v",
			len(d.Added), len(d.Removed), d.Head-d.Base, len(v.Findings), v.Findings)
	}
	fd := v.Findings[0].Delta
	if fd == nil || len(fd.Added) != 1 || fd.Added[0].File != "deploy/a.yaml" || fd.Added[0].Line != 4 {
		t.Fatalf("находка обязана нести одну добавленную строку deploy/a.yaml:4, получено %+v", fd)
	}
	if text := v.Findings[0].String(); !strings.Contains(text, "deploy/a.yaml:4 · "+vendorAxisName) {
		t.Fatalf("текст находки обязан называть координату добавленной строки:\n%s", text)
	}
}

// TestRetiredVendorCeiling_PureRemovalIsSilent — ПРЕДИКАТ 2, законный близнец
// предиката 1: та же убыль шести, добавленной строки нет. Ровно один факт другой.
func TestRetiredVendorCeiling_PureRemovalIsSilent(t *testing.T) {
	t.Parallel()
	root := vendorSixRemovableWorld(t, "9102")

	vendorWriteAt(t, root, "deploy/c.yaml", []byte(vendorOwnLine+"\n"))
	vendorGit(t, root, "commit", "--quiet", "-am", "убыль 6")

	v := vendorProbeVerdict(t, root)
	d := v.Deltas[vendorTreePlatform]
	if len(d.Removed) != 6 || len(d.Added) != 0 {
		t.Fatalf("фикстура: убыль %d · прирост %d, ждали 6 · 0 — условие не создано", len(d.Removed), len(d.Added))
	}
	if len(v.Findings) != 0 {
		t.Fatalf("чистое снятие обязано молчать: %v", v.Findings)
	}
}

// TestRetiredVendorCeiling_MovedBindingIsSilent — ПРЕДИКАТ 3: перенос
// существующей привязки — та же привязка на новом месте, а не новая. Строка,
// перенесённая в другой файл, и файл, перенесённый в другой каталог, молчат.
//
// Близнец в обратную сторону — перенесённая строка с ДРУГИМ текстом: это уже
// новая привязка, и она краснеет так же, как добавленная.
func TestRetiredVendorCeiling_MovedBindingIsSilent(t *testing.T) {
	t.Parallel()

	moved := vendorProbeLine + " # c6"
	cases := []struct {
		name, lane string
		change     func(t *testing.T, root string)
		red        bool
	}{
		{"строка перенесена в другой файл", "9103", func(t *testing.T, root string) {
			vendorWriteAt(t, root, "deploy/c.yaml", []byte(strings.Replace(vendorProbeFile("c", 6), moved+"\n", "", 1)))
			vendorWriteAt(t, root, "deploy/a.yaml", []byte(moved+"\n"+vendorProbeFile("a", 2)))
			vendorGit(t, root, "commit", "--quiet", "-am", "перенос строки")
		}, false},
		{"файл перенесён в другой каталог", "9104", func(t *testing.T, root string) {
			vendorGit(t, root, "mv", "deploy/c.yaml", "deploy/moved/c.yaml")
			vendorGit(t, root, "commit", "--quiet", "-m", "перенос файла")
		}, false},
		{"перенесённая строка переписана", "9105", func(t *testing.T, root string) {
			vendorWriteAt(t, root, "deploy/c.yaml", []byte(strings.Replace(vendorProbeFile("c", 6), moved+"\n", "", 1)))
			vendorWriteAt(t, root, "deploy/a.yaml", []byte(moved+" rewritten\n"+vendorProbeFile("a", 2)))
			vendorGit(t, root, "commit", "--quiet", "-am", "перенос с правкой")
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := vendorSixRemovableWorld(t, tc.lane)
			if err := os.MkdirAll(filepath.Join(root, "deploy", "moved"), 0o750); err != nil {
				t.Fatal(err)
			}
			tc.change(t, root)

			v := vendorProbeVerdict(t, root)
			d := v.Deltas[vendorTreePlatform]
			if d.Base != d.Head {
				t.Fatalf("фикстура: %d → %d, перенос обязан сохранять число — условие не создано", d.Base, d.Head)
			}
			switch {
			case tc.red && len(v.Findings) != 1:
				t.Fatalf("переписанная строка — новая привязка и обязана краснеть: %v", v.Findings)
			case !tc.red && len(v.Findings) != 0:
				t.Fatalf("перенос существующей привязки обязан молчать: %v", v.Findings)
			}
		})
	}
}

// TestRetiredVendorCeiling_ReplacedTreeAtTheSamePinIsJudged — ПРЕДИКАТ 4: дерево
// службы доступа подменено директивой `replace` на изменённую копию, пин тот
// же, копия несёт одну привязку. База обязана браться по тому, что фактически
// подставлено на базе (здесь — по пину, подмены на базе нет), а не равной
// голове: иначе рост в подменённом дереве невидим.
//
// Близнецы: та же подмена на обоих концах — дерево то же, модуль не
// запрашивается; подмена на базе локальным каталогом — дерево базы не
// восстановимо, и это отказ, а не «то же дерево».
func TestRetiredVendorCeiling_ReplacedTreeAtTheSamePinIsJudged(t *testing.T) {
	t.Parallel()

	access := retiredVendorTreeModules[vendorTreeAccess]
	replacedHead := map[string]vendorTreeCorpus{vendorTreePlatform: vendorCorpusOf(
		map[string]string{"go.mod": "module synthetic\n"}, nil, nil, nil, 1)}
	for tree, c := range vendorProbePinned() {
		replacedHead[tree] = c
	}
	replacedHead[vendorTreeAccess] = vendorCorpusOf(map[string]string{
		"go.mod":        "module x\n",
		"deploy/x.yaml": vendorProbeLine + "\n",
	}, nil, nil, nil, 2)
	clean := vendorProbePinned()[vendorTreeAccess]

	withReplace := func(gomod []byte, target string) []byte {
		return append(append([]byte{}, gomod...), []byte("\nreplace "+access+" => "+target+"\n")...)
	}
	commitGoMod := func(t *testing.T, root string, body []byte) string {
		t.Helper()
		vendorGit(t, root, "switch", "--quiet", "100")
		vendorWriteAt(t, root, "go.mod", body)
		vendorGit(t, root, "commit", "--quiet", "-am", "go.mod базы")
		return vendorGit(t, root, "rev-parse", "HEAD")
	}

	t.Run("подмена только в изменении", func(t *testing.T) {
		t.Parallel()
		root, fork := vendorProbeWorld(t)
		gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
		if err != nil {
			t.Fatal(err)
		}
		var asked []string
		fetch := func(module, version string) (vendorTreeCorpus, error) {
			asked = append(asked, module+"@"+version)
			return clean, nil
		}
		base, hows, err := retiredVendorBaseCorpora(root, fork, replacedHead,
			withReplace(gomod, "../kaname-copy"), fetch)
		if err != nil {
			t.Fatalf("дерево базы не собрано: %v", err)
		}
		t.Logf("%s · запрошено %v", strings.Join(hows, " · "), asked)
		findings, _, _, _, err := judgeRetiredVendorAgainstBase(replacedHead, base)
		if err != nil {
			t.Fatalf("суд не исполнился: %v", err)
		}
		if len(findings) != 1 || findings[0].Tree != vendorTreeAccess {
			t.Fatalf("привязка в подменённом дереве при прежнем пине обязана краснить дерево "+
				"службы доступа; запрошено %v, находок %d %v", asked, len(findings), findings)
		}
		if want := access + "@" + vendorProbePin; len(asked) != 1 || asked[0] != want {
			t.Fatalf("дерево базы обязано браться по пину базы %s, запрошено %v", want, asked)
		}
	})

	t.Run("та же подмена на обоих концах", func(t *testing.T) {
		t.Parallel()
		root, _ := vendorProbeWorld(t)
		gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
		if err != nil {
			t.Fatal(err)
		}
		const target = "github.com/PRO-Robotech/kaname-fork " + vendorProbePin
		rev := commitGoMod(t, root, withReplace(gomod, target))
		var asked []string
		fetch := func(module, version string) (vendorTreeCorpus, error) {
			asked = append(asked, module+"@"+version)
			return clean, nil
		}
		base, hows, err := retiredVendorBaseCorpora(root, rev, replacedHead, withReplace(gomod, target), fetch)
		if err != nil {
			t.Fatalf("дерево базы не собрано: %v", err)
		}
		t.Logf("%s · запрошено %v", strings.Join(hows, " · "), asked)
		if len(asked) != 0 || len(base[vendorTreeAccess].Bodies) != len(replacedHead[vendorTreeAccess].Bodies) {
			t.Fatalf("та же подмена на обоих концах — то же дерево, модуль не запрашивается: %v", asked)
		}
	})

	t.Run("подмена на базе локальным каталогом", func(t *testing.T) {
		t.Parallel()
		root, _ := vendorProbeWorld(t)
		gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
		if err != nil {
			t.Fatal(err)
		}
		rev := commitGoMod(t, root, withReplace(gomod, "../kaname-copy"))
		fetch := func(module, version string) (vendorTreeCorpus, error) {
			return clean, nil
		}
		_, hows, err := retiredVendorBaseCorpora(root, rev, replacedHead, withReplace(gomod, "../kaname-copy"), fetch)
		if !errors.Is(err, errVendorBase) {
			t.Fatalf("локальная подмена на базе не восстанавливает дерево базы — ждали отказ "+
				"errVendorBase, получено %v (%s)", err, strings.Join(hows, " · "))
		}
	})
}
