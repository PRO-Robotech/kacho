// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// recipient.go — чтение канона модели прав: какие типы субъектов принимает
// ОБЪЯВЛЕНИЕ отношения.
//
// # Зачем это здесь, а не в соседнем пакете
//
// Сверка посева выводит из-под вердикта выдачу ОТНОШЕНИЕМ (#1936). Задача-
// преемник объявлена так, будто достаточно завести форме ключ отношения. Этого
// НЕ достаточно, и вторая половина стоит в приёмке-основании
// (`module-manifest-roles-and-seed-grants.md`, §10 п. 6): форма обязана нести и
// ДОПУСТИМЫЙ ТИП ПОЛУЧАТЕЛЯ. Основание — §3.5 той же приёмки: получателем
// преднастроенной выдачи объявлена ТОЛЬКО группа, а модель ограничивает субъект
// самим отношением, и `system_viewer` на `cluster` объявлен `[user,
// service_account]` — членства группы он не принимает.
//
// Пересечение двух правил для живых строк ПУСТО, и это не мнение, а чтение
// канона. Чтобы утверждение не жило комментарием, оно читается машиной:
// [LookupRelation] отвечает по объявлению, а проба предпосылки
// (recipient_test.go) сверяет с ним каждую живую невыразимую выдачу.
//
// # Почему разбор текста, а не готовый разборщик модели
//
// Разборщика `.fga` в дереве нет ни одного, и заводить его ради одного вопроса
// значило бы завести второе место об одном предмете. Предмет чтения здесь узок
// до одной строки объявления, а его СПОСОБНОСТЬ ошибиться доказана инъекцией по
// шести осям (recipient_injection_test.go), включая обе изоляции — между
// отношениями одного типа и между одноимёнными отношениями разных типов.
//
// # Три ответа, а не два
//
// «Не нашёл» отличается от «нашёл, группу не принимает», и оба отличаются от
// «нашёл, прямых субъектов нет вовсе» (отношение вычисляемое: членство может
// прийти транзитивно, и судить о нём по этой строке нельзя). Схлопывание любых
// двух дало бы судью, который на нечитаемом каноне отвечает «предпосылка жива».
package moduleseedparity

import "strings"

// SubjectGroupMember — запись членства группы в прямом userset канона.
const SubjectGroupMember = "group#member"

// RelationDeclaration — объявление одного отношения у одного типа объекта.
type RelationDeclaration struct {
	ObjectType string
	Relation   string
	// DirectSubjects — типы субъектов прямого userset (то, что стоит в
	// квадратных скобках), в порядке объявления.
	DirectSubjects []string
	// HasDirectSubjects — были ли скобки вообще. false означает «отношение
	// вычисляемое», а НЕ «субъектов нет»: разница несущая, см. шапку файла.
	HasDirectSubjects bool
}

// AdmitsGroupMember — принимает ли объявление членство группы прямым userset.
//
// У вычисляемого отношения ответ `false` сам по себе ничего не значит —
// вызывающий обязан сперва спросить [RelationDeclaration.HasDirectSubjects].
func (d RelationDeclaration) AdmitsGroupMember() bool {
	for _, s := range d.DirectSubjects {
		if s == SubjectGroupMember {
			return true
		}
	}
	return false
}

// LookupRelation — объявление отношения `relation` у типа `objectType` в каноне.
//
// ok=false — такого типа в каноне нет либо у него нет такого отношения. Ответ
// даётся ПО ОБЪЯВЛЕНИЮ своего типа: одноимённое отношение соседнего типа
// подменять его не вправе, иначе судья ответил бы про чужой предмет.
func LookupRelation(model, objectType, relation string) (RelationDeclaration, bool) {
	var (
		inType bool
		prefix = "define " + relation + ":"
	)
	for _, raw := range strings.Split(model, "\n") {
		line := strings.TrimSpace(raw)
		if t, isType := strings.CutPrefix(line, "type "); isType {
			// Блок типа кончается там, где начинается следующий: искать дальше
			// значило бы отвечать объявлением соседа.
			if inType {
				return RelationDeclaration{}, false
			}
			inType = strings.TrimSpace(t) == objectType
			continue
		}
		if !inType {
			continue
		}
		rhs, isDefine := strings.CutPrefix(line, prefix)
		if !isDefine {
			continue
		}
		d := RelationDeclaration{ObjectType: objectType, Relation: relation}
		open := strings.Index(rhs, "[")
		closeAt := strings.Index(rhs, "]")
		if open < 0 || closeAt < open {
			// Вычисляемое отношение: прямого userset нет.
			return d, true
		}
		d.HasDirectSubjects = true
		for _, s := range strings.Split(rhs[open+1:closeAt], ",") {
			if s = strings.TrimSpace(s); s != "" {
				d.DirectSubjects = append(d.DirectSubjects, s)
			}
		}
		return d, true
	}
	return RelationDeclaration{}, false
}
