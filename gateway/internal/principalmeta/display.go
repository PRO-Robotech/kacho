// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package principalmeta

import (
	"net/http"

	"github.com/PRO-Robotech/kacho/pkg/grpcsrv"
)

// SetPrincipalDisplay ставит отображаемое имя принципала в ОБЕИХ заголовочных
// формах — голой и мостовой — уже закодированным для транспорта.
//
// Единственный законный писатель этого ключа на крае. Причина, по которой
// функция существует вместо двух строк на каждом пути аутентификации: имя —
// свободный ввод пользователя, а значение обычного metadata-ключа gRPC обязано
// быть печатаемым ASCII, иначе транспорт отвергает ВЕСЬ вызов, не доходя до
// обработчика (#873). Пока строк было две и стояли они в пяти местах, «здесь
// закодировали» было неотличимо от «здесь забыли»: путей аутентификации
// несколько, и правка одного из них молча не доезжала до остальных.
//
// Расшифровка — на другом конце, в единственном месте:
// `grpcsrv.DecodePrincipalDisplayName` внутри извлечения принципала. Для
// печатаемого ASCII кодирование тождественно, поэтому обычные имена едут байт
// в байт и читатель прежней сборки видит их без изменений.
func SetPrincipalDisplay(h http.Header, displayName string) {
	encoded := grpcsrv.EncodePrincipalDisplayName(displayName)
	h.Set(HeaderPrincipalDisplay, encoded)
	h.Set(HeaderGRPCMetaPrincipalDisplay, encoded)
}
