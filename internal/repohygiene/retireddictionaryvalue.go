// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// retireddictionaryvalue.go — анализатор класса «снятое значение словаря
// продолжает называться КОНТРАКТОМ».
//
// # Предмет
//
// Значение закрытого словаря очереди снимается миграцией: у него не было
// производителя, и подсистемы, ради которой оно заводилось, в дереве нет.
// Миграция снимает его из ограничения БД — и на этом снятие обычно
// заканчивается. Публичный контракт (`proto/`, а вслед за ним сгенерированные
// стабы `pkg/api/`) продолжает перечислять снятое как «канонические значения»,
// и это утверждение пережило свой предмет.
//
// Опаснее обычной устаревшей строки оно потому, что стоит в КОНТРАКТЕ: его
// читают чаще кода, и читает его вызывающий, который по этому перечню решает,
// какое значение ему позволено прислать. Прислав снятое, он получит отказ от
// ограничения БД — на стороне, до которой контракт обещал ему дойти.
//
// # Почему этот анализатор, когда рядом уже есть два соседних
//
//   - `TestQueueEventValueHasAProducer` смотрит на ЖИВОЙ словарь: у каждого
//     допустимого значения обязан быть производитель. О снятых значениях он не
//     говорит ничего — их в словаре уже нет;
//   - `TestCommentCitingDictionaryConstraintQuotesItWhole` читает ТОЛЬКО
//     комментарии Go-файлов и прямо объявляет слепую зону: «цитата в .proto,
//     .md, .mdx этому гейту невидима, и такие цитаты в дереве ЕСТЬ». Настоящий
//     экземпляр этого класса (#796) лежал ровно в этой зоне.
//
// # Что читается и почему поиск подстрокой здесь законен
//
// Корпус — отслеживаемые `.proto` и сгенерированные `.pb.go`/`.pb.gw.go` под
// `pkg/api`. Различать в них код, литерал и комментарий НЕ НУЖНО: значение
// снято НАВСЕГДА, поэтому находкой является любое его появление в контракте —
// и как имя, и как проза. Именно этим надгробие отличается от послабления.
//
// # Объявленные слепые зоны
//
//   - Документация (`.md`, `.mdx`) и прод-код сервисов сюда НЕ входят. Причина
//     названа, а не умолчана: в прод-коде снятое значение законно живёт в
//     разборе исторических строк и в самих миграциях (обратный блок `+goose
//     Down` ОБЯЗАН называть снятое — иначе откат невыразим). Контракт же не
//     имеет ни одного законного повода его называть.
//   - Словарь берётся из перечня `retiredDictionaryValues`, а не выводится из
//     миграций: «снято» — это решение, а не форма текста. Перечень
//     самоистекает в другую сторону (см. `LiveIn`).
package repohygiene

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RetiredDictionaryValue — НАДГРОБИЕ значения словаря очереди.
//
// Запись НЕ истекает от времени: значение снято решением, и вернуться оно может
// только решением же. Истекает она в одну сторону — если значение снова
// оказалось в живом словаре (тогда надгробие лжёт и это находка).
type RetiredDictionaryValue struct {
	// Value — само значение, как оно стояло в ограничении.
	Value string
	// Dictionary — «<схема>.<таблица>.<колонка>», откуда снято.
	Dictionary string
	// By — миграция, снявшая значение.
	By string
	// Reason — почему снято. Надгробие без надписи ничего не сообщает
	// следующему.
	Reason string
}

// ContractValueFinding — снятое значение, найденное в контракте.
type ContractValueFinding struct {
	Value string
	File  string // путь относительно корня
	Line  int
	Text  string
	Why   string
}

func (f ContractValueFinding) String() string {
	return fmt.Sprintf("%s:%d — контракт называет снятое значение %q (%s): %s",
		f.File, f.Line, f.Value, f.Why, strings.TrimSpace(f.Text))
}

// ContractValueCensus — объём осмотренного. «Ноль находок» обязано быть
// отличимо от «ноль прочитанного».
type ContractValueCensus struct {
	Files int
	Lines int
}

// AuditRetiredDictionaryValues ищет каждое снятое значение в переданном корпусе
// файлов контракта.
//
// files — АБСОЛЮТНЫЕ пути; root нужен только чтобы находка называла координату
// относительно дерева. Пустой перечень надгробий и пустой корпус — ОТКАЗ, а не
// «ноль находок»: инертный анализатор зеленеет на любом дереве.
func AuditRetiredDictionaryValues(root string, files []string, retired []RetiredDictionaryValue) ([]ContractValueFinding, ContractValueCensus, error) {
	var census ContractValueCensus
	if len(retired) == 0 {
		return nil, census, fmt.Errorf("перечень снятых значений пуст — анализатор инертен " +
			"и об этом не сообщает; надгробие не послабление, пустым оно быть не может")
	}
	for _, r := range retired {
		if strings.TrimSpace(r.Value) == "" || strings.TrimSpace(r.Reason) == "" ||
			strings.TrimSpace(r.Dictionary) == "" || strings.TrimSpace(r.By) == "" {
			return nil, census, fmt.Errorf("запись надгробия неполна (%+v): значение, словарь, "+
				"миграция и причина обязательны все четыре", r)
		}
	}
	if len(files) == 0 {
		return nil, census, fmt.Errorf("корпус контракта пуст — «снятых значений не найдено» " +
			"неотличимо от «ничего не читали»")
	}

	var findings []ContractValueFinding
	for _, abs := range files {
		// #nosec G304 -- путь пришёл из индекса git ЭТОГО дерева (treecorpus) либо
		// из синтетического корня инъекции; постороннего ввода тут нет.
		f, err := os.Open(abs)
		if err != nil {
			return nil, census, fmt.Errorf("не прочитан файл контракта %s: %w", abs, err)
		}
		rel, relErr := filepath.Rel(root, abs)
		if relErr != nil {
			rel = abs
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		line := 0
		for sc.Scan() {
			line++
			census.Lines++
			text := sc.Text()
			for _, r := range retired {
				if strings.Contains(text, r.Value) {
					findings = append(findings, ContractValueFinding{
						Value: r.Value, File: filepath.ToSlash(rel), Line: line,
						Text: text, Why: r.Reason,
					})
				}
			}
		}
		scanErr := sc.Err()
		_ = f.Close()
		if scanErr != nil {
			return nil, census, fmt.Errorf("чтение %s оборвалось: %w", abs, scanErr)
		}
		census.Files++
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
	return findings, census, nil
}

// LiveIn — вторая сторона самоистечения: снятое значение, снова оказавшееся в
// ЖИВОМ словаре, делает надгробие ложью. Возвращает координаты таких записей.
func LiveIn(retired []RetiredDictionaryValue, live map[string]map[string][]string) []string {
	var back []string
	for _, r := range retired {
		for table, cols := range live {
			for col, values := range cols {
				for _, v := range values {
					if v == r.Value {
						back = append(back, fmt.Sprintf("%s.%s = %q", table, col, v))
					}
				}
			}
		}
	}
	sort.Strings(back)
	return back
}
