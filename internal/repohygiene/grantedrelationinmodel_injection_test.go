// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Доказательство способности гейта упасть — ИНЪЕКЦИЕЙ, в ОБЕ стороны.
//
// Гейт `TestEveryRelationGrantedByMigrationsExistsInTheAppliedModel` зелёный на
// сегодняшнем дереве, и это ничего не доказывает: зелёным он был бы и с
// предикатом, который ничего не ищет. Поэтому его извлечение прогоняется здесь на
// синтетике, где ответ известен заранее:
//
//	(а) миграция выдаёт отношение, которого в модели НЕТ  → находка, с координатой;
//	(б) ЗАКОННЫЙ БЛИЗНЕЦ той же формы (отношение в модели есть) → молчание.
//
// Без (б) гейт ловил бы форму записи, а не существо, и первый же законный кортеж
// сделал бы его ложно-красным — после чего его снимут как мешающий.
func TestGrantedRelationGate_InjectionBothWays(t *testing.T) {
	t.Parallel()

	// Модель-фикстура: у типа `cluster` объявлено `quota_reader`, у `group` —
	// `member`. Отношения `phantom_reader` нет ни у кого.
	const model = `
model
  schema 1.1

type user

type group
    relations
        define member: [user, service_account]

type cluster
    relations
        define system_admin: [user, service_account]
        define quota_reader: [service_account, group#member] or system_admin
`

	rels := relationsByType(model)
	require.Len(t, rels, 3, "фикстура модели обязана разбираться, иначе проба ничего не проверяет")
	require.True(t, rels["cluster"]["quota_reader"], "предпосылка фикстуры")

	cases := []struct {
		name        string
		sql         string
		wantFinding bool
		wantPair    string
	}{
		{
			// (б) ЗАКОННЫЙ БЛИЗНЕЦ — ровно та же форма записи, отношение существует.
			name: "законная выдача существующего отношения — гейт молчит",
			sql: `INSERT INTO kaname.fga_outbox (event_type, payload, created_at) VALUES
  ('fga.tuple.write',
   jsonb_build_object(
     'user',     'group:grp1#member',
     'relation', 'quota_reader',
     'object',   'cluster:cluster_kacho_root'),
   now());`,
			wantFinding: false,
			wantPair:    "cluster#quota_reader",
		},
		{
			// (а) ВОЗВРАЩЁННЫЙ ДЕФЕКТ — отношения в модели нет.
			name: "выдача отношения, которого в модели нет — находка",
			sql: `INSERT INTO kaname.fga_outbox (event_type, payload, created_at) VALUES
  ('fga.tuple.write',
   jsonb_build_object(
     'user',     'group:grp1#member',
     'relation', 'phantom_reader',
     'object',   'cluster:cluster_kacho_root'),
   now());`,
			wantFinding: true,
			wantPair:    "cluster#phantom_reader",
		},
		{
			// Тип целиком неизвестен — тоже находка, и с ДРУГИМ текстом.
			name: "выдача на типе, которого в модели нет — находка",
			sql: `INSERT INTO kaname.fga_outbox (event_type, payload, created_at) VALUES
  ('fga.tuple.write',
   jsonb_build_object(
     'user',     'user:u1',
     'relation', 'member',
     'object',   'phantom_type:x'),
   now());`,
			wantFinding: true,
			wantPair:    "phantom_type#member",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "0999_injection.sql")
			require.NoError(t, os.WriteFile(path, []byte(tc.sql), 0o600))

			grants, read, blocks := grantsFromMigrations(t, []string{path})

			// Извлечение обязано СРАБОТАТЬ в обоих случаях — иначе «нет находки»
			// означало бы «ничего не прочитано», а не «всё в порядке».
			require.Equal(t, 1, read, "фикстура обязана быть прочитана как миграция очереди")
			require.Equal(t, 1, blocks, "блок кортежа обязан быть распознан")
			require.Len(t, grants, 1, "пара «тип+отношение» обязана быть извлечена")
			require.Equal(t, tc.wantPair, grants[0].objectType+"#"+grants[0].relation)

			g := grants[0]
			typeRels, typeKnown := rels[g.objectType]
			found := !typeKnown || !typeRels[g.relation]

			require.Equal(t, tc.wantFinding, found,
				"инъекция: ожидали находка=%v для пары %s", tc.wantFinding, tc.wantPair)

			if tc.wantFinding {
				// Находка обязана НАЗЫВАТЬ КООРДИНАТУ, иначе по ней нельзя действовать.
				require.Contains(t, g.where, "0999_injection.sql",
					"находка обязана нести путь файла, в котором она найдена")
			}
		})
	}
}

// Проверка предпосылки самого разбора: строки готовой JSON-формы модели, лежащей
// в том же файле, НЕ должны читаться как объявления типа или отношения. Если бы
// читались, гейт «находил» бы отношения там, где их нет, и молчал бы на настоящем
// расхождении.
func TestGrantedRelationGate_JSONFormIsNotMistakenForDSL(t *testing.T) {
	t.Parallel()

	const jsonish = `
data:
  model.json: |
    {"type_definitions":[{"type":"cluster","relations":{"quota_reader":{}}}]}
`
	require.Empty(t, relationsByType(jsonish),
		"JSON-форма модели не объявляет типов для этого разбора: иначе предикат читал бы "+
			"два источника сразу и не смог бы отличить объявленное от упомянутого")
}
