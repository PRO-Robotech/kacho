// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"strings"
	"testing"
)

// migrationmonotonic_injection_test.go — способность гейта монотонности упасть и
// СМОЛЧАТЬ, по каждой оси отдельно и с законным близнецом.
//
// Инъекция подменяет ВХОД разбора (состав добавленного и состав индекса), а не
// сам разбор: разбор, обращающийся к `*testing.T`, инъекции не поддаётся, и его
// «зелёное» ничего не доказывало бы.

func TestMigrationMonotonic_CanFailAndStaysSilent(t *testing.T) {
	// Индекс каталога: унаследованные номера по задаче плюс одна метка времени.
	// Ширина унаследованного берётся ЖИВАЯ — четыре цифры: их в дереве 169 против
	// двадцати шестизначных, и распознаватель обязан знать обе формы.
	tracked := []string{
		"services/iam/internal/migrations/0001_initial.sql",
		"services/iam/internal/migrations/0302_last_legacy.sql",
		"services/iam/internal/migrations/20260904135459_role_owner_module.sql",
		"services/vpc/internal/migrations/20260828130000_vpc_outbox.sql",
	}

	cases := []struct {
		name    string
		added   []string
		tracked []string
		want    string
		why     string
	}{
		{
			name:    "законный близнец: метка выше старшей применённой",
			added:   []string{"services/iam/internal/migrations/20260905090000_next.sql"},
			tracked: tracked,
			why: "положительный контроль: без него всякое красное ниже могло бы приходить " +
				"от самого разбора, а не от предмета",
		},
		{
			name:    "метка НИЖЕ старшей применённой того же каталога",
			added:   []string{"services/iam/internal/migrations/20260904102932_next.sql"},
			tracked: tracked,
			want:    "20260904135459",
			why: "ровно предмет #1895: часы дали номер ниже уже лежащего, накат пойдёт не в том " +
				"порядке, в каком миграции заводились, — а форма имени при этом безупречна",
		},
		{
			name:    "находка называет ОБА имени",
			added:   []string{"services/iam/internal/migrations/20260904102932_next.sql"},
			tracked: tracked,
			want:    "20260904102932_next.sql",
			why:     "находка, называющая только старшую, посылает читателя искать свою глазами",
		},
		{
			name:    "метка РАВНА старшей — тоже находка",
			added:   []string{"services/iam/internal/migrations/20260904135459_twin.sql"},
			tracked: tracked,
			want:    "строго больше",
			why: "равенство не есть «больше»: две версии с одним номером мигратор различит " +
				"порядком, которого никто не выбирал",
		},
		{
			name:    "соседний каталог не участвует",
			added:   []string{"services/vpc/internal/migrations/20260829000000_next.sql"},
			tracked: tracked,
			why: "версии нумеруются ПО КАТАЛОГУ: метка ниже чужой старшей о своём каталоге " +
				"не говорит ничего, и краснеть на ней значило бы связывать несвязанные линии",
		},
		{
			name:    "унаследованный номер по задаче у нового файла",
			added:   []string{"services/iam/internal/migrations/0303_next.sql"},
			tracked: tracked,
			want:    "выведен из номера задачи",
			why:     "форма имени — прежний предмет гейта, и он обязан остаться живым",
		},
		{
			name:    "номер не разобран вовсе",
			added:   []string{"services/iam/internal/migrations/next_thing.sql"},
			tracked: tracked,
			want:    "номер не разобран",
			why:     "имя без номера — тот же класс, только без версии, которую можно сравнить",
		},
		{
			name:    "добавлено не в каталог миграций",
			added:   []string{"services/iam/internal/repo/kaname/pg/role_repo.go"},
			tracked: tracked,
			why:     "предмет гейта — миграции; краснеть на чужом файле значило бы судить не своё",
		},
		{
			name:    "первый файл каталога: старшей применённой нет",
			added:   []string{"services/geo/internal/migrations/20260905090000_first.sql"},
			tracked: tracked,
			why: "каталог без унаследованных версий — законное состояние нового домена, " +
				"и сравнивать там не с чем by construction",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings, census := auditAddedMigrationVersions(tc.added, tc.tracked)
			if tc.want == "" {
				if len(findings) != 0 {
					t.Fatalf("разбор нашёл на законном близнеце то, чего в нём нет — первое же "+
						"ложное срабатывание снимает гейт.\nперепись: %s\nнаходки:\n  %s",
						census, strings.Join(findings, "\n  "))
				}
				return
			}
			if len(findings) == 0 {
				t.Fatalf("разбор смолчал на инъекции — он НЕ способен упасть по этой оси.\n"+
					"что должно было ловиться: %s\nперепись: %s", tc.why, census)
			}
			if !strings.Contains(strings.Join(findings, "\n"), tc.want) {
				t.Fatalf("разбор покраснел не на том: ждали %q\nнаходки:\n  %s",
					tc.want, strings.Join(findings, "\n  "))
			}
		})
	}
}

// TestMigrationMonotonicCensus_TellsBothNumbers — перепись печатает ОБЕ величины.
//
// Одно число («добавлено N») скрывает ровно тот случай, ради которого гейт
// заведён: добавленное есть, а сравнивать его не с чем, потому что состав
// индекса не прочитан. «Ноль находок» обязано быть отличимо от «ноль
// прочитанного».
func TestMigrationMonotonicCensus_TellsBothNumbers(t *testing.T) {
	added := []string{"services/iam/internal/migrations/20260905090000_next.sql"}
	tracked := []string{"services/iam/internal/migrations/20260904135459_prev.sql"}

	_, census := auditAddedMigrationVersions(added, tracked)
	text := census.String()
	for _, want := range []string{"добавлено", "старшая применённая", "20260904135459"} {
		if !strings.Contains(text, want) {
			t.Fatalf("перепись не называет %q: %s", want, text)
		}
	}

	_, empty := auditAddedMigrationVersions(nil, tracked)
	if !strings.Contains(empty.String(), "добавлено миграций 0") {
		t.Fatalf("перепись на пустом добавленном не называет нуля: %s", empty)
	}
}
