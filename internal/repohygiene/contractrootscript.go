// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/contractroot"
	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Близнец `contractrootliteral.go` для языков, которых тот не читает.
//
// Тот гейт разбирает Go-AST, поэтому оболочка и питон дерева в его популяцию не
// входят ВОВСЕ — и это не пробел охвата, а слепота: класс «популяция отбирается
// литералом корня контрактов» дал в оболочке ДВА живых дефекта, и оба были
// МОЛЧАЛИВЫМИ (kacho#1110, #2339). Молчание здесь — не фигура речи: проверка не
// краснеет и не зеленеет, она честно печатает ноль по опустевшей популяции.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕМ СУДИТСЯ — НАЗВАНО ВСЛУХ, ПОТОМУ ЧТО ЭТО РАЗНОЕ ПО ЯЗЫКАМ
//
// Полного разбора этих языков в дереве нет, поэтому вердикт выносится по
// ЛЕКСЕМЕ, а не по слову в сыром тексте, и лексема у каждого языка своя:
//
//   * питон — СТРОКОВЫЙ ЛИТЕРАЛ (`pythonStringLexemes`). Комментарии и код вне
//     строк не судятся вовсе. Строка, ОТКРЫВАЮЩАЯ логическую строку, — проза
//     (docstring), и вердикт по ней не выносится: перечень корней объясняют
//     словами чаще, чем им отбирают, и гейт, читающий сырой текст, покраснел бы
//     на собственном объяснении;
//
//   * оболочка — СЛОВО вне комментария (`shellLexemes`), с прозрачными кавычками.
//     Именно слово, а не строковый литерал: в оболочке путь нормально пишется
//     ГОЛЫМ (`proto/kacho/cloud`), и судить одни лишь литералы значило бы не
//     видеть обычной формы записи.
//
// ЧТО ОСТАЁТСЯ ВНЕ НАБЛЮДЕНИЯ — и это граница, а не находка:
//
//   * путь, СОБРАННЫЙ в рантайме из переменных (`"$d/$root/cloud"`), — лексемы
//     с корнем в нём нет, и статически её взять неоткуда;
//   * имя, полученное подстановкой команды;
//   * языки помимо этих двух и Go: их в дереве нет (перечень ВЫВОДИТСЯ обходом,
//     см. пробу), но появятся — популяция обязана вырасти вместе с ними.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО СЧИТАЕТСЯ ОТБОРОМ ПОПУЛЯЦИИ, А ЧТО ИМЕНЕМ ЧЛЕНА
//
// Разделяет ОДИН вопрос: назван ли домен. Литерал, дошедший до имени домена,
// адресует ОДИН предмет — переезд домена под второй корень такую координату
// ломает ГРОМКО (запрос не резолвится, файла нет). Литерал, оборвавшийся на
// корне, отбирает ВСЁ, что под ним лежит, — и переезд домена он переживает
// МОЛЧА, просто перестав его находить.
//
//	proto/kacho/cloud            → отбор популяции   (домен не назван)
//	proto/kacho/cloud/*/v1       → отбор популяции   (на месте домена звёздочка)
//	proto/kaname/cloud/iam/v1    → имя члена         (домен назван) — молчим
//	kacho.cloud.storage.v1.Get   → имя члена         — молчим
//	kacho.cloud.\([a-z0-9_]*\)   → отбор популяции   (на месте домена захват)
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ИМЕННАЯ ФОРМА СУДИТСЯ УЖЕ, ЧЕМ В GO-БЛИЗНЕЦЕ
//
// В Go `strings.HasPrefix(s, "kacho.")` отбирает имена контрактов, и там этого
// довольно. В оболочке и питоне `kacho.cloud` БЕЗ хвостовой точки — почти
// всегда НЕ дерево контрактов, а доменное имя: `spiffe://kacho.cloud/ns/...`,
// `https://hydra.api.kacho.cloud`, ключ аннотации. Замер по дереву: таких
// вхождений 24 файла, и НИ ОДНО не про контракты. Поэтому именная форма
// требует хвостовой точки (приставка ПАКЕТА), а не просто корня с `.cloud`.
// Гейт, краснеющий на верном коде, отключают первым.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЭКРАНИРОВАНИЕ СНИМАЕТСЯ ДО СУДА, И ЭТО НЕСУЩЕЕ
//
// В оболочке приставку пишут внутри регулярного выражения `sed`/`grep`, где
// точка экранирована: `kacho\.cloud\.`. Распознаватель, не знающий этой формы,
// был бы слеп ровно к ТОМУ дефекту, ради которого заведён, — и слеп молча.
// Все три исторических места оболочки записаны именно так.

// scriptLexeme — лексема, о которой выносится вердикт.
type scriptLexeme struct {
	Line int
	Text string
	// OpensLine — лексема открывает логическую строку. Для питона это признак
	// прозы (docstring); для оболочки не используется.
	OpensLine bool
}

// ContractRootScriptFinding — одно место, где популяция отбирается литералом
// приставки корня контрактов в оболочке или питоне.
type ContractRootScriptFinding struct {
	File    string
	Line    int
	Lang    string
	Lexeme  string
	Matched string
}

func (f ContractRootScriptFinding) String() string {
	return fmt.Sprintf(
		"%s:%d [%s]: %q отбирает популяцию ЛИТЕРАЛОМ приставки корня контрактов "+
			"(%s), а домен в нём НЕ НАЗВАН. Корней в дереве больше одного, и дерево "+
			"второго корня такой отбор не находит: он не краснеет и не зеленеет, а "+
			"МОЛЧИТ — проверка честно печатает ноль по опустевшей популяции. "+
			"Отбор берётся у ОБЪЯВЛЕННОГО перечня корней: в оболочке "+
			"KACHO_PROTO_ROOTS из gateway/scripts/lib/stage-proto-tree.sh, в питоне "+
			"— обход по всем корням, а не по одному",
		f.File, f.Line, f.Lang, trimForMessage(f.Lexeme), f.Matched)
}

func trimForMessage(s string) string {
	const max = 90
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// ContractRootScriptLangCensus — объём осмотренного ПО ОДНОМУ ЯЗЫКУ.
//
// Перепись ведётся по языкам ОТДЕЛЬНО намеренно: одно суммарное число скрывает
// ровно тот случай, ради которого гейт заведён. Язык, чей разбор сломался или
// чьи файлы перестали попадать в обход, при сложении неотличим от языка, в
// котором просто нет находок.
type ContractRootScriptLangCensus struct {
	Files       int
	Lexemes     int
	RootBearing int
	Findings    int
}

// ContractRootScriptCensus — объём осмотренного по всем языкам.
type ContractRootScriptCensus struct {
	ByLang map[string]*ContractRootScriptLangCensus
}

func (c ContractRootScriptCensus) String() string {
	langs := make([]string, 0, len(c.ByLang))
	for l := range c.ByLang {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	parts := make([]string, 0, len(langs))
	for _, l := range langs {
		v := c.ByLang[l]
		parts = append(parts, fmt.Sprintf(
			"%s: файлов %d · лексем %d · НЕСУЩИХ корень %d · находок %d",
			l, v.Files, v.Lexemes, v.RootBearing, v.Findings))
	}
	return strings.Join(parts, " | ") +
		fmt.Sprintf(" | объявленных корней %v", contractroot.Roots)
}

// Totals — суммы по всем языкам. Существуют для утверждений о пустом обходе, а
// НЕ для отчёта: отчёт обязан называть языки порознь.
func (c ContractRootScriptCensus) Totals() (files, lexemes, rootBearing, findings int) {
	for _, v := range c.ByLang {
		files += v.Files
		lexemes += v.Lexemes
		rootBearing += v.RootBearing
		findings += v.Findings
	}
	return
}

const (
	langPython = "питон"
	langShell  = "оболочка"
)

// scriptLangOf — язык файла по суффиксу; пустая строка означает «не наш».
func scriptLangOf(rel string) string {
	switch {
	case strings.HasSuffix(rel, ".py"):
		return langPython
	case strings.HasSuffix(rel, ".sh"), strings.HasSuffix(rel, ".bash"):
		return langShell
	}
	return ""
}

// unescapeDots — снимает экранирование точки, принятое в регулярных выражениях
// оболочки. См. «ЭКРАНИРОВАНИЕ СНИМАЕТСЯ ДО СУДА» выше: без этого
// распознаватель слеп к той форме, в которой класс и наблюдался.
func unescapeDots(s string) string {
	s = strings.ReplaceAll(s, `\.`, ".")
	s = strings.ReplaceAll(s, `[.]`, ".")
	return s
}

// isNameByte — байт, который может стоять внутри имени/пути и потому НЕ является
// границей слева от совпадения.
func isNameByte(b byte) bool {
	return b == '_' || b == '.' || b == '-' ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func isLowerByte(b byte) bool { return b >= 'a' && b <= 'z' }

// isPathTailByte — байт, продолжающий путь после `.../cloud`.
func isPathTailByte(b byte) bool {
	return b == '/' || b == '_' || (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// contractTreeBases — приставки пути, под которыми лежит дерево контрактов.
var contractTreeBases = []string{"proto/", "pkg/api/"}

// selectsContractPopulation — отбирает ли лексема ПОПУЛЯЦИЮ, а не члена.
//
// Возвращает совпавший литерал, чтобы находка называла предмет дословно.
func selectsContractPopulation(text string) (string, bool) {
	s := unescapeDots(text)

	// ── путевая форма: <база>/<корень>/cloud, домен не назван ────────────────
	for _, base := range contractTreeBases {
		for _, root := range contractroot.Roots {
			needle := base + root + "/cloud"
			for from := 0; ; {
				i := strings.Index(s[from:], needle)
				if i < 0 {
					break
				}
				i += from
				from = i + 1
				if i > 0 && isNameByte(s[i-1]) {
					continue // приставка внутри более длинного имени
				}
				rest := s[i+len(needle):]
				switch {
				case rest == "":
					return needle, true // литерал оборвался на корне
				case strings.HasPrefix(rest, "/*"):
					return needle + "/*", true // на месте домена звёздочка
				case !isPathTailByte(rest[0]):
					return needle, true // за корнем идёт что угодно, кроме домена
				}
				// иначе домен НАЗВАН — это имя члена, молчим
			}
		}
	}

	// ── именная форма: <корень>.cloud. — приставка ПАКЕТА, домен не назван ───
	for _, root := range contractroot.Roots {
		needle := root + ".cloud."
		for from := 0; ; {
			i := strings.Index(s[from:], needle)
			if i < 0 {
				break
			}
			i += from
			from = i + 1
			if i > 0 && isNameByte(s[i-1]) {
				continue
			}
			rest := s[i+len(needle):]
			if rest == "" || !isLowerByte(rest[0]) {
				return needle, true
			}
			// иначе домен НАЗВАН — имя члена, молчим
		}
	}
	return "", false
}

// bearsContractRoot — несёт ли лексема корень контрактов ВООБЩЕ.
//
// Это знаменатель переписи: без него «находок ноль» неотличимо от «в этом языке
// корень не встречается вовсе», то есть от пустой популяции.
func bearsContractRoot(text string) bool {
	s := unescapeDots(text)
	for _, root := range contractroot.Roots {
		for _, base := range contractTreeBases {
			if strings.Contains(s, base+root+"/") {
				return true
			}
		}
		if strings.Contains(s, root+".cloud") {
			return true
		}
	}
	return false
}

// pythonStringLexemes — строковые литералы питон-исходника.
//
// Судятся именно они: комментарии и код вне строк отбора популяции не несут, а
// прочитанные как сырой текст дали бы находку на объяснении класса.
func pythonStringLexemes(src string) []scriptLexeme {
	var out []scriptLexeme
	i, n, line := 0, len(src), 1
	bol := true // с начала логической строки шли только пробелы
	for i < n {
		c := src[i]
		switch c {
		case '\n':
			line++
			i++
			bol = true
			continue
		case ' ', '\t', '\r':
			i++
			continue
		case '#':
			for i < n && src[i] != '\n' {
				i++
			}
			continue
		}
		if q, plen := pythonQuoteAt(src, i); q != "" {
			start, startLine, opens := i, line, bol
			i += plen + len(q)
			for i < n {
				if src[i] == '\\' && len(q) == 1 {
					if i+1 < n && src[i+1] == '\n' {
						line++
					}
					i += 2
					continue
				}
				if strings.HasPrefix(src[i:], q) {
					i += len(q)
					break
				}
				if src[i] == '\n' {
					line++
				}
				i++
			}
			out = append(out, scriptLexeme{Line: startLine, Text: src[start:i], OpensLine: opens})
			bol = false
			continue
		}
		bol = false
		i++
	}
	return out
}

// pythonQuoteAt — открывается ли в позиции i строковый литерал; возвращает
// кавычку и длину префикса (r/b/u/f).
func pythonQuoteAt(src string, i int) (quote string, prefixLen int) {
	j := i
	for j < len(src) && j-i < 3 {
		c := src[j]
		if c == 'r' || c == 'R' || c == 'b' || c == 'B' ||
			c == 'u' || c == 'U' || c == 'f' || c == 'F' {
			j++
			continue
		}
		break
	}
	rest := src[j:]
	for _, q := range []string{`"""`, `'''`, `"`, `'`} {
		if strings.HasPrefix(rest, q) {
			return q, j - i
		}
	}
	return "", 0
}

// shellLexemes — слова оболочки вне комментариев; кавычки прозрачны.
//
// Слово, а не строковый литерал: в оболочке путь нормально пишется голым, и
// судить одни литералы значило бы не видеть обычной формы записи.
func shellLexemes(src string) []scriptLexeme {
	var out []scriptLexeme
	for ln, line := range strings.Split(src, "\n") {
		var cur strings.Builder
		var quote byte
		for i := 0; i < len(line); i++ {
			c := line[i]
			switch {
			case quote == 0 && c == '#' && cur.Len() == 0:
				i = len(line) // комментарий до конца строки
			case quote == 0 && (c == ' ' || c == '\t'):
				if cur.Len() > 0 {
					out = append(out, scriptLexeme{Line: ln + 1, Text: cur.String()})
					cur.Reset()
				}
			case quote == 0 && (c == '"' || c == '\''):
				quote = c
			case quote != 0 && c == quote:
				quote = 0
			default:
				cur.WriteByte(c)
			}
		}
		if cur.Len() > 0 {
			out = append(out, scriptLexeme{Line: ln + 1, Text: cur.String()})
		}
	}
	return out
}

// AuditContractRootScripts — вердикт о дереве для оболочки и питона.
//
// Состав берётся у ИНДЕКСА git, а не с диска: под деревом на всякой машине, где
// поднимали стенд или собирали фронтенд, лежат распаковки чартов и отчёты
// прогонов. Обход по диску сделал бы вердикт свойством рабочего каталога.
func AuditContractRootScripts(root string) ([]ContractRootScriptFinding, ContractRootScriptCensus, error) {
	census := ContractRootScriptCensus{ByLang: map[string]*ContractRootScriptLangCensus{
		langPython: {}, langShell: {},
	}}
	var findings []ContractRootScriptFinding

	files, err := treecorpus.UnderWithSuffix(root, ".sh", ".bash", ".py")
	if err != nil {
		return nil, census, fmt.Errorf(
			"состав дерева НЕ ИЗМЕРЕН (%w): обход был бы пуст, а «ноль находок» "+
				"неотличимо от «ноль прочитанного»", err)
	}

	for _, path := range files {
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil, census, rerr
		}
		rel = filepath.ToSlash(rel)
		lang := scriptLangOf(rel)
		if lang == "" {
			continue
		}
		src, rerr := readFileString(path)
		if rerr != nil {
			return nil, census, fmt.Errorf("файл %s не прочитан (%w): вердикт по нему "+
				"не выносится, и молчание не читается как «чисто»", rel, rerr)
		}
		lc := census.ByLang[lang]
		lc.Files++

		var lexemes []scriptLexeme
		if lang == langPython {
			lexemes = pythonStringLexemes(src)
		} else {
			lexemes = shellLexemes(src)
		}
		lc.Lexemes += len(lexemes)

		for _, lx := range lexemes {
			// Проза питона (docstring) вердикта не получает: перечень корней
			// объясняют словами чаще, чем им отбирают.
			if lang == langPython && lx.OpensLine {
				continue
			}
			if bearsContractRoot(lx.Text) {
				lc.RootBearing++
			}
			matched, bad := selectsContractPopulation(lx.Text)
			if !bad {
				continue
			}
			lc.Findings++
			findings = append(findings, ContractRootScriptFinding{
				File: rel, Line: lx.Line, Lang: lang, Lexeme: lx.Text, Matched: matched,
			})
		}
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	return findings, census, nil
}
