// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// credentialfieldsource_injection_test.go — ДОКАЗАТЕЛЬСТВО, ЧТО ГЕЙТ BAT-1-68
// СПОСОБЕН УПАСТЬ И СПОСОБЕН СМОЛЧАТЬ.
//
// # ЧЕТЫРЕ ОСИ ПРОГОНЯЮТСЯ НА НАСТОЯЩЕМ ДЕРЕВЕ, А НЕ НА СИНТЕТИКЕ
//
// Инъекция, собравшая себе корпус из строковых литералов, доказывает свойство
// СВОЕЙ копии кода: разойдясь с обходом дерева, она осталась бы зелёной, а
// исполнялся бы оригинал. Поэтому дефект вносится в НАСТОЯЩИЙ файл дерева —
// В ПАМЯТИ, через подмену тела на пути обхода (`readCredentialTreeOverriding`).
// Дерево на диске не трогается ни на байт, а осматривается ровно то, что
// осматривает гейт.
//
// Подмена, не нашедшая своего файла, роняет инъекцию: иначе она прогналась бы на
// нетронутом дереве и объявила бы гейт способным упасть, ничего не доказав.
//
// # ОСИ РАЗВЕДЕНЫ — одна проба «на всё» зеленела бы на трёх сломанных из семи
//
//	СОБСТВЕННЫЙ ИСТОЧНИК   снятие комментария у читаемого поля — НАХОДКА с координатой
//	ЗАКОННЫЙ БЛИЗНЕЦ       то же поле нетронутым — МОЛЧАНИЕ
//	ГРУППОВАЯ ФОРМА        комментарий на три поля покрывает все три; сузишь — двое станут находкой
//	НЕЧИТАЕМОЕ ПОЛЕ        поле без комментария, которого никто не читает, — МОЛЧАНИЕ
//	ПРЕДЕЛ ПО ИМЕНИ        полоса, названная иначе, ПРОПАДАЕТ из переписи — замерено, а не объявлено
//	ОБОБЩЁННЫЙ НОСИТЕЛЬ    `T[K, V]` в результате — тот же носитель, что `T`
//	САМОИСТЕЧЕНИЕ          запись ведомости, которой нечего прощать, — находка (ОБЕ формы)
//
// Каждая ось портит СВОЁ условие и только его: ось, оставшаяся зелёной при
// снятии того, что она стережёт, не держит ничего.

// credentialLaneFile / credentialLaneSrc — файл настоящей полосы и его тело.
const credentialLaneFile = "gateway/internal/middleware/basic_credential_lane.go"

func realCredentialLaneSource(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRoot(t), filepath.FromSlash(credentialLaneFile)))
	if err != nil {
		t.Fatalf("чтение настоящей полосы %s: %v", credentialLaneFile, err)
	}
	return string(body)
}

// judgeTree — обход настоящего дерева с подменой и суждение ТЕМ ЖЕ кодом,
// которым судит гейт.
func judgeTree(t *testing.T, overrides map[string]string, ledger []openCredentialFieldFinding) (
	lanes []credentialLane, carriers []credentialCarrier, findings, stale []string, read, sourced int) {
	t.Helper()
	facts, scanned := readCredentialTreeOverriding(t, overrides)
	if scanned == 0 {
		t.Fatal("обход прочитал ноль файлов — инъекция беспредметна")
	}
	lanes = collectCredentialLanes(facts)
	carriers = collectCredentialCarriers(facts, lanes)
	markCredentialFieldReaders(facts, carriers)
	findings, stale, read, sourced, _ = credentialFieldVerdict(carriers, ledger)
	return lanes, carriers, findings, stale, read, sourced
}

func findingsMentioning(findings []string, id string) []string {
	var out []string
	for _, f := range findings {
		if strings.HasPrefix(f, id+" ") {
			out = append(out, f)
		}
	}
	return out
}

// TestBAT1_68_InjectionOwnCommentRemovedIsAFinding — ОСЬ «собственный источник».
func TestBAT1_68_InjectionOwnCommentRemovedIsAFinding(t *testing.T) {
	src := realCredentialLaneSource(t)
	const own = "\t// CredentialID — идентификатор удостоверения; им адресуется отзыв.\n"
	if !strings.Contains(src, own) {
		t.Fatalf("в настоящей полосе нет комментария, который инъекция снимает, — "+
			"фикстура ПОТЕРЯЛА ПРЕДМЕТ и доказывает не то. Ищу: %q", own)
	}

	_, _, findings, _, read, _ := judgeTree(t,
		map[string]string{credentialLaneFile: strings.Replace(src, own, "", 1)},
		nil)

	if read == 0 {
		t.Fatal("читаемых полей ноль — суждение отработало на пустом множестве")
	}
	hit := findingsMentioning(findings, "BasicVerifiedCredential.CredentialID")
	if len(hit) != 1 {
		t.Fatalf("находок по BasicVerifiedCredential.CredentialID = %d, ожидалась 1.\nвсе находки: %v",
			len(hit), findings)
	}
	if !strings.Contains(hit[0], credentialLaneFile+":") {
		t.Errorf("находка не называет КООРДИНАТУ объявления: %q", hit[0])
	}
	if !strings.Contains(hit[0], "полоса BasicCredentialLane") {
		t.Errorf("находка не называет ПОЛОСУ: %q", hit[0])
	}
}

// TestBAT1_68_InjectionUntouchedTreeIsSilent — ОСЬ «законный близнец».
//
// Без неё отрицание выше зеленело бы на гейте, который краснеет всегда.
func TestBAT1_68_InjectionUntouchedTreeIsSilent(t *testing.T) {
	_, _, findings, stale, read, sourced := judgeTree(t, nil, openCredentialFieldFindings)
	if read == 0 || sourced == 0 {
		t.Fatalf("перепись пуста (читаемых %d, с источником %d) — суждение ослепло", read, sourced)
	}
	if len(findings) != 0 {
		t.Fatalf("гейт краснеет на НЕТРОНУТОМ дереве: %v", findings)
	}
	if len(stale) != 0 {
		t.Fatalf("записи ведомости потеряли предмет на нетронутом дереве: %v", stale)
	}
}

// TestBAT1_68_InjectionGroupCommentCoversTheFieldsItNames — ОСЬ «групповая форма».
//
// Первая редакция предиката читала только собственный комментарий поля и
// объявляла находкой два поля, у которых источник назван групповым
// комментарием. Здесь обе стороны: групповая форма ЗАСЧИТЫВАЕТСЯ, а суженный до
// одного имени комментарий перестаёт покрывать остальные два.
func TestBAT1_68_InjectionGroupCommentCoversTheFieldsItNames(t *testing.T) {
	src := realCredentialLaneSource(t)
	const group = "// PrincipalType / PrincipalID / DisplayName — из ответа авторитета."
	if !strings.Contains(src, group) {
		t.Fatalf("групповой формы в настоящей полосе нет — фикстура потеряла предмет: %q", group)
	}

	t.Run("названные групповым комментарием — МОЛЧАНИЕ", func(t *testing.T) {
		_, _, findings, _, _, _ := judgeTree(t, nil, nil)
		for _, id := range []string{"BasicVerifiedCredential.PrincipalID", "BasicVerifiedCredential.DisplayName"} {
			if hit := findingsMentioning(findings, id); len(hit) != 0 {
				t.Errorf("групповой источник не засчитан для %s: %v", id, hit)
			}
		}
	})

	t.Run("комментарий сужен до одного имени — двое становятся НАХОДКОЙ", func(t *testing.T) {
		_, _, findings, _, _, _ := judgeTree(t,
			map[string]string{credentialLaneFile: strings.Replace(src, group,
				"// PrincipalType — из ответа авторитета.", 1)},
			nil)
		for _, id := range []string{"BasicVerifiedCredential.PrincipalID", "BasicVerifiedCredential.DisplayName"} {
			if hit := findingsMentioning(findings, id); len(hit) != 1 {
				t.Errorf("%s не стало находкой при суженном комментарии: %v", id, findings)
			}
		}
		if hit := findingsMentioning(findings, "BasicVerifiedCredential.PrincipalType"); len(hit) != 0 {
			t.Errorf("названное поле стало находкой: %v", hit)
		}
	})
}

// TestBAT1_68_InjectionUnreadFieldNeedsNoSource — ОСЬ «нечитаемое поле».
//
// Законный близнец из НАСТОЯЩЕГО дерева: поле подписанного предъявителя, у
// которого нет ни комментария, ни читателя. Требовать источника у величины,
// которую никто не спрашивает, значило бы завести находку без предмета.
//
// Проба самоистекает: появится читатель — она покраснеет и потребует назвать
// источник, что и есть правильный исход.
func TestBAT1_68_InjectionUnreadFieldNeedsNoSource(t *testing.T) {
	_, carriers, findings, _, _, _ := judgeTree(t, nil, openCredentialFieldFindings)

	var found bool
	for _, c := range carriers {
		if c.typ != "VerifiedToken" {
			continue
		}
		for _, f := range c.fields {
			if f.name != "NotBefore" {
				continue
			}
			found = true
			if f.sourceNamed {
				t.Fatalf("VerifiedToken.NotBefore обзавёлся источником — фикстура потеряла " +
					"предмет: близнец должен быть БЕЗ источника, иначе он доказывает не то")
			}
			if f.read {
				t.Fatalf("VerifiedToken.NotBefore обзавёлся читателем — назовите его источник; " +
					"эта проба самоистекла, и это правильный исход")
			}
		}
	}
	if !found {
		t.Fatal("VerifiedToken.NotBefore в переписи не найден — близнеца нет, ось беспредметна")
	}
	if hit := findingsMentioning(findings, "VerifiedToken.NotBefore"); len(hit) != 0 {
		t.Errorf("нечитаемое поле объявлено находкой: %v", hit)
	}
}

// TestBAT1_68_InjectionMeasuresTheLimitOfTheNamePredicate — ОСЬ «предел по имени».
//
// Предел гейта назван в его шапке; здесь он ЗАМЕРЕН. Полоса, чей метод назван
// иначе, пропадает из переписи целиком — вместе со своим носителем и всеми его
// полями. Проба существует не чтобы предел одобрить, а чтобы он был числом, а не
// обещанием: следующий, кто заведёт полосу под другим именем, увидит здесь, чем
// это кончается.
func TestBAT1_68_InjectionMeasuresTheLimitOfTheNamePredicate(t *testing.T) {
	src := realCredentialLaneSource(t)
	const decl = "func (l *BasicCredentialLane) Verify("
	if !strings.Contains(src, decl) {
		t.Fatalf("объявления полосы в настоящем файле нет — фикстура потеряла предмет: %q", decl)
	}

	base, baseCarriers, _, _, _, _ := judgeTree(t, nil, openCredentialFieldFindings)
	renamed, renamedCarriers, _, _, _, _ := judgeTree(t,
		map[string]string{credentialLaneFile: strings.Replace(src, decl,
			"func (l *BasicCredentialLane) Attest(", 1)},
		openCredentialFieldFindings)

	if len(renamed) != len(base)-1 {
		t.Fatalf("полос до %d, после переименования %d — предикат по имени НЕ сужает, "+
			"и предел, названный в шапке гейта, описан неверно", len(base), len(renamed))
	}
	if len(renamedCarriers) != len(baseCarriers)-1 {
		t.Fatalf("носителей до %d, после %d — носитель переименованной полосы остался в переписи",
			len(baseCarriers), len(renamedCarriers))
	}
	for _, c := range renamedCarriers {
		if c.typ == "BasicVerifiedCredential" {
			t.Fatal("носитель переименованной полосы всё ещё судится — замер предела неверен")
		}
	}
}

// synthCredentialFacts — корпус, собранный здесь, ТЕМ ЖЕ разбором, каким гейт
// читает дерево. Второй реализации разбора нет by construction.
func synthCredentialFacts(t *testing.T, files map[string]string) []goFileFacts {
	t.Helper()
	fset := token.NewFileSet()
	var out []goFileFacts
	for rel, src := range files {
		file, err := parser.ParseFile(fset, rel, src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("разбор синтетики %s: %v", rel, err)
		}
		out = append(out, goFileFacts{
			rel: rel, dir: path.Dir(rel), pkgName: file.Name.Name,
			file: file, fset: fset, imports: map[string]bool{},
		})
	}
	return out
}

func judgeSynth(t *testing.T, files map[string]string, ledger []openCredentialFieldFinding) (
	carriers []credentialCarrier, findings, stale []string, read int) {
	t.Helper()
	facts := synthCredentialFacts(t, files)
	lanes := collectCredentialLanes(facts)
	carriers = collectCredentialCarriers(facts, lanes)
	markCredentialFieldReaders(facts, carriers)
	findings, stale, read, _, _ = credentialFieldVerdict(carriers, ledger)
	return carriers, findings, stale, read
}

// TestBAT1_68_InjectionGenericCarrierIsSeen — ОСЬ «обобщённый носитель».
//
// Соседний гейт этого пакета не читал обобщённый получатель, и слепой зоной
// оказался ровно тот носитель, ради которого его трогали; перепись при этом
// печатала правдоподобное число. Здесь обобщённая форма прогоняется рядом с
// простой: обе обязаны судиться одинаково.
func TestBAT1_68_InjectionGenericCarrierIsSeen(t *testing.T) {
	const laneSrc = `package lane

import "context"

type Envelope[T any] struct {
	Subject string
	payload T
}

type Lane[T any] struct{}

func (l *Lane[T]) Verify(ctx context.Context, presented string) (*Envelope[T], error) {
	_ = ctx
	_ = presented
	return nil, nil
}
`
	const readerSrc = `package control

func read(e struct{ Subject string }) string { return e.Subject }
`
	carriers, findings, _, read := judgeSynth(t, map[string]string{
		"gateway/internal/lane/lane.go":       laneSrc,
		"gateway/internal/control/control.go": readerSrc,
	}, nil)

	if len(carriers) != 1 || carriers[0].typ != "Envelope" {
		t.Fatalf("обобщённый носитель не распознан: %+v", carriers)
	}
	if read == 0 {
		t.Fatal("читаемых полей ноль — суждение отработало на пустом множестве")
	}
	if len(findingsMentioning(findings, "Envelope.Subject")) != 1 {
		t.Fatalf("поле обобщённого носителя без источника не стало находкой: %v", findings)
	}
}

// TestBAT1_68_InjectionLedgerExpiresOnItsOwn — ОСЬ «самоистечение», ОБЕ формы.
func TestBAT1_68_InjectionLedgerExpiresOnItsOwn(t *testing.T) {
	const bare = `package lane

import "context"

type Cred struct {
	Subject string
}

type Lane struct{}

func (l *Lane) Verify(ctx context.Context, presented string) (Cred, error) {
	_ = ctx
	_ = presented
	return Cred{}, nil
}
`
	const sourced = `package lane

import "context"

type Cred struct {
	// Subject — из ответа авторитета.
	Subject string
}

type Lane struct{}

func (l *Lane) Verify(ctx context.Context, presented string) (Cred, error) {
	_ = ctx
	_ = presented
	return Cred{}, nil
}
`
	const readerSrc = `package control

func read(c struct{ Subject string }) string { return c.Subject }
`
	ledger := []openCredentialFieldFinding{{carrier: "Cred", field: "Subject", owner: "#0, синтетика"}}

	t.Run("предмет есть — запись прощает, находок нет", func(t *testing.T) {
		_, findings, stale, read := judgeSynth(t, map[string]string{
			"gateway/internal/lane/lane.go":       bare,
			"gateway/internal/control/control.go": readerSrc,
		}, ledger)
		if read == 0 {
			t.Fatal("читаемых полей ноль — ось беспредметна")
		}
		if len(findings) != 0 || len(stale) != 0 {
			t.Fatalf("запись с живым предметом дала находки %v / просроченные %v", findings, stale)
		}
	})

	t.Run("источник назван — запись ПОТЕРЯЛА предмет", func(t *testing.T) {
		_, findings, stale, _ := judgeSynth(t, map[string]string{
			"gateway/internal/lane/lane.go":       sourced,
			"gateway/internal/control/control.go": readerSrc,
		}, ledger)
		if len(findings) != 0 {
			t.Fatalf("находки при названном источнике: %v", findings)
		}
		if len(stale) != 1 || !strings.HasPrefix(stale[0], "Cred.Subject") {
			t.Fatalf("просроченная запись не названа: %v", stale)
		}
	})

	t.Run("поле снято — запись ПОТЕРЯЛА предмет", func(t *testing.T) {
		_, _, stale, _ := judgeSynth(t, map[string]string{
			"gateway/internal/control/control.go": readerSrc,
		}, ledger)
		if len(stale) != 1 || !strings.HasPrefix(stale[0], "Cred.Subject") {
			t.Fatalf("просроченная запись не названа при снятом поле: %v", stale)
		}
	})
}

// TestBAT1_68_InjectionBlindWalkIsNotHealth — ОСЬ «ноль — это слепота».
//
// Суждение на пустом корпусе обязано давать ноль ЧИТАЕМЫХ полей, а не ноль
// находок при непустой переписи: иначе гейт, чей обход ослеп, читался бы как
// гейт над исправным деревом. Ветвь `read == 0` самого гейта на этом и стоит.
func TestBAT1_68_InjectionBlindWalkIsNotHealth(t *testing.T) {
	findings, stale, read, sourced, total := credentialFieldVerdict(nil, nil)
	if len(findings) != 0 || len(stale) != 0 {
		t.Fatalf("пустой корпус дал находки %v / просроченные %v", findings, stale)
	}
	if read != 0 || sourced != 0 || total != 0 {
		t.Fatalf("перепись на пустом корпусе непуста: полей %d, читаемых %d, с источником %d",
			total, read, sourced)
	}
}
