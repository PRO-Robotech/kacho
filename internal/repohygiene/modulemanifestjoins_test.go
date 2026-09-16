// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// modulemanifestjoins_test.go — манифест домена ВСТУПАЕТ только в группу,
// которую объявляет манифест службы доступа (задача PRO-Robotech/kacho#2687;
// приёмка PRO-Robotech/kaname
// `docs/engineering/acceptance/service-manifest-seeds-its-own-groups.md`,
// стадия S2а, сценарий MRW-15).
//
// # Предмет
//
// Вступление (`seed.joins[]`) объявляет назначение: «эта личность модуля
// состоит в этой группе, ПОТОМУ ЧТО группа что-то открывает». Группа, чья
// выдача отозвана (`module-quota-readers`: `quota_reader` снят миграцией службы
// `20260914091500`, kaname#59), пуста и не открывает ничего — вступление в неё
// объявляет назначение, которого нет. Хуже того, порядок стадий несущий (Р6):
// служба снимет строку группы миграцией (S2б) ТОЛЬКО после того, как платформа
// перестанет в неё вступать, — иначе применитель посева службы, встретив
// вступление в снятую группу, откажет, и служба не поднимется. Машинной
// проверки у этого порядка нет — миграция службы каталога доставки не читает;
// держит его этот гейт: пока он зелен, платформа не вступает никуда, кроме
// группы, которую служба объявляет и держит.
//
// # Почему ЗАКРЫТЫЙ набор, а не запрет по имени
//
// Отрицание «вступления в `module-quota-readers` нет» замолчало бы в тот прогон,
// когда группы не станет (S2б): образец не совпал бы больше никогда при целом
// обходе, и «ноль находок» стало бы свойством пустого предмета (`testing.md`
// §«Гейт на класс», п. 9). Поэтому судится ПОЛОЖИТЕЛЬНО и закрыто: каждый
// манифест с разделом `seed` вступает РОВНО В ОДНУ группу, и это та, которую
// объявляет манифест службы (Р1). Любая другая — находка с именем файла и
// группы. Заводя новое вступление, автор обязан назвать здесь группу, которую
// служба объявила, — вопрос «объявлена ли она у владельца» тогда задаётся при
// заведении, а не в чужой отладке.
//
// # Чего гейт НЕ судит
//
// Написание аккаунта группы (`kacho-system` против `system`) — предмет окна
// написаний применителя службы (kaname, `domain.SeedIdentityWindow`), и второй
// судья написания разошёлся бы с первым молча. Здесь сравнивается ИМЯ группы.
package repohygiene

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// serviceDeclaredJoinGroup — единственная группа, в которую платформенный
// манифест вправе вступать: её объявляет и держит манифест службы доступа
// (`PRO-Robotech/kaname:manifest.yaml`, раздел `seed.groups`, приёмка MRW-1 Р1).
//
// Литерал здесь — ВТОРОЕ место об одном имени, и это названо: первое лежит в
// другом репозитории и импортом недостижимо. Расхождение ловится не молча:
// переименуй служба группу — её применитель откажет на первом же вступлении
// платформы («группа не резолвится»), а этот гейт покраснеет на первом же
// манифесте, вступившем в новое имя.
const serviceDeclaredJoinGroup = "module-relation-writers"

// manifestJoin — одно вступление, как его читает этот гейт.
type manifestJoin struct {
	ServiceAccount string
	GroupAccount   string
	GroupName      string
}

// manifestJoinsFile — прочитанный манифест: координата, модуль, есть ли раздел
// `seed` вовсе и его вступления.
type manifestJoinsFile struct {
	Rel     string
	Module  string
	HasSeed bool
	Joins   []manifestJoin
}

// manifestSeedJoins — то немногое, что гейт читает из манифеста. Строгости
// `KnownFields` нет намеренно: форму судит загрузчик службы, а не этот гейт.
type manifestSeedJoins struct {
	Module string `yaml:"module"`
	Seed   *struct {
		Joins []struct {
			ServiceAccount struct {
				Name string `yaml:"name"`
			} `yaml:"serviceAccount"`
			Group struct {
				Account string `yaml:"account"`
				Name    string `yaml:"name"`
			} `yaml:"group"`
		} `yaml:"joins"`
	} `yaml:"seed"`
}

// readManifestJoins — вступления всех манифестов дерева и объём прочитанного.
func readManifestJoins(t *testing.T, tt *trackedTree) []manifestJoinsFile {
	t.Helper()
	rels := make([]string, 0, tt.count())
	for rel := range tt.files {
		if filepath.Base(rel) == manifestBaseName {
			rels = append(rels, rel)
		}
	}
	sort.Strings(rels)

	out := make([]manifestJoinsFile, 0, len(rels))
	for _, rel := range rels {
		body, err := os.ReadFile(filepath.Join(tt.root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("манифест %s не прочитан: %v — «ноль находок» означало бы "+
				"«ноль прочитанного»", rel, err)
		}
		var m manifestSeedJoins
		if err := yaml.Unmarshal(body, &m); err != nil {
			t.Errorf("манифест %s не разбирается (%v) — этот гейт о нём не утверждает ничего", rel, err)
			continue
		}
		f := manifestJoinsFile{Rel: rel, Module: m.Module, HasSeed: m.Seed != nil}
		if m.Seed != nil {
			for _, j := range m.Seed.Joins {
				f.Joins = append(f.Joins, manifestJoin{
					ServiceAccount: j.ServiceAccount.Name,
					GroupAccount:   j.Group.Account,
					GroupName:      j.Group.Name,
				})
			}
		}
		out = append(out, f)
	}
	return out
}

// manifestJoinFault — находка с координатой.
type manifestJoinFault struct {
	Rel    string
	Detail string
}

// findManifestJoinFaults — закрытый набор вступлений по каждому манифесту с
// разделом `seed`: ровно одно, и оно в группу, объявленную службой.
func findManifestJoinFaults(files []manifestJoinsFile) []manifestJoinFault {
	var faults []manifestJoinFault
	for _, f := range files {
		if !f.HasSeed {
			continue
		}
		declared := 0
		for _, j := range f.Joins {
			if j.GroupName == serviceDeclaredJoinGroup {
				declared++
				continue
			}
			faults = append(faults, manifestJoinFault{Rel: f.Rel, Detail: fmt.Sprintf(
				"модуль %q вступает в группу %s/%s, которую манифест службы доступа не объявляет: "+
					"вступление объявляет назначение, которого нет, а служба не снимет свою строку "+
					"миграцией, пока платформа в неё вступает (MRW-1 Р6). Остаться вправе только "+
					"вступление в %q — либо назови здесь группу, которую служба объявила",
				f.Module, j.GroupAccount, j.GroupName, serviceDeclaredJoinGroup)})
		}
		if declared != 1 {
			faults = append(faults, manifestJoinFault{Rel: f.Rel, Detail: fmt.Sprintf(
				"модуль %q вступает в %q %d раз(а), а членство ради которого группа существует — "+
					"одно на модуль (MRW-15: «в module-relation-writers — пять, по одному на модуль»)",
				f.Module, serviceDeclaredJoinGroup, declared)})
		}
	}
	return faults
}

// TestModuleManifestJoinsOnlyTheGroupTheServiceDeclares — сам гейт (MRW-15).
func TestModuleManifestJoinsOnlyTheGroupTheServiceDeclares(t *testing.T) {
	t.Parallel()
	files := readManifestJoins(t, newTrackedTree(t, repoRoot(t)))

	withSeed, joins := 0, 0
	var seeded []string
	for _, f := range files {
		if f.HasSeed {
			withSeed++
			joins += len(f.Joins)
			seeded = append(seeded, f.Module)
		}
	}
	t.Logf("перепись: манифестов %d · с разделом seed %d (%s) · вступлений %d",
		len(files), withSeed, strings.Join(seeded, ", "), joins)
	if withSeed == 0 {
		t.Fatalf("манифестов с разделом seed прочитано НОЛЬ (всего манифестов %d) — обход пуст, "+
			"«находок ноль» было бы свойством обхода, а не дерева", len(files))
	}

	for _, f := range findManifestJoinFaults(files) {
		t.Errorf("%s: %s", f.Rel, f.Detail)
	}
}
