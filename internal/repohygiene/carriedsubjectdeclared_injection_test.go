// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

// carriedsubjectdeclared_injection_test.go — доказательство того, что держатель
// судьбы предмета УМЕЕТ краснеть и УМЕЕТ молчать.
//
// # Одно-фактность
//
// Каждый отрицательный мир отличается от положительного близнеца РОВНО ОДНИМ
// названным фактом, иначе неизвестно, что дало красное. Положительный близнец
// один на все миры — [injCarriedControl]: связное надгробие из двух семей, где
// одна уехала целиком, а другая исчезла целиком.
//
//	контроль             → одна семья уехала, вторая исчезла, третья без стража → молчание
//	судьба не объявлена   → …тот же мир, но у гейта снято поле судьбы       → находка с его именем
//	судьба вне словаря    → …тот же мир, но судьба названа чужим словом     → находка со словарём
//	«нет» + координата    → …тот же мир, но у исчезнувшего явился преемник  → находка
//	«уехало» без координаты → …тот же мир, но у уехавшего координата снята   → находка
//	чужой репозиторий     → …тот же мир, но преемник не объявлен деревом     → находка
//	форма координаты      → …тот же мир, но координата абсолютна             → находка
//	расхождение в семье   → …тот же мир, но инъекция объявила исчезновение   → находка с именем семьи
//	остаток без репозитория → …тот же мир, но остаток не сказал, где предмет жив → находка
//	остаток с держателем  → …тот же мир, но остаток назвал координату стража  → находка
//	висячая координата    → …тот же мир + дерево-преемник БЕЗ этого файла    → находка
//	координата на месте   → …тот же мир + дерево-преемник С этим файлом      → молчание
//
// # Проба собственной предпосылки
//
// Разбор стоит на двух предпосылках, и обе проверяются здесь же, а не
// принимаются на веру: семья опознаётся одинаково у гейта, его годка, его
// инъекции и его граничной пробы ([TestCarriedSubject_FamilyOfACarrierIsItsRoot]);
// словарь репозиториев-преемников берётся у владельца имён и НЕ пуст
// ([TestCarriedSubject_SuccessorVocabularyComesFromTheOwner]). Пустой словарь
// обессмыслил бы половину миров ниже, и молчание гейта тогда доказывало бы
// свойство пустого входа.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/PRO-Robotech/kacho/internal/productnaming"
)

// injDeclaredRepo — репозиторий-преемник, ОБЪЯВЛЕННЫЙ деревом.
//
// Берётся у владельца имён, а не выписывается литералом: второе написание
// разошлось бы с деревом молча, и тогда миры ниже судили бы выдуманный
// репозиторий вместо настоящего.
func injDeclaredRepo(t *testing.T) string {
	t.Helper()
	repos := injRepos(t)
	names := make([]string, 0, len(repos))
	for r := range repos {
		names = append(names, r)
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatalf("словарь репозиториев-преемников пуст — миры инъекции судили бы пустой вход; " +
			"это отказ, а не пустой успех: владелец объявления — productnaming.ExternallySourcedServices")
	}
	return names[0]
}

// injRepos — словарь репозиториев-преемников ЭТОГО дерева. Отказ вместо пустого
// словаря: миры ниже судили бы пустой вход.
func injRepos(t *testing.T) map[string]bool {
	t.Helper()
	repos, err := successorRepos(repoRoot(t))
	if err != nil {
		t.Fatalf("собрать словарь репозиториев-преемников: %v", err)
	}
	return repos
}

// injCarriedPath — координата держателя в дереве-преемнике.
const injCarriedPath = "internal/check/synthetic_subject.go"

// injCarriedControl — положительный близнец: связное надгробие.
//
// Семья `alpha` уехала целиком (годок, гейт, инъекция), семья `beta` исчезла
// целиком. Именно над этим миром делаются все отрицательные: каждый меняет в нём
// один факт.
func injCarriedControl(t *testing.T) []GateCarrierRetirement {
	t.Helper()
	repo := injDeclaredRepo(t)
	carried := func(carrier, rel string) GateCarrierRetirement {
		return GateCarrierRetirement{
			Carrier:   carrier,
			Reason:    "синтетический носитель",
			Successor: "предмет уехал вместе со службой",
			Fate:      subjectFateCarried,
			CarriedTo: &CarriedCoordinate{Repo: repo, Path: rel},
		}
	}
	return []GateCarrierRetirement{
		carried(gateCorpusDir+"/alpha.go", injCarriedPath),
		carried(gateCorpusDir+"/alpha_test.go", "internal/check/synthetic_subject_test.go"),
		carried(gateCorpusDir+"/alpha_injection_test.go", "internal/check/synthetic_subject_injection_test.go"),
		{
			Carrier:   gateCorpusDir + "/beta_test.go",
			Reason:    "синтетический носитель",
			Successor: "предмета в природе нет: судился каталог, которого не стало",
			Fate:      subjectFateGone,
		},
		{
			Carrier:   gateCorpusDir + "/gamma_test.go",
			Reason:    "синтетический носитель",
			Successor: "предмет уехал и держателя не получил",
			Fate:      subjectFateUnguarded,
			CarriedTo: &CarriedCoordinate{Repo: repo},
		},
		// Семья `delta`: предмет остался ЗДЕСЬ и снова стережётся. Координаты
		// берутся у ЖИВЫХ файлов этого дерева — выдуманные доказали бы резолв по
		// несуществующему дереву, а не по тому, о котором гейт говорит.
		{
			Carrier:     gateCorpusDir + "/delta_test.go",
			Reason:      "синтетический носитель",
			Successor:   "предмет остался здесь, держатель завёлся заново",
			Fate:        subjectFateRegained,
			GuardedHere: injRegainedPath,
		},
		{
			Carrier:     gateCorpusDir + "/delta_injection_test.go",
			Reason:      "синтетический носитель",
			Successor:   "доказательство падучести держателя соседней записи семьи",
			Fate:        subjectFateRegained,
			GuardedHere: injRegainedInjectionPath,
		},
	}
}

// injRegainedPath, injRegainedInjectionPath — координаты держателей В ЭТОМ
// дереве. Живые файлы, а не выдуманные: судьба [subjectFateRegained] — та
// единственная, чью координату дерево резолвит само, и синтетика доказала бы
// резолв по несуществующему пути.
const (
	injRegainedPath          = gateCorpusDir + "/carriedsubjectdeclared.go"
	injRegainedInjectionPath = gateCorpusDir + "/carriedsubjectdeclared_injection_test.go"
)

// injCarriedJudge — весь путь суждения на синтетической ведомости. ТА ЖЕ
// функция, что зовёт держатель по дереву.
//
// Корнем подаётся ЖИВОЕ дерево: координата держателя судьбы
// [subjectFateRegained] резолвится в нём, и подставной корень доказывал бы
// резолв по выдуманному дереву, а не по тому, о котором гейт говорит.
func injCarriedJudge(
	t *testing.T, rows []GateCarrierRetirement, resolve carriedCoordinateResolver,
) []string {
	t.Helper()
	findings, census := judgeCarriedSubjects(repoRoot(t), rows, injRepos(t), resolve)
	t.Logf("осмотрено: записей %d; семей %d; %q %d, %q %d, %q %d, %q %d; "+
		"координат сверено %d, не сверялось %d",
		census.Rows, census.Families, subjectFateGone, census.Gone,
		subjectFateCarried, census.Carried, subjectFateUnguarded, census.Unguarded,
		subjectFateRegained, census.Regained,
		census.Checked, census.Unchecked)
	return findings
}

// injCarriedOnly — находки должны быть, и каждая обязана называть предмет.
func injCarriedOnly(t *testing.T, findings []string, want string) {
	t.Helper()
	if len(findings) == 0 {
		t.Fatalf("находок ноль, а ожидалась хотя бы одна про %q — гейт не умеет краснеть", want)
	}
	for _, f := range findings {
		if strings.Contains(f, want) {
			return
		}
	}
	t.Fatalf("ни одна находка не называет %q:\n%s", want, strings.Join(findings, "\n"))
}

func injCarriedSilent(t *testing.T, findings []string) {
	t.Helper()
	if len(findings) > 0 {
		t.Fatalf("законный мир дал находки — гейт краснеет на верной работе:\n%s",
			strings.Join(findings, "\n"))
	}
}

// ── ПРОБЫ ПРЕДПОСЫЛОК ──────────────────────────────────────────────────────

// TestCarriedSubject_FamilyOfACarrierIsItsRoot — годок, гейт, его инъекция и его
// граничная проба дают ОДНУ семью.
//
// Без этого свойства сверка по семье судила бы четыре разных предмета вместо
// одного и молчала бы на расхождении — то есть на том единственном случае, ради
// которого она заведена.
func TestCarriedSubject_FamilyOfACarrierIsItsRoot(t *testing.T) {
	t.Parallel()
	want := gateCorpusDir + "/alpha"
	for _, carrier := range []string{
		gateCorpusDir + "/alpha.go",
		gateCorpusDir + "/alpha_test.go",
		gateCorpusDir + "/alpha_injection_test.go",
		gateCorpusDir + "/alpha_boundary_test.go",
	} {
		if got := carrierFamily(carrier); got != want {
			t.Errorf("%s → семья %q, а ожидалась %q", carrier, got, want)
		}
	}
	// Отрицательная сторона предпосылки: чужое имя в ту же семью не попадает.
	if got := carrierFamily(gateCorpusDir + "/alphabet_test.go"); got == want {
		t.Errorf("носитель другого предмета попал в семью %q — сверка судила бы чужое", want)
	}
}

// TestCarriedSubject_SuccessorVocabularyComesFromTheOwner — словарь
// репозиториев-преемников непуст и совпадает с ведомостью владельца имён.
func TestCarriedSubject_SuccessorVocabularyComesFromTheOwner(t *testing.T) {
	t.Parallel()
	repos := injRepos(t)
	if len(repos) == 0 {
		t.Fatal("словарь репозиториев-преемников пуст — этот гейт судил бы пустой вход")
	}
	for _, repo := range productnaming.ExternallySourcedServices() {
		if !repos[repo] {
			t.Errorf("владелец имён объявляет преемником %q, а словарь гейта его не знает", repo)
		}
	}
}

// ── КОНТРОЛЬ ───────────────────────────────────────────────────────────────

func TestCarriedSubject_ControlCoherentLedgerIsSilent(t *testing.T) {
	t.Parallel()
	injCarriedSilent(t, injCarriedJudge(t, injCarriedControl(t), nil))
}

// ── ОТРИЦАТЕЛЬНЫЕ МИРЫ: по одному факту от контроля ────────────────────────

func TestCarriedSubject_FateNotDeclaredIsFound(t *testing.T) {
	t.Parallel()
	rows := injCarriedControl(t)
	rows[1].Fate = "" // ОДИН факт: у гейта семьи снято поле судьбы
	injCarriedOnly(t, injCarriedJudge(t, rows, nil), rows[1].Carrier)
}

func TestCarriedSubject_FateOutsideTheVocabularyIsFound(t *testing.T) {
	t.Parallel()
	rows := injCarriedControl(t)
	rows[1].Fate = "moved" // ОДИН факт: судьба названа словом, которого в словаре нет
	injCarriedOnly(t, injCarriedJudge(t, rows, nil), "moved")
}

func TestCarriedSubject_GoneWithASuccessorCoordinateIsFound(t *testing.T) {
	t.Parallel()
	rows := injCarriedControl(t)
	// ОДИН факт: у записи, объявившей исчезновение, явился преемник.
	rows[3].CarriedTo = &CarriedCoordinate{Repo: injDeclaredRepo(t), Path: injCarriedPath}
	injCarriedOnly(t, injCarriedJudge(t, rows, nil), rows[3].Carrier)
}

func TestCarriedSubject_CarriedWithoutACoordinateIsFound(t *testing.T) {
	t.Parallel()
	rows := injCarriedControl(t)
	rows[1].CarriedTo = nil // ОДИН факт: уехало, а куда — не сказано
	injCarriedOnly(t, injCarriedJudge(t, rows, nil), rows[1].Carrier)
}

func TestCarriedSubject_RepoNotDeclaredByTheTreeIsFound(t *testing.T) {
	t.Parallel()
	rows := injCarriedControl(t)
	// ОДИН факт: преемник назван репозиторием, которого дерево не объявляет.
	rows[1].CarriedTo = &CarriedCoordinate{Repo: "PRO-Robotech/synthetic-nowhere", Path: injCarriedPath}
	injCarriedOnly(t, injCarriedJudge(t, rows, nil), "PRO-Robotech/synthetic-nowhere")
}

func TestCarriedSubject_AbsoluteCoordinateIsFound(t *testing.T) {
	t.Parallel()
	rows := injCarriedControl(t)
	// ОДИН факт: координата абсолютна — путь чужой машины, а не чужого дерева.
	rows[1].CarriedTo = &CarriedCoordinate{Repo: injDeclaredRepo(t), Path: "/home/kto-to/kaname/" + injCarriedPath}
	injCarriedOnly(t, injCarriedJudge(t, rows, nil), rows[1].Carrier)
}

// TestCarriedSubject_FamilyDisagreementIsFound — та самая форма, которой дефект
// и наблюдался: гейт объявил переезд, его инъекция — исчезновение.
func TestCarriedSubject_FamilyDisagreementIsFound(t *testing.T) {
	t.Parallel()
	rows := injCarriedControl(t)
	// ОДИН факт: инъекция семьи объявила, что предмета нет.
	rows[2].Fate = subjectFateGone
	rows[2].CarriedTo = nil
	injCarriedOnly(t, injCarriedJudge(t, rows, nil), carrierFamily(rows[2].Carrier))
}

func TestCarriedSubject_UnguardedWithoutARepoIsFound(t *testing.T) {
	t.Parallel()
	rows := injCarriedControl(t)
	rows[4].CarriedTo = nil // ОДИН факт: остаток не сказал, ГДЕ предмет живёт
	injCarriedOnly(t, injCarriedJudge(t, rows, nil), rows[4].Carrier)
}

func TestCarriedSubject_UnguardedNamingAHolderIsFound(t *testing.T) {
	t.Parallel()
	rows := injCarriedControl(t)
	// ОДИН факт: остаток назвал координату того, кого сам объявил отсутствующим.
	rows[4].CarriedTo = &CarriedCoordinate{Repo: injDeclaredRepo(t), Path: injCarriedPath}
	injCarriedOnly(t, injCarriedJudge(t, rows, nil), rows[4].Carrier)
}

// ── СУДЬБА «ОСТАЛСЯ ЗДЕСЬ И СНОВА СТЕРЕЖЁТСЯ»: обе стороны по каждой оси ────
//
// Законный близнец у всех пяти отрицаний один и тот же — семья `delta`
// контрольного мира, которую судит TestCarriedSubject_ControlCoherentLedgerIsSilent.
// Каждое отрицание меняет в ней РОВНО ОДИН факт.

// TestCarriedSubject_RegainedWithoutAHolderIsFound — «снова стережётся» без
// координаты держателя неотличимо от «не стережётся никем».
func TestCarriedSubject_RegainedWithoutAHolderIsFound(t *testing.T) {
	t.Parallel()
	rows := injCarriedControl(t)
	rows[5].GuardedHere = "" // ОДИН факт: держатель объявлен и не назван
	injCarriedOnly(t, injCarriedJudge(t, rows, nil), rows[5].Carrier)
}

// TestCarriedSubject_RegainedWithADanglingHolderIsFound — координата держателя
// В ЭТОМ дереве резолвится ВСЕГДА, и висячей быть не вправе.
//
// Ось, которой нет ни у одной другой судьбы: координата чужого дерева
// проверяется лишь при поднятой ручке, а эта — при каждом прогоне.
func TestCarriedSubject_RegainedWithADanglingHolderIsFound(t *testing.T) {
	t.Parallel()
	rows := injCarriedControl(t)
	// ОДИН факт: путь той же формы, но файла с таким именем в дереве нет.
	rows[5].GuardedHere = gateCorpusDir + "/no_such_holder_ever_existed.go"
	injCarriedOnly(t, injCarriedJudge(t, rows, nil), rows[5].Carrier)
}

// TestCarriedSubject_RegainedNamingASuccessorIsFound — предмет не уезжал, и
// координата преемника рядом с ним есть второе утверждение об одном предмете.
func TestCarriedSubject_RegainedNamingASuccessorIsFound(t *testing.T) {
	t.Parallel()
	rows := injCarriedControl(t)
	// ОДИН факт: запись объявила «остался здесь» и назвала преемника.
	rows[5].CarriedTo = &CarriedCoordinate{Repo: injDeclaredRepo(t), Path: injCarriedPath}
	injCarriedOnly(t, injCarriedJudge(t, rows, nil), rows[5].Carrier)
}

// TestCarriedSubject_HolderHereUnderAnotherFateIsFound — поле держателя
// принадлежит одной судьбе и только ей.
//
// Мир меняет запись семьи `alpha` (судьба `carried`): она остаётся во всём
// остальном годной, поэтому красное приходит от НОВОЙ оси, а не от соседней.
func TestCarriedSubject_HolderHereUnderAnotherFateIsFound(t *testing.T) {
	t.Parallel()
	rows := injCarriedControl(t)
	rows[0].GuardedHere = injRegainedPath // ОДИН факт: «уехало» и тут же «стережётся здесь»
	injCarriedOnly(t, injCarriedJudge(t, rows, nil), rows[0].Carrier)
}

// TestCarriedSubject_RegainedFamilyDisagreementIsFound — гейт и его инъекция не
// вправе объявлять разную судьбу и в новой судьбе тоже.
func TestCarriedSubject_RegainedFamilyDisagreementIsFound(t *testing.T) {
	t.Parallel()
	rows := injCarriedControl(t)
	// ОДИН факт: инъекция семьи сказала «предмета нет» там, где гейт сказал
	// «остался здесь». Поле держателя снимается вместе с судьбой — иначе мир
	// менял бы два факта и вердикт был бы недействителен.
	rows[6].Fate = subjectFateGone
	rows[6].GuardedHere = ""
	injCarriedOnly(t, injCarriedJudge(t, rows, nil), "РАЗНУЮ судьбу")
}

// TestCarriedSubject_RegainedIsCountedApartFromTheOtherFates — перепись считает
// новую судьбу СВОЕЙ величиной.
//
// Без этой оси её можно было бы молча зачесть в остаток либо в «уехало», и
// число названного остатка перестало бы называть остаток.
func TestCarriedSubject_RegainedIsCountedApartFromTheOtherFates(t *testing.T) {
	t.Parallel()
	rows := injCarriedControl(t)
	_, census := judgeCarriedSubjects(repoRoot(t), rows, injRepos(t), nil)
	if census.Regained != 2 {
		t.Fatalf("перепись насчитала %q %d, а в контрольном мире их 2 — судьба не считается "+
			"своей величиной", subjectFateRegained, census.Regained)
	}
	if census.Gone+census.Carried+census.Unguarded+census.Regained != census.Rows {
		t.Fatalf("сумма судеб (%d+%d+%d+%d) не равна числу записей (%d) — часть записей "+
			"не попала ни в одну величину, и перепись перестала быть переписью",
			census.Gone, census.Carried, census.Unguarded, census.Regained, census.Rows)
	}
}

// ── СВЕРКА КООРДИНАТЫ С ДЕРЕВОМ-ПРЕЕМНИКОМ: обе стороны ────────────────────

// injSuccessorTree — синтетическое дерево-преемник. Файлы называются
// вызывающим: миру нужен либо тот, что назван координатой, либо любой другой.
func injSuccessorTree(t *testing.T, rels ...string) carriedCoordinateResolver {
	t.Helper()
	root := t.TempDir()
	for _, rel := range rels {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("package check\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return resolveInTrees(map[string]string{injDeclaredRepo(t): root})
}

func TestCarriedSubject_CoordinateResolvedInTheSuccessorTreeIsSilent(t *testing.T) {
	t.Parallel()
	rows := injCarriedControl(t)
	resolve := injSuccessorTree(t,
		injCarriedPath,
		"internal/check/synthetic_subject_test.go",
		"internal/check/synthetic_subject_injection_test.go")
	injCarriedSilent(t, injCarriedJudge(t, rows, resolve))
}

func TestCarriedSubject_DanglingCoordinateIsFound(t *testing.T) {
	t.Parallel()
	rows := injCarriedControl(t)
	// ОДИН факт против мира выше: в дереве-преемнике нет файла, названного гейтом.
	resolve := injSuccessorTree(t,
		"internal/check/synthetic_subject_test.go",
		"internal/check/synthetic_subject_injection_test.go")
	injCarriedOnly(t, injCarriedJudge(t, rows, resolve), injCarriedPath)
}

// ── ОТКАЗ НА ПУСТОМ ОБХОДЕ ─────────────────────────────────────────────────

// injLedgerRoot — синтетический корень дерева с надгробием заданного содержания.
func injLedgerRoot(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	if body == "" {
		return root
	}
	dir := filepath.Join(root, filepath.FromSlash(gateCorpusDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, gateCarrierLedgerName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestCarriedSubject_MissingLedgerRefusesInsteadOfReportingNoFindings(t *testing.T) {
	t.Parallel()
	if _, err := carriedSubjectRows(injLedgerRoot(t, "")); err == nil {
		t.Fatal("надгробия нет, а вход добыт без отказа — «ноль находок» тут неотличимо " +
			"от связной ведомости")
	}
}

func TestCarriedSubject_EmptyLedgerRefusesInsteadOfReportingNoFindings(t *testing.T) {
	t.Parallel()
	if _, err := carriedSubjectRows(injLedgerRoot(t, `{"retired": []}`)); err == nil {
		t.Fatal("надгробие пусто, а вход добыт без отказа — судить было нечего")
	}
}

// TestCarriedSubject_LedgerWithRowsIsRead — положительная сторона отказа выше:
// ведомость со строками читается, и строки доходят до суждения.
func TestCarriedSubject_LedgerWithRowsIsRead(t *testing.T) {
	t.Parallel()
	body := `{"retired": [{"carrier": "internal/repohygiene/alpha_test.go",
		"reason": "синтетический носитель", "successor": "предмета нет", "fate": "gone"}]}`
	rows, err := carriedSubjectRows(injLedgerRoot(t, body))
	if err != nil {
		t.Fatalf("ведомость со строкой не прочиталась: %v", err)
	}
	if len(rows) != 1 || rows[0].Fate != subjectFateGone {
		t.Fatalf("прочитано не то: %+v", rows)
	}
}

// ── РУЧКА ДЕРЕВЬЕВ-ПРЕЕМНИКОВ ──────────────────────────────────────────────

// TestCarriedSubject_TreesHandleIsParsed — разбор значения ручки.
//
// Судится ЗНАЧЕНИЕ, а не среда прогона: проба, правящая среду, обязана быть
// последовательной, и пакет платил бы за неё ядром (probeparallelism_test.go).
func TestCarriedSubject_TreesHandleIsParsed(t *testing.T) {
	t.Parallel()
	repo := injDeclaredRepo(t)
	root := t.TempDir()

	trees, how, err := parseCarriedSubjectTrees("")
	if err != nil || len(trees) != 0 {
		t.Fatalf("пустое значение обязано быть законным: trees=%v err=%v", trees, err)
	}
	if !strings.Contains(how, carriedSubjectTreesEnv) {
		t.Errorf("перепись обязана НАЗВАТЬ пропуск, а сказала: %q", how)
	}

	trees, how, err = parseCarriedSubjectTrees(repo + "=" + root)
	if err != nil {
		t.Fatalf("законная пара не разобралась: %v", err)
	}
	if trees[repo] != root {
		t.Errorf("корень %s разобран как %q, а задан %q", repo, trees[repo], root)
	}
	if !strings.Contains(how, root) {
		t.Errorf("перепись обязана назвать дерево, а сказала: %q", how)
	}

	if _, _, err := parseCarriedSubjectTrees(repo); err == nil {
		t.Error("пара без пути принята — форма `repo=path` не держится")
	}
	if _, _, err := parseCarriedSubjectTrees(repo + "=kaname-read"); err == nil {
		t.Error("относительный корень принят — вердикт зависел бы от рабочего каталога")
	}
	if _, _, err := parseCarriedSubjectTrees(
		repo + "=" + filepath.Join(root, "net-takogo-kataloga")); err == nil {
		t.Error("несуществующий корень принят — сверять было бы не с чем")
	}
}
