// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package declaredbreak adjudicates `buf breaking` findings against a declared list.
//
// # Зачем этот гейт существует
//
// Конвейер зовёт `buf breaking --against …#branch=main` и роняет прогон на любом
// ломающем изменении контракта. Это верно ровно до того дня, когда ломающее изменение
// становится ОСОЗНАННЫМ: снятие метода, который отвечает отказом при любом входе, или
// поля, которое сервис принимает и не читает. Тогда шаг красен по построению, и у
// следующего читателя остаётся два хода, оба плохих:
//
//   - снять шаг — потерять защиту от СЛУЧАЙНЫХ разрывов на всём дереве контрактов ради
//     одного объявленного (обратный порядок: сперва снимают, потом сужают);
//   - внести файл в `breaking.ignore` конфигурации buf — послабление БЕЗ ПРЕДМЕТА И БЕЗ
//     СРОКА: исключение по ПУТИ ослепляет проверку на целом файле навсегда, и следующий
//     случайный разрыв в этом файле пройдёт молча.
//
// Поэтому шаг перестаёт быть «buf breaking» и становится адъюдикацией: разрыв проходит,
// только если он ОБЪЯВЛЕН, а объявление живёт, только пока у него есть предмет.
//
// # Три исхода, а не два
//
//   - находка, которой нет в перечне, — красное: случайный разрыв по-прежнему ловится;
//   - запись перечня, которой не соответствует ни одна находка, — ТОЖЕ красное:
//     послабление обязано истекать само. Ровно этим адъюдикация отличается от
//     `breaking.ignore`, и ровно это делает перечень непригодным как свалка: разрыв,
//     ставший историей (база сравнения поднялась после вливания), обязан быть удалён из
//     перечня тем же изменением, иначе прогон краснеет;
//   - перепись печатается ВСЕГДА — сколько находок рассмотрено, сколько записей
//     сопоставлено. «Ноль находок» и «проверка не выполнялась» обязаны быть различимы, и
//     это не абстрактное требование: в том же файле конвейера записан прецедент, когда
//     при исчерпании квоты три шага получали статус skipped, а вердикт о них выдавался.
//
// # Проверка СВОЕЙ предпосылки
//
// Сопоставление опирается на факт о ЧУЖОМ выводе: сообщения buf называют В КАВЫЧКАХ и
// предмет разрыва, и объемлющий символ (`Previously present RPC "AddRoutes" on service
// "RouteTableService" was deleted.`), и из них собирается символ находки
// ([Finding.Symbol]). Факт снят с реального вывода buf 1.72.0 и закреплён фикстурами в
// testdata; если он перестанет быть верным, объявление не сможет сопоставиться НИКОГДА —
// и тогда оно попадёт не в «истекло», а в отдельный исход `SymbolMismatch`, который
// называет координату находки, её символ и объявленный символ. То есть отказ предпосылки
// виден как отказ, а не как ложное «послабление истекло».
//
// # Запись прощает РОВНО ОДИН разрыв (kacho#2911)
//
// Прежде запись сопоставлялась по вхождению `"<symbol>"` в текст сообщения и прощала
// все находки, на которые подходила. У снятия значения перечисления buf печатает только
// номер значения и имя перечисления, и запись `symbol: Status` прощала снятие любого
// значения этого словаря; запись с именем поля — одноимённое поле любого сообщения
// файла; запись с прежним именем типа ответа — смену типа у любого RPC файла. Теперь
// символ сравнивается строгим равенством, а простившая запись расходуется
// ([Adjudicate]).
//
// # Находка, у которой поля пути НЕТ (выведено 2026-08-15)
//
// Вторая предпосылка того же рода, и она была ложной: гейт считал, что путь есть у
// КАЖДОЙ находки. У снятия файла целиком (`FILE_NO_DELETE`) ключа `path` в JSON нет
// вовсе — и это не пропуск buf, а следствие предмета: указывать не на что, файла в новом
// дереве не существует. Имя файла живёт только внутри сообщения, в кавычках:
//
//	{"start_line":1,…,"type":"FILE_NO_DELETE","message":"Previously present file \"kacho/cloud/vpc/v1/x.proto\" was deleted."}
//
// Пока путь оставался пустым, объявить такой разрыв было НЕЛЬЗЯ НИ ПРИ КАКОМ ВХОДЕ —
// канонический класс «два правила об одном поле» (`api-conventions.md`): сопоставление
// требовало `path == ""` (пустое равно пустому), а `Validate` непустой путь ТРЕБОВАЛА.
// Добросовестное объявление (путь заполнен путём снятого файла) не сопоставлялось ни с
// чем, все записи объявлялись истёкшими, срабатывал `Incoherent`, и гейт выходил кодом 2
// с диагнозом «запускай из каталога контрактов» — при запуске ИЗ каталога контрактов.
// То есть инженер получал не отказ по существу, а указание чинить не сломанное.
//
// Поэтому путь такой находки ВОССТАНАВЛИВАЕТСЯ из её сообщения (subjectPathFromMessage)
// один раз, на разборе, — и дальше сопоставление, валидация и печать работают с одним
// понятием пути, без ветки «а у этого правила по-другому». Восстановление не угадывает:
// годится ровно один путь `.proto` в кавычках; ноль или больше одного — отказ гейта
// (код 2), а не молчаливая догадка.
//
// Признак, если это когда-нибудь отвалится: находка, которую нельзя объявить (перечень
// пополняется, а прогон краснеет тем же), либо координата вида `:1` в отчёте — пустой
// путь плюс вырожденный номер строки.
package declaredbreak

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Finding — одна находка `buf breaking --error-format=json`. Формат снят с реального
// вывода buf 1.72.0: по одному JSON-объекту НА СТРОКУ (не массив).
type Finding struct {
	// Path — путь к .proto относительно каталога контрактов. У находки о снятии файла
	// целиком ключа `path` в JSON НЕТ, и ParseFindings восстанавливает путь из
	// сообщения: за пределами разбора все находки несут путь одинаково.
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	Type      string `json:"type"`
	Message   string `json:"message"`
}

// Coordinate — то, что печатается человеку.
func (f Finding) Coordinate() string {
	return fmt.Sprintf("%s:%d", f.Path, f.StartLine)
}

// Symbol — символ находки: то, по чему запись перечня называет РОВНО ЭТОТ разрыв.
// Отчёт печатает его у каждого разрыва вне перечня, и запись берёт его оттуда дословно.
//
// ГДЕ СООБЩЕНИЕ НАЗЫВАЕТ ОБЪЕМЛЮЩИЙ СИМВОЛ, символ — `<контейнер>.<предмет>`:
//
//	Previously present field "10" with name "x" on message "SecurityGroupRule" …  → SecurityGroupRule.10
//	Previously present enum value "4" on enum "Status" was deleted.             → Status.4
//	RPC "Create" on service "NetworkService" changed response type from …       → NetworkService.Create
//	Message "RequiredProbe" had required field "2" deleted. …                   → RequiredProbe.2
//
// Предмет — ПЕРВОЕ кавычечное вхождение до контейнера: у поля и значения перечисления
// это НОМЕР, у RPC и зарезервированного имени — имя, у зарезервированного диапазона —
// его запись. Номер, а не имя поля, потому что номер buf печатает у поля в каждой форме
// сообщения, а имя — не в каждой (у переименования его нет), и одно поле получило бы два
// написания. У обязательного поля buf пишет контейнер ПЕРВЫМ, и символ — тот же `M.N`.
//
// ГДЕ КОНТЕЙНЕРА В СООБЩЕНИИ НЕТ, символ — первое кавычечное вхождение вида имени
// (снятое сообщение, перечисление, служба; параметр сообщения). Номер именем не является
// и отсеивается. Различить два таких разрыва одного файла символ не может, и держит их
// не символ, а расходование записи ([Adjudicate]).
//
// У снятия ФАЙЛА символ совпадает с путём: предмет такой находки — сам файл.
//
// Пустой символ — «предмет не назван»: контейнер есть, а кавычечного предмета до него
// нет, либо имени в кавычках нет вовсе. Такую находку [ParseFindings] отвергает, а не
// делает символом контейнер: символ-контейнер прощал бы любой разрыв внутри него.
//
// ЧЕГО СИМВОЛ НЕ РАЗЛИЧАЕТ. Контейнер buf называет простым именем, без объемлющих
// сообщений (`Instance.Status` печатается как `Status`). Два одноимённых вложенных
// контейнера одного файла символ не различает; запись прощает один разрыв из двух, но
// не выбирает, какой.
func (f Finding) Symbol() string {
	if f.Type == fileDeletionRule {
		return f.Path
	}
	if m := requiredFieldRe.FindStringSubmatch(f.Message); m != nil {
		return m[1] + "." + m[2]
	}
	if loc := containerRe.FindStringSubmatchIndex(f.Message); loc != nil {
		subject := quotedRe.FindStringSubmatch(f.Message[:loc[0]])
		if subject == nil {
			return ""
		}
		return f.Message[loc[2]:loc[3]] + "." + subject[1]
	}
	for _, m := range quotedRe.FindAllStringSubmatch(f.Message, -1) {
		if nameRe.MatchString(m[1]) {
			return m[1]
		}
	}
	return ""
}

// fileDeletionRule — правило снятия файла целиком: у его находки поля пути нет, и путь
// восстанавливается из сообщения (subjectPathFromMessage).
const fileDeletionRule = "FILE_NO_DELETE"

// Declaration — объявленный разрыв. Каждое поле обязательно, и у каждого есть причина
// быть обязательным (см. Validate).
type Declaration struct {
	Rule   string `yaml:"rule"`
	Path   string `yaml:"path"`
	Symbol string `yaml:"symbol"`
	Reason string `yaml:"reason"`
	Issue  string `yaml:"issue"`
}

// declaredFile — форма файла перечня.
type declaredFile struct {
	Declared []Declaration `yaml:"declared"`
}

var (
	// Идентификатор правила buf: заглавные и подчёркивания (`RPC_NO_DELETE`).
	ruleRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,}$`)
	// Ссылка на задачу ОБЯЗАНА быть разрешимой: голый `#71` разрешить нельзя ни
	// человеком, ни машиной, значит истечь по нему объявление не может в принципе.
	issueRe = regexp.MustCompile(`^([a-z0-9][a-z0-9._-]*#[0-9]+|https://github\.com/[A-Za-z0-9._-]+/[A-Za-z0-9._-]+/issues/[0-9]+)$`)
	// Путь контракта, названный В КАВЫЧКАХ внутри сообщения buf. Кавычки — та же
	// предпосылка, на которой стоит сопоставление символа, и у неё две пробы:
	// TestPremiseSymbolIsQuoted (символ внутри файла) и TestFileDeletionFindingHasNoPathKey
	// вместе с TestParseRestoresPathOfDeletedFile (путь снятого файла).
	quotedProtoRe = regexp.MustCompile(`"([^"]+\.proto)"`)
	// Имя в кавычках — материал символа находки (Finding.Symbol).
	quotedRe = regexp.MustCompile(`"([^"]+)"`)
	// Вид имени: номер поля либо значения перечисления («"4"») именем не является.
	nameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)
	// Объемлющий символ предмета: так buf пишет его во всех формах сообщения, где он
	// есть (описание поля, значение перечисления, RPC, oneof, зарезервированные имя и
	// диапазон).
	containerRe = regexp.MustCompile(` on (?:message|enum|service) "([^"]+)"`)
	// Единственная форма, где контейнер стоит ПЕРВЫМ, — обязательное поле.
	requiredFieldRe = regexp.MustCompile(`^Message "([^"]+)" had required field "([^"]+)" `)
)

// minReasonLen — причина короче этого не является причиной. Число не выведено из
// измерения и не притворяется им: это порог читаемости, при котором «x» и «нужно» не
// проходят, а осмысленная фраза проходит.
const minReasonLen = 24

// Validate проверяет саму запись перечня. Негодная запись — находка, а не повод
// пропустить разрыв: перечень, который принимает мусор, послабляет молча.
func (d Declaration) Validate() []string {
	var out []string
	switch {
	case d.Rule == "":
		out = append(out, "rule: пусто — без правила запись не сопоставима ни с одной находкой")
	case !ruleRe.MatchString(d.Rule):
		out = append(out, fmt.Sprintf("rule %q: не похоже на идентификатор правила buf (ожидается вид RPC_NO_DELETE)", d.Rule))
	}
	switch {
	case d.Path == "":
		out = append(out, "path: пусто — объявление без координаты пропускало бы разрыв в любом файле")
	case !strings.HasSuffix(d.Path, ".proto"):
		out = append(out, fmt.Sprintf("path %q: ожидается путь к .proto относительно каталога контрактов", d.Path))
	}
	if d.Symbol == "" {
		out = append(out, "symbol: пусто — объявление без символа пропускало бы любой разрыв того же правила в этом файле")
	}
	switch {
	case d.Reason == "":
		out = append(out, "reason: пусто — объявление без причины нечем оспорить при снятии")
	case len([]rune(d.Reason)) < minReasonLen:
		out = append(out, fmt.Sprintf("reason %q: короче %d символов — это не причина", d.Reason, minReasonLen))
	}
	switch {
	case d.Issue == "":
		out = append(out, "issue: пусто — у объявления нет предмета, по которому его снимут")
	case !issueRe.MatchString(d.Issue):
		out = append(out, fmt.Sprintf("issue %q: ссылка неразрешима — назови репозиторий (kacho#244) либо полный URL", d.Issue))
	}
	return out
}

// matches — сопоставление находки и объявления: правило, путь и символ находки
// СТРОГИМ равенством. Вхождение символа в текст сообщения здесь стояло до kacho#2911 и
// прощало соседей: `"Status"` входит в сообщение о снятии любого значения перечисления.
func (d Declaration) matches(f Finding, symbol string) bool {
	return d.Rule == f.Type && d.Path == f.Path && d.Symbol == symbol
}

// sameCoordinate — правило и путь совпали, а символ нет. Отдельный исход: он означает
// опечатку в символе, запись в прежней форме символа (перечисление без номера значения,
// поле без сообщения) либо отказ предпосылки о кавычках.
func (d Declaration) sameCoordinate(f Finding) bool {
	return d.Rule == f.Type && d.Path == f.Path
}

// Result — исход адъюдикации. Перепись обязательна и печатается всегда.
type Result struct {
	FindingsRead     int
	DeclarationsRead int
	Matched          int

	Undeclared     []Finding
	Expired        []Declaration
	SymbolMismatch []Mismatch
	Invalid        []InvalidDeclaration

	// spent — индексы Undeclared, чей символ совпал с записью, уже простившей другой
	// разрыв. Без пометки читатель увидел бы «необъявленный» у символа, который в
	// перечне стоит, и искал бы опечатку.
	spent map[int]bool
	// repeated — индексы Expired, чей разрыв есть, но его уже простила другая запись.
	// «Разрыва больше нет» о такой записи было бы неправдой.
	repeated map[int]bool
}

// Mismatch — объявление, у которого нашлась находка того же правила в том же файле, но
// символ не совпал.
type Mismatch struct {
	Declaration Declaration
	Findings    []Finding
}

// InvalidDeclaration — негодная запись перечня.
type InvalidDeclaration struct {
	Index       int
	Declaration Declaration
	Problems    []string
}

// Clean — нечего сообщать.
func (r Result) Clean() bool {
	return len(r.Undeclared) == 0 && len(r.Expired) == 0 && len(r.SymbolMismatch) == 0 && len(r.Invalid) == 0
}

// Incoherent — вердикт противоречит сам себе, значит гейт судил не о том, о чём думает.
//
// Подпись: находки есть · записи перечня есть · не сопоставилось НИ ОДНО · и при этом
// КАЖДАЯ запись объявлена истёкшей («разрыва больше нет»). Две половины исключают друг
// друга: если разрывов нет — не было бы находок; если находки есть — перечень не может
// быть пустым по предмету целиком.
//
// Так выглядит систематическое расхождение ВХОДА, а не состояние контракта.
//
// ПРИЧИН У ЭТОГО РАСХОЖДЕНИЯ НАБЛЮДАЛОСЬ НЕ МЕНЬШЕ ДВУХ, и они разные. 2026-08-13 —
// форма путей: `buf breaking` из корня репозитория печатает пути с сегментом каталога
// контрактов впереди, изнутри каталога — без него, а перечень написан во второй форме.
// 2026-09-11 (#2594) — судилось не то дерево: прогон шёл в рабочей копии, а уезжала
// другая ревизия, и числа отказа сошлись с копией. Сопоставление идёт по паре
// «правило + путь», поэтому в обоих случаях расходится всё и сразу.
//
// Отсюда правило для ТЕКСТА отказа ([Result.IncoherentReport]): причину гейт не
// называет. Из двух наблюдавшихся он не различает ни одной — подпись у них общая, — и
// названная наугад уводит читателя чинить исправное. Печатается то, что гейт видел:
// обе половины числами и по образцу с каждой стороны.
//
// Почему это отдельный исход, а не находка: у гейта три кода, и третий — «не смог
// работать» — существует ровно для случаев, когда он не вправе утверждать о предмете.
// Напечатать 22 строки про необъявленные разрывы значит заявить о состоянии контракта,
// которого гейт не проверял, — ложная уверенность вместо видимого отказа.
//
// Подпись НАМЕРЕННО узкая. «Ноль сопоставлено» само по себе законно (первое снятие при
// пустом перечне), «все записи истекли» тоже (перечень пережил свой предмет), и одно
// совпадение уже доказывает, что формы путей совместимы — тогда остаток есть
// содержательная находка, и подавлять её нельзя.
func (r Result) Incoherent() bool {
	return r.FindingsRead > 0 &&
		r.DeclarationsRead > 0 &&
		r.Matched == 0 &&
		len(r.Expired) == r.DeclarationsRead
}

// ParseFindings читает вывод `buf breaking --error-format=json`: по объекту на строку.
// Пустой ввод — законный исход (разрывов нет), а не ошибка.
func ParseFindings(r io.Reader) ([]Finding, error) {
	var out []Finding
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for line := 1; sc.Scan(); line++ {
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		if !strings.HasPrefix(raw, "{") {
			// buf печатает в stdout только находки; всё прочее — признак того, что
			// шаг вообще не сделал своей работы, и молчать об этом нельзя.
			return nil, fmt.Errorf("строка %d вывода buf не является объектом JSON: %q", line, truncate(raw, 200))
		}
		var f Finding
		if err := json.Unmarshal([]byte(raw), &f); err != nil {
			return nil, fmt.Errorf("строка %d вывода buf не разобрана: %w", line, err)
		}
		if f.Path == "" {
			p, err := subjectPathFromMessage(f.Message)
			if err != nil {
				return nil, fmt.Errorf("строка %d вывода buf: %w", line, err)
			}
			f.Path = p
		}
		if f.Symbol() == "" {
			return nil, fmt.Errorf("строка %d вывода buf: предмет разрыва не назван в кавычках (%q) — "+
				"символа, которым запись перечня назвала бы ровно этот разрыв, нет", line, truncate(f.Message, 200))
		}
		out = append(out, f)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("чтение вывода buf: %w", err)
	}
	return out, nil
}

// subjectPathFromMessage восстанавливает путь находки, у которой поля пути нет.
//
// ПОЧЕМУ ПО ТЕКСТУ СООБЩЕНИЯ, А НЕ ПО ПОЛЮ. Поля нет не потому, что buf его забыл, а
// потому, что предмет находки — сам файл, которого в новом дереве уже не существует:
// указывать не на что. Единственное место, где buf называет предмет, — сообщение, и
// называет он его в кавычках (`Previously present file "kacho/…/x.proto" was deleted.`).
// Это та же предпосылка о кавычках, на которой стоит сопоставление символа, и снята она
// с того же реального вывода buf 1.72.0 — фикстура testdata/buf-breaking-file-deleted.jsonl.
//
// Не снимай эту функцию как «разбор чужой прозы»: без неё объявить снятие файла нельзя
// НИ ПРИ КАКОМ входе (сопоставление хотело пустой путь, валидация — непустой), и гейт
// отвечал кодом «не смог работать» с диагнозом про рабочий каталог, к делу не
// относящимся. Предмет проверяется пробами TestFileDeletionCanBeDeclared и
// TestFileDeletionDeclarationExpires.
//
// НЕ УГАДЫВАЕТ: годится ровно один путь `.proto` в кавычках. Ноль (форма сообщения
// изменилась) и больше одного (сообщение называет два файла — какой из них предмет,
// решить нечем) — отказ, который вызывающий обязан довести до кода 2. Молчаливая
// догадка здесь дала бы объявление, сопоставленное не с тем разрывом.
func subjectPathFromMessage(msg string) (string, error) {
	m := quotedProtoRe.FindAllStringSubmatch(msg, -1)
	switch len(m) {
	case 1:
		return m[0][1], nil
	case 0:
		return "", fmt.Errorf(
			"находка без поля path, и её сообщение не называет ни одного пути .proto в кавычках: %q — "+
				"предпосылка гейта о форме вывода buf больше не верна, сопоставить такую находку нечем",
			truncate(msg, 200))
	default:
		return "", fmt.Errorf(
			"находка без поля path, а её сообщение называет %d путей .proto в кавычках: %q — "+
				"какой из них предмет находки, решить нечем, и угадывать гейт не вправе",
			len(m), truncate(msg, 200))
	}
}

// LoadDeclarations читает перечень. Отсутствие файла — НЕ законный исход: перечень
// обязан существовать, иначе «объявлений нет» неотличимо от «перечень не прочитан».
func LoadDeclarations(path string) ([]Declaration, error) {
	// #nosec G304 -- путь к перечню задаёт вызывающий этого инструмента: конвейер строкой
	// шага либо инженер аргументом командной строки. Инструмент собирается и запускается
	// ВНЕ обслуживания запросов и не принимает ввода извне периметра сборки, поэтому
	// подстановки чужого пути здесь неоткуда взяться. Сузить до постоянного имени нельзя:
	// именно возможность назвать другой перечень делает инструмент проверяемым — на ней
	// стоят пробы, подставляющие свои наборы объявлений.
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("перечень объявленных разрывов не прочитан (%s): %w", path, err)
	}
	var df declaredFile
	if err := yaml.Unmarshal(raw, &df); err != nil {
		return nil, fmt.Errorf("перечень %s не разобран: %w", path, err)
	}
	return df.Declared, nil
}

// Adjudicate сводит находки с объявлениями.
//
// ЗАПИСЬ ПРОЩАЕТ РОВНО ОДИН РАЗРЫВ: простившая запись расходуется, и следующая находка с
// тем же символом ищет СВОЮ запись. Без этого одна запись прощала бы сколько угодно
// находок, которые её символ не различает (kacho#2911): у параметра сообщения buf имени
// сообщения не печатает, одноимённые вложенные контейнеры одного файла он называет одним
// и тем же простым именем. Два неразличимых разрыва объявляются двумя записями — тогда
// число записей и есть утверждение объявившего, сколько таких разрывов он допускает.
//
// Нерасходованная запись — один из трёх исходов, и они не сливаются: её разрыв есть, но
// его простила другая запись (повтор); в том же файле тем же правилом назван другой
// символ (символ не совпал); разрыва нет вовсе (истекло).
func Adjudicate(findings []Finding, decls []Declaration) Result {
	res := Result{FindingsRead: len(findings), DeclarationsRead: len(decls)}

	for i, d := range decls {
		if problems := d.Validate(); len(problems) > 0 {
			res.Invalid = append(res.Invalid, InvalidDeclaration{Index: i, Declaration: d, Problems: problems})
		}
	}

	symbols := make([]string, len(findings))
	for i, f := range findings {
		symbols[i] = f.Symbol()
	}

	usedDecl := make([]bool, len(decls))
	var undeclared []int
	spentFinding := map[int]bool{}
	for i, f := range findings {
		hit, spent := -1, false
		for j, d := range decls {
			if !d.matches(f, symbols[i]) {
				continue
			}
			if usedDecl[j] {
				spent = true
				continue
			}
			hit = j
			break
		}
		if hit >= 0 {
			usedDecl[hit] = true
			res.Matched++
			continue
		}
		if spent {
			spentFinding[i] = true
		}
		undeclared = append(undeclared, i)
	}
	sort.SliceStable(undeclared, func(a, b int) bool {
		fa, fb := findings[undeclared[a]], findings[undeclared[b]]
		if fa.Path != fb.Path {
			return fa.Path < fb.Path
		}
		return fa.StartLine < fb.StartLine
	})
	for _, i := range undeclared {
		if spentFinding[i] {
			if res.spent == nil {
				res.spent = map[int]bool{}
			}
			res.spent[len(res.Undeclared)] = true
		}
		res.Undeclared = append(res.Undeclared, findings[i])
	}

	for j, d := range decls {
		if usedDecl[j] {
			continue
		}
		var repeat bool
		var sameCoord []Finding
		for i, f := range findings {
			switch {
			case d.matches(f, symbols[i]):
				repeat = true
			case d.sameCoordinate(f):
				sameCoord = append(sameCoord, f)
			}
		}
		switch {
		case repeat:
			if res.repeated == nil {
				res.repeated = map[int]bool{}
			}
			res.repeated[len(res.Expired)] = true
			res.Expired = append(res.Expired, d)
		case len(sameCoord) > 0:
			res.SymbolMismatch = append(res.SymbolMismatch, Mismatch{Declaration: d, Findings: sameCoord})
		default:
			res.Expired = append(res.Expired, d)
		}
	}
	return res
}

// IncoherentReport — текст отказа «гейт не смог работать». Пуст, когда вердикт
// согласован: отчёт об отказе существует только у отказа.
//
// # Отказ называет ТО, ЧТО ВИДЕЛ, и не называет причины, которой не проверял
//
// Здесь стояло «наиболее вероятная причина — форма путей, запускай из каталога
// контрактов». Как рассказ об одном случае (2026-08-13) это верно; как НАЗВАННАЯ
// ПРИЧИНА ЭТОГО отказа — нет: подпись одна, а причин наблюдалось не меньше двух
// (перечень — у [Result.Incoherent]). 2026-09-11 прогонщик звал `buf` именно изнутри
// каталога, а расхождение пришло оттуда, что судимое дерево было не тем, которое
// уезжало (#2594) — читатель пошёл чинить форму путей и дефекта там не нашёл, потому
// что его там не было.
//
// Догадка, которую гейт не в силах проверить, опаснее её отсутствия: она уводит от
// предмета и стоит захода. Поэтому отказ печатает обе половины вердикта числами и по
// ОБРАЗЦУ с каждой стороны — данные, которые гейт действительно видел, — а вывод о
// причине оставляет читателю: у него есть то, чего у гейта нет, — знание, какое
// дерево судилось и каким вызовом получен вход.
func (r Result) IncoherentReport() string {
	if !r.Incoherent() {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b,
		"declared-break: гейт не смог работать — вердикт противоречит сам себе: "+
			"осмотрено находок %d, записей перечня %d, сопоставлено 0, и при этом ВСЕ %d "+
			"записи объявлены истёкшими. Обе половины разом верны быть не могут.\n",
		r.FindingsRead, r.DeclarationsRead, r.DeclarationsRead)
	b.WriteString("Сопоставление идёт по паре «правило + путь». Вот по одному образцу с каждой стороны:\n")
	if len(r.Undeclared) > 0 {
		f := r.Undeclared[0]
		fmt.Fprintf(&b, "  находка buf:     rule=%s path=%s\n", f.Type, f.Path)
	}
	if len(r.Expired) > 0 {
		d := r.Expired[0]
		fmt.Fprintf(&b, "  запись перечня:  rule=%s path=%s\n", d.Rule, d.Path)
	}
	b.WriteString("Причину гейт не называет: он видел только эти две стороны и не знает ни того, " +
		"каким вызовом получен вход, ни того, о каком дереве он собран. Сверьте обе величины сами.\n")
	return b.String()
}

// Report — человекочитаемый вердикт. Перепись идёт ПЕРВОЙ строкой: вердикт без объёма,
// стоящего за ним, — тот самый класс утверждений, который этот гейт и ловит.
func (r Result) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "declared-break: осмотрено находок buf %d, записей перечня %d; сопоставлено %d\n",
		r.FindingsRead, r.DeclarationsRead, r.Matched)

	for _, iv := range r.Invalid {
		fmt.Fprintf(&b, "[НЕГОДНАЯ ЗАПИСЬ] перечень, запись %d (%s %s %s):\n", iv.Index+1,
			iv.Declaration.Rule, iv.Declaration.Path, iv.Declaration.Symbol)
		for _, p := range iv.Problems {
			fmt.Fprintf(&b, "        %s\n", p)
		}
	}
	for i, f := range r.Undeclared {
		fmt.Fprintf(&b, "[НЕОБЪЯВЛЕННЫЙ РАЗРЫВ] %s %s symbol=%q: %s\n", f.Coordinate(), f.Type, f.Symbol(), f.Message)
		if r.spent[i] {
			b.WriteString("        запись с этим символом уже простила другой разрыв — запись прощает ровно один " +
				"разрыв, и каждый разрыв с неразличимым символом объявляется своей записью\n")
		}
	}
	for _, m := range r.SymbolMismatch {
		fmt.Fprintf(&b, "[СИМВОЛ НЕ СОВПАЛ] объявлено %s %s symbol=%q, но находки того же правила в этом файле называют другое:\n",
			m.Declaration.Rule, m.Declaration.Path, m.Declaration.Symbol)
		for _, f := range m.Findings {
			fmt.Fprintf(&b, "        %s symbol=%q: %s\n", f.Coordinate(), f.Symbol(), f.Message)
		}
	}
	for i, d := range r.Expired {
		if r.repeated[i] {
			fmt.Fprintf(&b, "[ПОСЛАБЛЕНИЕ ИСТЕКЛО] %s %s symbol=%q (%s) — этот разрыв уже простила другая запись: "+
				"запись прощает ровно один разрыв, и повтор обязан быть удалён\n", d.Rule, d.Path, d.Symbol, d.Issue)
			continue
		}
		fmt.Fprintf(&b, "[ПОСЛАБЛЕНИЕ ИСТЕКЛО] %s %s symbol=%q (%s) — разрыва больше нет, запись обязана быть удалена\n",
			d.Rule, d.Path, d.Symbol, d.Issue)
	}

	if r.Clean() {
		fmt.Fprintf(&b, "[PASS] каждый разрыв объявлен, у каждого объявления есть предмет\n")
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
