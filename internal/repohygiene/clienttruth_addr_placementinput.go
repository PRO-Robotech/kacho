// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// clienttruth_addr_placementinput.go — анализатор «дискриминатор размещения в
// запросе создания объявлен ВЫВОДИМЫМ, а не входом».
//
// # Предмет
//
// Размещение ресурса задаётся парой: КООРДИНАТА (`zone_id` либо `region_id`) и
// ДИСКРИМИНАТОР (`placement_type ∈ {ZONAL, REGIONAL}`). Пара избыточна by
// construction — какая из двух координат задана, то и есть дискриминатор, —
// поэтому платформа выводит его и вход отвергает: `data-integrity.md`
// §«Placement-coherence» называет якорем канона подсеть, и она же отвечает
// `INVALID_ARGUMENT "placement_type is server-derived; set zone_id or region_id
// instead"`.
//
// Ресурс, требующий дискриминатор ОТ КЛИЕНТА, даёт ему единственную новую
// возможность — противоречить самому себе: назвать `ZONAL` и прислать регион.
// Отказ на этом законен, но сам класс отказа создан требованием поля.
//
// # Замер на день заведения (kacho#1621)
//
// Поле стоит в запросе создания у ТРЁХ ресурсов трёх доменов:
//
//	vpc      Subnet             отвергается — выводится из координаты
//	nlb      NetworkLoadBalancer отвергается — выводится из входа `placement`
//	compute  PlacementGroup      ТРЕБУЕТСЯ вместе с координатой
//
// Ещё два размещаемых ресурса (`registry.Registry`, `storage.Image`) поля в
// запросе создания НЕ несут вовсе: размещение у них постоянное. Это четвёртое,
// самое чистое состояние, и оно тоже согласуется с каноном.
//
// Отсюда правка формулировки самой задачи: правил не три, а ДВА, и расходятся
// они не «взаимоисключающе». vpc и nlb об этом поле говорят ОДНО — оно выводимо и
// отвергается; различаются они лишь тем, ИЗ ЧЕГО выводится, и у nlb вход богаче
// намеренно (`placement` несёт вторую ось — внутренний против внешнего, которую
// координата выразить не может). Отступает ОДИН ресурс из трёх.
//
// # Что судит анализатор
//
// Поле `placement_type` в сообщении `Create…Request` обязано нести комментарий,
// объявляющий его ВЫВОДИМЫМ: `derived`, `output-only` либо `server-derived`.
//
// # Чего он НЕ судит, и это названо, а не умолчано
//
//  1. САМО ПОВЕДЕНИЕ. Анализатор читает КОНТРАКТ — то, по чему клиент строит
//     запрос до первого вызова, и то, что расходилось. Отказ на пути запроса
//     держат пробы своего сервиса; здесь он не проверяется, и утверждать обратное
//     значило бы обещать проверку, которой нет.
//
//     Предикат выбран по предмету линии — «три поверхности говорят одно», — а не
//     по удобству. Поведенческий предикат отдельно рассматривался и отвергнут:
//     отказ записан у vpc сравнением ДОМЕННОГО значения, а у nlb — геттером
//     запроса, и распознаватель, знающий одну форму, объявил бы второй сервис
//     нарушителем при исправном коде.
//
//  2. РЕСУРС БЕЗ ПОЛЯ. Молчание — не нарушение, а лучшее из состояний: у реестра
//     и образа размещение постоянно, и поля в запросе создания нет вовсе.
//
//  3. ИЗ ЧЕГО выводится. Координата у подсети и отдельный вход у балансировщика —
//     оба законны; предмет в том, что дискриминатор не входной.
//
// # Падает на ПУСТОМ ОБХОДЕ
//
// Ноль прочитанных файлов контракта либо ноль найденных полей — «находок ноль»
// неотличимо от «прочитано ноль».
package repohygiene

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/treecorpus"
)

// PlacementInputOptions — вход анализатора.
type PlacementInputOptions struct {
	// Tree — СОСТАВ дерева, а не его корень: гейт берёт индекс git
	// (`treecorpus.NewTree`), инъекционная проба — синтетическое дерево
	// (`treecorpus.SyntheticTree`). Разбор — clienttruth_treefiles.go.
	Tree *treecorpus.Tree
	// ProtoRoot — каталог контрактов относительно корня дерева.
	ProtoRoot string
	// Exemptions — послабления; каждое обязано истекать само.
	Exemptions []PlacementInputExemption
}

// PlacementInputExemption — одно послабление. Ключ — предмет (файл и сообщение),
// а не номер строки: номер сдвигается от любой соседней правки.
type PlacementInputExemption struct {
	// File — путь файла контракта относительно Root.
	File string
	// Message — имя сообщения запроса создания.
	Message string
	// Reason — почему запись стоит и что её снимет. Пустая запрещена.
	Reason string
}

// PlacementInputCensus — объём осмотренного.
type PlacementInputCensus struct {
	// ProtoFiles — прочитано файлов контракта.
	ProtoFiles int
	// CreateMessages — распознано сообщений запроса создания.
	CreateMessages int
	// WithField — из них несущих дискриминатор размещения.
	WithField int
	// Derived — из них объявляющих его выводимым.
	Derived int
	// Exempted — находок, снятых послаблением.
	Exempted int
}

// PlacementInputFinding — одна находка.
type PlacementInputFinding struct {
	// File, Line — координата поля.
	File string
	Line int
	// Message — сообщение запроса создания.
	Message string
	// StaleExemption — запись послабления потеряла предмет.
	StaleExemption bool
	// Reason — причина послабления (только у устаревшего).
	Reason string
}

func (f PlacementInputFinding) String() string {
	if f.StaleExemption {
		return fmt.Sprintf("%s: послабление на %s потеряло предмет (%s) — снимите запись",
			f.File, f.Message, f.Reason)
	}
	return fmt.Sprintf("%s:%d: %s требует placement_type от клиента — "+
		"дискриминатор обязан выводиться из координаты и отвергаться на входе",
		f.File, f.Line, f.Message)
}

var (
	// placementCreateMsgRe — начало сообщения запроса создания.
	placementCreateMsgRe = regexp.MustCompile(`^message (Create[A-Za-z0-9]*Request)\s*\{`)

	// placementFieldRe — ОБЪЯВЛЕНИЕ поля дискриминатора. Читается объявление, а не
	// упоминание: имя поля стоит и в комментариях соседних полей («set iff
	// placement_type == ZONAL»), и предикат по подстроке нашёл бы поле там, где
	// объявлена координата.
	placementFieldRe = regexp.MustCompile(`^\s*[A-Za-z0-9_.]+\s+placement_type\s*=\s*\d+\s*;`)

	// placementDerivedRe — объявление выводимости. Три записи, потому что в дереве
	// их три и все законны: `server-derived` у подсети, `DERIVED output-only` у
	// балансировщика, `derived` как общая форма.
	//
	// Сверяется ТОЛЬКО ПЕРВАЯ строка доку-комментария поля — та, что объявляет,
	// ЧЕМ поле является. Это конвенция контракта, а не подгонка под инструмент:
	// подсеть открывает блок словами «Placement discriminator — SERVER-DERIVED, НЕ
	// ВХОД», балансировщик — «placement_type — DERIVED output-only», и клиент
	// читает именно эту строку.
	//
	// Сверять блок ЦЕЛИКОМ нельзя, и это измерено на себе: комментарий, который
	// ЧЕСТНО объясняет отступление и цитирует при этом текст канонического отказа
	// («placement_type is server-derived…»), удовлетворял бы предикату по всему
	// блоку — то есть ссылка на канон засчитывалась бы за его соблюдение.
	// Расхождение стало бы невидимым ровно у того ресурса, ради которого гейт
	// написан. Поймано самоистечением послабления, а не чтением.
	placementDerivedRe = regexp.MustCompile(`(?i)server-derived|output-only|\bderived\b`)
)

// AuditPlacementInput читает дерево контрактов и возвращает находки и перепись.
func AuditPlacementInput(
	opts PlacementInputOptions, log io.Writer,
) ([]PlacementInputFinding, PlacementInputCensus, error) {
	var census PlacementInputCensus
	var findings []PlacementInputFinding
	matched := map[string]bool{}

	for _, rel := range clientTruthTreeFiles(opts.Tree, opts.ProtoRoot, true, ".proto") {
		body, rerr := clientTruthReadTreeFile(opts.Tree, rel)
		if rerr != nil {
			return nil, census, rerr
		}
		census.ProtoFiles++

		lines := strings.Split(string(body), "\n")
		var msg string
		var comment []string
		for i, line := range lines {
			if m := placementCreateMsgRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
				msg, comment = m[1], nil
				census.CreateMessages++
				continue
			}
			if msg != "" && strings.TrimSpace(line) == "}" {
				msg, comment = "", nil
				continue
			}
			if msg == "" {
				continue
			}
			// Комментарий копится и обнуляется пустой строкой: он относится к
			// СЛЕДУЮЩЕМУ объявлению, а не ко всему сообщению.
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				comment = append(comment, trimmed)
				continue
			}
			if !placementFieldRe.MatchString(line) {
				if trimmed == "" {
					comment = nil
				} else {
					comment = nil
				}
				continue
			}
			census.WithField++
			if len(comment) > 0 && placementDerivedRe.MatchString(comment[0]) {
				census.Derived++
				comment = nil
				continue
			}
			f := PlacementInputFinding{File: rel, Line: i + 1, Message: msg}
			if key, ok := exemptedPlacementInput(opts.Exemptions, f); ok {
				matched[key] = true
				census.Exempted++
			} else {
				findings = append(findings, f)
			}
			comment = nil
		}
	}

	// Послабление, которому больше нечего исключать, — находка.
	for _, e := range opts.Exemptions {
		if matched[placementInputKey(e.File, e.Message)] {
			continue
		}
		findings = append(findings, PlacementInputFinding{
			File: e.File, Message: e.Message, StaleExemption: true, Reason: e.Reason,
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
			"перепись: файлов контракта %d · запросов создания %d · из них с дискриминатором %d "+
				"(объявлен выводимым %d, снято послаблением %d)\n",
			census.ProtoFiles, census.CreateMessages, census.WithField,
			census.Derived, census.Exempted)
	}
	return findings, census, nil
}

func placementInputKey(file, msg string) string { return file + "\x00" + msg }

func exemptedPlacementInput(
	list []PlacementInputExemption, f PlacementInputFinding,
) (string, bool) {
	key := placementInputKey(f.File, f.Message)
	for _, e := range list {
		if placementInputKey(e.File, e.Message) == key {
			return key, true
		}
	}
	return "", false
}
