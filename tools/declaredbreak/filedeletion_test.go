// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package declaredbreak

// Снятие файла контракта ЦЕЛИКОМ — разрыв, который до 2026-08-15 нельзя было объявить
// НИ ПРИ КАКОМ входе.
//
// ─────────────────────────────────────────────────────────────────────────────
// ПРЕДМЕТ
//
// У находки `FILE_NO_DELETE` ключа `path` в JSON нет вовсе: предмет находки — сам файл,
// которого в новом дереве уже не существует, и указывать не на что. Гейт же считал путь
// у находки данностью, и это давало канонический класс «два правила об одном поле»
// (`api-conventions.md`): сопоставление требовало `path == ""`, а `Validate` непустой
// путь ТРЕБОВАЛА. Исполнимого входа не существовало:
//
//   - путь заполнен путём снятого файла — не сопоставляется ни с чем, все записи
//     объявляются истёкшими, срабатывает `Incoherent`, гейт выходит кодом 2 с диагнозом
//     «запускай из каталога контрактов» — при запуске ИЗ каталога контрактов;
//   - путь пуст — сопоставляется, и ровно эту запись отвергает `Validate`.
//
// Класс дожил до сегодня потому, что фикстуры не несли НИ ОДНОЙ находки без пути:
// строка `FILE_NO_DELETE` не встречалась во всём дереве ни разу (пробы, testdata,
// перечень, конвейер, документация — ноль вхождений). Вход, обнажающий дефект, гейт не
// видел никогда, и «зелёные пробы» о нём не утверждали ничего.
//
// Предмет при этом был живой: снятия контрактов целиком случались, в том числе ПОСЛЕ
// появления гейта, а `FILE_NO_DELETE` не был объявлен ни разу — что согласуется с тем,
// что через него это не проходило в принципе.
//
// ─────────────────────────────────────────────────────────────────────────────
// ЧТО ЗДЕСЬ ПРОВЕРЯЕТСЯ (инъекция в обе стороны + законный близнец)
//
//	(а) снятие файла + запись в перечне  → сопоставлено, чисто;
//	(б) та же запись БЕЗ снятия          → «послабление истекло», НЕ чисто;
//	(в) законный близнец                 → снятие поля и метода сопоставляются как прежде.
//
// Без (б) починка ловила бы форму, а не существо: запись, которая сопоставляется всегда,
// пережила бы свой предмет и стала бы тем самым `breaking.ignore`, ради отказа от
// которого гейт и написан.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fileDeletedFixture — вывод, СНЯТЫЙ С РЕАЛЬНОГО buf 1.72.0 (той же версии, что пинит
// конвейер), а не написанный по догадке о его формате.
//
// Как снят: рабочая копия на origin/main, `git rm proto/kacho/cloud/vpc/v1/
// internal_dataplane_service.proto`, затем из каталога контрактов —
// `buf breaking --against '<repo>/.git#ref=origin/main,subdir=proto' --error-format=json`.
// Код возврата 100, одна строка на stdout, stderr пуст.
const fileDeletedFixture = "testdata/buf-breaking-file-deleted.jsonl"

// deletedFilePath — предмет находки в фикстуре. Он же путь, он же символ: у снятия файла
// целиком предмет ровно один, и buf называет в сообщении именно его.
const deletedFilePath = "kacho/cloud/vpc/v1/internal_dataplane_service.proto"

func loadFileDeleted(t *testing.T) []Finding {
	t.Helper()
	f, err := os.Open(filepath.FromSlash(fileDeletedFixture))
	if err != nil {
		t.Fatalf("фикстура снятия файла не открыта: %v", err)
	}
	defer f.Close()
	got, err := ParseFindings(f)
	if err != nil {
		t.Fatalf("реальный вывод buf о снятии файла не разобран: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("находок в фикстуре: %d, ожидалась 1", len(got))
	}
	return got
}

// TestFileDeletionFindingHasNoPathKey — ПРОВЕРКА ПРЕДПОСЫЛКИ восстановления пути.
//
// Восстановление из сообщения оправдано ровно тем, что поля пути у этой находки НЕТ.
// Утверждается это на сыром JSON фикстуры, а не на разобранной структуре: после разбора
// путь уже восстановлен, и проба, смотрящая на Finding, зеленела бы независимо от того,
// был ключ или нет. Положительный контроль — соседняя фикстура, где ключ есть: без него
// «ключа нет» было бы неотличимо от «проба ключей не читает».
func TestFileDeletionFindingHasNoPathKey(t *testing.T) {
	raw, err := os.ReadFile(filepath.FromSlash(fileDeletedFixture))
	if err != nil {
		t.Fatalf("фикстура не прочитана: %v", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(raw))), &keys); err != nil {
		t.Fatalf("строка фикстуры не разобрана как объект: %v", err)
	}
	if _, ok := keys["path"]; ok {
		t.Fatal("у находки о снятии файла ЕСТЬ ключ path — предпосылка восстановления пути отпала, " +
			"и subjectPathFromMessage стала разбором чужой прозы без причины")
	}
	if _, ok := keys["message"]; !ok {
		t.Fatal("у находки нет и ключа message — восстанавливать путь неоткуда")
	}
	t.Logf("осмотрено: ключей в находке %d, ключа path среди них нет", len(keys))

	// Положительный контроль на ту же перепись: у находок с предметом внутри файла ключ
	// path есть, и восстановление их не касается.
	other, err := os.ReadFile(filepath.FromSlash(realFixture))
	if err != nil {
		t.Fatalf("соседняя фикстура не прочитана: %v", err)
	}
	var withPath int
	for _, line := range strings.Split(strings.TrimSpace(string(other)), "\n") {
		var k map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &k); err != nil {
			t.Fatalf("строка соседней фикстуры не разобрана: %v", err)
		}
		if _, ok := k["path"]; ok {
			withPath++
		}
	}
	if withPath == 0 {
		t.Fatal("ни одна находка соседней фикстуры не несёт path — проба читает не ключи")
	}
	t.Logf("осмотрено: в соседней фикстуре находок с ключом path %d", withPath)
}

// TestParseRestoresPathOfDeletedFile — путь восстановлен, и координата перестала быть
// вырожденной. `:1` в отчёте — признак ровно того дефекта, который здесь чинится.
func TestParseRestoresPathOfDeletedFile(t *testing.T) {
	f := loadFileDeleted(t)[0]
	if f.Type != "FILE_NO_DELETE" {
		t.Fatalf("правило находки: %q", f.Type)
	}
	if f.Path != deletedFilePath {
		t.Errorf("путь не восстановлен: %q, ожидался %q", f.Path, deletedFilePath)
	}
	if got, want := f.Coordinate(), deletedFilePath+":1"; got != want {
		t.Errorf("координата %q, ожидалась %q", got, want)
	}
	if strings.HasPrefix(f.Coordinate(), ":") {
		t.Error("координата начинается с двоеточия — путь пуст, находка неадресуема")
	}
}

// TestFileDeletionCanBeDeclared — ИНЪЕКЦИЯ (а): разрыв есть, объявление есть → чисто.
//
// До починки эта проба была красной: сопоставлялось 0, запись объявлялась истёкшей,
// вердикт был самопротиворечив (`Incoherent`), и командная точка входа выходила кодом 2.
func TestFileDeletionCanBeDeclared(t *testing.T) {
	d := decl("FILE_NO_DELETE", deletedFilePath, deletedFilePath)

	// Сначала — сама запись годна. Прежде обе половины были невыполнимы разом: то, что
	// сопоставлялось, валидация отвергала.
	if problems := d.Validate(); len(problems) != 0 {
		t.Fatalf("объявление снятия файла негодно по форме: %v", problems)
	}

	res := Adjudicate(loadFileDeleted(t), []Declaration{d})
	if !res.Clean() {
		t.Fatalf("объявленное снятие файла не прошло:\n%s", res.Report())
	}
	if res.Matched != 1 {
		t.Errorf("сопоставлено %d, ожидалось 1", res.Matched)
	}
	if res.Incoherent() {
		t.Error("вердикт объявлен самопротиворечивым — гейт вернул бы код «не смог работать» " +
			"с диагнозом про рабочий каталог, к делу не относящимся")
	}
	if !strings.Contains(res.Report(), "[PASS]") {
		t.Errorf("отчёт не называет исход:\n%s", res.Report())
	}
}

// TestFileDeletionDeclarationExpires — ИНЪЕКЦИЯ (б): та же запись БЕЗ разрыва обязана
// краснеть. Без этой стороны починка ловила бы форму, а не существо: запись, которая
// сопоставляется всегда, пережила бы свой предмет.
func TestFileDeletionDeclarationExpires(t *testing.T) {
	d := decl("FILE_NO_DELETE", deletedFilePath, deletedFilePath)

	res := Adjudicate(nil, []Declaration{d})
	if res.Clean() {
		t.Fatal("запись о снятии файла прошла при отсутствии разрыва — послабление переживёт свой предмет")
	}
	if len(res.Expired) != 1 {
		t.Fatalf("истёкших: %d, ожидалась 1\n%s", len(res.Expired), res.Report())
	}
	rep := res.Report()
	if !strings.Contains(rep, "ПОСЛАБЛЕНИЕ ИСТЕКЛО") || !strings.Contains(rep, deletedFilePath) {
		t.Errorf("исход не назван либо не назван предмет:\n%s", rep)
	}

	// И зеркально: разрыв ЕСТЬ, а объявления нет — по-прежнему красное. Иначе
	// восстановление пути открыло бы дыру ровно там, где чинило другую.
	res = Adjudicate(loadFileDeleted(t), nil)
	if res.Clean() {
		t.Fatal("необъявленное снятие файла прошло — защита от случайного разрыва потеряна")
	}
	if len(res.Undeclared) != 1 {
		t.Fatalf("необъявленных: %d, ожидалась 1\n%s", len(res.Undeclared), res.Report())
	}
	if !strings.Contains(res.Report(), "НЕОБЪЯВЛЕННЫЙ РАЗРЫВ") {
		t.Errorf("исход не назван:\n%s", res.Report())
	}
}

// TestFileDeletionAlongsideOtherBreaks — ЗАКОННЫЙ БЛИЗНЕЦ (в): восстановление пути не
// трогает находки, у которых путь есть. Снятие поля и метода объявляются как прежде, в
// одном перечне со снятием файла, и лишняя запись здесь тоже краснеет.
func TestFileDeletionAlongsideOtherBreaks(t *testing.T) {
	findings := append(loadReal(t), loadFileDeleted(t)...)
	decls := []Declaration{
		decl("RPC_NO_DELETE", "kacho/cloud/vpc/v1/route_table_service.proto", "AddRoutes"),
		decl("FIELD_NO_DELETE", "kacho/cloud/vpc/v1/security_group.proto", "predefined_target"),
		decl("FILE_NO_DELETE", deletedFilePath, deletedFilePath),
	}

	res := Adjudicate(findings, decls)
	if !res.Clean() {
		t.Fatalf("смешанный перечень не прошёл:\n%s", res.Report())
	}
	if res.Matched != 3 {
		t.Errorf("сопоставлено %d, ожидалось 3", res.Matched)
	}

	// Отрицательная сторона близнеца: убрав снятие файла из находок, краснеем ровно на
	// его записи, а два прежних объявления остаются сопоставленными.
	res = Adjudicate(loadReal(t), decls)
	if len(res.Expired) != 1 || res.Expired[0].Rule != "FILE_NO_DELETE" {
		t.Fatalf("истёкшей объявлена не запись о снятии файла:\n%s", res.Report())
	}
	if res.Matched != 2 {
		t.Errorf("сопоставлено %d, ожидалось 2 — прежние объявления задеты починкой", res.Matched)
	}
}

// TestFileDeletionSymbolMismatchIsItsOwnOutcome — автор, назвавший символом короткое имя
// файла вместо пути, получает НАЗВАННЫЙ исход с фактическим сообщением buf, а не
// «послабление истекло». Сообщение и есть подсказка: в нём предмет стоит в кавычках.
func TestFileDeletionSymbolMismatchIsItsOwnOutcome(t *testing.T) {
	res := Adjudicate(loadFileDeleted(t), []Declaration{
		decl("FILE_NO_DELETE", deletedFilePath, "internal_dataplane_service.proto"),
	})
	if len(res.SymbolMismatch) != 1 {
		t.Fatalf("несовпадений символа: %d, ожидалось 1\n%s", len(res.SymbolMismatch), res.Report())
	}
	if len(res.Expired) != 0 {
		t.Errorf("несовпадение символа отнесено к истечению — исходы слиты: %+v", res.Expired)
	}
	if rep := res.Report(); !strings.Contains(rep, "СИМВОЛ НЕ СОВПАЛ") || !strings.Contains(rep, `"`+deletedFilePath+`"`) {
		t.Errorf("отчёт не называет ни исход, ни фактический предмет:\n%s", rep)
	}
}

// TestSubjectPathIsNotGuessed — восстановление НЕ угадывает. Ноль путей в кавычках и
// больше одного — отказ разбора, который командная точка входа обязана довести до кода
// «гейт не смог работать»; молчаливая догадка сопоставила бы объявление не с тем
// разрывом.
func TestSubjectPathIsNotGuessed(t *testing.T) {
	for _, c := range []struct {
		имя  string
		json string
		want string
	}{
		{
			имя:  "форма сообщения изменилась — путей в кавычках нет",
			json: `{"start_line":1,"type":"FILE_NO_DELETE","message":"Previously present file was deleted."}`,
			want: "не называет ни одного пути .proto",
		},
		{
			имя:  "сообщение называет два файла — предмет неопределим",
			json: `{"start_line":1,"type":"FILE_NO_DELETE","message":"file \"a/b.proto\" moved to \"a/c.proto\"."}`,
			want: "решить нечем",
		},
	} {
		t.Run(c.имя, func(t *testing.T) {
			_, err := ParseFindings(strings.NewReader(c.json + "\n"))
			if err == nil {
				t.Fatal("находка без пути принята — путь был бы либо пуст, либо угадан")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("причина не названа: %v", err)
			}
		})
	}

	// Положительный контроль: ровно один путь в кавычках восстанавливается. Без него
	// «всё отвергается» было бы неотличимо от работающего восстановления.
	got, err := ParseFindings(strings.NewReader(
		`{"start_line":1,"type":"FILE_NO_DELETE","message":"Previously present file \"a/b.proto\" was deleted."}` + "\n"))
	if err != nil {
		t.Fatalf("единственный путь в кавычках не восстановлен: %v", err)
	}
	if len(got) != 1 || got[0].Path != "a/b.proto" {
		t.Errorf("восстановлено неверно: %+v", got)
	}
}
