// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// quotaabsentauthority_injection_test.go — доказательство, что гейт объявления
// домена величин СПОСОБЕН упасть, и падает на существе, а не на форме.
//
// Гейт, чья способность краснеть не доказана, неотличим от гейта, который ничего
// не проверяет: оба молчат на чистом дереве. Здесь по каждой оси — пара: внесён
// ОДИН факт, и рядом законный близнец, отличающийся ровно им. Без второй половины
// гейт ловил бы «в файле упомянуто слово», и первый же ложный срабат его отключил
// бы.
//
// Инъекция зовёт ТУ ЖЕ функцию разбора, что и гейт (`quotaAbsentAuthorityProblems`),
// а не свою копию: копия разошлась бы с оригиналом молча и доказывала бы себя саму.
package repohygiene

import (
	"strings"
	"testing"
)

// goodBody — тело, отвечающее норме: читает объявление, несёт его в предикате
// списания и прикрывает им вызов производителя отказа.
const goodBody = `
CREATE OR REPLACE FUNCTION s.kacho_quota_count()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    v_absent boolean;
BEGIN
    SELECT authority_state = 'not-deployed' INTO v_absent
      FROM s.quota_sync_cursor
     WHERE id = 'limits';
    UPDATE s.project_resource_quotas
       SET used = used + 1, updated_at = now()
     WHERE carrier_type = 'project' AND carrier_id = v_project AND kind = v_kind
       AND (COALESCE(v_absent, false) OR used < limit_value);
    IF FOUND THEN
        RETURN NULL;
    END IF;
    IF COALESCE(v_absent, false) THEN
        RETURN NULL;
    END IF;
    PERFORM s.kacho_quota_refuse('project', v_project, v_kind);
    RETURN NULL;
END;
$$;
`

func TestQuotaAbsentAuthorityGate_FailsOnEachRemovedGuardAndIsSilentOnItsLegalTwin(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string // подстрока ожидаемой находки; пусто — находок быть не должно
	}{
		{
			name: "контроль: тело отвечает норме",
			body: goodBody,
			want: "",
		},
		{
			name: "снято чтение объявления — остальное на месте",
			body: strings.Replace(goodBody,
				"      FROM s.quota_sync_cursor\n", "      FROM s.some_other_table\n", 1),
			want: "не читает объявление домена величин",
		},
		{
			name: "снято объявленное отсутствие из предиката списания",
			body: strings.Replace(goodBody,
				"       AND (COALESCE(v_absent, false) OR used < limit_value);",
				"       AND used < limit_value;", 1),
			want: "списывающий оператор не несёт объявленного отсутствия",
		},
		{
			name: "снята охрана перед вызовом производителя отказа",
			body: strings.Replace(goodBody,
				"    IF COALESCE(v_absent, false) THEN\n        RETURN NULL;\n    END IF;\n",
				"", 1),
			want: "вызов производителя отказа не прикрыт",
		},
		{
			name: "охрана записана другой законной формой условия — молчим",
			body: strings.Replace(goodBody,
				"    IF COALESCE(v_absent, false) THEN\n        RETURN NULL;\n    END IF;\n"+
					"    PERFORM s.kacho_quota_refuse('project', v_project, v_kind);",
				"    IF NOT COALESCE(v_absent, false) THEN\n"+
					"        PERFORM s.kacho_quota_refuse('project', v_project, v_kind);\n"+
					"    END IF;", 1),
			want: "",
		},
		{
			name: "производителя отказа не зовут вовсе — охранять нечего",
			body: strings.Replace(goodBody,
				"    PERFORM s.kacho_quota_refuse('project', v_project, v_kind);\n", "", 1),
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Тело подаётся через тот же нормализатор, что и на живом дереве:
			// иначе доказательство говорило бы о другом входе, чем гейт.
			up := upBlock("-- +goose Up\n" + tc.body + "\n-- +goose Down\n")
			problems := quotaAbsentAuthorityProblems(up)
			joined := strings.Join(problems, "; ")
			switch {
			case tc.want == "" && len(problems) != 0:
				t.Fatalf("законное тело объявлено находкой: %s", joined)
			case tc.want != "" && !strings.Contains(joined, tc.want):
				t.Fatalf("внесённый дефект не назван: ожидалось %q, получено %q",
					tc.want, joined)
			}
		})
	}
}

// TestQuotaAbsentAuthorityGate_CommentIsNotTheExecutablePart — слова охраны,
// стоящие в ПРОЗЕ, гейт зачитывать не вправе.
//
// Класс прямой: обе миграции этого предмета объясняют охрану словами, и предикат
// по сырому тексту нашёл бы её в собственном объяснении, оставшись зелёным на
// снятой защите (`testing.md` §«Гейт на класс», п. 4).
func TestQuotaAbsentAuthorityGate_CommentIsNotTheExecutablePart(t *testing.T) {
	stripped := strings.Replace(goodBody,
		"    IF COALESCE(v_absent, false) THEN\n        RETURN NULL;\n    END IF;\n",
		"    -- Здесь стояла охрана IF COALESCE(v_absent, false) THEN RETURN NULL; END IF;\n"+
			"    -- Она читает quota_sync_cursor.authority_state и снимает предикат потолка.\n",
		1)
	up := upBlock("-- +goose Up\n" + stripped + "\n-- +goose Down\n")
	problems := quotaAbsentAuthorityProblems(up)
	if len(problems) == 0 {
		t.Fatal("охрана, пересказанная комментарием, зачтена как исполняемая: " +
			"гейт судил бы собственное объяснение")
	}
}

// TestQuotaAbsentAuthorityGate_DownBlockIsNotJudged — блок отката не судится.
//
// В нём стоит ПРЕЖНЕЕ тело, не знающее объявления, и это его смысл. Судя откат,
// гейт краснел бы на КАЖДОЙ верной миграции этого предмета.
func TestQuotaAbsentAuthorityGate_DownBlockIsNotJudged(t *testing.T) {
	legacy := strings.Replace(goodBody,
		"       AND (COALESCE(v_absent, false) OR used < limit_value);",
		"       AND used < limit_value;", 1)
	up := upBlock("-- +goose Up\n" + goodBody + "\n-- +goose Down\n" + legacy + "\n")
	if problems := quotaAbsentAuthorityProblems(up); len(problems) != 0 {
		t.Fatalf("тело отката зачтено предметом гейта: %s", strings.Join(problems, "; "))
	}
}

// TestQuotaAbsentAuthorityGate_ExemptionListIsNotEmptyAndSaysWhy — освобождение
// несёт причину, а не только имя.
//
// Запись без причины следующий читатель снимет как непонятную либо унаследует не
// разобравшись; самоистечение освобождения проверяет сам гейт на живом дереве.
func TestQuotaAbsentAuthorityGate_ExemptionListIsNotEmptyAndSaysWhy(t *testing.T) {
	if len(quotaAuthorityAwareExempt) == 0 {
		t.Fatal("перечень освобождённых пуст: у гейта нет ни одного объявленного различия, " +
			"хотя владелец величин отличается от потребителя by construction")
	}
	for svc, why := range quotaAuthorityAwareExempt {
		if strings.TrimSpace(why) == "" {
			t.Errorf("освобождение %q не называет причины", svc)
		}
	}
}
