// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProtoContractsCiteOnlyHoldersThatExist — координата, названная
// комментарием контракта, разрешается в составе дерева.
func TestProtoContractsCiteOnlyHoldersThatExist(t *testing.T) {
	tree := newTrackedTree(t, repoRoot(t))
	c, err := SurveyProtoCitedHolders(tree.Tree)
	if err != nil {
		t.Fatalf("обход контрактов: %v", err)
	}
	for _, f := range c.Findings {
		t.Error(f)
	}
	if c.Contracts == 0 {
		t.Fatal("обход пуст — гейт судил бы о непрочитанном")
	}
	t.Logf("перепись: контрактов прочитано %d · координат названо %d · разрешилось %d · висячих %d",
		c.Contracts, c.Citations, c.Resolved, len(c.Dangling))
}

// TestProtoCitedHolders_CanFailAndStaysSilent — способность упасть и смолчать,
// доказанная на СИНТЕТИЧЕСКОМ дереве: правка настоящих контрактов ради пробы
// оборвала бы соседнюю сессию в том же дереве.
func TestProtoCitedHolders_CanFailAndStaysSilent(t *testing.T) {
	cases := []struct {
		name     string
		contract string
		holder   string // файл, который в синтетическом дереве создаётся; "" — не создавать
		want     string
		why      string
	}{
		{
			name:     "законный близнец: цитата разрешается",
			contract: "// enforced by scripts/live_gate.sh\nmessage A { reserved 7; }\n",
			holder:   "scripts/live_gate.sh",
			why: "положительный контроль: без него всякое красное ниже могло бы приходить от самого " +
				"разбора, а не от инъекции",
		},
		{
			name:     "цитата в никуда",
			contract: "// enforced by scripts/tombstone_enforce.sh\nmessage A { reserved 7; }\n",
			holder:   "",
			want:     "цитирует scripts/tombstone_enforce.sh, которого в составе дерева НЕТ",
			why:      "ровно предмет #2025: контракт обещает держателя, за которым никого нет",
		},
		{
			name:     "координата вне комментария цитатой не считается",
			contract: "message A {\n  string scripts_path = 1; // поле\n}\nmessage B { string s = 1; }\n",
			holder:   "",
			why: "отрицательный контроль распознавателя: он судит комментарий, а не всякую строку — " +
				"иначе краснел бы на именах полей и на собственном объяснении",
		},
		{
			name:     "проза без координаты дерева",
			contract: "// дисциплина надгробий держится адъюдикацией buf breaking против ствола\nmessage A { reserved 7; }\n",
			holder:   "",
			why:      "второй отрицательный контроль: имя держателя без пути не обязано быть путём",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeCitedFile(t, root, "proto/kacho/cloud/iam/v1/a.proto", tc.contract)
			if tc.holder != "" {
				writeCitedFile(t, root, tc.holder, "#!/bin/sh\nexit 0\n")
			}
			tree := newSyntheticTree(t, root)
			c, err := SurveyProtoCitedHolders(tree.Tree)
			if err != nil {
				t.Fatalf("обход синтетики: %v", err)
			}
			if tc.want == "" {
				if len(c.Findings) != 0 {
					t.Fatalf("разбор нашёл на законной синтетике то, чего в ней нет — первое же ложное "+
						"срабатывание снимает гейт.\nчто проверялось: %s\nнаходки:\n  %s",
						tc.why, strings.Join(c.Findings, "\n  "))
				}
				if c.Contracts == 0 {
					t.Fatal("контроль ничего не доказывает: контрактов прочитано 0")
				}
				return
			}
			if len(c.Findings) == 0 {
				t.Fatalf("разбор смолчал на инъекции — он НЕ способен упасть по этой оси.\n"+
					"что должно было ловиться: %s", tc.why)
			}
			if !strings.Contains(strings.Join(c.Findings, "\n"), tc.want) {
				t.Fatalf("разбор покраснел не на том: ждали %q\nнаходки:\n  %s",
					tc.want, strings.Join(c.Findings, "\n  "))
			}
		})
	}
}

// writeCitedFile кладёт файл синтетического дерева, создавая каталоги.
func writeCitedFile(t *testing.T, root, rel, body string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
