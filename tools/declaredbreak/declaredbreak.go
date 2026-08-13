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
		out = append(out, f)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("чтение вывода buf: %w", err)
	}
	return out, nil
}

// LoadDeclarations читает перечень. Отсутствие файла — НЕ законный исход: перечень
// обязан существовать, иначе «объявлений нет» неотличимо от «перечень не прочитан».
func LoadDeclarations(path string) ([]Declaration, error) {
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
