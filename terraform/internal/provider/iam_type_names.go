// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package provider

// Имена типов ресурсов доступа и переезд состояния арендатора под новое имя.
//
// # Почему имена типов живут ОДНИМ словарём
//
// Имя типа — внешне адресуемая координата записи в состоянии оператора (ban #15): по нему
// Terraform находит ресурс, который уже создан. У пяти типов доступа это имя было записано
// ДВАЖДЫ независимыми литералами — в описании типа и в пространстве ключа повторной подачи,
// уезжающем на край, — и общей константы у двух написаний не было. Разойтись они могли
// молча: обе стороны по отдельности защитимы, а неверна их разница.
//
// Словарь ниже — единственное место, где имя типа доступа объявляется. Пространство ключа
// повторной подачи берётся из него же, поэтому «переехало одно, не переехало второе»
// перестало быть представимым.
//
// # Почему переезд объявляется, а не подразумевается
//
// Переименование типа без объявления переезда даёт оператору не ошибку, а УДАЛЕНИЕ живой
// записи и создание новой: прежняя запись состояния становится сиротой, а новая строится с
// нуля. Замер на OpenTofu 1.12.5: одного блока `moved` в настройке для этого НЕДОСТАТОЧНО —
// исполнитель шлёт запрос переезда целевому типу, и тип, не объявивший поддержки, роняет
// план словами «The target resource implementation does not include move resource state
// support». Поэтому переезд объявлен здесь, на стороне провайдера, а блок `moved` в модулях
// — вторая половина той же пары.

import (
	"context"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// providerTypeName — имя провайдера. Составные имена типов платформы выводятся из него, и
// объявлено оно ОДИН раз: второе объявление разошлось бы с первым молча.
const providerTypeName = "kacho"

// providerSourceAddress — пространство имён и тип провайдера в адресе `source`.
//
// Имя узла реестра сюда НЕ входит намеренно: у состояния, записанного другим исполнителем,
// узел другой (`registry.terraform.io` против `registry.opentofu.org`), а провайдер тот же.
// Сверять узел значило бы отвергать законный переезд по признаку, к делу не относящемуся.
const providerSourceAddress = "PRO-Robotech/kacho"

// Имена типов ресурсов доступа. Одно объявление на тип — и описание типа, и пространство
// ключа повторной подачи читают отсюда.
//
// #nosec G101 -- в блоке ТОЛЬКО имена типов ресурсов, и ни одно из них не является
// учётными данными. Подавление стоит на блоке, а не на двух строках внутри, потому что
// оно ровно со-объёмно своему обоснованию: разбор отмечает имена по подстрокам «key» и
// «token», а ключ служебной учётки и личный токен — это РЕСУРСЫ, чей материал живёт в
// состоянии оператора и у края, но не в этой строке. Строка, которая перестала бы быть
// именем типа, обязана уехать из блока — тогда подавление её не накроет.
const (
	typeNameIAMAccount           = "kaname_account"
	typeNameIAMProject           = "kaname_project"
	typeNameIAMGroup             = "kaname_group"
	typeNameIAMServiceAccount    = "kaname_service_account"
	typeNameIAMServiceAccountKey = "kaname_service_account_key"
	typeNameIAMRole              = "kaname_role"
	typeNameIAMAccessBinding     = "kaname_access_binding"
	typeNameIAMUserInvitation    = "kaname_user_invitation"
	typeNameIAMUserToken         = "kaname_user_token"
)

// retiredResourceTypeNames — прежнее имя типа по нынешнему.
//
// Словарь несёт ДВА предмета сразу, и оба обязательны. Первый — переезд: по нему каждый
// ресурс узнаёт, с какого имени он принимает состояние. Второй — перепись остатка: имя,
// оставшееся в дереве после перехода, читается арендатором как действующее, а типом уже не
// является; гейт остатка ходит по этому же словарю, поэтому «перечень» и «то, что реально
// переезжает» не могут разойтись.
//
// #nosec G101 -- то же основание, что у блока выше: значения — снятые имена типов.
var retiredResourceTypeNames = map[string]string{
	typeNameIAMAccount:           "kacho_iam_account",
	typeNameIAMProject:           "kacho_iam_project",
	typeNameIAMGroup:             "kacho_iam_group",
	typeNameIAMServiceAccount:    "kacho_iam_service_account",
	typeNameIAMServiceAccountKey: "kacho_iam_service_account_key",
	typeNameIAMRole:              "kacho_iam_role",
	typeNameIAMAccessBinding:     "kacho_iam_access_binding",
	typeNameIAMUserInvitation:    "kacho_iam_user_invitation",
	typeNameIAMUserToken:         "kacho_iam_user_token",
}

// movedFromRetiredTypeName — объявление переезда состояния для ресурса, сменившего имя типа.
//
// Имя типа и схема спрашиваются у САМОГО ресурса, а не выписываются вторым списком: у
// плоских и табличных ресурсов имя приходит из их описания, и рукописная копия отстала бы
// молча при заведении следующего типа. Ресурсу, чьё имя не менялось, объявлений не
// возвращается вовсе — исполнитель тогда честно скажет, что переезда у этого типа нет.
func movedFromRetiredTypeName(ctx context.Context, r resource.Resource) []resource.StateMover {
	var meta resource.MetadataResponse
	r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: providerTypeName}, &meta)

	retired, ok := retiredResourceTypeNames[meta.TypeName]
	if !ok {
		return nil
	}

	var sch resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &sch)
	// Схема источника и цели одна и та же: переезд меняет АДРЕС записи, а не её форму.
	// Объявив её, мы получаем разобранное состояние источника и переносим его целиком —
	// без пересборки по полям, которая отстала бы от схемы при первом же новом поле.
	source := sch.Schema

	return []resource.StateMover{{
		SourceSchema: &source,
		StateMover: func(_ context.Context, req resource.MoveStateRequest, resp *resource.MoveStateResponse) {
			// Молчание означает «это не мой переезд» и передаёт очередь следующему
			// объявлению. Отказ означает «мой, и он не выполним» — их нельзя путать:
			// первое даёт исполнителю шанс, второе называет предмет.
			if req.SourceTypeName != retired {
				return
			}
			if !sourceProviderIsOurs(req.SourceProviderAddress) {
				return
			}
			if req.SourceSchemaVersion != 0 {
				resp.Diagnostics.AddError(
					"Переезд состояния не выполнен",
					"Запись состояния типа "+retired+" имеет версию схемы "+
						strconv.FormatInt(req.SourceSchemaVersion, 10)+", а переезд объявлен для версии 0. "+
						"Обновите провайдер до версии, знающей эту версию схемы, прежде чем "+
						"переводить конфигурацию на "+meta.TypeName+".")
				return
			}
			if req.SourceState == nil {
				resp.Diagnostics.AddError(
					"Переезд состояния не выполнен",
					"Состояние типа "+retired+" не разобрано по объявленной схеме. "+
						"Запись состояния повреждена либо записана несовместимой версией "+
						"провайдера; переезд остановлен, чтобы не потерять её содержимое.")
				return
			}
			resp.TargetState = *req.SourceState
		},
	}}
}

// sourceProviderIsOurs — записан ли источник ЭТИМ провайдером.
//
// Сверяется хвост адреса (пространство имён и тип), а не адрес целиком: узел реестра у
// состояния зависит от исполнителя, которым оно писалось, и к вопросу «тот ли это
// провайдер» отношения не имеет. Регистр не значим — исполнитель приводит адрес к нижнему.
func sourceProviderIsOurs(addr string) bool {
	if addr == "" {
		return false
	}
	return strings.HasSuffix(strings.ToLower(addr), "/"+strings.ToLower(providerSourceAddress))
}
