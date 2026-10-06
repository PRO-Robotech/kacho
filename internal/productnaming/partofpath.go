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

// ЗДЕСЬ СТОЯЛ subchartKey — узкий образец ключа подчарта. Он снят вместе со своим
// предметом: подъём обязан останавливаться на ВСЯКОМ ключе верхнего уровня, а
// не только на похожем на имя чарта, и узкий образец такой ключ пропускал.
//
// topLevelKey — ключ нулевого отступа в наложении значений зонта.
//
// Приставкой имени платформы образец НЕ сужается: часть продукта вправе носить
// СВОЁ имя, и сужение по `kacho-` чужой ключ не отвергало бы, а НЕ ВИДЕЛО —
// строка переименованной части оставалась бы без хозяина. Кому принадлежит
// распознанный ключ, решает [ServiceDir].
// Шире прежнего образца намеренно: подъём обязан ОСТАНАВЛИВАТЬСЯ на всяком блоке
// верхнего уровня, включая те, чьё имя записано иначе (`opaSidecar`). Узкий
// образец такой ключ не видел вовсе и пропускал строку соседнему подчарту.
var topLevelKey = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_.-]*):`)

// umbrellaChartsPrefix — приставка пути подчарта зонта.
const umbrellaChartsPrefix = "deploy/helm/umbrella/charts/"

// servicesPrefix — приставка пути дерева службы.
const servicesPrefix = "services/"

// deliveryChartsPrefix — приставка чартов поставки вне зонта: чарт службы,
// которая ставится и отдельно (notify, NTF-1 З28, NTF1-I06), лежит в
// `deploy/helm/<каталог службы>/` и в зонт входит записью зависимости
// `file://`. Сам зонт и вендоренные архивы частью продукта не являются.
const deliveryChartsPrefix = "deploy/helm/"

// PartOfPath — каталог исходников части, чей это ПУТЬ.
//
// Три раскладки, все живые: `services/<svc>/…`, подчарт зонта
// `deploy/helm/umbrella/charts/<имя чарта>/…` и чарт поставки вне зонта
// `deploy/helm/<каталог службы>/…`. Путь, не подошедший ни под одну, даёт
// ложь — тогда часть называет ключ подчарта, см. [PartOfLine].
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
		return "", false
	}
	if s, ok := strings.CutPrefix(rel, deliveryChartsPrefix); ok {
		i := strings.IndexByte(s, '/')
		if i > 0 && s[:i] != "umbrella" && s[:i] != "vendor" {
			return s[:i], true
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
		m := topLevelKey.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		// ПЕРВЫЙ ключ верхнего уровня и решает — дальше вверх идти нельзя.
		//
		// Прежде подъём пропускал ключ, который ServiceDir не признал, и шёл
		// выше. Наложение зонта несёт наряду с подчартами общие блоки
		// (`global`, `mtls`, `namespace`, `security`, `opaSidecar`), и строка
		// под таким блоком приписывалась ПРЕДЫДУЩЕМУ подчарту — то есть соседу
		// (kacho#2260). Приписать строку соседу хуже, чем остановиться:
		// покрытой оказалась бы не та часть продукта.
		svc, known := ServiceDir(m[1])
		if !known {
			return "", false
		}
		return svc, true
	}
	return "", false
}

// umbrellaTemplatesPrefix — шаблоны самого зонта (не подчартов).
const umbrellaTemplatesPrefix = "deploy/helm/umbrella/templates/"

// UmbrellaTemplatePart — КАНДИДАТ в части продукта для шаблона самого зонта
// `deploy/helm/umbrella/templates/<файл>`: первый сегмент имени файла до `-`
// либо `.` (Д77 (а) NTF-1; проба notify-probe разворачивается шаблоном зонта,
// полоса D3).
//
// Кандидат, а не ответ: шаблоны зонта называются и не по частям
// (`mail-receiver.yaml`, `clusterissuer.yaml`), и приписать такой файл
// несуществующей части хуже, чем остановиться. Поэтому [PartOfPath] эту
// раскладку НЕ знает, а вызывающий подтверждает кандидата наличием каталога
// `services/<кандидат>` в индексе — состава дерева этот пакет не читает.
func UmbrellaTemplatePart(rel string) (string, bool) {
	name, ok := strings.CutPrefix(filepath.ToSlash(rel), umbrellaTemplatesPrefix)
	if !ok || strings.Contains(name, "/") {
		return "", false
	}
	if i := strings.IndexAny(name, "-."); i > 0 {
		return name[:i], true
	}
	return "", false
}
