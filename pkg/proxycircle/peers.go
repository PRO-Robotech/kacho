// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

package proxycircle

import (
	"fmt"
	"net/netip"
	"strings"
)

// ParsePeers разбирает ЗВЕНЬЯ ФРОНТА ПОИМЁННО (kacho#3028, круг 3): имена
// безголовых служб через запятую, чьи поды край признаёт доверенными звеньями.
//
// Сеть круга (Parse) выделяет «под кластера», а не «раздачу консоли»: сеть
// подов общая. Звено сужается до адресов подов, которые выбирает служба
// фронта, — край разрешает её имя в эти адреса. Одно суждение на двух
// читателей: край разбирает им ручку на старте, гейт рендера — значение,
// которое ручке выписывает каждая цепочка.
//
// Законно: пустой ввод (звеньев нет) и имена по RFC 1123 — метки из строчных
// латинских букв, цифр и дефиса, не с дефиса и не на дефис, до 63 знаков,
// через точку, всего до 253 знаков. Повтор имени читается один раз, порядок
// объявления сохраняется.
//
// Отказ: адрес вместо имени (он «разрешается» сам в себя, и звеном стал бы
// любой под по этому адресу — сужение обходится) и всё, что именем службы
// быть не может, включая абсолютное имя с точкой в конце: имя разрешается в
// пространстве имён края, а не от корня.
func ParsePeers(raw string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, item := range strings.Split(raw, ",") {
		name := strings.TrimSpace(item)
		if name == "" {
			continue
		}
		if _, err := netip.ParseAddr(name); err == nil {
			return nil, fmt.Errorf("звено %q — адрес, а не имя службы фронта: адрес «разрешается» сам в себя, "+
				"и звеном стал бы любой под по нему", name)
		}
		if err := checkServiceName(name); err != nil {
			return nil, fmt.Errorf("звено %q — не имя службы: %v", name, err)
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out, nil
}

func checkServiceName(name string) error {
	if len(name) > 253 {
		return fmt.Errorf("длина %d больше 253", len(name))
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" {
			return fmt.Errorf("пустая метка")
		}
		if len(label) > 63 {
			return fmt.Errorf("метка длиной %d больше 63", len(label))
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("метка %q начинается или кончается дефисом", label)
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return fmt.Errorf("знак %q вне строчных латинских букв, цифр и дефиса", c)
			}
		}
	}
	return nil
}
