// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// subscriptionformsingularity.go — анализатор «запрос подписки объявлен ОДИН раз».
//
// # Что он считает
//
// Объявления запроса подписки в дереве контракта: общее — в пакете
// `kacho.cloud.subscription`, доменные — где угодно ещё. Ожидаемое число
// ВЫВОДИТСЯ: одна общая форма плюс столько доменных, сколько стоит в ведомости
// послаблений. Снял запись — ожидание уменьшилось само; выписанной константы,
// которую надо помнить и править, здесь нет.
//
// # Почему не поиск по тексту
//
// Слово `message` встречается в комментариях, а комментариев в этом дереве
// больше, чем объявлений. Поэтому текст сперва вычищается от комментариев, и
// только потом считаются объявления ВЕРХНЕГО УРОВНЯ — вложенное сообщение с
// подходящим именем формой подписки не является и в счёт не идёт.
//
// Отдельно: `git grep -c` для этого не годится вовсе — он печатает `файл:число`
// по каждому файлу, а через `wc -l` считает ФАЙЛЫ, а не объявления. Два запроса
// в одном файле дали бы единицу.
//
// # Почему гейт, а не договорённость
//
// Второй язык фильтров заводится не злым умыслом, а естественным ходом: домену
// нужна подписка, общая форма чем-то не подошла, и рядом появляется своя — со
// своим набором осей, своей позицией и своим смыслом пустого поля. Через семь
// доменов их семь, и «единая подписка» существует только в названии эпика.
// Заметить это на обзоре нельзя: каждое отдельное объявление защитимо.
//
// # Ведомость послаблений и её самоистечение
//
// Доменное объявление, ещё не переведённое на общую форму, стоит в ведомости с
// причиной и условием истечения. Запись, которой в дереве больше нечего
// исключать, — САМА НАХОДКА: иначе она переживёт своё снятие и разрешит
// следующему завести доменный запрос под тем же именем.
//
// # Пустая ведомость — это ЦЕЛЬ, а не поломка
//
// Соседнее надгробие (`retiredrpcsurface.go`) на пустой переписи падает, и это
// верно ДЛЯ НЕГО: надгробие не истекает, пустое означает «гейту нечего
// охранять». Здесь всё наоборот — пустая ведомость означает, что переводить
// больше нечего, то есть ровно то состояние, ради которого эпик и заведён.
// Падение на ней толкало бы держать запись ради зелёного.
//
// Падает анализатор на ПУСТОМ ОБХОДЕ: ноль прочитанных файлов контракта или ноль
// прочитанных сообщений — тогда «ноль находок» неотличимо от «ноль прочитанного».
package repohygiene

import (
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// SubscriptionCommonPackage — пакет, в котором форма подписки объявляется один раз.
const SubscriptionCommonPackage = "kacho.cloud.subscription"

// SubscriptionCommonRequest — имя общей формы запроса подписки.
const SubscriptionCommonRequest = "SubscriptionRequest"

// SubscriptionRequestAllowance — одно послабление: доменный запрос подписки,
// который ещё не переведён на общую форму.
type SubscriptionRequestAllowance struct {
	// Symbol — полное имя сообщения (`kacho.cloud.compute.v1.WatchRequest`).
	Symbol string
	// Issue — РАЗРЕШИМАЯ ссылка на задачу перевода. Послабление без задачи не
	// может истечь даже в принципе: нечему закрыться.
	Issue string
	// Reason — почему домен ещё не переведён.
	Reason string
	// ExpiresWhen — предикат снятия записи, а не «когда-нибудь потом».
	ExpiresWhen string
}

// SubscriptionSingularityOptions — вход анализатора.
type SubscriptionSingularityOptions struct {
	// Root — корень репозитория.
	Root string
	// ProtoRoot — путь (относительно Root) к дереву исходного контракта.
	ProtoRoot string
	// Allow — ведомость послаблений. ПУСТАЯ ведомость законна и является целью.
	Allow []SubscriptionRequestAllowance
}

// SubscriptionSingularityCensus — то, что анализатор прочитал.
type SubscriptionSingularityCensus struct {
	ProtoFiles       int
	TopLevelMessages int
	NestedMessages   int
	// RequestDecls — объявлений запроса подписки найдено (общих и доменных вместе).
	RequestDecls int
	CommonDecls  int
	DomainDecls  int
	Allowances   int
	// Expected — выведенное ожидание: одна общая форма плюс ведомость.
	Expected int
}

// SubscriptionSingularityFinding — одна находка.
type SubscriptionSingularityFinding struct {
	// Kind — "undeclared-domain-request" | "stale-allowance" |
	// "missing-common-form" | "duplicate-common-form" | "second-form-in-common-package".
	Kind   string
	Symbol string
	Where  string
	Reason string
}

func (f SubscriptionSingularityFinding) String() string {
	where := f.Where
	if where == "" {
		where = "в дереве не найдено"
	}
	return f.Kind + " " + f.Symbol + " (" + where + "): " + f.Reason
}

var (
	subMessageRe = regexp.MustCompile(`\bmessage\s+([A-Za-z0-9_]+)`)
	subLineRe    = regexp.MustCompile(`(?m)//.*$`)
	subBlockRe   = regexp.MustCompile(`(?s)/\*.*?\*/`)
)

// isSubscriptionRequestName — принадлежит ли имя семейству «запрос подписки».
//
// Семейство закрыто и названо здесь, а не выведено из вкуса: `WatchRequest` —
// имя, под которым доменная форма жила у compute; `SubscribeRequest` — под
// которым её заводят чаще всего; суффикс `SubscriptionRequest` покрывает и общую
// форму, и любую доменную, названную по предмету.
func isSubscriptionRequestName(name string) bool {
	return name == "WatchRequest" ||
		name == "SubscribeRequest" ||
		strings.HasSuffix(name, "SubscriptionRequest")
}

// AuditSubscriptionFormSingularity читает дерево контракта и возвращает
// расхождения между объявленными запросами подписки и ожидаемым составом.
func AuditSubscriptionFormSingularity(
	opts SubscriptionSingularityOptions, out io.Writer,
) ([]SubscriptionSingularityFinding, SubscriptionSingularityCensus, error) {
	var c SubscriptionSingularityCensus
	c.Allowances = len(opts.Allow)
	c.Expected = 1 + c.Allowances

	allow := map[string]SubscriptionRequestAllowance{}
	for _, a := range opts.Allow {
		if !strings.Contains(a.Symbol, ".") {
			return nil, c, fmt.Errorf(
				"запись ведомости %q не имеет формы `<пакет>.<Сообщение>` — сопоставить её не с чем", a.Symbol)
		}
		if a.Issue == "" {
			return nil, c, fmt.Errorf(
				"запись ведомости %q не называет задачу — послабление без задачи не может истечь", a.Symbol)
		}
		allow[a.Symbol] = a
	}

	// symbol -> файл, где объявлено
	declaredAt := map[string]string{}
	var domain []string
	var common []string

	err := rootedWalk(filepath.Join(opts.Root, opts.ProtoRoot), func(rel string) bool {
		return strings.HasSuffix(rel, ".proto")
	}, func(path string, b []byte) error {
		c.ProtoFiles++
		// Комментарии снимаются ДО всего: скобка в прозе сместила бы уровень
		// вложенности на весь остаток файла, а слово `message` в комментарии
		// засчиталось бы за объявление.
		clean := subBlockRe.ReplaceAllString(string(b), "")
		clean = subLineRe.ReplaceAllString(clean, "")

		pkg := ""
		if m := protoPackageRe.FindStringSubmatch(clean); m != nil {
			pkg = m[1]
		}
		rel, relErr := filepath.Rel(opts.Root, path)
		if relErr != nil {
			rel = path
		}

		for _, loc := range subMessageRe.FindAllStringSubmatchIndex(clean, -1) {
			name := clean[loc[2]:loc[3]]
			depth := strings.Count(clean[:loc[0]], "{") - strings.Count(clean[:loc[0]], "}")
			if depth != 0 {
				c.NestedMessages++
				continue
			}
			c.TopLevelMessages++
			if !isSubscriptionRequestName(name) {
				continue
			}
			symbol := name
			if pkg != "" {
				symbol = pkg + "." + name
			}
			c.RequestDecls++
			declaredAt[symbol] = rel
			if pkg == SubscriptionCommonPackage {
				c.CommonDecls++
				common = append(common, symbol)
				continue
			}
			c.DomainDecls++
			domain = append(domain, symbol)
		}
		return nil
	})
	if err != nil {
		return nil, c, err
	}
	if c.ProtoFiles == 0 || c.TopLevelMessages == 0 {
		return nil, c, fmt.Errorf(
			"в дереве контракта %q прочитано файлов %d, сообщений верхнего уровня %d — "+
				"гейт не может отличить «ноль находок» от «ноль прочитанного»",
			opts.ProtoRoot, c.ProtoFiles, c.TopLevelMessages)
	}

	var findings []SubscriptionSingularityFinding
	sort.Strings(common)
	sort.Strings(domain)

	// ── 1. Общая форма: ровно одна, и именно та ──────────────────────────────
	wantCommon := SubscriptionCommonPackage + "." + SubscriptionCommonRequest
	seenCommon := false
	for _, sym := range common {
		if sym == wantCommon {
			if seenCommon {
				findings = append(findings, SubscriptionSingularityFinding{
					Kind: "duplicate-common-form", Symbol: sym, Where: declaredAt[sym],
					Reason: "общая форма объявлена дважды",
				})
			}
			seenCommon = true
			continue
		}
		findings = append(findings, SubscriptionSingularityFinding{
			Kind: "second-form-in-common-package", Symbol: sym, Where: declaredAt[sym],
			Reason: "в общем пакете объявлен ВТОРОЙ запрос подписки. Единственная общая форма — " +
				wantCommon + "; второй язык фильтров рядом с ней делает «единую подписку» " +
				"названием, а не свойством",
		})
	}
	if !seenCommon {
		findings = append(findings, SubscriptionSingularityFinding{
			Kind: "missing-common-form", Symbol: wantCommon,
			Reason: "общей формы запроса подписки в дереве нет. Пока её нет, всякое доменное " +
				"объявление — не послабление на время перевода, а единственная форма, и " +
				"следующий домен заведёт свою",
		})
	}

	// ── 2. Доменные объявления: только по ведомости ──────────────────────────
	for _, sym := range domain {
		if _, ok := allow[sym]; ok {
			if out != nil {
				_, _ = fmt.Fprintf(out, "  послабление: %s — %s (%s; истекает: %s)\n",
					sym, allow[sym].Reason, allow[sym].Issue, allow[sym].ExpiresWhen)
			}
			continue
		}
		findings = append(findings, SubscriptionSingularityFinding{
			Kind: "undeclared-domain-request", Symbol: sym, Where: declaredAt[sym],
			Reason: "домен объявил СВОЙ запрос подписки со своим набором осей. Подписка " +
				"объявляется один раз в " + SubscriptionCommonPackage + ", домен её ИМПОРТИРУЕТ. " +
				"Если перевод отложен — заведи послабление с задачей и предикатом истечения",
		})
	}

	// ── 3. Самоистечение: записи, которой нечего исключать, быть не должно ───
	stale := make([]string, 0, len(allow))
	for sym := range allow {
		if _, ok := declaredAt[sym]; !ok {
			stale = append(stale, sym)
		}
	}
	sort.Strings(stale)
	for _, sym := range stale {
		findings = append(findings, SubscriptionSingularityFinding{
			Kind: "stale-allowance", Symbol: sym,
			Reason: "послаблению больше нечего исключать: объявления с таким именем в дереве нет. " +
				"Запись обязана быть снята — оставленная, она унаследует следующий доменный " +
				"запрос того же имени и сделает его невидимым. Ждала: " + allow[sym].ExpiresWhen,
		})
	}

	// Страховка от расхождения между переписью и находками: если числа не сошлись,
	// а findings пусты, гейт молчит о том, чего сам не объяснил.
	if c.RequestDecls != c.Expected && len(findings) == 0 {
		return nil, c, fmt.Errorf(
			"объявлений запроса подписки %d при ожидаемых %d, а находок ноль — "+
				"анализатор не объяснил расхождение и потому не может быть прочитан как вердикт",
			c.RequestDecls, c.Expected)
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		return findings[i].Symbol < findings[j].Symbol
	})

	if out != nil {
		_, _ = fmt.Fprintf(out,
			"перепись: файлов контракта %d; сообщений верхнего уровня %d (вложенных %d); "+
				"объявлений запроса подписки %d (общих %d, доменных %d); послаблений %d; "+
				"ожидалось %d; находок %d\n",
			c.ProtoFiles, c.TopLevelMessages, c.NestedMessages,
			c.RequestDecls, c.CommonDecls, c.DomainDecls, c.Allowances, c.Expected, len(findings))
	}
	return findings, c, nil
}
