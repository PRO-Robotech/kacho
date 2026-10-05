// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package render

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/PRO-Robotech/corelib/notify/form"
)

// CheckOrigin — форма origin установки: абсолютный https:// с узлом, без
// удостоверения, пути (в том числе «/»), параметров и фрагмента. Ссылка
// письма — сложение origin и пути формы form.Path, поэтому лишний «/» или
// путь в origin дали бы ссылку не туда.
func CheckOrigin(origin string) error {
	u, err := url.Parse(origin)
	switch {
	case origin == "":
		return fmt.Errorf("%w: не задан", ErrOrigin)
	case err != nil:
		return fmt.Errorf("%w: не разбирается", ErrOrigin)
	case u.Scheme != "https":
		return fmt.Errorf("%w: схема обязана быть https", ErrOrigin)
	case u.Hostname() == "":
		return fmt.Errorf("%w: нет узла", ErrOrigin)
	case u.User != nil:
		return fmt.Errorf("%w: удостоверение не допускается", ErrOrigin)
	case u.Path != "" || u.RawPath != "" || u.Opaque != "":
		return fmt.Errorf("%w: путь не допускается", ErrOrigin)
	case u.RawQuery != "" || u.ForceQuery:
		return fmt.Errorf("%w: параметры запроса не допускаются", ErrOrigin)
	case u.Fragment != "" || strings.Contains(origin, "#"):
		return fmt.Errorf("%w: фрагмент не допускается", ErrOrigin)
	}
	return nil
}

// PathLink — ссылка на путь консоли: origin + Path.Value(). Нулевой
// form.Path — ошибка с причиной form.ErrUnset, ссылки нет (голый origin не
// выдаётся). Разрешения относительного адреса нет: путь формы form.Path
// абсолютен и без «.» и «..» (CX1-18).
func PathLink(origin string, p form.Path) (string, error) {
	if err := CheckOrigin(origin); err != nil {
		return "", err
	}
	path, err := p.Value()
	if err != nil {
		return "", fmt.Errorf("render: путь ссылки: %w", err)
	}
	return origin + path, nil
}

// TokenLink — ссылка кнопки с токеном: origin + литерал пути шаблона +
// «?token=» + токен. Нулевой путь либо токен — ошибка с причиной
// form.ErrUnset, ссылки нет.
func TokenLink(origin string, path form.Path, tok form.Token) (string, error) {
	if err := CheckOrigin(origin); err != nil {
		return "", err
	}
	p, err := path.Value()
	if err != nil {
		return "", fmt.Errorf("render: путь ссылки: %w", err)
	}
	t, err := tok.Value()
	if err != nil {
		return "", fmt.Errorf("render: токен ссылки: %w", err)
	}
	return origin + p + "?token=" + url.QueryEscape(t), nil
}
