// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package repohygiene

import (
	"os"
	"path/filepath"
	"testing"
)

// subscriptionstand_test.go — ИНЪЕКЦИОННЫЙ СТЕНД единой формы подписки.
//
// # Зачем он отдельным файлом
//
// Гейт, написанный ПОСЛЕ контракта, проверяет написанное и потому зелен по
// построению. Его способность упасть доказывает не дерево, а стенд: дефект сюда
// вносится по-настоящему, законный близнец ставится рядом, и обе стороны
// предъявляются до того, как гейт признан работающим.
//
// # Изоляция
//
// Дерево строится в `t.TempDir()` обычной записью файлов. Ни `git init`, ни
// `git add`, ни `git config` здесь нет и быть не должно: анализаторы читают
// файловую систему, а запись в индекс репозитория, из которого идёт прогон,
// делает лживыми ВСЕ гейты, читающие дерево, — испорченный индекс они
// добросовестно читают как «ноль файлов».
//
// # Что стенд умеет
//
// Он параметризован СОДЕРЖИМЫМ файлов, а не набором заранее заготовленных
// случаев. Поэтому осям, чьи гейты пишутся следующими фазами (закрытый перечень
// осей, различимость трёх состояний позиции, тип с нулём импортёров), новый
// харнесс не нужен — им нужен свой анализатор и свои две стороны на этом же
// стенде. Заготовки под них здесь НЕ лежат: фикстура без анализатора, который её
// судит, ничего не доказывает и остаётся мёртвой.

// subscriptionStand материализует временное дерево контракта: ключ — путь
// относительно корня, значение — содержимое файла.
func subscriptionStand(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

func subscriptionStandOptions(root string, allow ...SubscriptionRequestAllowance) SubscriptionSingularityOptions {
	return SubscriptionSingularityOptions{Root: root, ProtoRoot: "proto", Allow: allow}
}

// ── содержимое стенда ───────────────────────────────────────────────────────

// standCommonForm — общая форма: та, что объявляется один раз.
//
// Комментарий внутри содержит слово `message` и открывающую скобку намеренно:
// анализатор, считающий по сырому тексту, засчитает их за объявление и собьёт
// глубину вложенности на весь остаток файла.
const standCommonForm = `syntax = "proto3";
package kacho.cloud.subscription;
// Здесь могло бы стоять слово message { в прозе — и оно стоит.
message SubscriptionRequest {
  repeated string kinds = 1;
  string project_id = 2;
  repeated string ids = 3;
  message NestedWatchRequest {
    string ignored = 1;
  }
}
`

// standDomainOwnRequest — ДЕФЕКТ оси «единственность»: домен завёл свой запрос
// подписки со своим набором осей.
const standDomainOwnRequest = `syntax = "proto3";
package kacho.cloud.demo.v1;
message WatchRequest {
  repeated string kinds = 1;
  int64 from_sequence_no = 2;
}
`

// standDomainImportsCommon — ЗАКОННЫЙ БЛИЗНЕЦ той же оси: ТОТ ЖЕ домен, тот же
// путь файла, но общий тип ИМПОРТИРУЕТСЯ, а не переобъявляется.
//
// Близость нарочная: гейт, ключующийся на имени файла, на имени домена или на
// самом факте упоминания подписки, здесь покраснеет.
const standDomainImportsCommon = `syntax = "proto3";
package kacho.cloud.demo.v1;
import "kacho/cloud/subscription/subscription.proto";
message DemoSubscribeWiring {
  kacho.cloud.subscription.SubscriptionRequest request = 1;
}
`

// standFiller — обычный доменный контракт: он не про подписку вовсе и обязан
// быть невидим для этого гейта. Нужен, чтобы обход был непуст даже там, где
// подписки нет.
const standFiller = `syntax = "proto3";
package kacho.cloud.other.v1;
message GetThingRequest {
  string id = 1;
}
message Thing {
  string id = 1;
}
`
