// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: AGPL-3.0-or-later

// Инъекция держателя «страница не отрицает механизм, который дерево несёт» — В
// ОБЕ СТОРОНЫ И ПО КАЖДОЙ ОСИ.
//
// Осей у держателя две, и они отвечают на РАЗНЫЕ вопросы, поэтому доказываются
// порознь:
//
//  1. РАСПОЗНАВАТЕЛЬ СТРАНИЦЫ. Отрицание существования — находка; условная
//     фраза о ненастроенном узле — молчание. Второе и есть тот текст, которым
//     страницы правятся: без него «покраснело» не отличалось бы от «краснеет на
//     любом упоминании письма».
//  2. ЗАМЕР ЖИВОСТИ ПОЛОСЫ. Дерево с производителями — три из трёх; дерево без
//     них — ноль (тогда отрицание на странице ЗАКОННО); производители, названные
//     только в КОММЕНТАРИИ, — ноль, потому что комментарий ничего не производит.
//
// Дельта каждого мира против его положительного близнеца — ОДИН факт: либо
// формулировка страницы, либо наличие маркера в исполняемой части.
package supplyhygiene

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// pageDenies — страница в том виде, в каком она лгала до #2525.
const pageDenies = `## Приглашение

` + "`Operation.metadata`" + ` вернёт ` + "`magicLinkUrl`" + ` — ссылку первого входа, которую админ передаёт
приглашённому вручную (автоотправка email не интегрирована).
`

// pageTellsTheTruth — ЗАКОННЫЙ БЛИЗНЕЦ: тот же предмет, названный фактически.
// Отличается от мира выше ОДНИМ фактом — формулировкой, — и обязан молчать.
const pageTellsTheTruth = `## Приглашение

` + "`Operation.metadata`" + ` вернёт ` + "`magicLinkUrl`" + ` — ссылку первого входа. Автоотправка письма
существует: пока почтовый узел не объявлен ключами ` + "`inviteMail.*`" + `, письмо не уходит и
ссылку передаёт админ.
`

// pageDeniesInEnglish — та же находка на английском: предикат на одном языке
// недобирает МОЛЧА, поэтому словарь двуязычен и это доказывается.
const pageDeniesInEnglish = `## Invite

The admin hands the link over manually: automatic email is not implemented.
`

func TestInviteMailPageInjection_DenialIsAFinding(t *testing.T) {
	claims, census := findAbsenceClaims(map[string][]byte{"p.mdx": []byte(pageDenies)})
	require.Equal(t, 1, census.Claims, "отрицание существования обязано быть находкой")
	require.Len(t, claims, 1)
	require.Equal(t, "p.mdx", claims[0].Page)
	require.Equal(t, 4, claims[0].Line, "находка обязана называть строку")
}

func TestInviteMailPageInjection_TruthfulWordingIsSilent(t *testing.T) {
	claims, census := findAbsenceClaims(map[string][]byte{"p.mdx": []byte(pageTellsTheTruth)})
	require.NotZerof(t, census.Subjects, "предмет обязан быть ОСМОТРЕН и на законном "+
		"близнеце — иначе молчание означает «не читали», а не «нарушения нет»")
	require.Zero(t, census.Claims, "условная фраза о ненастроенном узле — законна")
	require.Empty(t, claims)
}

func TestInviteMailPageInjection_EnglishDenialIsAFinding(t *testing.T) {
	_, census := findAbsenceClaims(map[string][]byte{"p.mdx": []byte(pageDeniesInEnglish)})
	require.Equal(t, 1, census.Claims, "английская форма отрицания обязана находиться")
}

func TestInviteMailPageInjection_EmptyCorpusIsNotClean(t *testing.T) {
	_, census := findAbsenceClaims(map[string][]byte{})
	require.Zero(t, census.Pages)
	require.Zerof(t, census.Subjects, "пустой корпус обязан давать НОЛЬ осмотренного — "+
		"именно на это число держатель и роняет прогон")
}

// writeGo — один исходник синтетического дерева.
func writeGo(t *testing.T, dir, name, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
}

func TestInviteMailLaneProducers_LiveTreeIsFound(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "outbox.go", "package a\n\nconst t = \"kaname.invite_mail_outbox\"\n")
	writeGo(t, dir, "wiring.go", "package a\n\nvar m = \"kaname invite mail drainer starting\"\n")
	writeGo(t, dir, "metrics.go", "package a\n\nconst r = \"kaname_invite_mail_outcomes_total\"\n")

	found, filesRead, err := inviteMailLaneProducers(dir)
	require.NoError(t, err)
	require.Equal(t, 3, filesRead)
	require.Len(t, found, 3, "живая полоса обязана дать все три производителя")
}

func TestInviteMailLaneProducers_TreeWithoutTheLaneIsEmpty(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "other.go", "package a\n\nconst t = \"kaname.some_other_outbox\"\n")

	found, filesRead, err := inviteMailLaneProducers(dir)
	require.NoError(t, err)
	require.Equal(t, 1, filesRead, "обход обязан состояться — иначе «полосы нет» "+
		"неотличимо от «не искали»")
	require.Emptyf(t, found, "дерево без полосы обязано давать ноль производителей: "+
		"именно в этом мире отрицание на странице ЗАКОННО")
}

func TestInviteMailLaneProducers_CommentIsNotAProducer(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "prose.go", "package a\n\n"+
		"// Полоса пишет в kaname.invite_mail_outbox и считает\n"+
		"// kaname_invite_mail_outcomes_total, а процесс печатает\n"+
		"// kaname invite mail drainer starting.\n")

	found, filesRead, err := inviteMailLaneProducers(dir)
	require.NoError(t, err)
	require.Equal(t, 1, filesRead)
	require.Emptyf(t, found, "комментарий ничего не производит: держатель, читающий "+
		"прозу, признал бы производителем собственное объяснение")
}

func TestInviteMailLaneProducers_TestFileIsNotAProducer(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "x_test.go", "package a\n\nconst t = \"kaname.invite_mail_outbox\"\n")

	found, filesRead, err := inviteMailLaneProducers(dir)
	require.NoError(t, err)
	require.Zerof(t, filesRead, "проба не производит полосу: фикстура, назвавшая очередь, "+
		"выдала бы снятую полосу за живую")
	require.Empty(t, found)
}
