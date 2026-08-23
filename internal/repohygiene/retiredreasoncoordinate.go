// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// retiredreasoncoordinate.go — анализатор «причина надгробия называет МЁРТВУЮ
// координату».
//
// # Что он ищет
//
// Запись надгробия несёт причину снятия, и причина нередко указывает читателю
// живой путь: «то же самое делает вот этот метод», «живым остаётся вот тот». Имя
// метода в такой фразе — КООРДИНАТА: следующий читатель пойдёт по ней в дерево.
//
// Координата переживает свой предмет. Названный живым метод снимают другой
// задачей — и причина продолжает утверждать, что он жив, ничем этого не выдавая:
// надгробие не истекает, прогон зелёный, а фраза читается как действующее
// указание «вот живая форма, бери её».
//
// # Почему это гейт, а не внимательность на обзоре
//
// Симптома нет по построению. Обзор снятия метода смотрит на контракт, стабы и
// каталог прав; причина ЧУЖОЙ записи надгробия в этот радиус не попадает — она
// лежит в другом файле, её никто не менял, и дифф её не показывает. Найти такое
// можно только переписью, а перепись, сделанная один раз, стареет к следующему
// снятию.
//
// Наблюдалось на этом дереве: из ТРЁХ координат, стоявших в причинах, мёртвыми
// оказались ВСЕ ТРИ — две формы фида жизненного цикла и поток намерения
// датаплейну. Каждая была верна в день записи.
//
// # Чем это отличается от соседа
//
// `retiredrpcsurface.go` стережёт ВОЗВРАЩЕНИЕ снятого имени: он читает поле
// `FQN` и молчит, пока имени нет в дереве. Здесь предмет обратный — имя, которое
// причина называет ЖИВЫМ, а его в дереве нет. Первый гейт краснеет на избытке,
// второй на недостатке; ни один не видит того, что видит другой.
//
// # Граница гейта названа честно
//
// Он не разбирает естественный язык и не судит, утверждает ли фраза живость. Он
// требует более простого и проверяемого: КООРДИНАТА, названная в причине, обязана
// резолвиться в дереве. Причина, которой нужно упомянуть снятое имя, называет его
// не координатой (без формы `Сервис/Метод`) — ровно так же, как это делают
// правила воркспейса с мёртвыми путями.
//
// # Ноль упоминаний — это цель, а не поломка
//
// Причина без координат законна и предпочтительна. Поэтому нулевое число
// упоминаний гейт проходит, называя перепись. Падает он на ПУСТОМ ОБХОДЕ: нет
// записей надгробия либо не прочитано ни одного метода из стабов — тогда «ноль
// находок» неотличимо от «ноль прочитанного».
package repohygiene

import (
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// RetiredReasonOptions — вход анализатора.
type RetiredReasonOptions struct {
	// Root — корень репозитория.
	Root string
	// APIRoot — путь (относительно Root) к сгенерированным стабам: они и есть
	// таблица, по которой идёт диспатч, то есть авторитетный ответ «жив ли метод».
	APIRoot string
	// Retired — перепись снятого, чьи причины читаются.
	Retired []RetiredRPC
}

// RetiredReasonCensus — то, что анализатор прочитал.
type RetiredReasonCensus struct {
	Entries         int
	StubFiles       int
	DeclaredSvcs    int
	DeclaredMethods int
	// Mentions — координат найдено в причинах.
	Mentions int
	// Resolved — из них разрешилось в живой метод.
	Resolved int
}

// RetiredReasonFinding — одна находка.
type RetiredReasonFinding struct {
	// Entry — запись надгробия, в чьей причине стоит координата.
	Entry string
	// Mention — сама координата, как она написана в причине.
	Mention string
	// Reason — почему это находка.
	Reason string
}

func (f RetiredReasonFinding) String() string {
	return f.Entry + ": координата " + f.Mention + " — " + f.Reason
}

// reasonCoordinateRe вылавливает координату формы `[пакет.]<Что-то>Service/<Метод>`.
//
// Якорь — суффикс `Service` перед косой чертой: в этом дереве всякий gRPC-сервис
// им оканчивается, а обычная проза косой черты между двумя заглавными словами не
// содержит. Пакет необязателен: причины пишут и полное имя, и сокращённое.
var reasonCoordinateRe = regexp.MustCompile(`\b([A-Za-z0-9_.]*[A-Za-z0-9_]Service)/([A-Za-z0-9_]+)\b`)

// AuditRetiredReasonCoordinates читает причины надгробия и возвращает координаты,
// которым в дереве не отвечает ни один объявленный метод.
func AuditRetiredReasonCoordinates(
	opts RetiredReasonOptions, out io.Writer,
) ([]RetiredReasonFinding, RetiredReasonCensus, error) {
	var c RetiredReasonCensus
	c.Entries = len(opts.Retired)
	if c.Entries == 0 {
		return nil, c, fmt.Errorf(
			"перепись снятого пуста — читать нечего, и любой вердикт ниже беспредметен")
	}

	var rc CatalogReachabilityCensus
	declared, err := declaredMethods(filepath.Join(opts.Root, opts.APIRoot), &rc)
	if err != nil {
		return nil, c, err
	}
	c.StubFiles, c.DeclaredSvcs, c.DeclaredMethods = rc.StubFiles, rc.DeclaredSvcs, rc.DeclaredMethods
	if c.StubFiles == 0 || c.DeclaredMethods == 0 {
		return nil, c, fmt.Errorf(
			"из стабов %q прочитано файлов %d, методов %d — «координата жива» получено даром",
			opts.APIRoot, c.StubFiles, c.DeclaredMethods)
	}

	var findings []RetiredReasonFinding
	for _, r := range opts.Retired {
		for _, m := range reasonCoordinateRe.FindAllStringSubmatch(r.Reason, -1) {
			c.Mentions++
			mention := m[0]
			if reasonCoordinateAlive(declared, m[1], m[2]) {
				c.Resolved++
				continue
			}
			findings = append(findings, RetiredReasonFinding{
				Entry:   r.FQN,
				Mention: mention,
				Reason: "в дереве ей не отвечает ни один объявленный метод. Причина надгробия " +
					"указывает читателю путь, которого нет: она пережила свой предмет. Назови " +
					"живой путь его именем либо, если называть нечего, не пиши мёртвое имя " +
					"координатой — иначе следующий читатель пойдёт по ней",
			})
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Entry != findings[j].Entry {
			return findings[i].Entry < findings[j].Entry
		}
		return findings[i].Mention < findings[j].Mention
	})

	if out != nil {
		_, _ = fmt.Fprintf(out,
			"перепись: записей надгробия %d; стабов %d файлов (%d сервисов, %d методов); "+
				"координат в причинах %d, из них живых %d; находок %d\n",
			c.Entries, c.StubFiles, c.DeclaredSvcs, c.DeclaredMethods,
			c.Mentions, c.Resolved, len(findings))
	}
	return findings, c, nil
}

// reasonCoordinateAlive отвечает, объявлен ли метод у сервиса, названного
// координатой.
//
// Имя сервиса сопоставляется как СУФФИКС ПО СЕГМЕНТАМ: причины пишут то полное
// имя (`kacho.cloud.vpc.v1.FooService`), то сокращённое (`vpc.v1.FooService`,
// `FooService`). Сравнение по подстроке здесь не годится — оно приняло бы
// `AlphaService` за хвост `BetaAlphaService`.
func reasonCoordinateAlive(declared map[string]map[string]struct{}, svc, method string) bool {
	for full, methods := range declared {
		if full != svc && !strings.HasSuffix(full, "."+svc) {
			continue
		}
		if _, ok := methods[method]; ok {
			return true
		}
	}
	return false
}
