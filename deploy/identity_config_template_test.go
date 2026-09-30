// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// identity_config_template_test.go — координата объявления настроек службы
// личности, общая для проб пакета.
//
// ПОЧЕМУ ОНА ЗДЕСЬ, А НЕ В ПРОБЕ. Координату и разбор имени схемы по умолчанию
// читают многие пробы этого пакета (`git grep -l identityConfigTemplate`), и
// объявлены они ОДИН раз: вторая копия пути разошлась бы с первой при следующем
// переносе шаблона. Прежде они
// жили в пробе согласия посева церемонии со схемой чарта. Посев уехал в дерево
// службы вместе с набором, который он обслуживал (kacho#2858, парная
// PRO-Robotech/kaname#398), проба снята вместе с ним, а координата осталась:
// у её читателей свой предмет — объявление настроек, а не посев.
package deploy_test

import "regexp"

// identityConfigTemplate — объявление настроек службы личности. Содержимое
// карты настроек рендерится из именованного шаблона этого файла, поэтому
// судить надо его, а не карту-обёртку (см. _kratos-identity.tpl).
const identityConfigTemplate = "helm/umbrella/charts/kaname/templates/_kratos-identity.tpl"

// defaultSchemaIDDecl — строка `default_schema_id` в объявлении настроек:
// имя схемы личности, которую чарт объявляет действующей по умолчанию.
var defaultSchemaIDDecl = regexp.MustCompile(`(?m)^\s*default_schema_id:\s*(\S+)\s*$`)
