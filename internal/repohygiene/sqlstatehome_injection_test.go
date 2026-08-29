// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// sqlstatehome_injection_test.go — доказательство того, что оба гейта
// sqlstatehome СПОСОБНЫ упасть, и падают они на существе, а не на форме.
//
// Инъекция гоняет ТЕ ЖЕ функции разбора, что и гейты (`ScanSQLStateLiterals`,
// `ScanStatusMappers`), — иначе она доказывала бы способность падать у другого
// кода.
//
// Пара обязательна в обе стороны, и вторая сторона здесь тяжелее первой:
// «законный близнец» у каждого гейта — то, ради чего он вообще может краснеть
// ложно. У первого это код в ПРОЗЕ (в дереве таких вхождений вшестеро больше,
// чем исполняемых) и чужие коды, домом не разбираемые. У второго — терминальный
// возврат, подставляющий СВОИ значения: номер элемента, имя поля, ярлык
// оператора. Прежняя редакция второго гейта близнеца не различала и дала шесть
// находок из шести ложных.
package repohygiene

import (
	"strings"
	"testing"
)

// ─────────────────────────── гейт первый: МЕСТО решения ───────────────────────

// sqlStateInjectedOwnDecision — возвращённое решение по коду вне дома.
//
// Форм две, и обе законны в Go: ветвление и сравнение. Гейт, знающий одну,
// давал бы не находку и не молчание, а НЕВИДИМОСТЬ.
const sqlStateInjectedOwnDecision = `package pg

import "github.com/jackc/pgx/v5/pgconn"

func mapErr(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return ErrAlreadyExists
		case "23503":
			return ErrFailedPrecondition
		}
		if pgErr.Code == "23P01" {
			return ErrFailedPrecondition
		}
	}
	return ErrInternal
}
`

// sqlStateInjectedLegitimateTwin — то же место, переведённое на дом, вместе с
// законными соседями, каждый со своим способом вызвать ложную находку:
//
//   - код в КОММЕНТАРИИ, документирующем маршрут, — самая частая форма в этом
//     дереве (67 файлов из 85 на ревизии заведения);
//   - код в тексте СООБЩЕНИЯ, а не в решении;
//   - собственные коды продукта (полоса учёта величин) — не предмет правила и не
//     предмет дома;
//   - коды доступности сервера — предмет соседа `pkg/dbready`, другой вопрос.
const sqlStateInjectedLegitimateTwin = `package pg

import "github.com/PRO-Robotech/kacho/pkg/db/pgfault"

// mapErr отображает отказ хранилища: 23505 → уже существует, 23503 →
// предусловие, 23P01 → предусловие. Коды названы в прозе намеренно — маршрут
// обязан читаться вместе с кодом.
func mapErr(err error) error {
	f := pgfault.Classify(err)
	switch f.Class {
	case pgfault.Unique:
		return ErrAlreadyExists
	case pgfault.ForeignKey, pgfault.Exclusion:
		return ErrFailedPrecondition
	}
	// Полоса учёта величин: коды производит триггер схемы, не сервер.
	if f.SQLState == "KQ001" {
		return ErrQuotaExceeded
	}
	// Доступность сервера — предмет соседа, не этого правила.
	if f.SQLState == "57P03" || f.SQLState == "53300" {
		return ErrUnavailable
	}
	return fmt.Errorf("unexpected sqlstate 23505-like class: %w", ErrInternal)
}
`

func TestSQLStateHomeGateCatchesAnOwnDecision(t *testing.T) {
	sites, census, err := ScanSQLStateLiterals("injected/pg/errmap.go", []byte(sqlStateInjectedOwnDecision))
	if err != nil {
		t.Fatalf("разбор инъекции: %v", err)
	}
	if census.Literals == 0 {
		t.Fatal("инъекция не разобрана: литералов ноль — доказательство беспредметно")
	}
	if len(sites) != 3 {
		t.Fatalf("возвращённое решение опознано %d раз, ожидалось 3 (два case и одно сравнение); "+
			"формы записи в Go две, и гейт, знающий одну, даёт невидимость, а не молчание", len(sites))
	}
	// Находка обязана НАЗЫВАТЬ КООРДИНАТУ — иначе на неё тратят прогон и снимают
	// гейт как непонятный.
	for _, s := range sites {
		if s.Line == 0 || s.Func != "mapErr" {
			t.Fatalf("находка без координаты: %+v — читателю негде искать", s)
		}
	}
}

func TestSQLStateHomeGateIsSilentOnTheLegitimateTwin(t *testing.T) {
	sites, census, err := ScanSQLStateLiterals("injected/pg/errmap.go", []byte(sqlStateInjectedLegitimateTwin))
	if err != nil {
		t.Fatalf("разбор близнеца: %v", err)
	}
	if census.Literals == 0 {
		t.Fatal("близнец не разобран: литералов ноль — молчание сказано ни о чём")
	}
	if len(sites) != 0 {
		var got []string
		for _, s := range sites {
			got = append(got, s.Code)
		}
		t.Fatalf("гейт нашёл %d вхождений у законного близнеца (%s) — ложная находка. "+
			"Первый же ложный срабат отключает гейт, а вместе с ним и настоящую находку.",
			len(sites), strings.Join(got, ", "))
	}
}

// ─────────────────────── гейт второй: ТЕКСТ ветки по умолчанию ────────────────

// statusTailInjectedEcho — возвращённое эхо ошибки в ветке по умолчанию, в трёх
// написаниях: сама ошибка, её текст, слова СУБД из разобранного отказа.
const statusTailInjectedEcho = `package shared

func MapRepoErrA(err error) error {
	if errors.Is(err, ErrNotFound) {
		return status.Error(codes.NotFound, "not found")
	}
	if errors.Is(err, ErrInvalidArg) {
		return status.Error(codes.InvalidArgument, "invalid argument")
	}
	return status.Errorf(codes.Internal, "internal: %v", err)
}

func MapRepoErrB(err error) error {
	if errors.Is(err, ErrNotFound) {
		return status.Error(codes.NotFound, "not found")
	}
	if errors.Is(err, ErrInvalidArg) {
		return status.Error(codes.InvalidArgument, "invalid argument")
	}
	return status.Error(codes.Internal, err.Error())
}

func MapRepoErrC(err error, f pgfault.Fault) error {
	if errors.Is(err, ErrNotFound) {
		return status.Error(codes.NotFound, "not found")
	}
	if errors.Is(err, ErrInvalidArg) {
		return status.Error(codes.InvalidArgument, "invalid argument")
	}
	return status.Errorf(codes.Internal, "database said: %s", f.Message)
}
`

// statusTailInjectedLegitimateTwin — законные близнецы, каждый со своим
// способом вызвать ложную находку:
//
//   - фиксированный текст константой дома;
//   - ярлык оператора именем — правило разрешает его прямо;
//   - подстановка СВОИХ значений (номер элемента, имя поля, идентификатор из
//     запроса) — форматирование как таковое не запрещено, запрещено эхо ОШИБКИ;
//   - функция, вообще не принимающая ошибку: её терминал о ветке по умолчанию
//     отображения не утверждает ничего.
const statusTailInjectedLegitimateTwin = `package shared

func MapRepoErrA(err error) error {
	if errors.Is(err, ErrNotFound) {
		return status.Error(codes.NotFound, "not found")
	}
	if errors.Is(err, ErrInvalidArg) {
		return status.Error(codes.InvalidArgument, "invalid argument")
	}
	return status.Error(codes.Internal, pgfault.OpaqueMessage)
}

func MapRepoErrLeakSafe(err error, fallback string) error {
	if errors.Is(err, ErrNotFound) {
		return status.Error(codes.NotFound, "not found")
	}
	if errors.Is(err, ErrInvalidArg) {
		return status.Error(codes.InvalidArgument, "invalid argument")
	}
	return status.Error(codes.Internal, fallback)
}

func mapPeerTargetErr(idx int, field, id string, err error) error {
	if errors.Is(err, ErrNotFound) {
		return status.Errorf(codes.NotFound, "target[%d].%s '%s' not found", idx, field, id)
	}
	if errors.Is(err, ErrInvalidArg) {
		return status.Errorf(codes.InvalidArgument, "target[%d].%s invalid", idx, field)
	}
	return status.Errorf(codes.Internal, "target[%d].%s '%s': peer lookup failed", idx, field, id)
}

func lookupSubject(ctx context.Context, ext string) (*Subject, error) {
	if ext == "" {
		return nil, status.Errorf(codes.InvalidArgument, "external_id=%s is empty", ext)
	}
	return nil, status.Errorf(codes.NotFound, "subject not found by external_id=%s", ext)
}
`

func TestStatusTailGateCatchesAnEchoedError(t *testing.T) {
	mappers, census, err := ScanStatusMappers("injected/shared/errors.go", []byte(statusTailInjectedEcho))
	if err != nil {
		t.Fatalf("разбор инъекции: %v", err)
	}
	if census.ErrTaking == 0 {
		t.Fatal("инъекция не разобрана: функций, принимающих error, ноль — доказательство беспредметно")
	}
	var caught []string
	for _, m := range mappers {
		if m.TailIsStatus && !m.TailFixed {
			caught = append(caught, m.Func+" → "+m.TailText)
		}
	}
	if len(caught) != 3 {
		t.Fatalf("эхо опознано %d раз, ожидалось 3 (сама ошибка, её текст, слова СУБД); поймано: %v",
			len(caught), caught)
	}
	// Находка обязана называть, ЧТО именно выносится наружу: «производный текст»
	// посылает читателя искать не там.
	joined := strings.Join(caught, " | ")
	for _, want := range []string{"принятую ошибку", ".Error()", "слова СУБД"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("находка не называет %q; напечатано: %s", want, joined)
		}
	}
	for _, m := range mappers {
		if m.TailIsStatus && !m.TailFixed && (m.Line == 0 || m.TailLine == 0) {
			t.Fatalf("находка без координаты: %+v", m)
		}
	}
}

func TestStatusTailGateIsSilentOnTheLegitimateTwin(t *testing.T) {
	mappers, census, err := ScanStatusMappers("injected/shared/errors.go", []byte(statusTailInjectedLegitimateTwin))
	if err != nil {
		t.Fatalf("разбор близнеца: %v", err)
	}
	if census.ErrTaking == 0 {
		t.Fatal("близнец не разобран: функций, принимающих error, ноль — молчание сказано ни о чём")
	}
	// Положительный контроль внутри близнеца: отображения обязаны БЫТЬ найдены,
	// иначе молчание означает «разбор ослеп», а не «находок нет».
	var tails int
	for _, m := range mappers {
		if m.TailIsStatus {
			tails++
		}
	}
	if tails < 3 {
		t.Fatalf("у близнеца распознано %d отображений с терминалом-статусом, ожидалось ≥3 — "+
			"разбор не видит предмет, и его молчание ничего не значит", tails)
	}
	for _, m := range mappers {
		if m.TailIsStatus && !m.TailFixed {
			t.Fatalf("ложная находка на законном близнеце: %s() → %s. Правило запрещает эхо ОШИБКИ, "+
				"а не форматирование как таковое; первый же ложный срабат отключает гейт.",
				m.Func, m.TailText)
		}
	}
	// Функция, ошибку не принимающая, отображением не считается вовсе.
	for _, m := range mappers {
		if m.Func == "lookupSubject" {
			t.Fatalf("функция, не принимающая error, зачтена в отображения — предмет гейта расширен " +
				"за границу правила, которым он обоснован")
		}
	}
}

// ─────────────── гейт третий: ПРИЗНАК, по которому принято решение ────────────

// textFaultInjectedWordDecision — возвращённое решение по СЛОВАМ сервера.
//
// Форм записи здесь четыре, и все четыре законны в Go. Гейт, знающий одну,
// давал бы не находку и не молчание, а НЕВИДИМОСТЬ: остальные три уехали бы вне
// наблюдения (`testing.md` §«Гейт на класс», п.7).
const textFaultInjectedWordDecision = `package address

func isUniqueViolation(err error) bool {
	msg := err.Error()
	// (1) слова сервера подстрокой
	if strings.Contains(msg, "duplicate key value") {
		return true
	}
	// (2) код, добытый из форматирования драйвером
	if strings.Contains(msg, "SQLSTATE 23505") {
		return true
	}
	// (3) начало сообщения
	if strings.HasPrefix(msg, "ERROR: violates foreign key constraint") {
		return true
	}
	// (4) прямое сравнение с текстом драйвера
	return msg == "no rows in result set"
}
`

// textFaultInjectedLegitimateTwin — то же место, переведённое на сигнальную
// ошибку, вместе с законными соседями. Каждый сосед — СВОЙ способ вызвать
// ложную находку, и все четыре встречаются в этом дереве:
//
//   - те же слова в КОММЕНТАРИИ, объясняющем маршрут: самая частая форма, и
//     проверка по подстроке краснела бы здесь на собственной шапке (в ней
//     вокабуляр стоит весь до единой фразы);
//   - те же слова в тексте СООБЩЕНИЯ, а не в решении;
//   - сравнение строк по образцу, к отказу хранилища отношения не имеющему, —
//     маршрутизация;
//   - разбор текста МИГРАЦИИ (`drop constraint`): слово «constraint» есть, но
//     это синтаксис DDL, а не слова отказа, и предмет здесь чужой. Живой сосед
//     этого дерева — `keyalgorithmdictionary.go`.
const textFaultInjectedLegitimateTwin = `package address

// isUniqueViolation распознаёт конфликт уникальности. Слой repo разбирает отказ
// по коду (23505 → ErrAlreadyExists) и отдаёт сигнальную ошибку; прежняя
// редакция сверяла текст с "duplicate key value" и "SQLSTATE 23505", что на
// русской локали не совпадало бы вовсе — "violates unique constraint" сервер
// пишет только по-английски.
func isUniqueViolation(err error) bool {
	return errors.Is(err, repo.ErrAlreadyExists)
}

func explain(err error) string {
	if errors.Is(err, repo.ErrAlreadyExists) {
		return "duplicate key value: address already allocated"
	}
	return "allocation failed"
}

func routeOf(path string) string {
	if strings.Contains(path, "/vpc/v1/addresses") {
		return "address"
	}
	if strings.HasPrefix(path, "/iam/v1/") {
		return "iam"
	}
	return ""
}

func droppedConstraintIn(migrationSQL string) int {
	return strings.Index(strings.ToLower(migrationSQL), "drop constraint")
}
`

func TestTextFaultGateCatchesADecisionMadeByWords(t *testing.T) {
	sites, census, err := ScanTextFaultDecisions("injected/address/helpers.go", []byte(textFaultInjectedWordDecision))
	if err != nil {
		t.Fatalf("разбор инъекции: %v", err)
	}
	if census.Comparisons == 0 {
		t.Fatal("инъекция не разобрана: сравнений строк ноль — доказательство беспредметно")
	}
	if len(sites) != 4 {
		var got []string
		for _, s := range sites {
			got = append(got, s.How+" "+s.Needle)
		}
		t.Fatalf("решение по словам опознано %d раз, ожидалось 4 (подстрока, код в тексте, "+
			"начало сообщения, прямое сравнение); поймано: %v.\n"+
			"Форма, о которой распознаватель не знает, — не край и не редкость: всё, "+
			"записанное в ней, оказывается ВНЕ НАБЛЮДЕНИЯ.", len(sites), got)
	}
	// Находка обязана НАЗЫВАТЬ КООРДИНАТУ — иначе на неё тратят прогон и снимают
	// гейт как непонятный (`testing.md` §«Гейт на класс», п.8).
	for _, s := range sites {
		if s.Line == 0 || s.Func != "isUniqueViolation" || s.Needle == "" || s.Why == "" {
			t.Fatalf("находка без координаты либо без объяснения: %+v — читателю негде искать", s)
		}
	}
}

func TestTextFaultGateIsSilentOnTheLegitimateTwin(t *testing.T) {
	sites, census, err := ScanTextFaultDecisions("injected/address/helpers.go", []byte(textFaultInjectedLegitimateTwin))
	if err != nil {
		t.Fatalf("разбор близнеца: %v", err)
	}
	// Положительный контроль близнеца: он обязан НЕСТИ предмет наблюдения.
	// Молчание над неразобранным входом сказано ни о чём.
	if census.Comparisons == 0 {
		t.Fatal("близнец не разобран: сравнений строк ноль — молчание сказано ни о чём")
	}
	if census.Literals == 0 {
		t.Fatal("близнец не разобран: литералов ноль")
	}
	if len(sites) != 0 {
		var got []string
		for _, s := range sites {
			got = append(got, s.How+" "+s.Needle)
		}
		t.Fatalf("гейт нашёл %d решений у законного близнеца (%s) — ложная находка. "+
			"Первый же ложный срабат отключает гейт, а вместе с ним и настоящую находку.",
			len(sites), strings.Join(got, ", "))
	}
}

// TestTextFaultInjectionBreaksOnlyTheNewProperty — третий прогон пары, и он
// обязателен (`testing.md` §«Гейт на класс», п.2в, 2026-08-24).
//
// Инъекция, попутно нарушающая СОСЕДНИЙ контроль, доказательством не является:
// красное пришло бы от соседа, а новый гейт мог бы оказаться вакуумным, не
// показав этого ничем. Здесь проверяется, что инъекция словами НЕ трогает
// предмет соседей: ни места решения по коду, ни текста ветки по умолчанию.
func TestTextFaultInjectionBreaksOnlyTheNewProperty(t *testing.T) {
	src := []byte(textFaultInjectedWordDecision)

	// Сосед первый — МЕСТО решения по коду. Литерал "SQLSTATE 23505" коду
	// "23505" не равен, поэтому сосед обязан молчать: он о решении по тексту не
	// утверждает ничего и никогда не утверждал.
	codeSites, codeCensus, err := ScanSQLStateLiterals("injected/address/helpers.go", src)
	if err != nil {
		t.Fatalf("разбор соседа-по-коду: %v", err)
	}
	if codeCensus.Literals == 0 {
		t.Fatal("сосед-по-коду не разобрал инъекцию: литералов ноль — его молчание беспредметно")
	}
	if len(codeSites) != 0 {
		t.Fatalf("инъекция словами задела ЧУЖОЙ предмет: сосед-по-коду нашёл %d вхождений. "+
			"Тогда красное приходило бы от него, и вакуумность нового гейта осталась бы невидимой.",
			len(codeSites))
	}

	// Сосед второй — ТЕКСТ ветки по умолчанию. Инъекция не производит кодов
	// gRPC вовсе, значит отображением не является и его предмета не касается.
	mappers, _, err := ScanStatusMappers("injected/address/helpers.go", src)
	if err != nil {
		t.Fatalf("разбор соседа-по-тексту-хвоста: %v", err)
	}
	for _, m := range mappers {
		if m.TailIsStatus && !m.TailFixed {
			t.Fatalf("инъекция словами задела ЧУЖОЙ предмет: сосед-по-хвосту нашёл %s → %s",
				m.Func, m.TailText)
		}
	}
}
