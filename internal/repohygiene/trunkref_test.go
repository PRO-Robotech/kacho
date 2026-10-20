// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/gitenv"
)

// trunkRefNames — имена, которыми в этом дереве называется ствол, когда базу
// изменения никто не назвал. Порядок значим: `origin/main` — то, что видит
// конвейер, `main` — то, что есть у человека.
var trunkRefNames = []string{"origin/main", "main"}

// Источники НАЗВАННОЙ базы изменения, сильнейший первым.
//
// `GITHUB_BASE_REF` выставляет провайдер конвейера на событии запроса — имя
// ветки, в которую запрос вливается; ссылка на неё в клоне с полной историей —
// `origin/<имя>`. Вне запроса переменная есть, но пуста, и пустое — не имя.
//
// `KACHO_TRUNK_REF` — ручка, которой базу называют хук отправки
// (`scripts/hooks/pre-push`, `scripts/hooks/prepush-range.sh`) и выбор
// затронутого (`.github/scripts/affected.py`); гейт — её третий читатель, и
// значение берётся как есть, тем же способом, что у них.
const (
	trunkRefPRBaseEnv = "GITHUB_BASE_REF"
	trunkRefKnobEnv   = "KACHO_TRUNK_REF"
)

// trunkRefChoice — исход выбора ствола: ссылка и то, откуда она взята, либо
// отказ. Пустые ссылка и отказ вместе значат «ни одно имя по умолчанию не
// разрешилось» — его перепись печатает [requireTrunkRef], потому что ей нужен клон.
type trunkRefChoice struct {
	ref     string
	how     string
	refusal string
}

// chooseTrunkRef — выбор ствола без `*testing.T`: окружение и разрешимость
// ссылки подаются снаружи, поэтому выбор поддаётся инъекции.
//
// # Предмет (#3323): ствол — база ЗАПРОСА, а не всегда `origin/main`
//
// Гейт порядка миграций исключает из «старших применённых» всё добавленное
// относительно ствола. При стволе `origin/main` и запросе в ветку эпика миграция,
// уже влитая в эпик предыдущей волной и накатанная его выкаткой, считается
// «добавленной» и с новой не сравнивается: новая с номером НИЖЕ неё проходит, а
// мигратор стенда эпика её не применит. Поэтому порядок: база запроса, затем
// ручка, затем имена ствола по умолчанию.
//
// # Названная, но неразрешимая база — отказ, а не откат к стволу
//
// Тихий откат к `origin/main` вернул бы ровно эту дыру и выглядел бы зелёным.
// Тот, кто назвал базу, ждёт суда относительно неё; не разрешилась — гейт не
// выполнился, и это называется словами.
func chooseTrunkRef(getenv func(string) string, resolves func(string) bool) trunkRefChoice {
	named := []struct {
		env, value, ref string
	}{
		{env: trunkRefPRBaseEnv, value: strings.TrimSpace(getenv(trunkRefPRBaseEnv))},
		{env: trunkRefKnobEnv, value: strings.TrimSpace(getenv(trunkRefKnobEnv))},
	}
	named[0].ref = "origin/" + named[0].value
	named[1].ref = named[1].value

	for _, n := range named {
		if n.value == "" {
			continue
		}
		how := fmt.Sprintf("база названа %s=%s", n.env, n.value)
		if resolves(n.ref) {
			return trunkRefChoice{ref: n.ref, how: how}
		}
		return trunkRefChoice{refusal: fmt.Sprintf("не выполнилось: %s, но ссылка %s в "+
			"клоне не разрешается. Это отказ ПРЕДПОСЫЛКИ, а не проход: откат к %s "+
			"молча судил бы относительно другой базы — миграция ниже уже влитой в базу "+
			"запроса прошла бы (#3323). Починка на стороне клона: дотянуть ветку базы "+
			"(`git fetch --no-tags origin +refs/heads/<база>:refs/remotes/origin/<база>`) "+
			"либо снять переменную, если база названа ошибочно",
			how, n.ref, strings.Join(trunkRefNames, ", "))}
	}

	for _, ref := range trunkRefNames {
		if resolves(ref) {
			return trunkRefChoice{ref: ref, how: "ствол по умолчанию: база изменения не названа"}
		}
	}
	return trunkRefChoice{}
}

// requireTrunkRef — ссылка на ствол, относительно которой считается «добавленное
// ЭТИМ изменением». Выбор — [chooseTrunkRef]: база запроса, ручка, ствол по
// умолчанию; названная неразрешимая база — отказ.
//
// # Предмет: «ствол не разрешается» — отказ ПРЕДПОСЫЛКИ, а не отсутствие предмета
//
// Здесь у двух гейтов стоял `t.Skip`, и цена измерена (#1211): единственная джоба
// конвейера с `go test` чекаутится мелко (`actions/checkout` без `fetch-depth`),
// поэтому ни `origin/main`, ни `main` в её клоне не резолвятся — ни на
// `pull_request`, ни на `push`. Прогон идёт без `-v`, строка `SKIP` в вывод не
// попадает вовсе. Итог: гейт, машинно держащий ban #1 («кодировать без APPROVED
// запрещено»), присутствовал, был провязан, отчитывался зелёным — и не отработал
// в конвейере НИ РАЗУ за свою жизнь.
//
// Пропуск здесь неотличим от успеха, а «ноль находок» — от «ноль прочитанного».
// Поэтому исход один: ОТКАЗ, называющий и причину, и починку.
//
// # Два случая разведены, и это не педантизм
//
// «Предмета нет by construction» и «предпосылка не создана» выглядят одинаково —
// сравнивать не с чем, — но означают разное и лечатся разным:
//
//   - корень НЕ является рабочим деревом git (распакованный архив, каталог без
//     `.git`): «добавленного этим изменением» не существует как понятия, спросить
//     не у кого. Это граница — пропуск, но ГОВОРЯЩИЙ, и его причина обязана быть
//     объявлена в ведомости `.github/scripts/gate-skips-allowed.txt`, иначе
//     прогонщик юнитов сочтёт её находкой;
//   - рабочее дерево git есть, а ствола в нём нет: это НАСТРОЙКА клона, и чинится
//     она глубиной checkout, а не терпимостью гейта. Отказ.
func requireTrunkRef(t *testing.T, root string) string {
	t.Helper()

	if err := gitenv.Command(root, "rev-parse", "--is-inside-work-tree").Run(); err != nil {
		t.Skip("корень не является рабочим деревом git — «добавленного этим " +
			"изменением» не существует как понятия; это граница проверки, а не её " +
			"зелёный исход (причина объявлена в .github/scripts/gate-skips-allowed.txt)")
	}

	choice := chooseTrunkRef(os.Getenv, func(ref string) bool {
		return gitenv.Command(root, "rev-parse", "--verify", "--quiet", ref).Run() == nil
	})
	if choice.refusal != "" {
		t.Fatalf("%s", choice.refusal)
	}
	if choice.ref != "" {
		t.Logf("ствол гейта: %s — %s", choice.ref, choice.how)
		return choice.ref
	}

	// Перепись осмотренного: «ноль подходящих ссылок» обязано быть отличимо от
	// «ноль ссылок вообще» — второе означает, что сломан сам клон, а не его глубина.
	var refs []string
	if out, err := gitenv.Command(root, "for-each-ref", "--format=%(refname)").Output(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line != "" {
				refs = append(refs, line)
			}
		}
	}
	sample := refs
	if len(sample) > 8 {
		sample = sample[:8]
	}

	t.Fatalf("ствол не разрешается: ни одна из ссылок %s не резолвится. "+
		"Осмотрено ссылок клона: %d (первые: %s). Это отказ ПРЕДПОСЫЛКИ, а не "+
		"отсутствие предмета: проба судит ДОБАВЛЕННОЕ этим изменением, и без ствола "+
		"сравнивать не с чем — пропуск здесь читался бы как зелёный исход (именно так "+
		"гейт приёмок, машинно державший ban #1, не отказал в конвейере ни разу, #1211). "+
		"Починка на стороне клона: джоба, гоняющая пробы дерева, обязана чекаутиться "+
		"полной глубиной (actions/checkout `fetch-depth: 0`) либо дотягивать ствол "+
		"отдельным шагом (`git fetch --no-tags origin "+
		"+refs/heads/main:refs/remotes/origin/main`)",
		strings.Join(trunkRefNames, ", "), len(refs), strings.Join(sample, " "))
	return ""
}
