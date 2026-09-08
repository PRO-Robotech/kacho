// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// readpathconcat_injection_test.go — ГЕЙТ #758 СПОСОБЕН УПАСТЬ И СПОСОБЕН
// СМОЛЧАТЬ.
//
// Инъекция идёт НАСТОЯЩИМ входом из дерева: файлы пути чтения копируются во
// временный корень, и в копию возвращается ТА САМАЯ форма, которая в дереве
// стояла. Синтетический литерал доказывал бы, что гейт понимает синтетику.
//
// Плечи парные, и второе не менее важно первого: рядом с возвращённым дефектом
// стоит ЗАКОННЫЙ близнец той же формы — склейка в проекции и склейка, названная
// SQL-комментарием у самого исправленного места. Гейт, краснеющий на них,
// был бы снят первым же ложным срабатыванием.
package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// concatOldReverseJoin — форма, стоявшая в обратных вопросах до #758, дословно.
const concatOldReverseJoin = "\n  JOIN kaname.group_members gm ON g.subject IN " +
	"('group:' || gm.group_id, 'group:' || gm.group_id || '#member')"

// concatOldCensusPredicate — форма, стоявшая в приборе замера до #758, дословно.
const concatOldCensusPredicate = "\n\t\t  WHERE bs.subject_type || ':' || bs.subject_id = ANY($1::text[])"

func TestReadPathConcatGateCanFailAndCanStaySilent(t *testing.T) {
	root := repoRoot(t)
	files, dirs := readPathGoFiles(t, root)

	t.Run("молчит на дереве как есть — и ВСТРЕТИЛ законных близнецов", func(t *testing.T) {
		copyRoot := mirrorReadPath(t, root, files, dirs, nil)
		got, c := collectPredicateConcats(t, copyRoot, files)
		if len(got) != 0 {
			t.Fatalf("на дереве как есть гейт нашёл %d: %+v", len(got), got)
		}
		if c.concatsElsewhere == 0 {
			t.Fatal("гейт не встретил НИ ОДНОЙ склейки вне условия: его молчание о находках " +
				"ничего не стоит, потому что он не дочитал до законного близнеца")
		}
		t.Logf("плечо молчания: склеек вне условия встречено %d, находок 0", c.concatsElsewhere)
	})

	t.Run("краснеет на возвращённом развороте членства", func(t *testing.T) {
		target := pickFile(t, files, "relverdict/expand.go")
		copyRoot := mirrorReadPath(t, root, files, dirs, map[string]func(string) string{
			target: func(s string) string {
				return strings.Replace(s, "  FROM ground g{{members_join}}",
					"  FROM ground g"+concatOldReverseJoin, 1)
			},
		})
		got, _ := collectPredicateConcats(t, copyRoot, files)
		assertNamesFile(t, got, target)
	})

	t.Run("краснеет на возвращённой склейке прибора замера", func(t *testing.T) {
		target := pickFile(t, files, "scalegrid/census.go")
		copyRoot := mirrorReadPath(t, root, files, dirs, map[string]func(string) string{
			target: func(s string) string {
				i := strings.Index(s, "\t\t   JOIN (SELECT DISTINCT split_part(w")
				if i < 0 {
					t.Fatalf("в %s не нашлось разобранной формы отбора — инъекция не имеет предмета", target)
				}
				j := strings.Index(s[i:], "sp.s_id`")
				if j < 0 {
					t.Fatalf("в %s не нашлось конца разобранной формы отбора", target)
				}
				return s[:i] + strings.TrimPrefix(concatOldCensusPredicate, "\n") + s[i+j+len("sp.s_id"):]
			},
		})
		got, _ := collectPredicateConcats(t, copyRoot, files)
		assertNamesFile(t, got, target)
	})

	t.Run("молчит на SQL-комментарии, называющем прежнюю форму", func(t *testing.T) {
		target := pickFile(t, files, "relverdict/expand.go")
		copyRoot := mirrorReadPath(t, root, files, dirs, map[string]func(string) string{
			target: func(s string) string {
				return strings.Replace(s, "  FROM ground g{{members_join}}",
					"  FROM ground g\n  -- прежде здесь стояло: ON g.subject IN "+
						"('group:' || gm.group_id, 'group:' || gm.group_id || '#member')"+
						"{{members_join}}", 1)
			},
		})
		got, _ := collectPredicateConcats(t, copyRoot, files)
		if len(got) != 0 {
			t.Fatalf("гейт покраснел на КОММЕНТАРИИ, объясняющем собственный запрет (%d находок: %+v).\n"+
				"Комментарий у исправленного места обязан называть прежнюю форму дословно — иначе "+
				"следующий читатель не поймёт, что запрещено", len(got), got)
		}
		t.Log("плечо молчания на объяснении запрета: находок 0")
	})
}

// mirrorReadPath — временный корень с копией пути чтения и правкой по адресу.
func mirrorReadPath(t *testing.T, root string, files, dirs []string,
	patch map[string]func(string) string) string {
	t.Helper()
	dst := t.TempDir()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(dst, d), 0o750); err != nil {
			t.Fatalf("каталог %s: %v", d, err)
		}
	}
	// Объявление предмета замера копируется тоже: гейт выводит из него объём, и
	// временный корень без него был бы корнем, о котором гейт ничего не знает.
	all := append(append([]string{}, files...), fingerprintSource)
	for _, rel := range all {
		body, err := readRepoFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("копирование %s: %v", rel, err)
		}
		text := string(body)
		if fn, ok := patch[rel]; ok {
			text = fn(text)
			if text == string(body) {
				t.Fatalf("инъекция в %s ничего не изменила: плечо проверяло бы неправленое дерево", rel)
			}
		}
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dst, rel)), 0o750); err != nil {
			t.Fatalf("каталог для %s: %v", rel, err)
		}
		if err := os.WriteFile(filepath.Join(dst, rel), []byte(text), 0o600); err != nil {
			t.Fatalf("запись %s: %v", rel, err)
		}
	}
	return dst
}

func pickFile(t *testing.T, files []string, suffix string) string {
	t.Helper()
	var got []string
	for _, f := range files {
		if strings.HasSuffix(f, suffix) {
			got = append(got, f)
		}
	}
	if len(got) != 1 {
		t.Fatalf("файлов с окончанием %q в объёме гейта %d, ожидался ровно один: "+
			"инъекция не знает, куда возвращать дефект", suffix, len(got))
	}
	return got[0]
}

func assertNamesFile(t *testing.T, got []concatFinding, want string) {
	t.Helper()
	if len(got) == 0 {
		t.Fatalf("возвращённый дефект в %s гейт НЕ нашёл: он зелен на том, ради чего написан", want)
	}
	for _, f := range got {
		if f.file == want {
			t.Logf("плечо падения: %s:%d — %s", f.file, f.line, f.operand)
			return
		}
	}
	t.Fatalf("гейт нашёл %d находок, но НИ ОДНА не называет %s: координата не та, и по сообщению "+
		"нельзя понять, что чинить. Находки: %+v", len(got), want, got)
}
