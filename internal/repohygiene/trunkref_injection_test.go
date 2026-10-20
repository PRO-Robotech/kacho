// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PRO-Robotech/corelib/gitenv"
)

// trunkref_injection_test.go — держатель одного свойства [chooseTrunkRef]: СТВОЛ
// ГЕЙТА — БАЗА ЗАПРОСА, А НЕ ВСЕГДА `origin/main` (#3323).
//
// Почему это несущее. Гейт порядка миграций исключает из «старших применённых»
// всё ДОБАВЛЕННОЕ относительно ствола. Когда ствол — `origin/main`, а запрос идёт
// в ветку эпика, миграция, уже влитая в эпик предыдущей волной (и накатанная его
// выкаткой), считается «добавленной» и с новой не сравнивается. Новая миграция с
// номером НИЖЕ неё проходит гейт, а мигратор стенда эпика её не применит: «found
// N missing migrations before current version».
//
// Проба прогоняет ровно тот путь, что гейт, — выбор ствола и
// [judgeAddedMigrationVersions] — на синтетическом репозитории, где ствол, эпик и
// полосы заведены коммитами, а ссылки `origin/*` — ссылками удалённой линии, как
// в клоне конвейера с полной историей.

// migrationOrderDir — каталог миграций синтетического репозитория. Форма пути
// та же, что у живых каталогов: разбор узнаёт миграцию по `/migrations/`.
const migrationOrderDir = "services/vpc/internal/migrations"

// migrationOrderRepo — синтетический репозиторий и вершины, на которых судит
// проба. Локальных веток `main` и `epic` в нём НЕТ намеренно: иначе выбор,
// потерявший префикс `origin/`, разрешился бы локальной веткой и смолчал бы.
type migrationOrderRepo struct {
	root string
	// laneBelow — полоса от эпика с номером НИЖЕ уже влитой в эпик миграции.
	laneBelow string
	// laneAbove — законный близнец: та же полоса, номер ВЫШЕ влитой.
	laneAbove string
	// waveTwo — две миграции одной невлитой волны; младшая добавлена второй.
	waveTwo string
}

func newMigrationOrderRepo(t *testing.T) migrationOrderRepo {
	t.Helper()
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		out, err := gitenv.Command(root, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s — условие пробы не создано, и её исход ничего "+
				"не значил бы", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	commitMigration := func(name string) string {
		t.Helper()
		full := filepath.Join(root, migrationOrderDir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("каталог миграций: %v", err)
		}
		if err := os.WriteFile(full, []byte("-- проба\nSELECT 1;\n"), 0o600); err != nil {
			t.Fatalf("миграция %s: %v", name, err)
		}
		git("add", "-A")
		git("commit", "-q", "-m", name)
		return git("rev-parse", "HEAD")
	}

	git("init", "-q", "-b", "work", ".")
	git("config", "user.email", "probe@example.invalid")
	git("config", "user.name", "probe")

	// Ствол: одна применённая миграция.
	trunk := commitMigration("20260101000000_trunk.sql")
	git("update-ref", "refs/remotes/origin/main", trunk)

	// Эпик: миграция предыдущей волны уже влита в ветку эпика.
	epic := commitMigration("20261020120000_epic_landed.sql")
	git("update-ref", "refs/remotes/origin/epic", epic)

	git("checkout", "-q", "--detach", epic)
	laneBelow := commitMigration("20261020110000_lane_below.sql")

	git("checkout", "-q", "--detach", epic)
	laneAbove := commitMigration("20261020130000_lane_above.sql")

	git("checkout", "-q", "--detach", epic)
	commitMigration("20261020150000_wave_a.sql")
	waveTwo := commitMigration("20261020140000_wave_b.sql")

	// Предпосылки: ссылки линии есть, одноимённых локальных — нет.
	for _, ref := range []string{"origin/main", "origin/epic"} {
		if gitenv.Command(root, "rev-parse", "--verify", "--quiet", ref).Run() != nil {
			t.Fatalf("ссылка %s в синтетическом репозитории не разрешается — проба "+
				"судила бы не свой предмет", ref)
		}
	}
	for _, ref := range []string{"refs/heads/main", "refs/heads/epic"} {
		if gitenv.Command(root, "rev-parse", "--verify", "--quiet", ref).Run() == nil {
			t.Fatalf("локальная ветка %s есть — выбор без префикса `origin/` "+
				"разрешился бы ею, и мутант смолчал бы", ref)
		}
	}
	return migrationOrderRepo{root: root, laneBelow: laneBelow, laneAbove: laneAbove, waveTwo: waveTwo}
}

// TestMigrationOrderGateJudgesAgainstTheMergeRequestBase — миграция ниже уже
// влитой в базу запроса краснеет; законные близнецы молчат; названная, но
// неразрешимая база — отказ «не выполнилось», а не молчаливый откат к стволу.
func TestMigrationOrderGateJudgesAgainstTheMergeRequestBase(t *testing.T) {
	t.Parallel()
	repo := newMigrationOrderRepo(t)
	resolves := func(ref string) bool {
		return gitenv.Command(repo.root, "rev-parse", "--verify", "--quiet", ref).Run() == nil
	}

	cases := []struct {
		name    string
		head    string
		env     map[string]string
		wantRef string
		// want — подстроки находки; пусто — ждём молчания.
		want []string
		// compared — сколько добавленных сравнено со старшей применённой: молчание
		// без сравнения ничего не доказывало бы.
		compared int
		// refusal — подстрока отказа; непусто — ждём отказа, а не вердикта.
		refusal []string
		why     string
	}{
		{
			name:    "ниже влитой в базу запроса — находка",
			head:    repo.laneBelow,
			env:     map[string]string{trunkRefPRBaseEnv: "epic"},
			wantRef: "origin/epic",
			want:    []string{"20261020110000_lane_below.sql", "20261020120000_epic_landed.sql"},
			why: "предмет #3323: относительно `origin/main` влитая в эпик миграция считается " +
				"добавленной и выпадает из сравнения — новая ниже неё проходит",
		},
		{
			name:     "та же полоса без названной базы — ствол по умолчанию",
			head:     repo.laneBelow,
			env:      map[string]string{},
			wantRef:  "origin/main",
			compared: 2,
			why: "без названной базы гейт сравнивает со стволом, как и прежде: человеку без " +
				"окружения конвейера и хука поведение не меняется",
		},
		{
			name:    "ручка хука KACHO_TRUNK_REF",
			head:    repo.laneBelow,
			env:     map[string]string{trunkRefKnobEnv: "origin/epic"},
			wantRef: "origin/epic",
			want:    []string{"20261020110000_lane_below.sql", "20261020120000_epic_landed.sql"},
			why:     "ручку уже читают хук отправки и выбор затронутого; гейт — третий читатель",
		},
		{
			name:    "база запроса сильнее ручки",
			head:    repo.laneBelow,
			env:     map[string]string{trunkRefPRBaseEnv: "epic", trunkRefKnobEnv: "origin/main"},
			wantRef: "origin/epic",
			want:    []string{"20261020110000_lane_below.sql"},
			why:     "на событии запроса базу знает провайдер; ручка — для отправки, где запроса нет",
		},
		{
			name:    "пустая база запроса (событие push) не считается названной",
			head:    repo.laneBelow,
			env:     map[string]string{trunkRefPRBaseEnv: "", trunkRefKnobEnv: "origin/epic"},
			wantRef: "origin/epic",
			want:    []string{"20261020110000_lane_below.sql"},
			why:     "провайдер выставляет переменную и вне запроса, пустой; пустое — не имя",
		},
		{
			name:     "законный близнец: номер выше влитой",
			head:     repo.laneAbove,
			env:      map[string]string{trunkRefPRBaseEnv: "epic"},
			wantRef:  "origin/epic",
			compared: 1,
			why:      "меняется ровно один факт против первого случая — номер полосы",
		},
		{
			name:     "две миграции одной невлитой волны",
			head:     repo.waveTwo,
			env:      map[string]string{trunkRefPRBaseEnv: "epic"},
			wantRef:  "origin/epic",
			compared: 2,
			why: "волна вливается одним запросом, мигратор накатит обе по возрастанию: " +
				"младшая, добавленная второй, — не дефект, пока обе не влиты",
		},
		{
			name:    "названная неразрешимая база запроса — отказ, а не ствол",
			head:    repo.laneBelow,
			env:     map[string]string{trunkRefPRBaseEnv: "нет-такой"},
			refusal: []string{"не выполнилось", "origin/нет-такой", trunkRefPRBaseEnv},
			why:     "молчаливый откат к `origin/main` вернул бы ровно ту дыру, ради которой заведено",
		},
		{
			name:    "названная неразрешимая ручка — отказ, а не ствол",
			head:    repo.laneBelow,
			env:     map[string]string{trunkRefKnobEnv: "refs/heads/нет-такой"},
			refusal: []string{"не выполнилось", "refs/heads/нет-такой", trunkRefKnobEnv},
			why:     "тот, кто назвал базу, ждёт суда относительно неё, а не относительно ствола",
		},
	}

	// Подслучаи идут ПОСЛЕДОВАТЕЛЬНО: каждый переключает HEAD и индекс одного
	// синтетического репозитория.
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if out, err := gitenv.Command(repo.root, "checkout", "-q", "--detach", tc.head).CombinedOutput(); err != nil {
				t.Fatalf("переключение на %s: %v\n%s — условие случая не создано", tc.head, err, out)
			}
			choice := chooseTrunkRef(func(k string) string { return tc.env[k] }, resolves)

			if len(tc.refusal) > 0 {
				if choice.refusal == "" {
					t.Fatalf("выбор ствола не отказал, а взял %q (%s) — %s", choice.ref, choice.how, tc.why)
				}
				for _, w := range tc.refusal {
					if !strings.Contains(choice.refusal, w) {
						t.Errorf("отказ не называет %q: %s", w, choice.refusal)
					}
				}
				return
			}
			if choice.refusal != "" {
				t.Fatalf("выбор ствола отказал там, где база разрешима: %s", choice.refusal)
			}
			if choice.ref != tc.wantRef {
				t.Errorf("ствол гейта %q (%s), ждали %q — %s", choice.ref, choice.how, tc.wantRef, tc.why)
			}

			findings, census, indexed, err := judgeAddedMigrationVersions(repo.root, choice.ref)
			if err != nil {
				t.Fatalf("гейт на синтетическом репозитории: %v", err)
			}
			if indexed == 0 {
				t.Fatalf("индекс пуст — проба ничего не прочитала")
			}
			if len(tc.want) == 0 {
				if len(findings) != 0 {
					t.Errorf("гейт нашёл на законном случае то, чего в нём нет (%s): %s\nперепись: %s",
						tc.why, strings.Join(findings, "\n  "), census)
				}
				if census.Compared != tc.compared {
					t.Errorf("сравнено с каталогом %d, ждали %d — молчание без сравнения не "+
						"доказывает ничего; перепись: %s", census.Compared, tc.compared, census)
				}
				return
			}
			if len(findings) == 0 {
				t.Fatalf("гейт смолчал на миграции ниже влитой в базу (относительно %s) — %s\n"+
					"перепись: %s", choice.ref, tc.why, census)
			}
			text := strings.Join(findings, "\n")
			for _, w := range tc.want {
				if !strings.Contains(text, w) {
					t.Errorf("находка не называет %q: %s", w, text)
				}
			}
		})
	}
}
