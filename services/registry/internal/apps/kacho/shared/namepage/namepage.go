// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package namepage — единый horizontal-хелпер cursor-пагинации по имени (ASC) для
// output-only проекций zot. Общий источник для transport-слоя (handler) и
// zot-адаптера, чтобы курсор был БАЙТ-совместим между слоями: адаптер режет окно у
// источника (bound per-request fan-out к zot/iam — CWE-770), handler довершает
// authz-фильтр окна. Курсор — opaque base64 последнего имени; невалидный →
// ErrInvalidArg-sentinel (маппинг в gRPC — на границе serviceerr/mapErr).
package namepage

import (
	"fmt"
	"strconv"

	"github.com/PRO-Robotech/kacho/pkg/pagetoken"
	corevalidate "github.com/PRO-Robotech/kacho/pkg/validate"

	regerrors "github.com/PRO-Robotech/kacho/services/registry/internal/errors"
)

// Window режет отсортированный по имени (ASC) срез по (pageSize, pageToken).
// Возвращает страницу + next-token ("" если больше нет). pageSize вне [0..1000] →
// InvalidArgument (corevalidate.PageSize; 0 → default 50); garbage token →
// ErrInvalidArg-sentinel. keyOf извлекает имя-ключ элемента.
func Window[T any](items []T, keyOf func(T) string, pageSize int64, pageToken string) ([]T, string, error) {
	size, err := corevalidate.PageSize("page_size", pageSize)
	if err != nil {
		return nil, "", err
	}
	start := 0
	if pageToken != "" {
		tok, derr := Decode(pageToken)
		if derr != nil {
			return nil, "", fmt.Errorf("%w: invalid page_token", regerrors.ErrInvalidArg)
		}
		for start < len(items) && keyOf(items[start]) <= tok {
			start++
		}
	}
	if start >= len(items) {
		return nil, "", nil
	}
	end := start + int(size)
	if end >= len(items) {
		return items[start:], "", nil
	}
	page := items[start:end]
	return page, Encode(keyOf(page[len(page)-1])), nil
}

// WindowByOffset режет отсортированный срез по ОПАКОВОМУ offset-курсору (позиция, а не
// имя элемента). Для проекций, где per-item authz-фильтр применяется ПОСЛЕ окна
// (ListRepositories: per-repo v_list в handler'е): name-курсор echo'ил бы имя
// отфильтрованного (скрытого) элемента → existence-oracle. Offset ничего не именует и
// доводит пагинацию до всех разрешённых элементов даже через полностью-скрытые окна.
// pageSize вне [0..1000] → InvalidArgument; garbage token → ErrInvalidArg-sentinel.
// items ожидается детерминированно отсортированным (позиция стабильна между вызовами).
func WindowByOffset[T any](items []T, pageSize int64, pageToken string) ([]T, string, error) {
	size, err := corevalidate.PageSize("page_size", pageSize)
	if err != nil {
		return nil, "", err
	}
	start := 0
	if pageToken != "" {
		off, derr := decodeOffset(pageToken)
		if derr != nil || off < 0 {
			return nil, "", fmt.Errorf("%w: invalid page_token", regerrors.ErrInvalidArg)
		}
		start = off
	}
	if start >= len(items) {
		return nil, "", nil
	}
	end := start + int(size)
	if end >= len(items) {
		return items[start:], "", nil
	}
	return items[start:end], encodeOffset(end), nil
}

// Курсоры этого пакета разного СМЫСЛА — по имени и по позиции — прежде кодировали
// голую строку одной и той же кодировкой. Тег с цифровым именем давал токен,
// БАЙТ-В-БАЙТ равный offset-курсору (`Encode("5") == EncodeOffset(5) == "NQ=="`), и оба
// окна принимали чужой токен без ошибки, отдавая не ту страницу. Цифровые имена тегов —
// обычный случай, поэтому это был не теоретический, а рабочий отказ.
//
// Теперь оба курсора собирает общий кодек `pkg/pagetoken`, и смысл едет В ТОКЕНЕ
// отдельным полем порядка: разбор чужого курсора — отказ, а не случайный успех.
// Разбор мусора тоже стал отказом: прежде любой валидный base64 был валидным «именем».
const (
	orderByName   = "name asc"
	orderByOffset = "offset"
)

// Encode кодирует имя в опаковый курсор.
func Encode(name string) string {
	return pagetoken.Encode(pagetoken.Cursor{Order: orderByName, Keys: []string{name}})
}

// Decode разбирает опаковый курсор в имя. Курсор позиции и мусор — отказ.
func Decode(token string) (string, error) {
	c, err := pagetoken.DecodeInOrder(token, orderByName)
	if err != nil {
		return "", err
	}
	if len(c.Keys) != 1 {
		return "", fmt.Errorf("%w: name cursor names no single key", pagetoken.ErrInvalid)
	}
	return c.Keys[0], nil
}

// EncodeOffset/DecodeOffset — опаковый offset-курсор для источников, которые режут окно
// У СЕБЯ (движок с server-side пагинацией), а не отдают весь набор в WindowByOffset.
// Позиция, а не имя: name-курсор эхо'ил бы имя отфильтрованного (скрытого) элемента →
// existence-oracle.
func EncodeOffset(offset int) string { return encodeOffset(offset) }

// DecodeOffset разбирает опаковый offset-курсор (пустой → 0).
func DecodeOffset(token string) (int, error) {
	if token == "" {
		return 0, nil
	}
	return decodeOffset(token)
}

func encodeOffset(offset int) string {
	return pagetoken.Encode(pagetoken.Cursor{
		Order: orderByOffset,
		Keys:  []string{strconv.Itoa(offset)},
	})
}

// decodeOffset разбирает опаковый offset-курсор обратно в позицию. Курсор имени и
// мусор — отказ.
func decodeOffset(token string) (int, error) {
	c, err := pagetoken.DecodeInOrder(token, orderByOffset)
	if err != nil {
		return 0, err
	}
	if len(c.Keys) != 1 {
		return 0, fmt.Errorf("%w: offset cursor names no single key", pagetoken.ErrInvalid)
	}
	n, cerr := strconv.Atoi(c.Keys[0])
	if cerr != nil {
		return 0, fmt.Errorf("%w: offset cursor is not a position", pagetoken.ErrInvalid)
	}
	return n, nil
}
