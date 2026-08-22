// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package restmux

import (
	"net/http"
	"testing"

	"github.com/PRO-Robotech/kacho/gateway/internal/principalmeta"
)

// TestPrincipalKeyHasExactlyOneProducer — каждый ключ принципала приезжает в
// metadata РОВНО ОДИН раз, при любой форме входного заголовка.
//
// # Предмет
//
// Производителей было два: аннотатор собирает metadata из заголовка сам, и
// сверх того мост REST→gRPC переносит ОБЕ формы того же заголовка. На живом
// пути каждый ключ уезжал трижды.
//
// Дефекта поведения это не давало — потребитель читает первую копию, а копии
// совпадали. Цена в другом: равенство копий держалось не проверкой, а
// совпадением источника, и переставало держаться в тот день, когда источников
// стало два. Расхождение при этом было бы ненаблюдаемым.
//
// # Что утверждается
//
// Кратность И сохранность: проба на «не больше одного» зеленела бы и на нуле,
// то есть на потерянной личности. Поэтому каждое утверждение — пара.
func TestPrincipalKeyHasExactlyOneProducer(t *testing.T) {
	// Обе формы заголовка ставятся ОДНОВРЕМЕННО — так их и ставит слой
	// аутентификации. Проба на одной форме объявила бы снятие единственного
	// работавшего пути успехом.
	both := []struct{ bare, bridged string }{
		{principalmeta.HeaderPrincipalType, principalmeta.HeaderGRPCMetaPrincipalType},
		{principalmeta.HeaderPrincipalID, principalmeta.HeaderGRPCMetaPrincipalID},
		{principalmeta.HeaderPrincipalDisplay, principalmeta.HeaderGRPCMetaPrincipalDisplay},
		{principalmeta.HeaderTokenACR, principalmeta.HeaderGRPCMetaTokenACR},
	}

	for _, form := range []string{"обе формы", "только голая", "только мостовая"} {
		t.Run(form, func(t *testing.T) {
			r, err := http.NewRequest(http.MethodGet, "/vpc/v1/networks", nil)
			if err != nil {
				t.Fatalf("запрос: %v", err)
			}
			for _, h := range both {
				switch form {
				case "обе формы":
					r.Header.Set(h.bare, "V")
					r.Header.Set(h.bridged, "V")
				case "только голая":
					r.Header.Set(h.bare, "V")
				case "только мостовая":
					r.Header.Set(h.bridged, "V")
				}
			}

			md := buildPrincipalMetadata(r)

			// (1) Аннотатор кладёт каждый ключ ровно один раз — и кладёт.
			want := []string{
				principalmeta.MetaPrincipalType,
				principalmeta.MetaPrincipalID,
				principalmeta.MetaPrincipalDisplayBin,
				principalmeta.MetaTokenACR,
			}
			for _, k := range want {
				switch n := len(md.Get(k)); n {
				case 1: // норма
				case 0:
					t.Errorf("%s: значение ПОТЕРЯНО — личность не доедет до сервиса", k)
				default:
					t.Errorf("%s: копий %d, ожидалась одна — у значения больше одного производителя", k, n)
				}
			}

			// (2) Мост тот же ключ НЕ пропускает — иначе к копии аннотатора
			// добавятся ещё две, по одной на форму заголовка.
			for _, h := range both {
				for _, key := range []string{h.bare, h.bridged} {
					if name, ok := principalHeaderMatcher(key); ok {
						t.Errorf("мост пропустил %s как %q — значение поедет второй копией", key, name)
					}
				}
			}
		})
	}

	// (3) Положительный контроль моста: ключ закрытого набора, которого
	// аннотатор НЕ кладёт, мост обязан пропускать — он его единственный
	// производитель. Без этого утверждения правка выглядела бы верной и
	// молча теряла бы срок и область действия удостоверения.
	for _, key := range []string{"X-Kacho-Token-Jti", "Grpc-Metadata-X-Kacho-Token-Scope"} {
		name, ok := principalHeaderMatcher(key)
		if !ok || name == "" {
			t.Errorf("мост отбросил %s — у этого ключа других производителей нет, значение потеряно", key)
		}
	}

	// (4) Чужое в зарезервированном пространстве по-прежнему не проходит:
	// сужение моста не должно было ослабить защиту от подлога личности.
	for _, key := range []string{"X-Kacho-Admin", "Grpc-Metadata-X-Kacho-Project-Id"} {
		if _, ok := principalHeaderMatcher(key); ok {
			t.Errorf("мост пропустил %s — это подлог, а не наш ключ", key)
		}
	}
}
