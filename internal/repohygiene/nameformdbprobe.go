// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"path/filepath"
	"sort"
	"strings"
)

// Разбор дерева для гейта «ограничение формы имени, поставленное миграцией
// сервиса, обязано быть ДОКАЗАНО вставкой».
//
// Вынесено в не-тестовый файл пакета, чтобы инъекционная проба звала тот же
// разбор, а не свою копию: копия разошлась бы с оригиналом молча и доказывала бы
// способность упасть у кода, который не исполняется.

// nameFormProbeMarker — вызов общего двигателя пробы. Ищется подстрокой, а не
// разбором синтаксиса, намеренно: двигатель зовётся составным литералом
// (`nameformdb.Probe{…}.Run(…)`), и любая законная форма вызова — присваивание,
// возврат из помощника, поле фикстуры — содержит эту подстроку. Разбор дал бы ту
// же находку дороже.
//
// Собственный файл гейта под обход не подпадает: обходятся только пути
// `services/…`.
const nameFormProbeMarker = "nameformdb.Probe"

// nameFormDBCoverage — исход обхода: кто ставит форму имени в базе и кто
// доказывает её действие.
//
// Объём осмотренного — часть исхода, а не украшение лога: без него «покрыто
// всё» неотличимо от «прочитано ноль».
type nameFormDBCoverage struct {
	MigrationsRead int
	TestsRead      int
	// Constrained — сервис → файлы миграций, объявляющие канон формы.
	Constrained map[string][]string
	// Probed — сервис → файлы проб, зовущих общий двигатель.
	Probed map[string][]string
}

// Services возвращает сервисы, ставящие форму, в устойчивом порядке.
func (c nameFormDBCoverage) Services() []string {
	out := make([]string, 0, len(c.Constrained))
	for svc := range c.Constrained {
		out = append(out, svc)
	}
	sort.Strings(out)
	return out
}

// Unproven — сервисы, которые форму ставят и действие её ничем не доказывают.
func (c nameFormDBCoverage) Unproven() []string {
	out := []string{}
	for _, svc := range c.Services() {
		if len(c.Probed[svc]) == 0 {
			out = append(out, svc)
		}
	}
	return out
}

// analyseNameFormDBCoverage разбирает соответствие «путь → содержимое».
//
// Принимает уже прочитанное, а не читает само, чтобы инъекция подавала
// синтетическое дерево тем же входом, каким гейт получает настоящее.
func analyseNameFormDBCoverage(files map[string]string, canonPattern string) nameFormDBCoverage {
	cov := nameFormDBCoverage{
		Constrained: map[string][]string{},
		Probed:      map[string][]string{},
	}

	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, p := range paths {
		rel := filepath.ToSlash(p)
		svc, ok := nameFormServiceOf(rel)
		if !ok {
			continue
		}
		body := files[p]

		switch {
		case strings.HasSuffix(rel, ".sql") &&
			strings.Contains(rel, "/internal/migrations/"):
			cov.MigrationsRead++
			// Канон ищется в тексте миграции. Форма приезжает параметром из
			// единственного объявления в дереве, поэтому выписанной копии здесь
			// нет и разойтись с каноном гейту не на чем.
			if canonPattern != "" && strings.Contains(body, canonPattern) {
				cov.Constrained[svc] = append(cov.Constrained[svc], rel)
			}
		case strings.HasSuffix(rel, "_test.go"):
			cov.TestsRead++
			if strings.Contains(body, nameFormProbeMarker) {
				cov.Probed[svc] = append(cov.Probed[svc], rel)
			}
		}
	}
	return cov
}

// nameFormServiceOf выделяет имя сервиса из пути `services/<svc>/…`.
//
// Своя, а не соседняя `serviceOfPath` из переписи политики отзыва: та на пути
// вне `services/` возвращает САМ ПУТЬ, потому что ей нужен ключ переписи на
// любой вход. Здесь такой исход завёл бы фантомный «сервис» с именем файла и
// перепись перестала бы что-либо значить, поэтому нужен ответ «это не сервис».
func nameFormServiceOf(rel string) (string, bool) {
	parts := strings.Split(rel, "/")
	if len(parts) < 3 || parts[0] != "services" {
		return "", false
	}
	return parts[1], true
}
