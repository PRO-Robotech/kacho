// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package productnaming

import (
	"path/filepath"
	"regexp"
	"strings"
)

// partofpath.go — какой ЧАСТИ продукта принадлежит строка дерева.
//
// # Почему здесь, а не у каждого спрашивающего
//
// Ответ на вопрос «чьё это» выводится из ИМЕНИ: из имени каталога службы либо из
// имени чарта, которым наложение значений зонта называет подчарт. Имена
// принадлежат этому пакету, поэтому и вывод из них — тоже.
//
// До 2026-09-07 тот же вывод стоял копией в пробе формы вызова накатчика
// (`internal/migratorapply`), и она НЕ импортируема: пакет несёт этот разбор
// только в тестовых файлах. Второй спрашивающий (гейт имени накатчика) завёл бы
// вторую копию, а две копии об одном предмете расходятся молча — и разошлись бы
// именно на переименовании, где расхождение дороже всего.

// subchartKey — ключ подчарта в наложении значений зонта: имя в НУЛЕВОЙ колонке.
//
// Приставкой имени платформы образец НЕ сужается: часть продукта вправе носить
// СВОЁ имя, и сужение по `kacho-` чужой ключ не отвергало бы, а НЕ ВИДЕЛО —
// строка переименованной части оставалась бы без хозяина. Кому принадлежит
// распознанный ключ, решает [ServiceDir].
var subchartKey = regexp.MustCompile(`^([a-z][a-z0-9-]*):`)

// umbrellaChartsPrefix — приставка пути подчарта зонта.
const umbrellaChartsPrefix = "deploy/helm/umbrella/charts/"

// servicesPrefix — приставка пути дерева службы.
const servicesPrefix = "services/"

// PartOfPath — каталог исходников части, чей это ПУТЬ.
//
// Две раскладки, обе живые: `services/<svc>/…` и подчарт зонта
// `deploy/helm/umbrella/charts/<имя чарта>/…`. Путь, не подошедший ни под одну,
// даёт ложь — тогда часть называет ключ подчарта, см. [PartOfLine].
//
// Приписать строку соседу хуже, чем остановиться: покрытой оказалась бы не та
// часть продукта.
func PartOfPath(rel string) (string, bool) {
	rel = filepath.ToSlash(rel)
	if s, ok := strings.CutPrefix(rel, servicesPrefix); ok {
		if i := strings.IndexByte(s, '/'); i > 0 {
			return s[:i], true
		}
		return "", false
	}
	if s, ok := strings.CutPrefix(rel, umbrellaChartsPrefix); ok {
		if i := strings.IndexByte(s, '/'); i > 0 {
			return ServiceDir(s[:i])
		}
	}
	return "", false
}

// PartOfLine — каталог исходников части, чью строку номер at объявляет файл rel.
//
// Источников ДВА, и второй не запасной: наложение значений зонта
// (`deploy/helm/umbrella/values.*.yaml`) настраивает ВСЕ подчарты одним файлом,
// поэтому путь там части не называет — её называет ключ подчарта над строкой.
// Обход, знающий только путь, на таком файле остановился бы; знающий только
// первый попавшийся ключ — приписал бы строку соседу.
func PartOfLine(rel string, lines []string, at int) (string, bool) {
	if svc, ok := PartOfPath(rel); ok {
		return svc, true
	}
	for i := at; i >= 0 && i < len(lines); i-- {
		m := subchartKey.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		if svc, known := ServiceDir(m[1]); known {
			return svc, true
		}
	}
	return "", false
}
