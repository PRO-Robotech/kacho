// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package release проверяет публикуемый Go-модуль с позиции внешнего потребителя.
//
// Существующая проба П8 пакует отслеживаемую ревизию и собирает внешний
// reference witness. Consumers preflight отдельно составляет полный перечень импортов
// объявленных repository или external-program consumers, включая тестовые
// исходники и build constraints, и собирает их из проверяемого Go module ZIP.
// Источники связаны с точными Git revisions; file proxy и module cache изолированы.
//
// Consumers mode не публикует refs и не заменяет остальные release gates,
// проверку защищённого main, опубликованного архива или разрешение выпуска.
package release
