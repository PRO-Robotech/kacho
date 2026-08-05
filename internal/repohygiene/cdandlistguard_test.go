// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// cdandlistguard_test.go — смена рабочего каталога внутри `&&`-списка не может
// ни проглотить отказ, ни утечь в следующие команды.
//
// # Предмет
//
// `set -e` НЕ прерывает выполнение, когда падает команда внутри `&&`-списка и
// после неё есть ещё одно звено: правило bash освобождает от выхода все звенья
// списка, кроме последнего. Проверяется однострочником — и это не цитата из
// документации, а замер:
//
//	bash -c 'set -e; false && echo x && echo y; echo ПОСЛЕ'  → печатает ПОСЛЕ, rc=0
//
// Вторая половина того же класса — рабочий каталог. `cd X && cmd && cd ..`
// возвращается назад ПОСЛЕДНИМ звеном, то есть ровно тем, которое не исполнится,
// если упало предыдущее. Дальше по рецепту относительные пути считаются от
// чужого каталога.
//
// Обе половины наблюдались вместе 2026-08-05 в `deploy/Makefile` (цель `dev-up`,
// подкачка подчартов): загрузка зависимости не удалась, выход не сработал, возврат
// не исполнился — и следующая команда упала с сообщением «path "./helm/umbrella"
// not found». Сообщение уводит от причины: каталог был на месте, не на месте был
// рабочий каталог. Тот же файл уже несёт объяснение соседнего класса («упавшая
// команда НЕ прерывает цель») и лечение — `set -e`; здесь видно, что `set -e`
// закрывает не всё.
//
// # Что считается находкой
//
// Список, ПЕРВОЕ звено которого — `cd`, и при этом выполнено хотя бы одно из:
//
//   - в списке ДВА и более `&&` — значит есть незавершающее звено, чей отказ
//     освобождён от `set -e`;
//   - в той же логической строке после списка стоит ещё одна команда — значит
//     смена каталога до неё доживает.
//
// # Что находкой НЕ является (законные близнецы, проверены инъекцией)
//
//   - `( cd X && cmd )` — подоболочка. Её отказ есть отказ statement-уровня
//     (`set -e` срабатывает в точке причины), а рабочий каталог наружу не выходит.
//     Это и есть предписываемая форма; она уже стоит в дереве;
//   - `cd X && cmd` как ЕДИНСТВЕННАЯ команда логической строки: `cmd` —
//     последнее звено, его отказ `set -e` видит, а каталог умирает вместе с
//     рецептом. Таких строк в дереве большинство, и трогать их незачем;
//   - многострочная подстановка процесса `< <( cd X && … )` — тоже подоболочка,
//     просто записанная в несколько физических строк. Разбор склеивает строки не
//     только по обратному слэшу, но и по НЕЗАКРЫТОЙ скобке, иначе такая
//     конструкция читалась бы как голый `cd` в начале строки (ровно эта ложная
//     находка и была у первой редакции предиката).
//
// # Читается исполняемая часть, а не текст
//
// Литералы сворачиваются, комментарии отбрасываются (`shellExecutable`,
// pipefailguard_test.go). Без этого гейт указал бы на `echo "… 'cd proto && buf
// generate' …"` в конвейере CI — строку сообщения, а не команду.
//
// # Перепись
//
// «Ноль находок» обязано отличаться от «ноль прочитанного»: гейт печатает, сколько
// файлов прочитал и в скольких логических строках вообще встретил `cd`. Пустой
// обход — провал.
package repohygiene

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// cdFinding — один список, где смена каталога стоит первым звеном.
type cdFinding struct {
	File string
	Line int
	Code string
	Why  string
}

// cdLogicalLine — логическая строка оболочки: физические склеены по обратному
// слэшу И по незакрытой скобке, литералы свёрнуты, комментарии убраны.
type cdLogicalLine struct {
	Line int
	Text string
}

// cdLogicalLines склеивает физические строки в логические.
//
// Скобочная глубина считается по УЖЕ свёрнутым литералам: скобка внутри кавычек
// не открывает вложенности.
func cdLogicalLines(body string) []cdLogicalLine {
	var out []cdLogicalLine
	var buf strings.Builder
	start, depth := 0, 0
	flush := func() {
		if start != 0 {
			out = append(out, cdLogicalLine{Line: start, Text: buf.String()})
		}
		buf.Reset()
		start, depth = 0, 0
	}
	for n, raw := range strings.Split(body, "\n") {
		_, masked := shellExecutable(raw)
		if start == 0 {
			start = n + 1
		} else {
			buf.WriteByte(' ')
		}
		trimmed := strings.TrimRight(masked, " \t")
		continued := strings.HasSuffix(trimmed, "\\")
		trimmed = strings.TrimSuffix(trimmed, "\\")
		buf.WriteString(trimmed)
		depth += cdDepthDelta(trimmed)
		if continued || depth > 0 {
			continue
		}
		flush()
	}
	flush()
	return out
}

func cdDepthDelta(s string) int {
	d := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '{':
			d++
		case ')', '}':
			d--
		}
	}
	return d
}

// cdSplitTop делит строку по разделителю на НУЛЕВОЙ скобочной глубине.
func cdSplitTop(s, sep string) []string {
	var parts []string
	var cur strings.Builder
	depth := 0
	for i := 0; i < len(s); {
		c := s[i]
		switch c {
		case '(', '{':
			depth++
		case ')', '}':
			depth--
		}
		if depth == 0 && strings.HasPrefix(s[i:], sep) {
			parts = append(parts, cur.String())
			cur.Reset()
			i += len(sep)
			continue
		}
		cur.WriteByte(c)
		i++
	}
	parts = append(parts, cur.String())
	return parts
}

// cdShellKeywords — слова, которые могут стоять ПЕРЕД командой в том же
// statement'е после деления по `;`.
var cdShellKeywords = map[string]bool{
	"if": true, "then": true, "else": true, "elif": true, "fi": true,
	"do": true, "done": true, "while": true, "until": true, "esac": true,
	"{": true, "}": true, "!": true,
}

// cdStripKeywords снимает ведущие ключевые слова, чтобы добраться до КОМАНДЫ.
func cdStripKeywords(s string) string {
	s = strings.TrimSpace(s)
	for {
		head, rest, found := strings.Cut(s, " ")
		if !cdShellKeywords[head] {
			return s
		}
		if !found {
			return ""
		}
		s = strings.TrimSpace(rest)
	}
}

var reCdCommand = regexp.MustCompile(`^cd\s+\S`)

// cdAndListFindings — списки, где `cd` стоит первым звеном небезопасно.
// Второе возвращаемое значение — перепись: сколько логических строк вообще
// содержали `cd`.
func cdAndListFindings(body string) (finds []cdFinding, seen int) {
	for _, ll := range cdLogicalLines(body) {
		if !strings.Contains(ll.Text, "cd ") {
			continue
		}
		seen++
		stmts := cdSplitTop(ll.Text, ";")
		for i, st := range stmts {
			cmd := cdStripKeywords(st)
			if strings.HasPrefix(cmd, "(") || !reCdCommand.MatchString(cmd) {
				continue
			}
			links := len(cdSplitTop(cmd, "&&")) - 1
			if links < 1 {
				continue // просто смена каталога, не `&&`-список
			}
			tail := false
			for _, rest := range stmts[i+1:] {
				if cdStripKeywords(rest) != "" {
					tail = true
					break
				}
			}
			var why []string
			if links >= 2 {
				why = append(why, "в списке есть незавершающее звено — его отказ освобождён от `set -e`")
			}
			if tail {
				why = append(why, "после списка в той же логической строке есть команды — смена каталога до них доживает")
			}
			if len(why) == 0 {
				continue
			}
			finds = append(finds, cdFinding{
				Line: ll.Line,
				Code: strings.TrimSpace(cmd),
				Why:  strings.Join(why, "; "),
			})
		}
	}
	return finds, seen
}

// cdShellBearing — файлы, которые исполняются оболочкой.
func cdShellBearing(rel string) bool {
	base := filepath.Base(rel)
	switch {
	case base == "Makefile" || strings.HasSuffix(rel, ".mk"):
		return true
	case strings.HasSuffix(rel, ".sh"):
		return true
	case strings.HasPrefix(rel, ".github/workflows/") &&
		(strings.HasSuffix(rel, ".yml") || strings.HasSuffix(rel, ".yaml")):
		return true
	}
	return false
}

func TestCdInAndListNeitherSwallowsFailureNorLeaksCwd(t *testing.T) {
	root := repoRoot(t)
	tree := newTrackedTree(t, root)

	var files []string
	for rel := range tree.files {
		if cdShellBearing(rel) {
			files = append(files, rel)
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("осмотрено НОЛЬ исполняемых оболочкой файлов — «ноль находок» здесь " +
			"означало бы «ноль прочитанного». Почини обход.")
	}

	var all []cdFinding
	seen := 0
	for _, rel := range files {
		raw, rerr := os.ReadFile(filepath.Join(root, rel))
		if rerr != nil {
			t.Fatalf("%s: %v", rel, rerr)
		}
		body := string(raw)
		if filepath.Base(rel) == "Makefile" || strings.HasSuffix(rel, ".mk") {
			body = stripMakeComments(body)
		}
		f, s := cdAndListFindings(body)
		seen += s
		for _, x := range f {
			x.File = rel
			all = append(all, x)
		}
	}

	if seen == 0 {
		t.Fatalf("прочитано %d файлов, но НИ В ОДНОМ не встретилось `cd` — предикат "+
			"перестал видеть свой предмет; это отказ разбора, а не чистое дерево", len(files))
	}

	if len(all) > 0 {
		var b strings.Builder
		for _, f := range all {
			b.WriteString("\n  " + f.File + ":" + strconv.Itoa(f.Line) + "\n    " + f.Code + "\n    → " + f.Why)
		}
		t.Fatalf("смена рабочего каталога внутри `&&`-списка, %d шт.%s\n\n"+
			"Пиши подоболочкой: ( cd X && cmd ). Её отказ — отказ statement-уровня "+
			"(`set -e` срабатывает в точке причины), а рабочий каталог не выходит наружу.",
			len(all), b.String())
	}

	t.Logf("осмотрено файлов: %d; логических строк с `cd`: %d; находок: 0", len(files), seen)
}

// TestCdPredicateCutsBothWays — предикат обязан краснеть на настоящем входе и
// молчать на законных близнецах той же формы. Без второй половины гейт ловил бы
// форму, а не существо, и первый же ложный срабат его снял бы.
func TestCdPredicateCutsBothWays(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{
			// Настоящий вход: строка из истории deploy/Makefile, укусившая 2026-08-05.
			name: "дефект: cd + команда + возврат, и рецепт продолжается",
			body: "dev-up:\n\t@set -e; \\\n\tcd helm/umbrella && helm dep update >/dev/null && cd ../..; \\\n\thelm upgrade --install x ./helm/umbrella\n",
			want: 1,
		},
		{
			name: "законно: подоболочка",
			body: "dev-up:\n\t@set -e; \\\n\t( cd helm/umbrella && helm dep update >/dev/null ); \\\n\thelm upgrade --install x ./helm/umbrella\n",
			want: 0,
		},
		{
			name: "законно: двузвенный список — единственная команда строки",
			body: "helm-lint:\n\tcd helm/umbrella && helm lint -f values.dev.yaml\n",
			want: 0,
		},
		{
			name: "законно: многострочная подстановка процесса",
			body: "mapfile -t R < <(\n  cd \"$ROOT\" && git ls-files | sort \\\n  | while IFS= read -r rel; do echo \"$rel\"; done\n)\n",
			want: 0,
		},
		{
			name: "законно: `cd` в тексте сообщения, а не команда",
			body: "check:\n\techo \"прогони 'cd proto && buf generate' и закоммить\"; \\\n\texit 1\n",
			want: 0,
		},
		{
			name: "дефект: три звена даже без продолжения строки",
			body: "gen:\n\tcd proto && buf generate && cd ..\n",
			want: 1,
		},
		{
			name: "законно: голый cd без списка",
			body: "run:\n\tcd build; \\\n\tmake all\n",
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.body
			if strings.Contains(body, "\t") {
				body = stripMakeComments(body)
			}
			got, _ := cdAndListFindings(body)
			if len(got) != tc.want {
				t.Fatalf("находок %d, ожидалось %d: %+v", len(got), tc.want, got)
			}
		})
	}
}
