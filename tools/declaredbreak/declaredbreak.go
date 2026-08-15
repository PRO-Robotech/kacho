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
// Сопоставление опирается на факт о ЧУЖОМ выводе: сообщения buf называют символ В
// КАВЫЧКАХ (`Previously present RPC "AddRoutes" on service "RouteTableService" was
// deleted.`). Факт снят с реального вывода buf 1.72.0 и закреплён фикстурами в testdata;
// если он перестанет быть верным, объявление не сможет сопоставиться НИКОГДА — и тогда
// оно попадёт не в «истекло», а в отдельный исход `SymbolMismatch`, который называет
// координату находки и объявленный символ. То есть отказ предпосылки виден как отказ, а
// не как ложное «послабление истекло».
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

// matches — сопоставление находки и объявления. Символ ищется В КАВЫЧКАХ: это
// предпосылка, снятая с реального вывода buf, а не догадка о его прозе.
func (d Declaration) matches(f Finding) bool {
	return d.Rule == f.Type && d.Path == f.Path && strings.Contains(f.Message, `"`+d.Symbol+`"`)
}

// sameCoordinate — правило и путь совпали, а символ нет. Отдельный исход: он означает
// либо опечатку в символе, либо отказ предпосылки о кавычках.
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
// Так выглядит систематическое расхождение ВХОДА, а не состояние контракта. Наблюдалось
// вживую: `buf breaking` из корня репозитория печатает пути с сегментом каталога
// контрактов впереди, изнутри каталога — без него; сопоставление идёт по паре
// «правило + путь», поэтому расходится всё и сразу.
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
func Adjudicate(findings []Finding, decls []Declaration) Result {
	res := Result{FindingsRead: len(findings), DeclarationsRead: len(decls)}

	for i, d := range decls {
		if problems := d.Validate(); len(problems) > 0 {
			res.Invalid = append(res.Invalid, InvalidDeclaration{Index: i, Declaration: d, Problems: problems})
		}
	}

	usedFinding := make([]bool, len(findings))
	for _, d := range decls {
		var hit bool
		var sameCoord []Finding
		for i, f := range findings {
			switch {
			case d.matches(f):
				usedFinding[i] = true
				hit = true
				res.Matched++
			case d.sameCoordinate(f):
				sameCoord = append(sameCoord, f)
			}
		}
		if hit {
			continue
		}
		if len(sameCoord) > 0 {
			res.SymbolMismatch = append(res.SymbolMismatch, Mismatch{Declaration: d, Findings: sameCoord})
			continue
		}
		res.Expired = append(res.Expired, d)
	}
	for i, f := range findings {
		if !usedFinding[i] {
			res.Undeclared = append(res.Undeclared, f)
		}
	}

	sort.SliceStable(res.Undeclared, func(i, j int) bool {
		if res.Undeclared[i].Path != res.Undeclared[j].Path {
			return res.Undeclared[i].Path < res.Undeclared[j].Path
		}
		return res.Undeclared[i].StartLine < res.Undeclared[j].StartLine
	})
	return res
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
	for _, f := range r.Undeclared {
		fmt.Fprintf(&b, "[НЕОБЪЯВЛЕННЫЙ РАЗРЫВ] %s %s: %s\n", f.Coordinate(), f.Type, f.Message)
	}
	for _, m := range r.SymbolMismatch {
		fmt.Fprintf(&b, "[СИМВОЛ НЕ СОВПАЛ] объявлено %s %s symbol=%q, но находки того же правила в этом файле называют другое:\n",
			m.Declaration.Rule, m.Declaration.Path, m.Declaration.Symbol)
		for _, f := range m.Findings {
			fmt.Fprintf(&b, "        %s: %s\n", f.Coordinate(), f.Message)
		}
	}
	for _, d := range r.Expired {
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
