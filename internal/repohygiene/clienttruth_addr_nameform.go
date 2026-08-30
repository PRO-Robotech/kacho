// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_addr_nameform.go — анализатор «форма имени, ОБЪЯВЛЕННАЯ клиенту,
// равна форме, которую сервер ПРИМЕНЯЕТ».
//
// # Предмет
//
// Форма имени у платформы ОДНА — DNS label по RFC 1123, решение владельца
// 2026-08-18 (`api-conventions.md` §«Имя ресурса: одна форма, пустого не
// бывает»). Валидатор в дереве один, [NameFormSourceRel], и его же читает
// доменный newtype каждого сервиса; тот же предикат продублирован ограничением
// таблицы в пяти схемах. Разойтись исполнению не на чем.
//
// Расходится ОБЪЯВЛЕНИЕ. Комментарий контракта и таблица ограничений на сайте
// документации — то единственное, по чему клиент узнаёт алфавит имени ДО первого
// вызова; сервер он спрашивать не обязан. Объявление, разошедшееся с формой,
// обещает то, что сервер отвергнет, либо запрещает то, что он примет, — и в обе
// стороны это стоит клиенту круга: соглашение об именовании принимается один раз
// на весь парк, а отвергается поресурсно.
//
// # Замер на день заведения (kacho#1602)
//
// Форм, объявленных дереву, ЧЕТЫРЕ, и НИ ОДНА не равна применяемой:
//
//	применяется   [a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?
//	объявлено ×7  [a-zA-Z]([-_a-zA-Z0-9]{0,61}[a-zA-Z0-9])?   — контракты vpc
//	объявлено ×3  [a-z]([-a-z0-9]{0,61}[a-z0-9])?             — контракты iam/vpc, сайт
//	объявлено ×1  [a-z]([-_a-z0-9]{0,61}[a-z0-9])?            — сайт compute
//
// Три из четырёх расхождений видны глазом (заглавные, подчёркивание); четвёртое —
// нет: `[a-z]` против `[a-z0-9]` в ПЕРВОМ знаке. Имя `9lives` сервер принимает, а
// всякое объявление в дереве его запрещало. Такое расхождение не находится
// чтением — только сверкой с источником, чем анализатор и занят.
//
// Отдельно: страница вычислений объявляла форму «строже, чем в некоторых других
// доменах Kachō». Утверждение о РАЗЛИЧИИ доменов пережило своё основание — общий
// валидатор свёл их к одной форме ещё в #715.
//
// # Что судит анализатор
//
// Истина берётся из ЕДИНСТВЕННОГО источника — объявления `const Form = ` в
// [NameFormSourceRel]. Второй рукописной копии формы здесь не заводится: она и
// есть тот класс, который анализатор ловит.
//
// Утверждением о форме имени считается запись вида `[A]([B]{0,61}[C])?` в любом
// из трёх видов носителя — комментарий контракта, таблица ограничений сайта,
// страница сайта. Отбор идёт по `{0,61}` — границе длины, общей всему семейству;
// она отличает объявление формы ИМЕНИ от regex ключа метки и от прочих регулярок,
// которых в дереве десятки.
//
// Судится ТРОЙКА СИМВОЛЬНЫХ КЛАССОВ (первый знак · середина · последний), то есть
// АЛФАВИТ. Именно алфавит клиент переносит между ресурсами, и именно он расходился.
//
// # Чего анализатор НЕ судит, и это названо, а не умолчано
//
//  1. ДОПУСТИМОСТЬ ПУСТОГО. Обёртки `^(…)?$` и ведущей `|` анализатор не судит:
//     пустое имя — законный вход СОЗДАНИЯ (сервер подставляет производное от id) и
//     незаконный вход ПРАВКИ, то есть одна форма отвечает на этот вопрос по-разному
//     на двух путях. Свойство настоящее, но это свойство ПУТИ, а не алфавита, и
//     объявляется оно прозой.
//
//  2. ПОЛНОТА. Молчание о форме нарушением не является: ресурс вправе не объявлять
//     алфавит вовсе.
//
//  3. ФОРМА, НЕ ОТНОСЯЩАЯСЯ К ИМЕНИ. Regex без `{0,61}` не рассматривается — ключ
//     метки, идентификатор, путь имеют свои границы и свои правила.
//
// # Падает на ПУСТОМ ОБХОДЕ
//
// Ноль прочитанных носителей либо ноль распознанных утверждений — «находок ноль»
// неотличимо от «прочитано ноль».
package repohygiene

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/internal/treecorpus"
)

// NameFormSourceRel — единственный источник применяемой формы имени. Объявлен
// здесь, а не по местам вызова: литерал координаты, повторённый вызывающими,
// разъезжается молча.
const NameFormSourceRel = "pkg/validate/nameform/nameform.go"

// NameFormClaimOptions — вход анализатора.
type NameFormClaimOptions struct {
	// Tree — СОСТАВ дерева, а не его корень: гейт берёт индекс git
	// (`treecorpus.NewTree`), инъекционная проба — синтетическое дерево
	// (`treecorpus.SyntheticTree`). Разбор — clienttruth_treefiles.go.
	Tree *treecorpus.Tree

	// ProtoRoot — каталог контрактов относительно корня дерева. Пустой —
	// контракты не читаются (используется инъекцией).
	ProtoRoot string

	// DocsRoots — каталоги сайтов документации относительно корня дерева.
	// Каждый читается целиком; берутся носители видов, названных в
	// [nameFormCarrier].
	DocsRoots []string

	// FormSource — файл с объявлением применяемой формы относительно корня
	// дерева. Пустое значение означает [NameFormSourceRel].
	FormSource string

	// Exemptions — послабления. Каждое обязано истекать само: запись, которой
	// больше нечего исключать, — находка (`testing.md` §«Гейт на класс», п.5).
	Exemptions []NameFormClaimExemption
}

// NameFormClaimExemption — одно послабление. Ключ — предмет (файл и объявленный
// алфавит), а не номер строки: номер сдвигается от любой соседней правки, и
// послабление истекало бы по чужой причине.
type NameFormClaimExemption struct {
	// File — путь носителя относительно Root.
	File string
	// Claimed — объявленный алфавит в канонической записи (см. [NameAlphabet.String]).
	Claimed string
	// Reason — почему запись стоит и что её снимет. Пустая запрещена:
	// послабление без причины неотличимо от забытого.
	Reason string
}

// NameFormClaimCensus — объём осмотренного. Печатается всегда: «ноль находок»
// обязано быть отличимо от «ноль прочитанного».
type NameFormClaimCensus struct {
	// ProtoFiles — прочитано файлов контракта.
	ProtoFiles int
	// DocsFiles — прочитано носителей сайта документации.
	DocsFiles int
	// Claims — распознано утверждений о форме имени.
	Claims int
	// Agreeing — из них совпавших с применяемой формой.
	Agreeing int
	// Exempted — находок, снятых послаблением.
	Exempted int
}

// NameAlphabet — форма имени, разобранная на то, что она СООБЩАЕТ КЛИЕНТУ:
// три символьных класса (первый знак · середина · последний) и границу длины.
//
// Разбор, а не строка целиком: обёртка необязательности и ведущая `|` описывают
// допустимость ПУСТОГО, а это свойство ПУТИ, а не формы (см. шапку, п.1), и две
// записи, различные только ею, объявляют клиенту одно и то же.
//
// Длина входит в разбор, потому что расходилась ОТДЕЛЬНО от алфавита: описание
// фильтра называло границу `{1,61}` там, где применяется `{0,61}`.
type NameAlphabet struct {
	First, Mid, Last string
	Lo, Hi           string
}

// String — каноническая запись формы; она же ключ послабления.
func (a NameAlphabet) String() string {
	return "[" + a.First + "]([" + a.Mid + "]{" + a.Lo + "," + a.Hi + "}[" + a.Last + "])?"
}

// NameFormClaimFinding — одна находка.
type NameFormClaimFinding struct {
	// File, Line — координата утверждения.
	File string
	Line int
	// Claimed — объявленная форма.
	Claimed NameAlphabet
	// Actual — применяемая форма; пуста у устаревшего послабления.
	Actual NameAlphabet
	// ClaimedRaw — запись формы из ключа послабления. Отдельным полем, а не
	// разбором [NameFormClaimExemption.Claimed]: ключ послабления — СТРОКА, и
	// печатать его разобранным значило бы показать читателю не то, что он ищет
	// в своей записи.
	ClaimedRaw string
	// StaleExemption — запись послабления потеряла предмет.
	StaleExemption bool
	// Reason — причина послабления (только у устаревшего).
	Reason string
}

func (f NameFormClaimFinding) String() string {
	if f.StaleExemption {
		return fmt.Sprintf("%s: послабление на %s потеряло предмет (%s) — снимите запись",
			f.File, f.ClaimedRaw, f.Reason)
	}
	return fmt.Sprintf("%s:%d: объявлено %s, применяется %s%s",
		f.File, f.Line, f.Claimed, f.Actual, nameAlphabetDiff(f.Claimed, f.Actual))
}

// nameAlphabetDiff — по какой ОСИ разошлись. Находка, называющая только две
// строки, посылает читателя сличать их посимвольно; ось `[a-z]` против `[a-z0-9]`
// в первом знаке иначе не видна вовсе (`testing.md` §«Гейт на класс», п.8:
// диагностика — часть свойства).
func nameAlphabetDiff(claimed, actual NameAlphabet) string {
	var axes []string
	if claimed.First != actual.First {
		axes = append(axes, fmt.Sprintf("первый знак [%s] против [%s]", claimed.First, actual.First))
	}
	if claimed.Mid != actual.Mid {
		axes = append(axes, fmt.Sprintf("середина [%s] против [%s]", claimed.Mid, actual.Mid))
	}
	if claimed.Last != actual.Last {
		axes = append(axes, fmt.Sprintf("последний знак [%s] против [%s]", claimed.Last, actual.Last))
	}
	if claimed.Lo != actual.Lo || claimed.Hi != actual.Hi {
		axes = append(axes, fmt.Sprintf("длина {%s,%s} против {%s,%s}",
			claimed.Lo, claimed.Hi, actual.Lo, actual.Hi))
	}
	if len(axes) == 0 {
		return ""
	}
	return " — расходятся: " + strings.Join(axes, "; ")
}

var (
	// nameFormSourceRe — ОБЪЯВЛЕНИЕ применяемой формы. Читается объявление, а не
	// упоминание: строка формы стоит и в комментариях того же файла, и предикат по
	// подстроке взял бы истину из прозы о ней.
	nameFormSourceRe = regexp.MustCompile("(?m)^\\s*const Form\\s*=\\s*`([^`]+)`")

	// nameFormClaimRe — утверждение о форме имени в любом носителе.
	//
	// Отбор идёт по СТРОЕНИЮ «класс · класс с границей длины · класс»: именно так
	// записывается форма имени во всех носителях дерева. Граница `{lo,hi}`
	// захватывается, а не выписывается константой, — она объявляет ДЛИНУ, и та
	// расходилась отдельно от алфавита.
	//
	// СКОБКИ ВОКРУГ ХВОСТА НЕОБЯЗАТЕЛЬНЫ, и это не мелочь распознавания. Форма
	// пишется в дереве двумя строениями: `[A]([B]{0,61}[C])?` у поля имени и
	// `[A][B]{1,61}[C]` в описании фильтра. Требуй распознаватель скобок — и
	// ШЕСТЬ объявлений второго строения не судились бы вовсе: не «редкий край», а
	// целый вид носителя вне наблюдения (`testing.md` §«Гейт на класс», п.7).
	// Замер расширения: осмотренных утверждений 16 → 22, и все шесть прибавленных
	// оказались расхождениями, а не регрессией дерева.
	//
	// Экранирование носителя (`\|` перед классом, обратные кавычки вокруг) в отбор
	// не входит: оно вне захватываемых классов.
	// СИМВОЛЬНЫЙ КЛАСС НЕ СОДЕРЖИТ СКОБКИ — ни закрывающей, ни ОТКРЫВАЮЩЕЙ.
	// Запрет открывающей не украшение: `[^\]]+` жадно съедает всё до первой `]`,
	// поэтому на строке `name: ['regex ^[a-c0-8](…` первым классом становится
	// `['regex ^[a-c0-8]` — обёртка массива вместе с алфавитом. Находка тогда
	// называет мусор, а верное объявление объявляется расхождением. Настоящее
	// дерево этого не показывало: там объявление переносится, и лишней `[` на
	// строке не оказывалось. Дефект нашла инъекция, а не чтение.
	nameFormClaimRe = regexp.MustCompile(
		`\[([^\]\[]+)\]\(?\[([^\]\[]+)\]\{(\d+),(\d+)\}\[([^\]\[]+)\]\)?`)
)

// nameFormCarrier сообщает, является ли файл носителем объявления формы имени.
//
// Виды перечислены, а не выведены из расширения: сайт документации несёт десятки
// файлов, где regex стоит примером в фрагменте кода, — судить их значило бы
// краснеть на чужом предмете.
func nameFormCarrier(rel string) bool {
	switch {
	case strings.HasSuffix(rel, ".proto"):
		return true
	case strings.HasSuffix(rel, "/constants/restrictions.ts"):
		return true
	case strings.HasSuffix(rel, ".mdx"):
		return true
	default:
		return false
	}
}

// AuditNameFormClaims читает дерево и возвращает находки и перепись.
func AuditNameFormClaims(
	opts NameFormClaimOptions, log io.Writer,
) ([]NameFormClaimFinding, NameFormClaimCensus, error) {
	var census NameFormClaimCensus

	src := opts.FormSource
	if src == "" {
		src = NameFormSourceRel
	}
	raw, err := clientTruthReadTreeFile(opts.Tree, src)
	if err != nil {
		return nil, census, fmt.Errorf("источник применяемой формы %s: %w", src, err)
	}
	m := nameFormSourceRe.FindStringSubmatch(string(raw))
	if m == nil {
		return nil, census, fmt.Errorf(
			"источник %s не объявляет `const Form` — истину брать неоткуда", src)
	}
	actualParts := nameFormClaimRe.FindStringSubmatch(m[1])
	if actualParts == nil {
		return nil, census, fmt.Errorf(
			"форма %q из %s не разобралась в тройку классов — распознаватель разошёлся с источником",
			m[1], src)
	}
	actual := nameAlphabetOf(actualParts)

	var roots []string
	if opts.ProtoRoot != "" {
		roots = append(roots, opts.ProtoRoot)
	}
	roots = append(roots, opts.DocsRoots...)

	var findings []NameFormClaimFinding
	matched := map[string]bool{}

	// Каталог сайта может отсутствовать у сервиса — это не находка: состав
	// такого каталога в индексе пуст, и цикл ниже просто не сделает ни одного
	// шага. Отдельная проверка существования была нужна обходу диска, который на
	// отсутствующем каталоге отдавал ошибку; у индекса такого состояния нет.
	for _, r := range roots {
		for _, rel := range clientTruthTreeFiles(opts.Tree, r, true) {
			if !nameFormCarrier(rel) {
				continue
			}
			body, rerr := clientTruthReadTreeFile(opts.Tree, rel)
			if rerr != nil {
				return nil, census, rerr
			}
			if strings.HasSuffix(rel, ".proto") {
				census.ProtoFiles++
			} else {
				census.DocsFiles++
			}
			for i, line := range strings.Split(string(body), "\n") {
				for _, c := range nameFormClaimRe.FindAllStringSubmatch(line, -1) {
					census.Claims++
					claimed := nameAlphabetOf(c)
					if claimed == actual {
						census.Agreeing++
						continue
					}
					f := NameFormClaimFinding{
						File: rel, Line: i + 1, Claimed: claimed, Actual: actual,
					}
					if key, ok := exemptedNameFormClaim(opts.Exemptions, f); ok {
						matched[key] = true
						census.Exempted++
						continue
					}
					findings = append(findings, f)
				}
			}
		}
	}

	// Послабление, которому больше нечего исключать, — находка: иначе слепая зона
	// переживёт свой предмет и достанется следующему как «тут так принято».
	for _, e := range opts.Exemptions {
		if matched[nameFormClaimKey(e.File, e.Claimed)] {
			continue
		}
		findings = append(findings, NameFormClaimFinding{
			File: e.File, ClaimedRaw: e.Claimed,
			StaleExemption: true, Reason: e.Reason,
		})
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})

	if log != nil {
		_, _ = fmt.Fprintf(log,
			"перепись: файлов контракта %d · носителей сайта %d · утверждений о форме имени %d "+
				"(совпало с применяемой %d, снято послаблением %d) · применяется %s\n",
			census.ProtoFiles, census.DocsFiles, census.Claims,
			census.Agreeing, census.Exempted, actual)
	}
	return findings, census, nil
}

// nameAlphabetOf — разбор групп [nameFormClaimRe] в форму. Порядок групп задан
// одной регуляркой и читается одной функцией: два места разбора одной записи
// разошлись бы на первой же правке распознавателя.
func nameAlphabetOf(g []string) NameAlphabet {
	return NameAlphabet{First: g[1], Mid: g[2], Lo: g[3], Hi: g[4], Last: g[5]}
}

func nameFormClaimKey(file, claimed string) string { return file + "\x00" + claimed }

func exemptedNameFormClaim(
	list []NameFormClaimExemption, f NameFormClaimFinding,
) (string, bool) {
	key := nameFormClaimKey(f.File, f.Claimed.String())
	for _, e := range list {
		if nameFormClaimKey(e.File, e.Claimed) == key {
			return key, true
		}
	}
	return "", false
}
