// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// retiredidentityvendorceiling.go — УБЫВАЮЩИЙ ПОТОЛОК привязок к снимаемому
// издателю личности (задача #2730).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Службы прежнего провайдера личности (Hydra — выдача токена, Kratos — сессия и
// самообслуживание) снимаются. Финальный предикат снятия существовал только в
// отчёте: его никто не прогонял, и РОСТ числа привязок не замечал никто. Здесь
// он переезжает в дерево гейтом.
//
// Форма — потолок, и он УБЫВАЮЩИЙ. В дереве записано текущее число; гейт
// краснеет при его РОСТЕ и требует переписать запись при УБЫВАНИИ. Так каждый шаг
// снятия доказывается числом, а не обещанием, и обратный ход стоит отдельного
// решения человека.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЕДИНИЦА СЧЁТА — СТРОКА, И ЭТО НЕ КООРДИНАТА
//
// Считается ИСХОДНАЯ СТРОКА, несущая привязку. Строка с тремя вхождениями имени
// — одна единица. Архив — тоже ОДНА единица независимо от числа совпадений
// внутри: он либо принадлежит издателю, либо нет. Число этого гейта нельзя
// читать как «столько мест в коде» — мест меньше, а вхождений больше.
//
// ─────────────────────────────────────────────────────────────────────────────
// ТРИ ДЕРЕВА, И ФУНДАМЕНТ СРЕДИ НИХ
//
// Обход идёт по ТРЁМ деревьям продукта, а не по двум: платформа `kacho`
// (рабочее дерево прогона), служба доступа `kaname` и фундамент `corelib`. Два
// последних читаются ПО ПИНУ из `go.mod` — тем деревом, против которого
// платформа собирается, — а не по случайному состоянию соседнего клона.
//
// Фундамент сегодня даёт НОЛЬ, и это не повод его не смотреть: движок личности
// вводится именно туда, и дыра откроется ровно в тот день, когда он туда
// приедет. Ноль под потолком ноль — исправное состояние, которое гейт удержит.
//
// ─────────────────────────────────────────────────────────────────────────────
// РАСПОЗНАВАТЕЛЬ СТРОИТСЯ ОТ КЛАССА, А НЕ ОТ ПЕРЕЧНЯ ФОРМ
//
// Перечень форм («переменная окружения вида ЧТОТО_HYDRA_ЧТОТО», «имя службы
// hydra-admin», «алиас подчарта pg-hydra», «столбец hydra_client_id», «имя
// печенья ory_kratos_session», «образ oryd/hydra») — это НЕ класс, а его
// сегодняшние отпечатки: форма, о которой перечень не знает, даёт не красное и
// не зелёное, а молчание. Два гейта этого дерева уже возвращались именно за это.
//
// Класс здесь один и называется так: СТРОКА НЕСЁТ ИМЯ СНЯТОГО ИЗДАТЕЛЯ. Имя —
// `hydra`, `kratos` и пространство его образов `oryd/`; вхождение ищется
// подстрокой без учёта регистра, поэтому ВСЕ перечисленные выше формы (и любая
// будущая) попадают под ось одним признаком, а не шестью выражениями. Проверено
// обходом: каждое из восьми именных плеч источника — подмножество этой оси.
//
// Безымянная поверхность — единственное, что именем не ловится: пути API
// издателя, в которых его имени нет. Их перечень здесь НЕ заводится: он уже
// имеет единственный дом — `ProviderSurfaces` в `providersurface.go`, с
// объявленным правилом отбора («путь, который обслуживает ТОЛЬКО поставщик») и
// собственной инъекцией. Вторая копия перечня разошлась бы с первой молча.
//
// ─────────────────────────────────────────────────────────────────────────────
// АРХИВ СУДИТСЯ ПО ИМЕНИ И ПО СОДЕРЖИМОМУ — ОБЪЕДИНЕНИЕМ
//
// Плечо архива держалось на совпадениях ВНУТРИ архива (их сегодня по четыре на
// архив, а не по триста, как утверждал источник). Переименование у издателя
// внутри чарта уводит такое плечо в НОЛЬ при физически присутствующем архиве —
// зелёное при невыполненном предмете.
//
// Поэтому архив — находка, если ЛИБО его имя несёт имя издателя, ЛИБО его
// содержимое его несёт. Имя подпирает содержимое, содержимое подпирает имя.
//
// Различение своих от чужих — ПО ИМЕНИ, а не по количеству: архивов в дереве
// пять, и три из них — чужое вендоренное (cert-manager, ingress-nginx,
// postgresql), которое никуда не уходит. Предикат «архивов ноль» снёс бы их
// заодно; перечень имён наших архивов здесь не выписан — состав архивов
// ВЫВОДИТСЯ ОБХОДОМ дерева по суффиксу, а принадлежность решает тот же класс
// имени.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ГЕЙТ НЕ ВИДИТ — НАЗВАНО ВСЛУХ
//
//  1. ПРОЗА. Файлы `.md`/`.mdx` из обхода исключены, как и строки, НАЧИНАЮЩИЕСЯ
//     маркером комментария: упоминание издателя в тексте не привязывает к нему
//     дерево, а утверждение о нём как о действующем судит соседнее надгробие
//     `retiredissuerclaim.go`. Давить числом на надгробия — значит требовать
//     стереть историю.
//  2. ПОРТ. `4444`/`4445` под ось не заведены: порт принадлежит АДРЕСУ, а адрес
//     приезжает настройкой; то же число стоит у нашего собственного стенда и у
//     чужого набора соответствия. Измерено: строк с этими портами и БЕЗ имени
//     издателя на строке — единицы, и почти все они комментарии.
//  3. ПУТЬ, СОБРАННЫЙ В РАНТАЙМЕ, и адрес, прочитанный из настройки. Такого
//     предиката не существует; это держится обзором.
//  4. СВОЙ ХВОСТ. Этот файл и его пробы сами несут имя издателя и в число
//     ВХОДЯТ. Когда предмет уйдёт, гейт снимается вместе с ним — и число уйдёт
//     в ноль тем же изменением.

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// retiredVendorMarks — имя снятого издателя личности, по которому узнаётся
// класс. Не перечень форм записи: подстрока без учёта регистра накрывает любую
// форму — переменную окружения, имя службы, алиас подчарта, столбец, печенье,
// идентификатор в коде, путь файла.
//
// `oryd/` — пространство ОБРАЗОВ издателя; голое `ory` сюда НЕ входит, и это
// решение, а не упущение: под тем же именем издатель публикует БИБЛИОТЕКИ, и
// одна из них — `github.com/ory/fosite` — законно остаётся у собственной
// чеканки токенов. Ось по слову `ory` краснела бы на коде, ради которого снятие
// и делается.
//
// Предикат-источник вычитал `github.com/ory/fosite` отдельным выражением.
// Измерено здесь: строк с этой зависимостью в трёх деревьях СЕГОДНЯ ноль
// (`git grep -n "ory/fosite"` по каждому дереву; в платформе два вхождения, и
// оба — этот файл и его проба). То есть вычитание источника было исключением,
// которому нечего исключать. Здесь оно не переносится вычитанием: класс просто
// не знает слова `ory` отдельно от пространства образов, а проба
// `TestRetiredVendorCeiling_RetainedDependencyIsSilent` держит это свойство на
// синтетике — она переживёт день, когда библиотека в дерево приедет.
var retiredVendorMarks = []string{"hydra", "kratos", "oryd/"}

// retiredVendorTrees — три дерева продукта, имена как в переписи.
const (
	vendorTreePlatform   = "kacho"   // рабочее дерево прогона
	vendorTreeAccess     = "kaname"  // служба доступа, по пину go.mod
	vendorTreeFoundation = "corelib" // фундамент, по пину go.mod
)

// retiredVendorCeilings — УБЫВАЮЩИЙ ПОТОЛОК по дереву. Единица — СТРОКА.
//
// Числа получены прогоном ЭТОГО гейта на ревизии, которой он заведён (команда —
// в шапке пробы `retiredidentityvendorceiling_test.go`); чисел источника здесь
// нет ни одного: 324 и 314, названные отчётом, не воспроизводятся ни одним
// выражением обхода, а 726 посчитаны другим набором плеч и другим корпусом.
//
// Запись меняется ТОЛЬКО вниз и тем же изменением, которым снижено число.
var retiredVendorCeilings = map[string]int{
	// платформа: 1876 строк по имени + 45 по пути API + 2 архива издателя.
	// В числе 23 строки САМОГО гейта и его проб: он несёт имя издателя, как и
	// всё, что о нём говорит исполняемым текстом, и уйдёт вместе с предметом.
	vendorTreePlatform: 1923,
	// служба доступа по пину `go.mod`: 1057 по имени + 40 по пути API, архивов нет
	vendorTreeAccess: 1097,
	// фундамент по пину `go.mod`: движок сюда ещё не приехал, и потолок держит ноль
	vendorTreeFoundation: 0,
}

// errVendorEmptyWalk — обход не принёс ни одного файла: «ноль находок» здесь
// означало бы «ноль прочитанного».
var errVendorEmptyWalk = errors.New("обход пуст")

// vendorArchive — один архив дерева: его имя и извлечённый текст.
type vendorArchive struct {
	// Name — путь архива в дереве.
	Name string
	// Text — распакованное содержимое. Пусто, если распаковка не понадобилась
	// (имя уже решило) либо архив нечитаем — тогда решает одно имя.
	Text string
}

// vendorTreeCorpus — прочитанное одного дерева. Судья получает ТЕЛА, а не пути:
// инъекция обязана звать то же, что исполняется на дереве.
type vendorTreeCorpus struct {
	Bodies   map[string]string
	Archives []vendorArchive
	// LinesRead — объём осмотренного, считает читатель.
	LinesRead int
	// ProseSkipped — прозы и двоичного, не прочитанного читателем. Объём
	// осмотренного обязан быть отличим от объёма пропущенного.
	ProseSkipped int
}

// vendorBinding — одна привязка: строка либо архив.
type vendorBinding struct {
	Tree string
	File string
	// Line — номер строки; 0 у архива, который судится целиком.
	Line int
	// Axis — чем поймано: имя издателя, путь его API, имя архива, содержимое архива.
	Axis string
	Text string
}

// Оси. Названы константами, чтобы инъекция утверждала ОСЬ, а не подстроку текста.
const (
	vendorAxisName        = "имя издателя"
	vendorAxisSurface     = "путь API издателя"
	vendorAxisArchiveName = "имя архива"
	vendorAxisArchiveBody = "содержимое архива"
)

// vendorTreeCensus — объём осмотренного по дереву.
type vendorTreeCensus struct {
	Files     int
	Prose     int
	Lines     int
	Archives  int
	Bindings  int
	ByName    int
	BySurface int
	ByArchive int
	Ceiling   int
}

func (c vendorTreeCensus) String() string {
	return fmt.Sprintf("файлов судимо %d · строк прочитано %d · прозы и двоичного пропущено %d · "+
		"архивов %d · привязок %d строк (по имени %d · по пути API %d · архивов %d) · потолок %d",
		c.Files, c.Lines, c.Prose, c.Archives, c.Bindings, c.ByName, c.BySurface, c.ByArchive, c.Ceiling)
}

// vendorCeilingFinding — расхождение числа с записью.
type vendorCeilingFinding struct {
	Tree    string
	Ceiling int
	Actual  int
	Kind    string
}

// Виды расхождений: их два, и лечатся они по-разному.
const (
	// vendorFindingGrown — привязок стало БОЛЬШЕ записанного.
	vendorFindingGrown = "число выросло над потолком"
	// vendorFindingShrunk — привязок стало МЕНЬШЕ: потолок убывающий, запись
	// обязана быть переписана тем же изменением, которым число снижено.
	vendorFindingShrunk = "число убыло, а запись потолка осталась прежней"
)

func (f vendorCeilingFinding) String() string {
	switch f.Kind {
	case vendorFindingGrown:
		return fmt.Sprintf("дерево %s: привязок к снятому издателю %d строк при потолке %d "+
			"(+%d) — %s. Снятие идёт в одну сторону: либо снимите привязку, либо "+
			"объясните рост в задаче и поднимите запись СОЗНАТЕЛЬНО, отдельным решением",
			f.Tree, f.Actual, f.Ceiling, f.Actual-f.Ceiling, f.Kind)
	default:
		return fmt.Sprintf("дерево %s: привязок к снятому издателю %d строк при потолке %d "+
			"(−%d) — %s. Перепишите потолок на %d тем же изменением: потолок, переживший "+
			"своё число, прощает возврат ровно настолько, насколько успели снять",
			f.Tree, f.Actual, f.Ceiling, f.Ceiling-f.Actual, f.Kind, f.Actual)
	}
}

// vendorProseFile — файл, чьи строки НЕ судятся: проза и двоичное.
//
// Архивы сюда не попадают: у них своя ось.
func vendorProseFile(rel string) bool {
	lower := strings.ToLower(rel)
	for _, suf := range []string{".md", ".mdx", ".png", ".jpg", ".jpeg", ".ico", ".gif",
		".woff", ".woff2", ".wasm", ".pdf", ".svg", ".zip", ".jar", ".bin"} {
		if strings.HasSuffix(lower, suf) {
			return true
		}
	}
	return false
}

// vendorArchiveFile — архив, судимый целиком.
func vendorArchiveFile(rel string) bool {
	lower := strings.ToLower(rel)
	return strings.HasSuffix(lower, ".tgz") || strings.HasSuffix(lower, ".tar.gz")
}

// vendorCommentLine — строка НАЧИНАЕТСЯ маркером комментария.
//
// Маркеры выведены обходом дерева, а не памятью: измерены префиксы строк,
// несущих имя издателя. Форма `--`/`*` требует ПРОБЕЛА следом — без него это не
// комментарий, а флаг командной строки (`--env-var "…HYDRA_PUBLIC_PORT…"`),
// то есть настоящая привязка; измерено 15 таких строк на `--` в платформе.
func vendorCommentLine(line string) bool {
	s := strings.TrimLeft(line, " \t")
	switch {
	case strings.HasPrefix(s, "#"), strings.HasPrefix(s, "//"),
		strings.HasPrefix(s, "<!--"), strings.HasPrefix(s, ";"):
		return true
	case s == "--" || s == "*":
		return true
	case strings.HasPrefix(s, "-- "), strings.HasPrefix(s, "--\t"),
		strings.HasPrefix(s, "* "), strings.HasPrefix(s, "*\t"):
		return true
	}
	return false
}

// vendorLineAxis — чем строка привязана к издателю. Пусто — не привязана.
//
// lower — та же строка в нижнем регистре: класс ищется подстрокой, а не
// выражением, и регистр записи формы значения не имеет.
func vendorLineAxis(lower string) string {
	for _, m := range retiredVendorMarks {
		if strings.Contains(lower, m) {
			return vendorAxisName
		}
	}
	// Безымянная поверхность берётся у единственного дома — словаря путей
	// поставщика. Своего перечня здесь нет.
	for _, s := range ProviderSurfaces {
		if strings.Contains(lower, strings.ToLower(s.Path)) {
			return vendorAxisSurface
		}
	}
	return ""
}

// vendorArchiveAxis — чем архив привязан к издателю. Пусто — не привязан.
func vendorArchiveAxis(a vendorArchive) string {
	base := a.Name
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	lower := strings.ToLower(base)
	for _, m := range retiredVendorMarks {
		if strings.Contains(lower, strings.TrimSuffix(m, "/")) {
			return vendorAxisArchiveName
		}
	}
	body := strings.ToLower(a.Text)
	for _, m := range retiredVendorMarks {
		if strings.Contains(body, m) {
			return vendorAxisArchiveBody
		}
	}
	return ""
}

// judgeRetiredVendorCeiling — тело гейта над прочитанным. Инъекция зовёт ЕГО же.
//
// Отказ (а не находка) — там, где судить не по чему: дерева нет в обходе, либо
// его обход пуст. Третья категория не растворяется в зелёном.
func judgeRetiredVendorCeiling(
	corpora map[string]vendorTreeCorpus,
	ceilings map[string]int,
) ([]vendorCeilingFinding, map[string]vendorTreeCensus, []vendorBinding, error) {
	if len(ceilings) == 0 {
		return nil, nil, nil, fmt.Errorf("%w: потолков объявлено ноль — судить нечем", errVendorEmptyWalk)
	}
	trees := make([]string, 0, len(ceilings))
	for tree := range ceilings {
		trees = append(trees, tree)
	}
	sort.Strings(trees)

	census := make(map[string]vendorTreeCensus, len(trees))
	var findings []vendorCeilingFinding
	var bindings []vendorBinding

	for _, tree := range trees {
		corpus, ok := corpora[tree]
		if !ok {
			return nil, census, nil, fmt.Errorf("%w: дерево %q не обойдено — его число "+
				"неизвестно, и «ноль находок» означало бы «ноль прочитанного»", errVendorEmptyWalk, tree)
		}
		if len(corpus.Bodies) == 0 && len(corpus.Archives) == 0 {
			return nil, census, nil, fmt.Errorf("%w: дерево %q принесло ноль файлов", errVendorEmptyWalk, tree)
		}

		c := vendorTreeCensus{
			Lines:    corpus.LinesRead,
			Prose:    corpus.ProseSkipped,
			Archives: len(corpus.Archives),
			Ceiling:  ceilings[tree],
		}

		rels := make([]string, 0, len(corpus.Bodies))
		for rel := range corpus.Bodies {
			rels = append(rels, rel)
		}
		sort.Strings(rels)
		for _, rel := range rels {
			// Проза отсеивается ЗДЕСЬ, а не только у читателя: инъекция зовёт
			// судью напрямую, и правило отбора корпуса обязано жить в нём.
			if vendorProseFile(rel) {
				c.Prose++
				continue
			}
			c.Files++
			body := corpus.Bodies[rel]
			lowerBody := strings.ToLower(body)
			if vendorLineAxis(lowerBody) == "" {
				continue // быстрый путь: в файле нет ни одного признака класса
			}
			lines := strings.Split(body, "\n")
			lowerLines := strings.Split(lowerBody, "\n")
			for i, line := range lines {
				if vendorCommentLine(line) {
					continue
				}
				axis := vendorLineAxis(lowerLines[i])
				if axis == "" {
					continue
				}
				switch axis {
				case vendorAxisName:
					c.ByName++
				case vendorAxisSurface:
					c.BySurface++
				}
				bindings = append(bindings, vendorBinding{
					Tree: tree, File: rel, Line: i + 1, Axis: axis, Text: strings.TrimSpace(line),
				})
			}
		}

		archives := append([]vendorArchive(nil), corpus.Archives...)
		sort.Slice(archives, func(i, j int) bool { return archives[i].Name < archives[j].Name })
		for _, a := range archives {
			axis := vendorArchiveAxis(a)
			if axis == "" {
				continue
			}
			c.ByArchive++
			bindings = append(bindings, vendorBinding{Tree: tree, File: a.Name, Axis: axis, Text: a.Name})
		}

		c.Bindings = c.ByName + c.BySurface + c.ByArchive
		census[tree] = c

		switch {
		case c.Bindings > c.Ceiling:
			findings = append(findings, vendorCeilingFinding{
				Tree: tree, Ceiling: c.Ceiling, Actual: c.Bindings, Kind: vendorFindingGrown})
		case c.Bindings < c.Ceiling:
			findings = append(findings, vendorCeilingFinding{
				Tree: tree, Ceiling: c.Ceiling, Actual: c.Bindings, Kind: vendorFindingShrunk})
		}
	}
	return findings, census, bindings, nil
}
