// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package knownfailingsubject

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// suiteFixture builds a minimal newman-suite layout under t.TempDir.
func suiteFixture(t *testing.T, files map[string]string) (root, suiteRel string) {
	t.Helper()
	root = t.TempDir()
	suiteRel = "services/x/tests/newman"
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(suiteRel), filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	return root, suiteRel
}

// TestUnsupervisedDeclarationIsFoundAndItsLawfulTwinIsNot — инъекция в обе стороны.
//
// Без второй половины мерка ловила бы ФОРМУ (слово в тексте), а не существо
// (незанадзорное объявление), и первое же ложное срабатывание сняло бы её вместе
// со всем, что она ловит. Каждый молчащий случай здесь — настоящая конструкция с
// дерева, а не выдуманная: разбор снятого механизма, объявление на своём месте,
// доменный глагол рядом с маркером, свободная проза о починке.
func TestUnsupervisedDeclarationIsFoundAndItsLawfulTwinIsNot(t *testing.T) {
	declaring := "# Newman\n\n## Known failing tests — product bugs\n\n" +
		"> `X-CASE-ONE` — persistent-RED, kacho#1.\n\n## Другое\n\nпрочее\n"

	cases := []struct {
		name  string
		files map[string]string
		want  string // подстрока координаты находки; пусто ⇒ находок быть не должно
	}{
		{
			name: "маркер в файле кейса, вердикта снятия рядом нет",
			files: map[string]string{
				"docs/RESULTS.md": declaring,
				"cases/a.py":      "# BUG(persistent-RED): продукт не отвергает пустое имя.\nCASES = []\n",
			},
			want: "cases/a.py:1",
		},
		{
			name: "тот же маркер в объявляющем разделе своего документа",
			files: map[string]string{
				"docs/RESULTS.md": declaring,
				"cases/a.py":      "CASES = []\n",
			},
			want: "",
		},
		{
			name: "маркер в НЕобъявляющем разделе того же документа",
			files: map[string]string{
				"docs/RESULTS.md": "# Newman\n\n## Сводка\n\n100% PASS, кроме persistent-RED ниже.\n",
			},
			want: "docs/RESULTS.md:5",
		},
		{
			name: "разбор УЖЕ СНЯТОГО механизма — вердикт стоит вплотную",
			files: map[string]string{
				"docs/RESULTS.md": declaring,
				"scripts/run.sh":  "# Здесь стоял known-RED whitelist; он removed вместе с вычитанием.\n",
			},
			want: "",
		},
		{
			name: "снятие названо по-русски рядом с маркером",
			files: map[string]string{
				"docs/RESULTS.md": declaring,
				"cases/a.py":      "# Запись persistent-RED снята вместе со своим предметом.\nCASES = []\n",
			},
			want: "",
		},
		{
			name: "вердикт уехал на конец предыдущей строки — то же предложение",
			files: map[string]string{
				"docs/RESULTS.md": declaring,
				"scripts/gen.py":  "# каждый читатель — включая removed\n# known-RED whitelist — верил в тридцать секунд\n",
			},
			want: "",
		},
		{
			// Контроль, поймавший настоящий дефект мерки: в этом продукте ресурсы
			// УДАЛЯЮТСЯ, и доменный глагол рядом с маркером не есть вердикт о сроке
			// жизни объявления. Строка каталога кейсов уезжала из находок именно так.
			name: "доменный глагол рядом с маркером оправданием НЕ является",
			files: map[string]string{
				"docs/RESULTS.md": declaring,
				"docs/CASES-INDEX.md": "| `X-DEL-NEG` | NEG | P0 | Delete SG → FailedPrecondition. " +
					"**persistent-RED** (verifies #27): пока триггера нет, SG удаляется, оставляя ссылку. |\n",
			},
			want: "docs/CASES-INDEX.md:1",
		},
		{
			name: "свободная проза о починке маркером не является",
			files: map[string]string{
				"docs/RESULTS.md": declaring,
				"cases/a.py":      "# ROOT CAUSE of the persistent red this fixes: общий аккаунт.\nCASES = []\n",
			},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, rel := suiteFixture(t, tc.files)
			f, files, markers := findUnsupervised(root, rel)
			if files == 0 {
				t.Fatalf("прочитано 0 файлов — мерка ничего не осмотрела, и её вердикт ничего не значит")
			}
			if tc.want == "" {
				if len(f) != 0 {
					t.Fatalf("законная конструкция признана находкой (%d, маркеров %d):\n%s",
						len(f), markers, strings.Join(f, "\n"))
				}
				return
			}
			if len(f) != 1 {
				t.Fatalf("ожидалась одна находка, получено %d (маркеров %d):\n%s",
					len(f), markers, strings.Join(f, "\n"))
			}
			if !strings.Contains(f[0], tc.want) {
				t.Fatalf("находка не называет координату %q:\n%s", tc.want, f[0])
			}
		})
	}
}

// TestUnsupervisedCensusSeparatesNothingFoundFromNothingRead — «ноль находок»
// обязано быть отличимо от «ноль прочитанного».
func TestUnsupervisedCensusSeparatesNothingFoundFromNothingRead(t *testing.T) {
	f, files, markers := findUnsupervised(t.TempDir(), "services/none/tests/newman")
	if len(f) != 0 || files != 0 || markers != 0 {
		t.Fatalf("пустая сюита: находок %d, файлов %d, маркеров %d — ожидались нули", len(f), files, markers)
	}

	root, rel := suiteFixture(t, map[string]string{
		"docs/RESULTS.md": "# Newman\n\nни одного объявления\n",
		"cases/a.py":      "CASES = []\n",
	})
	f, files, markers = findUnsupervised(root, rel)
	if len(f) != 0 {
		t.Fatalf("чистая сюита дала находки: %s", strings.Join(f, "\n"))
	}
	if files == 0 {
		t.Fatal("чистая сюита прочитана как пустая — вердикты «чисто» и «нечего проверять» слились")
	}
	_ = markers
}
