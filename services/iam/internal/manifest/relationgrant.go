// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// relationgrant.go — выдача ОТНОШЕНИЕМ: форма и её связность с каноном модели
// прав.
//
// Задача продукта #1936, приёмка
// `services/iam/docs/engineering/acceptance/module-manifest-relation-grant.md`
// (APPROVED круг 1).
//
// # Что здесь было неверно и почему это чинится формой, а не данными
//
// Форма выдачи несла один ключ — `roleId`, — и валидатор связности требовал его
// непустым. Живые же выдачи посева двух модулей наделяют служебную запись
// ОТНОШЕНИЕМ, ключа для которого у формы не было ни одного. Возможность была
// объявлена, задокументирована, покрыта типами — и неисполнима ни при каком
// входе: пересечение двух правил об одном поле пусто.
//
// Обе стороны по отдельности защитимы, неисполним их СТЫК. Именно поэтому класс
// не виден в обзоре изменения и обнаруживается только вызовом.
//
// # Правило о получателе — вторая половина, без которой ключа мало
//
// Ключ сам по себе живые строки объявимыми не делает: модель ограничивает
// субъект самим отношением, и `cluster.system_viewer` объявлен принимающим
// человека и служебную запись — членства группы он не принимает. Форма,
// объявившая отношение и промолчавшая о допустимом виде получателя, завела бы ту
// же неисполнимость этажом выше — уже на уровне схемы.
//
// # Допустимый вид получателя спрашивается У КАНОНА, а не выписывается
//
// Второй перечень разошёлся бы с моделью молча, и разошёлся бы там, где это не
// видно: оба отвечают «да» на законном входе. Чтение живёт в единственном
// экземпляре у владельца канона (`internal/authzmodel`), и берётся оттуда ВШИТАЯ
// копия — не файл и не обход дерева: загрузчик зовут в том числе из оснастки
// дерева и из работающей службы, а файла канона нет ни у первой гарантированно,
// ни у второй вовсе.
//
// # Якорь судится ТОЛЬКО у формы отношения — и это не выборочность
//
// У формы отношения якорь судится **by construction**: чтобы спросить канон, надо
// знать тип объекта, а его даёт резолв якоря — без него спрашивать не о чем.
// У формы РОЛИ якорь сегодня не судится ничем, и это отдельный предмет: он
// заведён задачей продукта #1953 вместе со своим предикатом. Расширять здесь
// значило бы чинить чужой предмет в этом изменении и сделать вердикт
// непрослеживаемым.
package manifest

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/PRO-Robotech/kacho/services/iam/internal/authzmodel"
	"github.com/PRO-Robotech/kacho/services/iam/internal/domain"
)

// grantFormKeys — два ключа, которыми выдача говорит, ЧТО она выдаёт.
//
// Перечень объявлен ОДИН раз и попадает в текст обоих отказов: автор, назвавший
// обе формы или ни одной, обязан узнать не только что ошибся, но и чем это
// чинить. Вторая копия перечня разошлась бы с первой на первом же ключе.
var grantFormKeys = []string{"roleId", "grantedRelation"}

// relationGrantAnchor — единственный якорь, на котором объявляется выдача
// отношением.
//
// Посев модуля выдаёт только на кластере: выдачи на аккаунт и проект заводит iam
// при создании самих этих объектов, а не установка модуля — арендаторов на
// момент установки не существует.
const relationGrantAnchor = domain.ScopeTypeClusterDotted

// validateGrantForm — форм выдачи ровно одна: обе — отказ, ни одной — отказ.
//
// Зеркало живого ключа хранилища, а не новое правило:
//
//	(role_id IS NOT NULL AND granted_relation = '')
//	 OR (role_id IS NULL AND granted_relation <> '')
//
// Прежде этот отказ требовал именно `roleId` и говорил «выдача не сказала, ЧТО
// она выдаёт». Довод остался верным, требование стало однобоким: после введения
// второй формы выдача вправе сказать то же вторым ключом.
func validateGrantForm(doc *yaml.Node, i int, b AccessBinding) []error {
	role := strings.TrimSpace(b.RoleID)
	relation := strings.TrimSpace(b.GrantedRelation)

	switch {
	case role != "" && relation != "":
		return []error{linkFault{
			kind:  ErrBindingIncomplete,
			coord: locate(doc, "seed", "accessBindings", i),
			detail: fmt.Sprintf(
				"seed.accessBindings[%d]: названы ОБЕ формы выдачи — roleId %q и grantedRelation %q; "+
					"выдача выдаёт ровно одно, оставьте один ключ", i, role, relation),
		}}
	case role == "" && relation == "":
		return []error{linkFault{
			kind:  ErrBindingIncomplete,
			coord: locate(doc, "seed", "accessBindings", i),
			detail: fmt.Sprintf(
				"seed.accessBindings[%d]: не названа ни одна форма выдачи — ни %s; "+
					"выдача не сказала, ЧТО она выдаёт", i, strings.Join(grantFormKeys, ", ни ")),
		}}
	}
	return nil
}

// validateRelationGrant — связность выдачи ОТНОШЕНИЕМ с каноном модели прав.
//
// Порядок проверок ОБЪЯВЛЕН, а не выведен из порядка чтения полей, и каждая
// следующая опирается на предыдущую:
//
//  1. якорь резолвится — иначе спрашивать канон не о чем;
//  2. канон разобран — иначе судить нечем, и это отдельный отказ от «не
//     объявляет»: уверенный вердикт по недочитанному был бы вымыслом;
//  3. отношение у типа якоря объявлено — с перечнем объявленных, взятым У
//     КАНОНА;
//  4. отношение не вычисляемое — свой отказ, иначе автор чинил бы получателя
//     там, где чинить надо выбор отношения;
//  5. вид каждого получателя объявлением принят.
//
// Вызывается ТОЛЬКО для формы отношения: у формы роли предмета нет — роль
// раздаёт глаголы своими правилами, и канон о ней не спрашивают.
//
// seededSubjects — субъекты, уже признанные заведёнными этим же посевом. Вид,
// который посев не заводит вовсе (человек), сюда не доходит: его отвергает
// прежняя проверка, и назвать канон виновником значило бы отправить автора
// чинить не то — канон человека как раз принимает.
func validateRelationGrant(doc *yaml.Node, i int, b AccessBinding, seededSubjects []int) []error {
	relation := strings.TrimSpace(b.GrantedRelation)
	if relation == "" {
		return nil
	}

	anchor := strings.TrimSpace(b.ScopeType)
	if anchor != relationGrantAnchor {
		return []error{linkFault{
			kind:  ErrRelationAnchor,
			coord: locate(doc, "seed", "accessBindings", i, "scopeType"),
			detail: fmt.Sprintf(
				"seed.accessBindings[%d].scopeType: выдача отношением объявляется только на якоре %q, "+
					"получено %q; выдача ролью этой проверкой не затронута (#1953)",
				i, relationGrantAnchor, anchor),
		}}
	}
	objectType, ok := domain.ScopeTypeFromDotted(anchor)
	if !ok {
		// Недостижимо, пока якорь пиннут выше: ветвь оставлена ради того, чтобы
		// смена якоря не превратила резолв в молчаливый пропуск.
		return []error{linkFault{
			kind:   ErrRelationAnchor,
			coord:  locate(doc, "seed", "accessBindings", i, "scopeType"),
			detail: fmt.Sprintf("seed.accessBindings[%d].scopeType: якорь %q не резолвится в тип объекта", i, anchor),
		}}
	}

	canon, err := authzmodel.Shared()
	if err != nil {
		return []error{linkFault{
			kind:  ErrCanonUnparsed,
			coord: locate(doc, "seed", "accessBindings", i, "grantedRelation"),
			detail: fmt.Sprintf(
				"seed.accessBindings[%d].grantedRelation: канон модели прав не разобран — "+
					"судить об отношении %q нечем: %v", i, relation, err),
		}}
	}

	decl, declared := canon.RelationSubjects(objectType, relation)
	if !declared {
		names, _ := canon.RelationNames(objectType)
		return []error{linkFault{
			kind:  ErrRelationNotDeclared,
			coord: locate(doc, "seed", "accessBindings", i, "grantedRelation"),
			detail: fmt.Sprintf(
				"seed.accessBindings[%d].grantedRelation: отношения %q канон модели прав у типа %q "+
					"не объявляет; объявлены: %s", i, relation, objectType, strings.Join(names, ", ")),
		}}
	}
	if !decl.Direct {
		return []error{linkFault{
			kind:  ErrRelationComputed,
			coord: locate(doc, "seed", "accessBindings", i, "grantedRelation"),
			detail: fmt.Sprintf(
				"seed.accessBindings[%d].grantedRelation: отношение %q у типа %q объявлено ВЫЧИСЛЯЕМЫМ — "+
					"прямых субъектов у него нет, и выдать его напрямую нечем; назовите отношение, "+
					"которое канон объявляет прямым", i, relation, objectType),
		}}
	}

	var faults []error
	for _, j := range seededSubjects {
		s := b.Subjects[j]
		if decl.AcceptsKind(s.Type) {
			continue
		}
		faults = append(faults, linkFault{
			kind:  ErrRelationRecipientKind,
			coord: locate(doc, "seed", "accessBindings", i, "subjects", j),
			detail: fmt.Sprintf(
				"seed.accessBindings[%d].subjects[%d]: отношение %q у типа %q получателя вида %q "+
					"не принимает; объявление принимает: %s",
				i, j, relation, objectType, s.Type, strings.Join(decl.Accepts, ", ")),
		})
	}
	return faults
}
