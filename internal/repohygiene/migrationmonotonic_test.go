// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestNewMigrationOutranksEveryAppliedOne — номер НОВОЙ миграции больше любого
// уже применённого.
//
// # Предмет: номер по задаче монотонности не даёт
//
// Номер выводится из номера задачи (`<задача><порядковый>`), и это решает своё —
// две линии не соревнуются за слот в каталоге. Но задачи берутся не по
// возрастанию: задача с номером НИЖЕ максимального применённого даёт версию
// меньше текущей версии базы.
//
// Мигратор такую миграцию не применяет и роняет старт: «found N missing
// migrations before current version». Свежая база получает одну схему,
// существующая — другую, и расходятся они МОЛЧА: стенд поднимается, боевой
// кластер не мигрирует.
//
// Наблюдалось при работе над задачей #703: клейм строки требовал колонки-аренды,
// номер встал бы ниже применённого, и вместо клейма был взят замок прохода —
// приём слабее по существу, выбранный ограничением нумерации, а не задачей.
//
// # Решение: метка времени заведения
//
// Версия новой миграции — `YYYYMMDDHHMMSS`. Она:
//
//   - монотонна by construction: часы идут вперёд, и любая метка времени больше
//     любого унаследованного номера (14 цифр против шести);
//   - по-прежнему ВЫВОДИТСЯ, а не выбирается из каталога, — то есть свойство,
//     ради которого заводился номер по задаче, сохраняется: две линии не смотрят
//     на один и тот же последний файл;
//   - совпадение двух линий до секунды ловит существующая проверка
//     уникальности номера — это её предмет, и здесь он не дублируется.
//
// # Что проверяет ЭТА проверка
//
// Только форму НОВЫХ файлов: добавленные относительно ствола миграции обязаны
// нести метку времени. Унаследованные 302 файла остаются как есть — переномеровать
// применённое нельзя, и правило действует ВПЕРЁД.
//
// Граница названа честно: без ссылки на ствол сравнивать не с чем, и тогда
// проверка объявляет себя беспредметной, а не зелёной.
func TestNewMigrationOutranksEveryAppliedOne(t *testing.T) {
	root := repoRoot(t)

	base := resolveTrunkRef(t, root)
	if base == "" {
		t.Skip("ссылка на ствол не резолвится — сравнивать новые файлы не с чем; " +
			"это граница проверки, а не её зелёный исход")
	}

	out, err := exec.Command("git", "-C", root, "diff", "--name-only",
		"--diff-filter=A", base+"...HEAD").Output()
	if err != nil {
		t.Skipf("состав добавленного относительно %s не прочитан: %v", base, err)
	}

	timestamped := regexp.MustCompile(`^(\d{14})_`)
	legacy := regexp.MustCompile(`^(\d{6})_`)

	added, findings := 0, []string(nil)
	for _, rel := range strings.Split(string(out), "\n") {
		rel = strings.TrimSpace(rel)
		if !strings.HasSuffix(rel, ".sql") || !strings.Contains(rel, "/migrations/") {
			continue
		}
		added++
		name := filepath.Base(rel)
		if timestamped.MatchString(name) {
			continue
		}
		m := legacy.FindStringSubmatch(name)
		if m == nil {
			findings = append(findings, rel+" — номер не разобран: ждали метку времени YYYYMMDDHHMMSS")
			continue
		}
		v, _ := strconv.ParseInt(m[1], 10, 64)
		findings = append(findings, rel+" — номер "+strconv.FormatInt(v, 10)+
			" выведен из номера задачи, а он МЕНЬШЕ уже применённых: мигратор такую версию "+
			"не применит и уронит старт. Новая миграция именуется меткой времени заведения "+
			"(YYYYMMDDHHMMSS) — она больше любого унаследованного номера by construction")
	}

	t.Logf("добавлено относительно %s миграций: %d; из них не по метке времени: %d",
		base, added, len(findings))
	for _, f := range findings {
		t.Error(f)
	}
}

// resolveTrunkRef — ссылка на ствол, если она есть в этом клоне.
func resolveTrunkRef(t *testing.T, root string) string {
	t.Helper()
	for _, ref := range []string{"origin/main", "main"} {
		if err := exec.Command("git", "-C", root, "rev-parse", "--verify", "--quiet", ref).Run(); err == nil {
			return ref
		}
	}
	return ""
}
