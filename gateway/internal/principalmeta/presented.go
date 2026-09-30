// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package principalmeta

// presented.go — предъявленное ЦЕЛИКОМ: то значение, которым полоса приёма
// спрашивала службу на пути запроса, для ТОГО ЖЕ вопроса с открытого соединения
// (kacho#2900, приёмка F6b, условие 7 (а) чек-листа поверхности).
//
// # Предмет
//
// Подтверждённость адреса почты служба называет только в ответ на вопрос о
// предъявленном: о сессии — по носителю (`Resolve`), о нашем токене — сверкой по
// самому токену. Вопроса по идентификатору, который её называл бы, у службы нет.
// Перепрос открытых потоков, спрашивающий только по идентификаторам, отметки не
// видит вовсе: поток, открытый подтверждённым человеком, пережил бы снятие отметки
// до конца своего срока, хотя путь запроса отказал бы ему на следующем же
// обращении.
//
// # Почему это не новое хранилище секретов
//
// Поток держит свой запрос весь срок жизни: обработчик проекции получает его при
// открытии и возвращается, только когда поток закрыт. Носитель и токен лежат в
// заголовках этого запроса всё это время. Здесь хранится ССЫЛКА на то же значение,
// снятая полосой, которая его проверила, а не копия с новым сроком жизни.
//
// Новое — только то, что значение достижимо из реестра открытых потоков. Поэтому
// поля закрыты, а все формы печати и журнала отдают заглушку: значение выходит
// отсюда только вызывающему, который задаёт им вопрос службе.
//
// # Почему контекстом, а не заголовком
//
// Заголовок уезжает за край мостом и попадает в чужие журналы, а снятие
// удостоверения перед пересылкой (credential_strip.go) держится перечнем имён.
// Значение контекста за процесс не выходит by construction: на провод едут только
// метаданные, собранные закрытым перечнем ключей.

import (
	"context"
	"fmt"
	"io"
	"log/slog"
)

// Presented — предъявленное целиком, каким его проверила полоса приёма.
//
// Нулевое значение означает «полоса ничего не записала». Для полосы, у которой
// вопрос задаётся предъявленным, это не «спросить не о чем», а «спросить нечем»:
// решение по нему принимает перепрос, и оно закрывающее.
//
// Тип СРАВНИМ: перепрос группирует потоки по удостоверению, и две вкладки одной
// сессии обязаны попасть в одну группу, а две сессии одного человека — в разные.
type Presented struct {
	// sessionBearer — значение носителя НАШЕЙ сессии, как его передала служба
	// полоса сессии (`tryOwnSession`).
	sessionBearer string
	// token — подписанный токен, как его проверила полоса предъявителя.
	token string
	// tokenAsksOurAuthority — полоса отзыва токена: наш авторитет (сверка) либо
	// запись отзыва по идентификатору. Ставит её та же пометка записи издателя,
	// которой путь запроса выбирает вопрос.
	tokenAsksOurAuthority bool
}

// PresentedSession — предъявленное полосой нашей сессии.
func PresentedSession(bearer string) Presented {
	return Presented{sessionBearer: bearer}
}

// PresentedToken — предъявленное полосой подписанного токена. asksOurAuthority —
// та же пометка записи издателя, по которой путь запроса выбрал вопрос об отзыве.
func PresentedToken(raw string, asksOurAuthority bool) Presented {
	return Presented{token: raw, tokenAsksOurAuthority: asksOurAuthority}
}

// SessionBearer — носитель нашей сессии; пусто, если полоса сессии не записала.
func (p Presented) SessionBearer() string { return p.sessionBearer }

// Token — подписанный токен и полоса его вопроса об отзыве; recorded=false,
// если полоса предъявителя ничего не записала.
func (p Presented) Token() (raw string, asksOurAuthority, recorded bool) {
	return p.token, p.tokenAsksOurAuthority, p.token != ""
}

// presentedRedacted — то, чем предъявленное печатается в любой форме.
const presentedRedacted = "[presented credential withheld]"

// String / GoString / Format / LogValue — ни одна форма печати не несёт
// значения: ни fmt любым глаголом (включая %#v и печать удостоверения, в которое
// предъявленное вложено), ни журнал. Значение выходит только аксессорами выше.
func (p Presented) String() string   { return presentedRedacted }
func (p Presented) GoString() string { return presentedRedacted }

// Format — fmt.Formatter: перекрывает и %v с флагами, и %q.
func (p Presented) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, presentedRedacted) }

// LogValue — slog.LogValuer.
func (p Presented) LogValue() slog.Value { return slog.StringValue(presentedRedacted) }

// presentedKey — ключ контекста. Неэкспортируемый тип: подложить значение под
// этот ключ вне пакета нельзя.
type presentedKey struct{}

// WithPresented кладёт предъявленное в контекст запроса. Зовёт ТОЛЬКО полоса
// приёма, после своего положительного вердикта.
func WithPresented(ctx context.Context, p Presented) context.Context {
	return context.WithValue(ctx, presentedKey{}, p)
}

// PresentedFrom — предъявленное, записанное полосой приёма; нулевое, если не
// записано.
func PresentedFrom(ctx context.Context) Presented {
	p, _ := ctx.Value(presentedKey{}).(Presented)
	return p
}
