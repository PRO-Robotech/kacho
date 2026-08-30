// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"regexp"
	"sort"
	"strings"
)

// Разбор четырёх проекций схемы vpc. Функции берут СОДЕРЖИМОЕ, а не пути:
// иначе доказать способность гейта упасть можно было бы только правкой дерева,
// то есть на живых файлах соседней полосы.

var (
	vpcSchemaGooseUpRe   = regexp.MustCompile(`(?is)--\s*\+goose\s+Up(.*?)(?:--\s*\+goose\s+Down|$)`)
	vpcSchemaCreateTblRe = regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([\w".]+)`)
	vpcSchemaDropTblRe   = regexp.MustCompile(`(?i)DROP\s+TABLE\s+(?:IF\s+EXISTS\s+)?([\w".]+)`)
	vpcSchemaDbmlTableRe = regexp.MustCompile(`(?m)^Table\s+"[\w]+"\."([\w]+)"`)
	vpcSchemaProtoSvcRe  = regexp.MustCompile(`(?ms)^service\s+(\w+)\s*\{(.*?)^\}`)
	vpcSchemaProtoRPCRe  = regexp.MustCompile(`\brpc\s+(\w+)`)
	vpcSchemaPageRowRe   = regexp.MustCompile(`<tr><td><code>([\w\\]+)</code>.*?<td>([^<]*)</td></tr>`)
	vpcSchemaPageTbodyRe = regexp.MustCompile(`(?s)<tbody>(.*?)</tbody>`)
)

// vpcSchemaUnqualify снимает схему и кавычки: `kacho_vpc."networks"` → `networks`.
func vpcSchemaUnqualify(raw string) string {
	raw = strings.ReplaceAll(raw, `"`, "")
	if i := strings.LastIndex(raw, "."); i >= 0 {
		raw = raw[i+1:]
	}
	return raw
}

// vpcSchemaMigration — одна миграция цепочки: имя решает порядок применения.
type vpcSchemaMigration struct {
	Name string
	Body string
}

// vpcSchemaLiveTables — таблицы, которые цепочка миграций создаёт и не снимает.
//
// Судится ТОЛЬКО раздел `+goose Up`: секция отката тех же файлов снимает живые
// таблицы, и обход целого файла объявил бы половину схемы мёртвой.
func vpcSchemaLiveTables(files []vpcSchemaMigration) []string {
	sorted := append([]vpcSchemaMigration(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	alive := map[string]bool{}
	for _, f := range sorted {
		up := f.Body
		if m := vpcSchemaGooseUpRe.FindStringSubmatch(f.Body); m != nil {
			up = m[1]
		}
		for _, m := range vpcSchemaCreateTblRe.FindAllStringSubmatch(up, -1) {
			alive[vpcSchemaUnqualify(m[1])] = true
		}
		for _, m := range vpcSchemaDropTblRe.FindAllStringSubmatch(up, -1) {
			alive[vpcSchemaUnqualify(m[1])] = false
		}
	}
	return vpcSchemaSortedTrue(alive)
}

// vpcSchemaDiagramTables — таблицы, нарисованные на ER-диаграмме (данные DBML).
func vpcSchemaDiagramTables(dbml string) []string {
	out := map[string]bool{}
	for _, m := range vpcSchemaDbmlTableRe.FindAllStringSubmatch(dbml, -1) {
		out[m[1]] = true
	}
	return vpcSchemaSortedTrue(out)
}

// vpcSchemaPageRow — строка перечня «Таблицы схемы»: имя и объявленный столбцом ответ
// «попала ли она в изображение».
type vpcSchemaPageRow struct {
	Table     string
	OnDiagram string
}

// vpcSchemaPageRows читает перечень схемы со страницы модели данных.
//
// Перечень ищется от заголовка раздела, а не «первым <tbody> файла»: страница
// несёт несколько таблиц, и привязка к порядку сломалась бы от перестановки
// разделов молча.
func vpcSchemaPageRows(mdx, heading string) []vpcSchemaPageRow {
	i := strings.Index(mdx, heading)
	if i < 0 {
		return nil
	}
	body := vpcSchemaPageTbodyRe.FindStringSubmatch(mdx[i:])
	if body == nil {
		return nil
	}
	var out []vpcSchemaPageRow
	for _, m := range vpcSchemaPageRowRe.FindAllStringSubmatch(body[1], -1) {
		out = append(out, vpcSchemaPageRow{
			Table:     strings.ReplaceAll(m[1], `\`, ""),
			OnDiagram: strings.TrimSpace(m[2]),
		})
	}
	return out
}

// vpcSchemaFullVerbServices — службы контракта, несущие ВЕСЬ набор глаголов ресурса.
//
// Именно это свойство задаёт границу диаграммы. Служба, несущая часть набора
// (чтение величин, внутренние глаголы), ресурсом домена в этом смысле не
// является и таблицы за собой не ведёт.
func vpcSchemaFullVerbServices(protos map[string]string) []string {
	// Набор стандартных методов ресурса — из конвенций Kachō (`api-conventions.md`
	// §«Стандартные методы ресурса»). Он ЛОКАЛЕН намеренно: верхнеуровневый
	// словарь этих же имён завёл бы общее объявление, которое можно
	// импортировать и расширить в стороне от читателя, — а здесь читатель один,
	// и всякая правка набора немедленно меняет вердикт этого же гейта.
	verbs := [...]string{"Create", "Get", "List", "Update", "Delete"}

	out := map[string]bool{}
	for _, body := range protos {
		for _, svc := range vpcSchemaProtoSvcRe.FindAllStringSubmatch(body, -1) {
			name := svc[1]
			if strings.HasPrefix(name, "Internal") {
				continue
			}
			have := map[string]bool{}
			for _, r := range vpcSchemaProtoRPCRe.FindAllStringSubmatch(svc[2], -1) {
				have[r[1]] = true
			}
			full := true
			for _, v := range verbs {
				if !have[v] {
					full = false
					break
				}
			}
			if full {
				out[name] = true
			}
		}
	}
	return vpcSchemaSortedTrue(out)
}

// vpcSchemaTableOfService выводит имя таблицы из имени службы по ПРАВИЛУ, а не по
// перечню: `SecurityGroupService` → `security_groups`, `AddressService` →
// `addresses`. Перечень пришлось бы вести руками, и он разошёлся бы с деревом
// молча — ровно тот класс, ради которого гейт заведён.
func vpcSchemaTableOfService(svc string) string {
	stem := strings.TrimSuffix(svc, "Service")
	var b strings.Builder
	for i, r := range stem {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	snake := b.String()
	switch {
	case strings.HasSuffix(snake, "s"), strings.HasSuffix(snake, "x"),
		strings.HasSuffix(snake, "z"), strings.HasSuffix(snake, "ch"),
		strings.HasSuffix(snake, "sh"):
		return snake + "es"
	default:
		return snake + "s"
	}
}

func vpcSchemaSortedTrue(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// vpcSchemaMissingFrom — что есть в want и нет в have.
func vpcSchemaMissingFrom(want, have []string) []string {
	set := map[string]bool{}
	for _, h := range have {
		set[h] = true
	}
	var out []string
	for _, w := range want {
		if !set[w] {
			out = append(out, w)
		}
	}
	sort.Strings(out)
	return out
}

// vpcSchemaInfra — таблицы диаграммы, не принадлежащие ни одному ресурсу.
var vpcSchemaInfra = []string{"operations", "vpc_outbox"}

// vpcSchemaAdjudicate выносит вердикт по четырём проекциям схемы и возвращает
// находки текстом.
//
// Вынесено из пробы намеренно: инъекция обязана гонять ТОТ ЖЕ код, что и гейт.
// Копия предиката в пробе доказывала бы способность упасть у копии.
func vpcSchemaAdjudicate(live, diagram []string, rows []vpcSchemaPageRow, services []string) []string {
	var out []string
	add := func(s string) { out = append(out, s) }

	listed := make([]string, 0, len(rows))
	for _, r := range rows {
		listed = append(listed, r.Table)
	}
	sort.Strings(listed)

	if miss := vpcSchemaMissingFrom(live, listed); len(miss) > 0 {
		add("перечень объявлен полным, но живой таблицы в нём нет: " + strings.Join(miss, ", "))
	}
	if extra := vpcSchemaMissingFrom(listed, live); len(extra) > 0 {
		add("перечень называет таблицу, которой цепочка миграций не создаёт: " + strings.Join(extra, ", "))
	}
	if extra := vpcSchemaMissingFrom(diagram, listed); len(extra) > 0 {
		add("диаграмма рисует таблицу вне перечня: " + strings.Join(extra, ", "))
	}

	onDiagram := map[string]bool{}
	for _, d := range diagram {
		onDiagram[d] = true
	}
	for _, r := range rows {
		want := vpcSchemaDiagramNo
		if onDiagram[r.Table] {
			want = vpcSchemaDiagramYes
		}
		if r.OnDiagram != want {
			add("строка " + r.Table + " объявляет «На диаграмме: " + r.OnDiagram +
				"», изображение говорит «" + want + "»")
		}
	}

	want := append([]string(nil), vpcSchemaInfra...)
	for _, svc := range services {
		tbl := vpcSchemaTableOfService(svc)
		if !vpcSchemaContainsStr(live, tbl) {
			add("служба " + svc + " даёт по правилу образования имя таблицы " + tbl +
				", живой такой таблицы нет — правь ПРАВИЛО, а не перечень исключений")
			continue
		}
		want = append(want, tbl)
	}
	sort.Strings(want)
	if miss := vpcSchemaMissingFrom(want, diagram); len(miss) > 0 {
		add("свойство требует таблицу на диаграмме, изображение её не несёт: " + strings.Join(miss, ", "))
	}
	if extra := vpcSchemaMissingFrom(diagram, want); len(extra) > 0 {
		add("диаграмма несёт таблицу, которой свойство не требует: " + strings.Join(extra, ", ") +
			" — граница перестала быть свойством и стала перечислением")
	}
	return out
}

func vpcSchemaContainsStr(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

const (
	vpcSchemaDiagramYes = "да"
	vpcSchemaDiagramNo  = "нет"
)
