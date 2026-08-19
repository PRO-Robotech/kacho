// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package pagetoken — ЕДИНСТВЕННОЕ объявление формата курсора страницы (`page_token`)
// на всё дерево.
//
// # Зачем пакет заведён
//
// Формат токена был записан в дереве одиннадцатью различными байтовыми формами, и три
// проверки формата воспроизводили эту форму РУКОПИСНО, стоя ПЕРЕД авторитетным разбором.
// Смена формата у владельца не ломала компиляцию зеркала: оно продолжало бы принимать
// токены прежней формы и отвергать новые. Две формы к тому же совпали побайтово при
// разном СМЫСЛЕ (курсор по имени и курсор по позиции), и обе стороны принимали чужой
// токен без ошибки, отдавая не ту страницу, — молча неверный ответ вместо отказа.
//
// Поэтому здесь объявлен ровно один формат, и его разбор — единственный исполняемый.
// Проверка формата на пути запроса ОБЯЗАНА звать Decode, а не описывать его.
//
// # Форма
//
//		RawURLEncoding( "kct1" || netstring(order) || netstring(key₀) || … || netstring(keyₙ) )
//		netstring(s) = decimal(len(s)) || ":" || s
//
//	  - «kct1» — метка формата. Она делает разбор чужого или мусорного токена ОТКАЗОМ, а
//	    не случайным успехом: без метки любой валидный base64 был бы валидным курсором.
//	  - длина перед каждым полем снимает вопрос экранирования: значение ключа вправе
//	    содержать любой байт, включая разделитель. Формы с одним разделительным байтом
//	    (`:`, `|`, NUL) этим свойством не обладают и ломаются на первом же имени с ним.
//	  - order — идентификатор ПОРЯДКА, в котором курсор выдан. Курсор keyset-пагинации
//	    описывает позицию в конкретном порядке; если вызывающий сменит порядок между
//	    страницами, позиция станет описывать порядок, которого больше нет. Поэтому порядок
//	    едет В САМОМ ТОКЕНЕ, и владелец сверяет его с заказанным (см. Cursor.Order).
//
// # Что курсор НЕ обещает
//
// Токен опаковый и живёт один сеанс пагинации. Он не переносится между ревизиями
// продукта и не подлежит сохранению вызывающим: смена формата даёт INVALID_ARGUMENT,
// и вызывающий начинает обход заново. Это ровно то, что происходит сегодня с любым
// испорченным токеном, и это единственный исход, при котором страница не бывает тихо
// неверной.
package pagetoken

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// magic — метка формата. Версия внутри метки, а не рядом: разбор чужой версии обязан
// быть отказом на первом байте, а не попыткой истолковать хвост.
const magic = "kct1"

// ErrInvalid — sentinel негодного токена. Владелец маппит его в свой контрактный
// InvalidArgument; текст наружу владелец выбирает сам, причина разбора НЕ течёт
// клиенту (она бы называла внутреннюю форму).
var ErrInvalid = errors.New("page token is invalid")

// Cursor — позиция страницы в объявленном порядке.
type Cursor struct {
	// Order — идентификатор порядка, в котором курсор был выдан. Пустая строка —
	// законное значение и означает «порядок по умолчанию у этого ресурса».
	Order string

	// Keys — значения ключа сортировки в порядке следования, ЗАВЕРШАЯСЬ уникальным
	// ключом строки. Последний элемент обязан быть уникален в пределах ресурса —
	// иначе обход зациклится либо пропустит строки на совпадающих значениях.
	Keys []string
}

// Encode собирает опаковый токен. Пустой Cursor даёт пустую строку: «страницы до этой
// нет» — это отсутствие курсора, а не курсор нулевой позиции.
func Encode(c Cursor) string {
	if len(c.Keys) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(magic)
	writeNetstring(&b, c.Order)
	for _, k := range c.Keys {
		writeNetstring(&b, k)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(b.String()))
}

// Decode разбирает токен. Пустая строка — НЕ ошибка: это первая страница.
//
// Всякий иной негодный вход даёт ErrInvalid. Разбор потребляет вход ЦЕЛИКОМ: хвост
// после последнего поля — отказ, иначе токен с приписанным мусором читался бы как
// законный.
func Decode(token string) (Cursor, error) {
	if token == "" {
		return Cursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return Cursor{}, fmt.Errorf("%w: not opaque cursor encoding", ErrInvalid)
	}
	s := string(raw)
	if !strings.HasPrefix(s, magic) {
		return Cursor{}, fmt.Errorf("%w: not a cursor of this contract", ErrInvalid)
	}
	s = s[len(magic):]

	order, s, err := readNetstring(s)
	if err != nil {
		return Cursor{}, err
	}
	var keys []string
	for s != "" {
		var k string
		k, s, err = readNetstring(s)
		if err != nil {
			return Cursor{}, err
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return Cursor{}, fmt.Errorf("%w: cursor names no position", ErrInvalid)
	}
	return Cursor{Order: order, Keys: keys}, nil
}

// DecodeInOrder разбирает токен и требует, чтобы он был выдан в ЗАКАЗАННОМ порядке.
//
// Это и есть защита, ради которой порядок едет в токене: курсор keyset-пагинации
// описывает позицию в конкретном порядке, и предъявленный вместе с другим порядком он
// описывал бы позицию в порядке, которого больше нет. Тихо отдать такую страницу
// значило бы пропустить строки или вернуть их дважды, ничем этого не обозначив.
func DecodeInOrder(token, order string) (Cursor, error) {
	c, err := Decode(token)
	if err != nil {
		return Cursor{}, err
	}
	if c.Order != order {
		return Cursor{}, fmt.Errorf("%w: cursor was issued for another order", ErrInvalid)
	}
	return c, nil
}

func writeNetstring(b *strings.Builder, s string) {
	b.WriteString(strconv.Itoa(len(s)))
	b.WriteByte(':')
	b.WriteString(s)
}

func readNetstring(s string) (value, rest string, err error) {
	i := strings.IndexByte(s, ':')
	if i <= 0 {
		return "", "", fmt.Errorf("%w: cursor field has no length", ErrInvalid)
	}
	n, cerr := strconv.Atoi(s[:i])
	if cerr != nil || n < 0 {
		return "", "", fmt.Errorf("%w: cursor field length is not a number", ErrInvalid)
	}
	s = s[i+1:]
	if len(s) < n {
		return "", "", fmt.Errorf("%w: cursor field is shorter than its length", ErrInvalid)
	}
	return s[:n], s[n:], nil
}

// DefaultOrder — порядок по умолчанию: `(created_at, id)` по возрастанию. Пустая
// строка выбрана намеренно: пока у ресурса ровно один порядок, токен не несёт его
// имени, и заведение ИМЕНОВАННЫХ порядков не обесценивает ни одного уже выданного
// курсора. Именованный порядок приходит вместе со своим идентификатором.
const DefaultOrder = ""

// EncodeKeysetTime собирает курсор для самой частой формы ключа платформы —
// `(created_at, id)`. Отметка времени едет наносекундами эпохи: это единственное
// представление, у которого сравнение строк совпадает с сравнением моментов и которое
// не теряет разрядов при обходе часовых поясов.
func EncodeKeysetTime(order string, createdAt time.Time, id string) string {
	return Encode(Cursor{
		Order: order,
		Keys: []string{
			strconv.FormatInt(createdAt.UTC().UnixNano(), 10),
			id,
		},
	})
}

// DecodeKeysetTime разбирает курсор формы `(created_at, id)` и требует заказанного
// порядка. Пустой токен — первая страница: нулевой момент и пустой идентификатор.
func DecodeKeysetTime(token, order string) (createdAt time.Time, id string, err error) {
	c, err := DecodeInOrder(token, order)
	if err != nil {
		return time.Time{}, "", err
	}
	if len(c.Keys) == 0 {
		return time.Time{}, "", nil
	}
	if len(c.Keys) != 2 {
		return time.Time{}, "", fmt.Errorf("%w: cursor does not name (created_at, id)", ErrInvalid)
	}
	ns, cerr := strconv.ParseInt(c.Keys[0], 10, 64)
	if cerr != nil {
		return time.Time{}, "", fmt.Errorf("%w: cursor timestamp is not a moment", ErrInvalid)
	}
	if c.Keys[1] == "" {
		return time.Time{}, "", fmt.Errorf("%w: cursor names no row", ErrInvalid)
	}
	return time.Unix(0, ns).UTC(), c.Keys[1], nil
}
