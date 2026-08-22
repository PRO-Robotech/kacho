// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// token_acceptance.go — ОБЪЯВЛЕННЫЕ записи приёма токена на крае: «издатель →
// источник его набора проверочных ключей».
//
// # Издатель — МНОЖЕСТВО, и у каждого СВОЯ запись
//
// Платформа чеканит свои токены сама, а прежний издатель на переходе остаётся.
// Принять двух издателей, имея ОДИН набор ключей, значило бы разрешить ключу
// одного проверять токен другого — то есть отменить ровно ту защиту, ради
// которой развязка и делается. Поэтому у каждого принимаемого издателя своя
// запись: свой адрес набора, свой снимок, свой срок годности, своё окно
// вынужденного перезапроса.
//
// # Адрес ОБЪЯВЛЯЕТСЯ и НИКОГДА не выводится из издателя
//
// Издатель приходит от предъявителя — это недоверенный вход. Кроме прямого
// вреда (значение от предъявителя управляет тем, куда мы ходим), производный
// адрес получался бы у ВСЯКОГО издателя, и состояние «записи нет» не наступало
// бы никогда: страж старта остался бы в тексте, не имея возможности упасть.
//
// # Нумерация конфигураций — назначена здесь и больше нигде
//
// Построений проверяющего в дереве ДВА, и они нумеруются так:
//
//   - ПЕРВАЯ — плоскость данных реестра (`services/registry/internal/clients/jwks`),
//     переведена фазой Ф1 (задача #897);
//   - ВТОРАЯ — КРАЙ, вот это построение, переведено Ф1б (задача #926).
//
// Порядок — хронологический, по фазам перевода, и никакой другой. Он назван
// здесь потому, что «первая»/«вторая» встречается в профилях развёртывания, в
// пробах и в приёмке: пока референт не закреплён одним местом, оператор и
// инженер читают одни и те же слова про разные предметы. Это уже расходилось.
//
// Решение принято приёмкой Ф1 §2.8 и здесь исполняется на второй конфигурации.
package middleware

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/PRO-Robotech/kacho/pkg/tokenpolicy"
)

const (
	// PlatformTokenType — тип токена доступа НАШЕЙ чеканки (RFC 9068).
	// Значение берётся из общего объявления в `pkg/tokenpolicy`, а не пишется
	// здесь заново.
	PlatformTokenType = tokenpolicy.TokenTypeAccess
	// LegacyTokenType — тип, которым помечает свои токены прежний издатель.
	LegacyTokenType = "JWT"
	// ProofTokenType — тип доказательства владения (RFC 9449). Токеном
	// ДОСТУПА не является ни при каком издателе, поэтому объявить его
	// принимаемым нельзя: это было бы доказательством, выданным за
	// удостоверение.
	ProofTokenType = "dpop+jwt"
)

// IssuerKeySet — ОБЪЯВЛЕННАЯ запись приёма одного издателя.
type IssuerKeySet struct {
	// Issuer — точное значение `iss`, которое обязан объявить токен. Служит
	// ТОЛЬКО ключом поиска в таблице записей: ни частью адреса, ни частью
	// ключа кэша, ни частью текста, уходящего наружу.
	Issuer string
	// KeySetURL — объявленный адрес набора проверочных ключей ЭТОГО издателя.
	KeySetURL string
	// TokenTypes — закрытый набор значений заголовка `typ`, которые эта полоса
	// принимает. Непустой: незаданный набор означал бы «любой тип», а один
	// подписант, обслуживающий два контура, делает путаницу типов настоящей
	// возможностью.
	//
	// Набор, а не единственное значение, — по измеренной причине: производитель
	// типа на полосе прежнего издателя не мы, и дерево называет ДВА значения,
	// которые он ставит. Единственное значение здесь означало бы отказ каждому
	// запросу той полосы, на которой мы форму заголовка не выбираем.
	TokenTypes []string
	// TolerateAbsentTokenType — принимать токен ЭТОЙ полосы, если заголовок
	// типа не несёт вовсе. НЕСОВПАДАЮЩИЙ тип отвергается всё равно.
	//
	// Послабление выдано ровно полосе прежнего издателя и по названной причине:
	// его токены чеканим не мы, форму заголовка диктует он, и потребовать от
	// неё того, чего мы у него не проверяли, значило бы поставить работу
	// живого контура на непроверенное допущение о третьей стороне. Защиты
	// строгость здесь не добавляет — подпись, издатель, адресат и привязка
	// ключа уже отвергли бы чужой токен.
	//
	// На НАШЕЙ полосе послабления нет и быть не может: производитель типа — мы
	// сами. ПРЕДИКАТ СНЯТИЯ: послабление уходит вместе с записью прежнего
	// издателя.
	TolerateAbsentTokenType bool
	// ReadRevocation — спрашивать авторитет отзыва на предъявлении токена
	// этого издателя. Полоса прежнего издателя своего поведения не меняет.
	ReadRevocation bool
}

// issuerRecord — состояние ОДНОЙ записи: свой снимок набора, свой срок
// годности, своё окно перезапроса. Раздельность здесь не про
// производительность: общий снимок означал бы, что ключ одного издателя
// проверяет токен другого.
type issuerRecord struct {
	issuer         string
	keySetURL      string
	tokenTypes     []string
	tolerateNoTyp  bool
	readRevocation bool
	jwks           *JWKSCache
}

// acceptsTokenType отвечает, принимает ли полоса объявленный заголовком тип.
//
// ОТСУТСТВИЕ типа и НЕСОВПАДЕНИЕ типа — разные вещи, и различает их только эта
// функция. Несовпадение отвергается всегда; отсутствие — всегда, кроме полосы,
// которой послабление выдано явно и с предикатом снятия.
func (r *issuerRecord) acceptsTokenType(typ string) bool {
	if typ == "" {
		return r.tolerateNoTyp
	}
	for _, want := range r.tokenTypes {
		// Сравнение регистронезависимо: `typ` — медиа-тип, а они
		// регистронезависимы по RFC.
		if strings.EqualFold(typ, want) {
			return true
		}
	}
	return false
}

// ErrNoIssuerRecord — объявленный токеном издатель не имеет записи источника.
//
// Ошибка НЕ несёт самого издателя: он приходит от предъявителя, и уносить его
// дословно в текст, уходящий наружу, значило бы пустить недоверенный вход в
// журнал и в ответ.
var ErrNoIssuerRecord = errors.New("token issuer has no declared key-set record")

// normaliseIssuerKeySets приводит объявленные записи к состоянию, с которым
// проверяющий вообще имеет право работать, и отвергает всё прочее.
//
// Отказ вместо старта, если: записей нет · у записи пуст издатель, адрес или
// набор типов · издатель объявлен дважды · адрес не абсолютен · полоса
// объявляет принимаемым тип доказательства владения. Каждое из этих состояний
// при пустом значении означает «не сужаем», а «не сужаем» на проверке
// подлинности — это «принимаем любого».
func normaliseIssuerKeySets(sources []IssuerKeySet) (map[string]*issuerRecord, error) {
	if len(sources) == 0 {
		return nil, errors.New("jwt verifier: at least one declared issuer key-set record is required " +
			"(an empty issuer set means «accept any issuer»)")
	}
	out := make(map[string]*issuerRecord, len(sources))
	for _, s := range sources {
		issuer := strings.TrimSpace(s.Issuer)
		if issuer == "" {
			return nil, errors.New("jwt verifier: key-set record with an empty issuer")
		}
		if _, dup := out[issuer]; dup {
			return nil, fmt.Errorf("jwt verifier: issuer %q is declared twice — "+
				"one issuer, one key-set record", issuer)
		}
		keySetURL := strings.TrimSpace(s.KeySetURL)
		if err := absoluteKeySetURL(keySetURL); err != nil {
			return nil, fmt.Errorf("jwt verifier: key-set record for issuer %q: %w", issuer, err)
		}
		types := make([]string, 0, len(s.TokenTypes))
		for _, raw := range s.TokenTypes {
			typ := strings.TrimSpace(raw)
			if typ == "" {
				continue
			}
			if strings.EqualFold(typ, ProofTokenType) {
				return nil, fmt.Errorf("jwt verifier: issuer %q declares %q as an accepted access-token "+
					"type — that media type is a proof of possession, not a credential, and accepting it "+
					"would let a proof be presented as the token it proves", issuer, ProofTokenType)
			}
			types = append(types, typ)
		}
		if len(types) == 0 {
			return nil, fmt.Errorf("jwt verifier: issuer %q declares no expected token type "+
				"(an unset type means «any type»)", issuer)
		}
		out[issuer] = &issuerRecord{
			issuer:         issuer,
			keySetURL:      keySetURL,
			tokenTypes:     types,
			tolerateNoTyp:  s.TolerateAbsentTokenType,
			readRevocation: s.ReadRevocation,
		}
	}
	return out, nil
}

// absoluteKeySetURL отвергает адрес, который источником не является.
//
// «Только разделители» (`/`, `//`, `///`) — самый коварный вид вырожденного
// значения: строка непуста, глазом читается как путь, а адресом не является.
func absoluteKeySetURL(raw string) error {
	if raw == "" {
		return errors.New("key-set URL is empty (an unset source is not a declared source)")
	}
	if strings.Trim(raw, "/ \t") == "" {
		return fmt.Errorf("key-set URL %q consists of separators only", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("key-set URL %q is not a URL: %w", raw, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("key-set URL %q is not absolute (scheme and host are required; "+
			"a relative path cannot be a declared source)", raw)
	}
	return nil
}

// keyIDWellFormed ограничивает форму идентификатора ключа ДО его использования.
//
// Идентификатор приходит от предъявителя. Он попадает в поиск по снимку набора,
// в текст журнала и в повод вынужденного перезапроса, поэтому его форма
// ограничивается прежде, чем он куда-либо попадёт, — а не после.
//
// Словарь тот же, что у формы идентификатора ключа на стороне чеканки: буквы,
// цифры, точка, подчёркивание, двоеточие, дефис; длина 1..128.
func keyIDWellFormed(kid string) bool {
	if kid == "" || len(kid) > tokenpolicy.KeyIDMaxLen {
		return false
	}
	for _, r := range kid {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == ':' || r == '-':
		default:
			return false
		}
	}
	return true
}
