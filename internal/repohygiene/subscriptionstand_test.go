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
// ВЫБОР НАЧАЛА стоит здесь ветвлением, как в настоящей форме, и это не
// украшение: позиция общей формы лежит ВНУТРИ `oneof`, поэтому фикстура без него
// не предъявляла бы разбору тела ту работу, ради которой он написан, — и ветвь
// состава на стенде не наблюдала бы ничего.
const standCommonForm = `syntax = "proto3";
package kacho.cloud.subscription;
// Здесь могло бы стоять слово message { в прозе — и оно стоит.
message SubscriptionRequest {
  repeated string kinds = 1;
  string project_id = 2;
  repeated string ids = 3;
  oneof start {
    string anchor = 10;
    string position = 11;
  }
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

// ── содержимое стенда: ось «признак — свойство, а не имя» (задача #1072) ─────

// standForeignNamedSubscription — ДЕФЕКТ ветви состава: запрос подписки под
// именем, которого в семействе имён нет. Имя автор нового домена выбирает сам, и
// `WatchNetworksRequest` ничем не хуже прочих; гейт, судивший по имени,
// пропускал такое молча.
const standForeignNamedSubscription = `syntax = "proto3";
package kacho.cloud.demo.v1;
message WatchNetworksRequest {
  repeated string kinds = 1;
  string project_id = 2;
  int64 from_sequence_no = 3;
}
`

// standTailEventsRequest — тот же дефект под третьим именем: ни `Watch`, ни
// `Subscribe`, ни суффикса `SubscriptionRequest` в имени нет вовсе.
const standTailEventsRequest = `syntax = "proto3";
package kacho.cloud.demo.v1;
message TailEventsRequest {
  repeated string resource_types = 1;
  string checkpoint = 2;
}
`

// standFeedRequest — и под четвёртым, максимально далёким от семейства.
const standFeedRequest = `syntax = "proto3";
package kacho.cloud.demo.v1;
message FeedRequest {
  repeated string event_kinds = 1;
  string resume_token = 2;
  string project_id = 3;
}
`

// standPagedListTwin — ЗАКОННЫЙ БЛИЗНЕЦ ветви состава, и он выбран САМЫМ
// ТРУДНЫМ из возможных: страничный список несёт И ось видов, И поле позиции.
// Разделяет их ровно одно — РАЗМЕР СТРАНИЦЫ, то есть утверждение о конечной
// выдаче. Гейт, ключующийся на «позиция рядом с осью», здесь покраснеет.
//
// Форма не выдумана: `ListChangedLimitsRequest` (`internal_limit_service.proto`)
// — тот же силуэт, непрозрачный курсор плюс размер страницы. Побайтовой копией
// она здесь не является намеренно: у близнеца своя ось видов и своё имя поля
// позиции, иначе проба утверждала бы про одну подстановку, а не про свойство.
const standPagedListTwin = `syntax = "proto3";
package kacho.cloud.other.v1;
message ListThingsRequest {
  repeated string kinds = 1;
  string project_id = 2;
  string page_token = 3;
  int64 page_size = 4;
}
message ListThingsResponse {
  repeated string ids = 1;
  string next_page_token = 2;
}
`

// standStreamingVerbOverPlainName — ДЕФЕКТ ветви употребления: сообщение не
// названо по семейству и состава подписки не несёт, но стоит ВХОДОМ
// серверно-потокового глагола. Поток и есть подписка: одиночный ответ так не
// объявляют.
const standStreamingVerbOverPlainName = `syntax = "proto3";
package kacho.cloud.demo.v1;
message DemoFeedInput {
  string project_id = 1;
}
message DemoFeedChunk {
  string payload = 1;
}
service DemoFeedService {
  rpc Follow(DemoFeedInput) returns (stream DemoFeedChunk);
}
`

// standClientStreamingTwin — ЗАКОННЫЙ БЛИЗНЕЦ ветви употребления: слово `stream`
// в глаголе СТОИТ, но на стороне ЗАПРОСА. Это выгрузка, а не подписка, и гейт,
// ищущий подстроку `stream` в объявлении глагола, здесь покраснеет.
const standClientStreamingTwin = `syntax = "proto3";
package kacho.cloud.other.v1;
message UploadChunkRequest {
  bytes chunk = 1;
}
message UploadResult {
  string id = 1;
}
service UploadService {
  rpc Upload(stream UploadChunkRequest) returns (UploadResult);
}
`

// standNameOnlyRequest — ДЕФЕКТ ветви ИМЕНИ и только её: имя из семейства, а
// состава подписки нет (ни оси видов, ни позиции). Нужен, чтобы доказать, что
// прежняя ветвь жива: без него её молчание неотличимо от мёртвой.
const standNameOnlyRequest = `syntax = "proto3";
package kacho.cloud.demo.v1;
message SubscribeRequest {
  string project_id = 1;
}
`

// standNestedShapeIsNotADeclaration — ЗАКОННЫЙ БЛИЗНЕЦ разбора тела: состав
// подписки лежит во ВЛОЖЕННОМ сообщении, а владелец его не несёт. Гейт,
// читающий поля построчно вместо разбора тела, засчитает вложенные поля
// владельцу и покраснеет.
const standNestedShapeIsNotADeclaration = `syntax = "proto3";
package kacho.cloud.other.v1;
message ThingHolder {
  string id = 1;
  message InnerFilter {
    repeated string kinds = 1;
    string position = 2;
  }
  InnerFilter filter = 2;
}
`
