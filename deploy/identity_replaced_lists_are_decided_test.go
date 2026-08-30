// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_replaced_lists_are_decided_test.go — СПИСОК, объявленный обеими
// сторонами настроек службы личности, обязан нести ЯВНОЕ решение.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// Процесс службы личности получает ДВА файла настроек и сливает их по порядку;
// наш идёт ВТОРЫМ. Слияние идёт по КАРТАМ: одноимённые ключи-карты дополняются,
// а СПИСОК под общим ключом не дополняется — он ЗАМЕЩАЕТСЯ целиком. Значит
// всякая запись списка, объявленная слоем под нами, из действующей конфигурации
// ИСЧЕЗАЕТ — молча, без диагностики при старте и без единого признака в рендере:
// обе секции по отдельности валидны.
//
// Замер, из которого гейт выведен (задача #1238, ревизия заведения):
//
//	цепочки a8f60d / dev / dev-prod / prorobotech:
//	                  selfservice.allowed_return_urls   10 записей → 1
//	цепочка  fe3455:  selfservice.allowed_return_urls    2 записи  → 1
//
// Отказа на этом сужении СЕГОДНЯ нет — наша единственная запись покрывает то,
// что консоль реально шлёт. Предмет гейта не в сегодняшнем состоянии, а в том,
// что СЛЕДУЮЩЕЕ сужение прошло бы так же молча и обнаружилось бы поведением у
// пользователя. Ровно так прошли #1222 (адреса) и #1236 (схемы) — тот же
// механизм, третий случай подряд.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ИМЕННО УТВЕРЖДАЕТСЯ
//
// Для КАЖДОГО списка, объявленного ОБЕИМИ сторонами, требуется ровно одно из
// двух, и третьего нет:
//
//	(1) ПОКРЫТИЕ — вычисляется, а не объявляется: каждая запись поставщика
//	    присутствует в нашем списке. Тогда замещение ничего не теряет, и
//	    записи в ведомости решений НЕ НУЖНО;
//	(2) РЕШЕНИЕ — путь назван в ведомости `identityReplacedListDecisions` ниже
//	    вместе с причиной. Сужение объявлено намеренным и записано.
//
// Список, у которого нет ни (1), ни (2), — находка С ИМЕНЕМ ПУТИ и перечнем
// потерянных записей.
//
// Ведомость ИСТЕКАЕТ САМА, и по двум разным осям — иначе она пережила бы свой
// предмет и стала бы слепой зоной, выданной вперёд:
//
//	(3) БЕЗ ПРЕДМЕТА — запись ведомости на путь, который больше не объявлен
//	    обеими сторонами ни на одном стеке (сняли список — снимите решение);
//	(4) ИЗЛИШНЯЯ — запись ведомости на путь, который ВЕЗДЕ покрыт вычислением.
//	    Решение о сужении, которого больше нет, читается как действующее.
//
// Оси (3) и (4) обратны оси (1)/(2) ровно так же, как «самоистечение» обратно
// «покрытию» у соседа по механизму
// (deploy/identity_inherited_schemas_are_declared_test.go).
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧЕГО ЭТО НЕ УТВЕРЖДАЕТ
//
//   - НЕ утверждает, что сужение неверно. Вопрос гейта — «решал ли это
//     кто-нибудь», а не «правильно ли решено». На «правильно» машинного
//     предиката нет, и объявлять его было бы формой без содержания.
//   - НЕ утверждает ничего о ключах-НЕ-списках: карты сливаются, скаляр под
//     общим ключом замещается ОДНОЙ величиной, и потери записей там не бывает
//     by construction. Расширить проверку на скаляры значило бы краснеть на
//     каждой законной переопределённой величине.
//   - НЕ утверждает ничего о списке, который объявляет ТОЛЬКО поставщик: его
//     наша секция не замещает, он доезжает до процесса целиком. Это законный
//     близнец, и он обязан молчать.
//   - НЕ повторяет соседей. Покрытие `identity.schemas` держит гейт
//     унаследованных схем (#1236); здесь оно не пересказывается, а называется
//     ссылкой в ведомости: два места об одном предмете разошлись бы молча.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ВЕДОМОСТЬ КЛЮЧУЕТСЯ ПУТЁМ, А НЕ ПАРОЙ «СТЕК + ПУТЬ»
//
// Наша сторона — ОДИН шаблон на все посадки: сужение, если оно есть, одинаково
// везде. Ключ «стек + путь» дал бы шесть записей об одном решении и разошёлся
// бы между собой при первой же правке. Различие стеков остаётся там, где оно
// действительно есть, — в перечне ПОТЕРЯННЫХ записей, и он печатается по стеку.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ БЕЗ helm
//
// Гейт читает ОБЪЯВЛЕНИЯ: профили значений (сторона поставщика) и шаблон тела
// настроек (наша сторона). Рендер умбреллы требует скачанных зависимостей,
// которых в свежем клоне нет, и пропущенная проверка не краснеет никогда.
// Рендер читает ВТОРАЯ проверка этого файла — но не как вердикт о дереве, а как
// ПРОВЕРКА ПРЕДПОСЫЛКИ разбора: всякий список, который видит рендер, обязан
// увидеть и строчный разбор.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДПОСЫЛКИ, ОБЪЯВЛЕННЫЕ ВСЛУХ (проверяются, а не подразумеваются)
//
//	П1. Порядок источников. Подчарт поставщика передаёт процессу
//	    `--config /etc/config/kratos.yaml`, затем дописывает `deployment.extraArgs`,
//	    где стоит НАШ файл. Наш — последний, значит замещает он, а не его.
//	    Проверяется: цепочка обязана доводить наши настройки до процесса
//	    (identityChainMountsOurConfig) — иначе замещения нет вовсе и предмета нет.
//	П2. Сторона поставщика ЧИТАЕТСЯ: узел `kratos.kratos.config` обязан
//	    резолвиться хотя бы у одного стека. Переименуют его — гейт перестанет
//	    читать ту сторону, которую сверяет, и позеленеет на вернувшемся дефекте.
//	П3. Наша сторона ЧИТАЕТСЯ строчным разбором. Тело настроек — Go-шаблон, и
//	    валидным YAML оно не является ни на одной ревизии (величины стоят
//	    подстановками), поэтому разбор идёт по отступам — тем же приёмом, что у
//	    соседних проверок методов и полос регистрации. Разбор знает ЧЕТЫРЕ формы
//	    записи списка (блочная · поточная · список карт · записи, порождённые
//	    `range`) и НЕ знает пятой — списка, целиком вычисленного действием
//	    шаблона (`toYaml`/`nindent`). Появление такой формы — НАХОДКА, а не
//	    молчание: иначе всё записанное в ней ушло бы из-под наблюдения.
//	П4. Объём осмотренного печатается всегда: «ноль находок» обязано быть
//	    отличимо от «ноль прочитанного».
//
// Способность упасть и смолчать доказана инъекцией —
// identity_replaced_lists_injection_test.go.
package deploy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// ВЕДОМОСТЬ РЕШЕНИЙ.
//
// Запись здесь означает: сужение (или замену) записей поставщика по этому пути
// РЕШИЛИ, и вот причина. Запись, у которой не осталось предмета, — находка
// (ось 3), как и запись на путь, который везде покрыт вычислением (ось 4).
//
// Ключ — путь ВНУТРИ тела настроек службы личности, точками.
var identityReplacedListDecisions = map[string]string{
	"identity.schemas": "покрытие держит ОТДЕЛЬНЫЙ гейт — " +
		"deploy/identity_inherited_schemas_are_declared_test.go (задача #1236): " +
		"схема поставщика объявляется у нас тем же узлом YAML через " +
		"global.kacho.identity.inheritedSchemas, а тело приезжает якорем, поэтому " +
		"расхождение невозможно by construction. Здесь это не пересказывается: два " +
		"места об одном предмете разошлись бы молча",

	"secrets.cookie": "поставщик объявляет ЗАГЛУШКУ-умолчание своего чарта, наша запись " +
		"подставляет настоящий секрет из окружения пода. Замещение — предмет самой " +
		"записи; «покрыть» заглушку значило бы оставить её действующим ключом подписи",
	"secrets.cipher": "то же, что у secrets.cookie: заглушка-умолчание чарта замещается " +
		"настоящим ключом из окружения пода, и это предмет записи, а не её потеря",

	"serve.public.cors.allowed_origins": "сужение ЖЕЛАЕМОЕ и названо в задаче #1238: " +
		"поставщик объявляет `*` (любой origin), наша запись — один origin консоли той же " +
		"посадки. «Покрыть» `*` значило бы вернуть разрешение любому origin, то есть " +
		"отменить ровно то, ради чего наша секция и существует",

	"selfservice.allowed_return_urls": "белый список целей перенаправления сужается " +
		"НАМЕРЕННО до одного префикса — origin консоли той же посадки. Относительный " +
		"`return_to` («/», «/dashboard») служба разрешает против этого же origin и под " +
		"префикс попадает, поэтому одноимённые записи поставщика ему не нужны. Записи " +
		"port-forward-origin'ов (`http://localhost:*`, `http://127.0.0.1:*`) уходят вместе " +
		"с dev-insecure-посадкой: production-mode обязателен ВЕЗДЕ (core ban #16). " +
		"ЦЕНА НАЗВАНА, а не умолчана: `?return_to=` на port-forward-origin теперь " +
		"отвергается — на стенде, поднятом пробросом порта, программное перенаправление " +
		"надо вести тем же origin, что объявлен посадкой. Расширять список обратно " +
		"нельзя: это защита от открытого перенаправления, и каждая лишняя запись её " +
		"ослабляет",
}

// ─────────────────────────────────────────────────────────────────────────────
// ЕДИНИЦА СЧЁТА И ВЕРДИКТ.

// identityStackLists — обе стороны одного стека, как их читает гейт.
type identityStackLists struct {
	Stack    string
	Provider map[string][]string
	Ours     map[string][]string
}

// identityListFinding — одна находка адъюдикатора. Причина строкой, а не кодом:
// стороны расхождения чинятся по-разному, и вердикт обязан это различать.
type identityListFinding struct {
	Stack  string
	Path   string
	Reason string
	Lost   []string
}

// Причины — поимённо, чтобы инъекция сверялась с ними, а не с текстом отказа.
const (
	replacedListNotDecided = "список объявлен обеими сторонами, наш его НЕ покрывает, решения нет"
	replacedListNoSubject  = "решение записано на путь, который больше не объявлен обеими сторонами"
	replacedListRedundant  = "решение записано на путь, который ВЕЗДЕ покрыт вычислением"
)

// identityListCovers — покрывает ли наш список записи поставщика.
//
// Сравнение по СОСТАВУ, а не по порядку и не по длине: замещение теряет ЗАПИСЬ,
// и вопрос ровно в том, осталась ли каждая. Порядок здесь не значим — ни у
// белого списка перенаправлений, ни у списка origin'ов, ни у секретов; там, где
// порядок значим (перечни хуков), наш список длиннее и порядок задаём мы.
func identityListCovers(ours, provider []string) bool {
	have := make(map[string]bool, len(ours))
	for _, o := range ours {
		have[o] = true
	}
	for _, p := range provider {
		if !have[p] {
			return false
		}
	}
	return true
}

// identityListLost — записи поставщика, которых наш список не несёт.
func identityListLost(ours, provider []string) []string {
	have := make(map[string]bool, len(ours))
	for _, o := range ours {
		have[o] = true
	}
	var lost []string
	for _, p := range provider {
		if !have[p] {
			lost = append(lost, p)
		}
	}
	return lost
}

// adjudicateReplacedIdentityLists — ЕДИНСТВЕННЫЙ дискриминатор гейта. Зовётся и
// переписью по дереву, и инъекцией: копия предиката разошлась бы с оригиналом
// молча и доказывала бы саму себя.
func adjudicateReplacedIdentityLists(views []identityStackLists, ledger map[string]string) []identityListFinding {
	var out []identityListFinding

	// Ось (1)/(2): по каждому стеку — общий список без покрытия и без решения.
	// Заодно копим, где путь вообще был общим и где он покрыт вычислением, —
	// это и есть предмет осей (3) и (4).
	common := map[string]bool{}
	uncovered := map[string]bool{}
	for _, v := range views {
		paths := make([]string, 0, len(v.Provider))
		for p := range v.Provider {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			ours, ok := v.Ours[p]
			if !ok {
				continue // объявляет только поставщик — мы его не замещаем
			}
			common[p] = true
			if identityListCovers(ours, v.Provider[p]) {
				continue
			}
			uncovered[p] = true
			if _, decided := ledger[p]; decided {
				continue
			}
			out = append(out, identityListFinding{
				Stack:  v.Stack,
				Path:   p,
				Reason: replacedListNotDecided,
				Lost:   identityListLost(ours, v.Provider[p]),
			})
		}
	}

	// Оси (3) и (4) — самоистечение ведомости. Вне стека: решение одно на все
	// посадки, значит и предмет у него один на все.
	keys := make([]string, 0, len(ledger))
	for p := range ledger {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	for _, p := range keys {
		switch {
		case !common[p]:
			out = append(out, identityListFinding{Path: p, Reason: replacedListNoSubject})
		case !uncovered[p]:
			out = append(out, identityListFinding{Path: p, Reason: replacedListRedundant})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		if out[i].Stack != out[j].Stack {
			return out[i].Stack < out[j].Stack
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// СТОРОНА ПОСТАВЩИКА — разбирается YAML-библиотекой.
//
// Четыре формы записи списка (блочная последовательность · поточная `[a, b]` ·
// ссылка на якорь `*anchor` · последовательность карт) сводятся к одной и той же
// структуре ЕЩЁ ДО нас — их различает разборщик YAML, а не мы. Инъекция
// проверяет каждую форму отдельно именно потому, что «сводится by construction»
// есть утверждение, а не наблюдение.

// identityProviderLists собирает «путь → записи» по всем последовательностям
// узла настроек поставщика. Вторым значением — формы, которых нормализатор не
// знает: они уходят в НАХОДКУ предпосылки, а не в молчание.
func identityProviderLists(node any) (map[string][]string, []string) {
	out := map[string][]string{}
	var unknown []string
	var walk func(any, []string)
	walk = func(n any, prefix []string) {
		switch t := n.(type) {
		case map[string]any:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(t[k], append(prefix, k))
			}
		case map[any]any:
			unknown = append(unknown, strings.Join(prefix, ".")+": карта с нестроковым ключом")
		case []any:
			p := strings.Join(prefix, ".")
			items := make([]string, 0, len(t))
			for _, it := range t {
				s, ok := normalizeIdentityListItem(it)
				if !ok {
					unknown = append(unknown, p+": запись списка формы, которой нормализатор не знает")
					continue
				}
				items = append(items, s)
			}
			out[p] = items
		}
	}
	walk(node, nil)
	return out, unknown
}

// normalizeIdentityListItem приводит запись списка к строке, сравнимой с записью
// ДРУГОЙ стороны. Скаляр — сам собой; карта — плоским перечнем ЛИСТЬЕВ
// `ключ=величина`, отсортированным по ключу.
//
// Почему листья, а не структура: наша сторона читается по строкам шаблона, и
// вложенность там восстанавливается только отступом. Плоский перечень — единая
// форма для обеих сторон; рендерит её ОДНА функция (identityJoinPairs), поэтому
// стороны не могут разойтись в способе сравнения.
//
// Направление огрубления названо: пустой лист поставщика (`ключ: ""`) в перечень
// ПОПАДАЕТ, а на нашей стороне пустая величина неотличима от контейнера и в
// перечень не попадает. Значит такая запись поставщика окажется НЕ покрытой —
// то есть огрубление даёт находку, а не молчание.
func normalizeIdentityListItem(v any) (string, bool) {
	switch t := v.(type) {
	case map[string]any:
		leaves := map[string]string{}
		if !identityFlattenLeaves(t, leaves) {
			return "", false
		}
		return identityJoinPairs(leaves), true
	case []any:
		return "", false // список внутри списка — формы в дереве нет, молчать о ней нельзя
	case nil:
		return "", true
	default:
		return fmt.Sprint(t), true
	}
}

// identityFlattenLeaves раскладывает карту в плоский перечень листьев.
func identityFlattenLeaves(m map[string]any, out map[string]string) bool {
	for k, v := range m {
		switch t := v.(type) {
		case map[string]any:
			if !identityFlattenLeaves(t, out) {
				return false
			}
		case []any:
			return false
		case nil:
			out[k] = ""
		default:
			out[k] = fmt.Sprint(t)
		}
	}
	return true
}

// identityJoinPairs — ЕДИНСТВЕННЫЙ рендерер записи-карты, общий обеим сторонам.
func identityJoinPairs(pairs map[string]string) string {
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+pairs[k])
	}
	return strings.Join(parts, ",")
}

// ─────────────────────────────────────────────────────────────────────────────
// НАША СТОРОНА — разбирается ПО ОТСТУПАМ.

// identityConfigBodyDecl вырезает тело настроек из шаблона: от объявления
// именованного шаблона до его закрытия.
var identityConfigBodyDecl = regexp.MustCompile(
	`(?s)\{\{-?\s*define\s+"kacho\.identity\.configYaml"\s*-?\}\}(.*?)\n\{\{-\s*end\s*-\}\}\s*\n\s*\{\{/\*`)

// identityTemplateControl — действие шаблона, которое НИЧЕГО не печатает.
// Всё остальное, стоящее на строке в одиночку, печатает — и строчный разбор
// такого не читает.
var identityTemplateControl = regexp.MustCompile(
	`^\{\{-?\s*(?:(?:define|end|if|else|range|with|fail)\b|/\*|\$[A-Za-z_][A-Za-z0-9_]*\s*:=)`)

// identityTemplateCommentOpen — начало комментария шаблона. Пишется он и слитно
// (` + "`{{/*`" + `), и с дефисом-пробелом (` + "`{{- /*`" + `); узнавать только первую форму
// значило бы принять комментарий за печатающее действие и объявить находкой
// собственное объяснение.
var identityTemplateCommentOpen = regexp.MustCompile(`^\{\{-?\s*/\*`)

// identityStructureProducer — действия, способные напечатать СТРУКТУРУ (карту
// или список) на месте величины. Их появление в теле — пятая форма записи
// списка, которой разбор не знает.
var identityStructureProducer = regexp.MustCompile(`\btoYaml\b|\bnindent\b|\bindent\s`)

// identityOurLists собирает «путь → записи» из тела настроек нашего шаблона.
// Вторым значением — формы, которых разбор не знает (предпосылка П3).
//
// Разбор строчный, а не через YAML-библиотеку, и это не лень: тело — Go-шаблон,
// в котором величины стоят подстановками (`{{ $app }}`), поэтому валидным YAML
// оно не является ни на одной ревизии. Тот же приём и у соседних проверок
// методов входа и полос регистрации.
func identityOurLists(body string) (map[string][]string, []string) {
	lists := map[string][]string{}
	var unknown []string

	type frame struct {
		indent int
		key    string
	}
	var stack []frame
	pathOf := func() string {
		parts := make([]string, 0, len(stack))
		for _, f := range stack {
			parts = append(parts, f.key)
		}
		return strings.Join(parts, ".")
	}

	seqPath, seqIndent := "", -1
	var itemPairs map[string]string
	itemScalar := ""
	itemIsMap, itemOpen := false, false

	flush := func() {
		if !itemOpen {
			return
		}
		if itemIsMap {
			lists[seqPath] = append(lists[seqPath], identityJoinPairs(itemPairs))
		} else {
			lists[seqPath] = append(lists[seqPath], itemScalar)
		}
		itemOpen = false
	}
	openItem := func(rest string) {
		if k, v, ok := identitySplitPair(rest); ok {
			itemIsMap, itemPairs = true, map[string]string{}
			if v != "" {
				itemPairs[k] = v
			}
			itemOpen = true
			return
		}
		itemIsMap, itemScalar, itemOpen = false, identityUnquote(rest), true
	}

	inBlockComment := false
	for _, raw := range strings.Split(body, "\n") {
		t := strings.TrimSpace(raw)
		if identityTemplateCommentOpen.MatchString(t) {
			inBlockComment = true
		}
		if inBlockComment {
			if strings.Contains(t, "*/") {
				inBlockComment = false
			}
			continue
		}
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if identityStructureProducer.MatchString(t) {
			unknown = append(unknown, "структуру печатает действие шаблона: "+t)
			continue
		}
		if strings.HasPrefix(t, "{{") {
			if !identityTemplateControl.MatchString(t) {
				unknown = append(unknown, "действие шаблона печатает содержимое: "+t)
			}
			continue
		}
		ind := len(raw) - len(strings.TrimLeft(raw, " "))

		// Запись последовательности.
		if t == "-" || strings.HasPrefix(t, "- ") {
			if seqIndent >= 0 && ind < seqIndent {
				flush()
				seqPath, seqIndent = "", -1
			}
			for len(stack) > 0 && stack[len(stack)-1].indent >= ind {
				stack = stack[:len(stack)-1]
			}
			flush()
			if seqIndent != ind {
				seqPath, seqIndent = pathOf(), ind
				if _, ok := lists[seqPath]; !ok {
					lists[seqPath] = nil
				}
			}
			openItem(strings.TrimSpace(strings.TrimPrefix(t, "-")))
			continue
		}

		// Продолжение записи-карты: строка глубже отступа записи.
		if seqIndent >= 0 && itemOpen && itemIsMap && ind > seqIndent {
			if k, v, ok := identitySplitPair(t); ok && v != "" {
				itemPairs[k] = v
			}
			continue
		}
		if seqIndent >= 0 && ind <= seqIndent {
			flush()
			seqPath, seqIndent = "", -1
		}

		// Строка «ключ: величина».
		for len(stack) > 0 && stack[len(stack)-1].indent >= ind {
			stack = stack[:len(stack)-1]
		}
		k, v, ok := identitySplitPair(t)
		if !ok {
			continue
		}
		stack = append(stack, frame{indent: ind, key: k})
		p := pathOf()
		switch {
		case strings.HasPrefix(v, "["):
			lists[p] = identityFlowSeq(v)
			stack = stack[:len(stack)-1]
		case v != "":
			stack = stack[:len(stack)-1]
		}
	}
	flush()

	// Пустого «списка-по-ошибке» здесь не бывает by construction: ключ-карта в
	// перечень не попадает вовсе (он только кладётся на стек пути), а запись
	// `ключ: []` — это ЯВНО объявленный пустой список, то есть максимальное
	// сужение из возможных. Отбросить его как «ничего не нашли» значило бы
	// сделать невидимым худший случай предмета этого гейта.
	return lists, unknown
}

// identityFlowSeq разбирает поточную форму `[a, b, c]`; `[]` — пустой список.
func identityFlowSeq(v string) []string {
	end := strings.LastIndex(v, "]")
	if end < 0 {
		return nil
	}
	inner := strings.TrimSpace(v[1:end])
	if inner == "" {
		return []string{}
	}
	parts := strings.Split(inner, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, identityUnquote(strings.TrimSpace(p)))
	}
	return out
}

// identitySplitPair режет `ключ: величина`, отбрасывая хвостовой комментарий.
func identitySplitPair(t string) (key, val string, ok bool) {
	i := strings.Index(t, ":")
	if i <= 0 {
		return "", "", false
	}
	if i+1 < len(t) && t[i+1] != ' ' {
		return "", "", false // `http://…` и прочие двоеточия внутри величины
	}
	key = strings.TrimSpace(t[:i])
	val = strings.TrimSpace(t[i+1:])
	if strings.HasPrefix(val, "#") {
		val = ""
	} else if j := strings.Index(val, " #"); j >= 0 {
		val = strings.TrimSpace(val[:j])
	}
	if key == "" || strings.ContainsAny(key, " \t") {
		return "", "", false
	}
	return key, identityUnquote(val), true
}

// identityUnquote снимает кавычки, если величина в них целиком.
func identityUnquote(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

// identityOurListsFromTree читает нашу сторону из шаблона дерева.
func identityOurListsFromTree(t *testing.T) (map[string][]string, []string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(identityConfigTemplate)) // #nosec G304 -- путь — константа собственного дерева
	if err != nil {
		t.Fatalf("объявление настроек личности не прочитано (%s): %v — предпосылка исчезла, "+
			"а не дерево стало чистым", identityConfigTemplate, err)
	}
	m := identityConfigBodyDecl.FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatalf("в %s не найдено тело именованного шаблона `kacho.identity.configYaml` — "+
			"НАША сторона не прочитана вовсе, и «расхождений не найдено» здесь означало бы "+
			"«ничего не сверялось»", filepath.Base(identityConfigTemplate))
	}
	lists, unknown := identityOurLists(m[1])
	return lists, unknown
}

// ─────────────────────────────────────────────────────────────────────────────
// (I) ОБЪЯВЛЕНИЯ.

func TestIdentityReplacedListsAreDecided(t *testing.T) {
	stacks := deployStacks(t)
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	ours, unknownOurs := identityOurListsFromTree(t)
	if len(unknownOurs) > 0 {
		t.Fatalf("тело наших настроек несёт %d форм(ы) записи, которых строчный разбор НЕ ЧИТАЕТ:\n  %s\n"+
			"Это не край и не редкость: всё записанное в такой форме уходит ИЗ-ПОД НАБЛЮДЕНИЯ — "+
			"гейт не даёт ни красного, ни зелёного, он молчит. Либо верните форму к тем четырём, "+
			"что разбор знает (блочная последовательность · поточная `[a, b]` · последовательность "+
			"карт · записи, порождённые `range`), либо научите разбор пятой И докажите инъекцией по ней",
			len(unknownOurs), strings.Join(unknownOurs, "\n  "))
	}
	if len(ours) == 0 {
		t.Fatalf("в теле наших настроек не найдено НИ ОДНОГО списка — вердикта нет: "+
			"«общих списков не найдено» здесь неотличимо от «наша сторона не прочитана» (%s)",
			identityConfigTemplate)
	}

	var (
		filesRead, bytesRead   int
		raising, mounting      int
		stacksWithProviderNode int
		providerListPaths      = map[string]bool{}
		views                  []identityStackLists
	)
	for _, name := range names {
		chain := stacks[name]
		texts := make([]string, 0, len(chain))
		for _, prof := range chain {
			texts = append(texts, readFileForTest(t, filepath.Join(umbrellaDir, prof)))
		}
		// П1: без службы личности и без доведения наших настроек до процесса
		// замещения не происходит — предмета нет, и молчание здесь законно.
		if !identityChainRaisesIdentity(texts) {
			continue
		}
		raising++
		if !identityChainMountsOurConfig(texts) {
			continue
		}
		mounting++

		merged, f, b := mergedValuesOfStack(t, chain)
		filesRead += f
		bytesRead += b

		provNode, ok := lookup(merged, "kratos", "kratos", "config")
		if !ok {
			continue
		}
		stacksWithProviderNode++

		prov, unknownProv := identityProviderLists(provNode)
		if len(unknownProv) > 0 {
			t.Fatalf("стек %q: сторона поставщика несёт %d форм(ы) записи, которых нормализатор "+
				"НЕ ЗНАЕТ:\n  %s\nМолчать о них нельзя — они ушли бы из-под наблюдения",
				name, len(unknownProv), strings.Join(unknownProv, "\n  "))
		}
		for p := range prov {
			providerListPaths[p] = true
		}
		views = append(views, identityStackLists{Stack: name, Provider: prov, Ours: ours})
	}

	findings := adjudicateReplacedIdentityLists(views, identityReplacedListDecisions)
	for _, f := range findings {
		switch f.Reason {
		case replacedListNotDecided:
			t.Errorf("стек %q: %s — путь %q.\nНаша секция ЗАМЕЩАЕТ список поставщика целиком, "+
				"поэтому эти %d запис(и/ей) из действующей конфигурации ИСЧЕЗАЮТ молча:\n  %s\n"+
				"Исходов ровно два: (1) внесите их в наш список в %s — тогда замещение ничего не "+
				"теряет и записи в ведомости не нужно; (2) если сужение намеренное — назовите путь "+
				"в `identityReplacedListDecisions` (%s) вместе с причиной и ценой. "+
				"«Сегодня отказа нет» исходом НЕ является: следующее сужение пройдёт так же молча",
				f.Stack, f.Reason, f.Path, len(f.Lost), strings.Join(f.Lost, "\n  "),
				identityConfigTemplate, "deploy/"+filepath.Base(identityReplacedListsGateFile))
		case replacedListNoSubject:
			t.Errorf("%s — путь %q. Решению больше нечего решать: списка под этим путём нет "+
				"ни у поставщика, ни у нас (либо есть только у одной стороны). Запись, которой "+
				"нечего исключать, — это слепая зона, выданная вперёд: следующая НАСТОЯЩАЯ "+
				"находка по этому пути уедет под неё незамеченной. Снимите запись из "+
				"`identityReplacedListDecisions`", f.Reason, f.Path)
		case replacedListRedundant:
			t.Errorf("%s — путь %q. Наш список теперь ПОКРЫВАЕТ записи поставщика на каждом "+
				"стеке, то есть сужения больше нет, а решение о нём читается как действующее. "+
				"Снимите запись из `identityReplacedListDecisions`: покрытие вычисляется, и "+
				"объявлять его не требуется", f.Reason, f.Path)
		}
	}

	// П4 — объём осмотренного. Печатается ВСЕГДА.
	commonPaths := 0
	coveredPaths := 0
	seen := map[string]bool{}
	for _, v := range views {
		for p := range v.Provider {
			if _, ok := v.Ours[p]; !ok || seen[p] {
				continue
			}
			seen[p] = true
			commonPaths++
		}
	}
	for p := range seen {
		all := true
		for _, v := range views {
			if pv, ok := v.Provider[p]; ok {
				if !identityListCovers(v.Ours[p], pv) {
					all = false
				}
			}
		}
		if all {
			coveredPaths++
		}
	}

	if len(names) == 0 || filesRead == 0 || bytesRead == 0 {
		t.Fatalf("перепись пуста (стеков %d, профилей %d, байт %d) — вердикта НЕТ: "+
			"«нарушений не найдено» здесь неотличимо от «ничего не прочитано»",
			len(names), filesRead, bytesRead)
	}
	// П2 — сторона поставщика читается.
	if stacksWithProviderNode == 0 {
		t.Fatalf("узел настроек подчарта поставщика (`kratos.kratos.config`) не резолвится "+
			"НИ У ОДНОГО из %d стеков — гейт больше не читает ту сторону, которую сверяет, "+
			"и позеленел бы на вернувшемся дефекте", len(names))
	}
	// П1 — замещение вообще происходит.
	if mounting == 0 {
		t.Fatalf("ни один из %d стеков не доводит наши настройки до процесса (%s) — "+
			"замещения не происходит нигде, значит предмета у гейта нет, и его зелёное "+
			"ничего не утверждает", len(names), identityRenderedConfigPath)
	}

	t.Logf("перепись: стеков %d · поднимают службу %d · доводят наши настройки %d · "+
		"узел поставщика резолвится у %d · профилей прочитано %d · байт %d · "+
		"списков поставщика (различных путей) %d · наших списков %d · "+
		"объявлено ОБЕИМИ сторонами %d · покрыто вычислением %d · решений в ведомости %d · находок %d",
		len(names), raising, mounting, stacksWithProviderNode, filesRead, bytesRead,
		len(providerListPaths), len(ours), commonPaths, coveredPaths,
		len(identityReplacedListDecisions), len(findings))

	if commonPaths == 0 {
		t.Logf("ВНЕ ОХВАТА: ни один список не объявлен обеими сторонами — свойство " +
			"выполняется пусто. Это НЕ поломка (пустая ведомость и есть цель), но и не " +
			"свидетельство: проверять было нечего")
	}
}

// identityReplacedListsGateFile — имя файла ведомости, чтобы отказ называл, куда
// идти. Выписано один раз и здесь же проверяется на существование.
const identityReplacedListsGateFile = "identity_replaced_lists_are_decided_test.go"

// TestIdentityReplacedListsLedgerIsUsable — ведомость обязана называть ПРИЧИНУ,
// а файл, на который показывает отказ, обязан существовать.
//
// Запись без причины — это «решено» без решения: следующий читатель не сможет ни
// проверить её, ни снять.
func TestIdentityReplacedListsLedgerIsUsable(t *testing.T) {
	if _, err := os.Stat(identityReplacedListsGateFile); err != nil {
		t.Fatalf("отказ гейта показывает на %s, которого нет: %v — координата в тексте "+
			"отказа читается как адрес, и мёртвый адрес хуже отсутствующего",
			identityReplacedListsGateFile, err)
	}
	paths := make([]string, 0, len(identityReplacedListDecisions))
	for p := range identityReplacedListDecisions {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if len(strings.TrimSpace(identityReplacedListDecisions[p])) < 40 {
			t.Errorf("решение по пути %q не несёт причины (или она короче осмысленной): %q. "+
				"Запись ведомости — это ЗАПИСАННОЕ решение, а не отметка о молчании",
				p, identityReplacedListDecisions[p])
		}
	}
	t.Logf("перепись ведомости: решений %d · все с причиной %v", len(paths), !t.Failed())
}

// ─────────────────────────────────────────────────────────────────────────────
// (II) ПРЕДПОСЫЛКА П3 ПРОТИВ РЕНДЕРА.
//
// Строчный разбор — прокси нашей стороны, и у прокси есть цена: список, которого
// он не увидел, для гейта не существует. Здесь прокси сверяется с тем, что
// процесс получает НА САМОМ ДЕЛЕ: всякий список отрендеренных настроек обязан
// быть найден разбором. Обратное НЕ требуется — ветка `{{ if }}` даёт список,
// которого в этой посадке в рендере нет, и разбор видит обе ветки by construction.

func TestIdentityOurListParseSeesEverythingTheRenderDoes(t *testing.T) {
	stacks := deployStacks(t)
	names := make([]string, 0, len(stacks))
	for n := range stacks {
		names = append(names, n)
	}
	sort.Strings(names)

	parsed, unknown := identityOurListsFromTree(t)
	if len(unknown) > 0 {
		t.Fatalf("разбор нашей стороны назвал %d неизвестных форм — сверять прокси с рендером "+
			"нечем: %s", len(unknown), strings.Join(unknown, "; "))
	}

	rendered, checked, missed := 0, 0, 0
	for _, name := range names {
		out, err := renderIdentitySubchart(t, stacks[name])
		if err != nil {
			t.Fatalf("стек %q: рендер отказал: %v\n%s", name, err, out)
		}
		rendered++
		cfg, _ := identityConfigOf(t, out)
		got, unknownRendered := identityProviderLists(cfg)
		if len(unknownRendered) > 0 {
			t.Fatalf("стек %q: отрендеренные настройки несут формы, которых нормализатор не знает: %s",
				name, strings.Join(unknownRendered, "; "))
		}
		if len(got) == 0 {
			t.Fatalf("стек %q: в отрендеренных настройках НЕТ НИ ОДНОГО списка — вердикта нет: "+
				"«разбор ничего не пропустил» неотличимо от «сверять было не с чем»", name)
		}
		paths := make([]string, 0, len(got))
		for p := range got {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			checked++
			if _, ok := parsed[p]; !ok {
				missed++
				t.Errorf("стек %q: рендер несёт список %q, а строчный разбор его НЕ ВИДИТ. "+
					"Значит всё, записанное в этой форме, ушло из-под наблюдения гейта — "+
					"он не даёт ни красного, ни зелёного, он молчит", name, p)
			}
		}
	}

	if rendered == 0 {
		t.Fatalf("не отрендерено НИ ОДНОГО стека из %d — предпосылка не проверена", len(names))
	}
	t.Logf("перепись предпосылки: стеков отрендерено %d · списков рендера сверено %d · "+
		"разбор нашёл своими силами %d · пропущено разбором %d",
		rendered, checked, len(parsed), missed)
}
