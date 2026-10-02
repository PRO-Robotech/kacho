// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"sort"
	"strings"
)

// processroots.go — правило счёта процессов каталога службы (kacho#2915,
// замысел З32, решение Д74; держатель правила — полоса D4).
//
// Процесс — корень `services/<svc>/cmd/<корень>` вне `migrator` (точка наката
// процессом не является: она бежит до старта процесса и гаснет). Каталог с
// двумя корнями — два бинаря одного каталога (notify: шлюз `cmd/notify` и
// проба-источник `cmd/notify-probe`), а не две службы и не находка «лишний
// корень». Гейт, считающий процессы по каталогу службы, обязан считать КОРНИ:
// судя каталог целиком, он приписал бы величины одного процесса другому, а
// ручку одного — дескриптору другого.

// migratorRoot — корень точки наката: не процесс.
const migratorRoot = "migrator"

// catalogProcessRoots — корни процессов каждого каталога службы в составе rels
// (пути от корня дерева, через `/`): каталог → отсортированные корни.
func catalogProcessRoots(rels []string) map[string][]string {
	seen := map[string]map[string]bool{}
	for _, rel := range rels {
		parts := strings.Split(rel, "/")
		if len(parts) < 5 || parts[0] != "services" || parts[2] != "cmd" ||
			parts[3] == migratorRoot || !strings.HasSuffix(rel, ".go") {
			continue
		}
		if seen[parts[1]] == nil {
			seen[parts[1]] = map[string]bool{}
		}
		seen[parts[1]][parts[3]] = true
	}
	out := make(map[string][]string, len(seen))
	for svc, roots := range seen {
		for r := range roots {
			out[svc] = append(out[svc], r)
		}
		sort.Strings(out[svc])
	}
	return out
}

// processOfFile — процесс, которому принадлежит файл rel.
//
// У каталога с одним корнем процесс один — каталог (`<svc>`), как было до
// правила. У каталога с несколькими корнями файл под `cmd/<корень>/`
// принадлежит своему корню; ГЛАВНЫЙ корень — тот, что назван именем каталога
// (`cmd/<svc>`), — зовётся именем каталога (`<svc>`), прочие —
// `<svc>/<корень>`. Общий код каталога вне `cmd/` (`internal/…`) принадлежит
// главному корню: его конфигурация и стражи — шлюза notify, а корень пробы
// держит своё под `cmd/notify-probe/internal/`. Второй результат — false для
// пути вне `services/`.
func processOfFile(roots map[string][]string, rel string) (string, bool) {
	parts := strings.Split(rel, "/")
	if len(parts) < 2 || parts[0] != "services" {
		return "", false
	}
	svc := parts[1]
	if len(roots[svc]) > 1 && len(parts) >= 5 && parts[2] == "cmd" &&
		parts[3] != migratorRoot && parts[3] != svc {
		return svc + "/" + parts[3], true
	}
	return svc, true
}

// processMetricSegment — сегмент имён серий процесса (`kacho_<сегмент>_…`):
// у каталога с одним корнем — имя каталога, у корня каталога с несколькими —
// имя корня без дефисов (серии ASCII без дефиса): `notify/notify-probe` →
// `notifyprobe`, главный корень `notify` → `notify`.
func processMetricSegment(process string) string {
	if i := strings.IndexByte(process, '/'); i >= 0 {
		return strings.ReplaceAll(process[i+1:], "-", "")
	}
	return process
}
