// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Инъекции разбора ScanJournalFaultMappers: каждая законная форма решения о
// 23514 (ветка `case pgfault.Check`, предикат локальный и экспортируемый)
// без охраны — находка; та же форма с охраной раньше решения — молчание;
// охрана ПОСЛЕ решения — находка; определение предиката — не предмет.

const jfHeader = "package pg\n\nimport (\n\t\"github.com/PRO-Robotech/corelib/db/pgfault\"\n\t\"github.com/PRO-Robotech/kacho/pkg/journalfault\"\n)\n\n"

func scanJF(t *testing.T, body string) []JournalFaultMapper {
	t.Helper()
	got, census, err := ScanJournalFaultMappers("x.go", []byte(jfHeader+body))
	require.NoError(t, err)
	require.Positive(t, census.Funcs, "перепись функций обязана быть ненулевой")
	return got
}

func TestJournalFaultGateFindsAnUnguardedCheckDecision(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"case": `func m(err error) error {
	f := pgfault.Classify(err)
	switch f.Class {
	case pgfault.Unique, pgfault.Check:
		return err
	}
	return nil
}`,
		"local predicate": `func m(err error) error {
	if isCheckViolation(err) {
		return err
	}
	return nil
}`,
		"exported predicate": `func m(err error) error {
	if helpers.IsCheckViolation(err) {
		return err
	}
	return nil
}`,
		"guard after decision": `func m(err error) error {
	f := pgfault.Classify(err)
	switch f.Class {
	case pgfault.Check:
		return err
	}
	if journalfault.Report(f) {
		return nil
	}
	return nil
}`,
	} {
		t.Run(name, func(t *testing.T) {
			got := scanJF(t, body)
			require.Len(t, got, 1, "решение о 23514 обязано быть найдено")
			assert.False(t, got[0].Guarded, "охраны раньше решения нет — находка")
			assert.Equal(t, "m", got[0].Func)
			assert.Positive(t, got[0].Line)
		})
	}
}

func TestJournalFaultGateIsSilentOnTheGuardedTwin(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"case": `func m(err error) error {
	f := pgfault.Classify(err)
	if journalfault.Report(f, "kind", "x") {
		return nil
	}
	switch f.Class {
	case pgfault.Check:
		return err
	}
	return nil
}`,
		"predicate": `func m(err error) error {
	if journalfault.Initiator(pgfault.Classify(err)) {
		return nil
	}
	if isCheckViolation(err) {
		return err
	}
	return nil
}`,
	} {
		t.Run(name, func(t *testing.T) {
			got := scanJF(t, body)
			require.Len(t, got, 1)
			assert.True(t, got[0].Guarded, "охрана раньше решения — молчание")
		})
	}
}

func TestJournalFaultGateIgnoresThePredicateDefinitionAndOtherClasses(t *testing.T) {
	t.Parallel()
	got := scanJF(t, `func isCheckViolation(err error) bool { return pgfault.Classify(err).Is(pgfault.Check) }
func IsCheckViolation(err error) bool { return isCheckViolation(err) }
func u(err error) error {
	switch pgfault.Classify(err).Class {
	case pgfault.Unique:
		return err
	}
	return nil
}`)
	assert.Empty(t, got, "определение предиката и решение о других классах — не предмет")
}

func TestJournalInitiatorConstraintScanSkipsCommentsAndReadsTheName(t *testing.T) {
	t.Parallel()
	sql := `-- ALTER TABLE x.fake_outbox ADD COLUMN initiator text CONSTRAINT wrong_name
-- +goose Up
ALTER TABLE kacho_vpc.vpc_outbox
    ADD COLUMN initiator text NOT NULL
        DEFAULT NULLIF(current_setting('kacho_journal.initiator', true), '')
        CONSTRAINT vpc_outbox_initiator_form
            CHECK (initiator ~ '^(user:usr-?[0-9a-z]{1,17})$');
-- +goose Down
ALTER TABLE kacho_vpc.vpc_outbox DROP COLUMN IF EXISTS initiator;
`
	got := ScanJournalInitiatorConstraints(sql)
	require.Len(t, got, 1, "объявление в комментарии — не предмет; снятие колонки — не объявление")
	assert.Equal(t, JournalInitiatorConstraint{Table: "vpc_outbox", Constraint: "vpc_outbox_initiator_form"}, got[0])

	misnamed := ScanJournalInitiatorConstraints(`ALTER TABLE public.compute_outbox ADD COLUMN initiator text NOT NULL CONSTRAINT compute_initiator_chk CHECK (true);`)
	require.Len(t, misnamed, 1)
	assert.NotEqual(t, misnamed[0].Table+"_initiator_form", misnamed[0].Constraint,
		"ограничение не по форме имени обязано читаться как есть — гейт дерева назовёт его находкой")
}
