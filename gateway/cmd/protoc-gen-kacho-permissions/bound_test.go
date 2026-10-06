// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"encoding/json"
	"strings"
	"testing"

	authzv1 "github.com/PRO-Robotech/corelib/api/corelib/authz/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// boundOpts — аннотация формы ScopeBound (kacho#2915, замысел NTF-1 §З14):
// объект проверки — экземпляр типа object_type, к которому процесс привязал
// сервер при подъёме; запрос его не называет. Это ровно форма
// `InternalNotificationFeedService/{Claim,Ack}` из proto/corelib/notify/feed.proto.
func boundOpts(sx *authzv1.ScopeExtractor) *descriptorpb.MethodOptions {
	opts := &descriptorpb.MethodOptions{}
	proto.SetExtension(opts, authzv1.E_Permission, "platform.notification_feed.claim")
	proto.SetExtension(opts, authzv1.E_RequiredRelation, "reader")
	proto.SetExtension(opts, authzv1.E_RequiredAcrMin, "1")
	proto.SetExtension(opts, authzv1.E_ScopeExtractor, sx)
	return opts
}

// TestExtractEntry_BoundToServerIsALawfulScopeSource — bound_to_server есть
// законный ИСТОЧНИК идентификатора наравне с from_request_field: строка без
// from_request_field не «пропустила опцию», и каталог обязан нести признак,
// иначе край прочтёт пустое поле как подстановку `*`.
func TestExtractEntry_BoundToServerIsALawfulScopeSource(t *testing.T) {
	entry, warn := extractEntry("corelib.notify.InternalNotificationFeedService/Claim",
		boundOpts(&authzv1.ScopeExtractor{ObjectType: "notification_feed", BoundToServer: true}))
	if warn != "" {
		t.Fatalf("bound_to_server — законная форма источника, а вывод назвал её пропуском: %s", warn)
	}
	if !entry.ScopeExtractor.BoundToServer {
		t.Fatalf("строка каталога потеряла bound_to_server: %+v", entry.ScopeExtractor)
	}
	blob, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(blob), `"bound_to_server":true`) {
		t.Fatalf("признак не дошёл до JSON строки: %s", blob)
	}
}

// TestExtractEntry_BoundToServerKeyAbsentElsewhere — законный близнец: строка
// формы from_request_field ключа bound_to_server не несёт вовсе, и вшитый
// каталог для 349 прочих строк не меняется ни байтом.
func TestExtractEntry_BoundToServerKeyAbsentElsewhere(t *testing.T) {
	entry, warn := extractEntry("kacho.cloud.vpc.v1.NetworkService/Create",
		buildOpts(t, "vpc.networks.create", "editor", "2", "project", "project_id"))
	if warn != "" {
		t.Fatalf("unexpected warning: %s", warn)
	}
	blob, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(blob), "bound_to_server") {
		t.Fatalf("строка формы from_request_field несёт ключ bound_to_server: %s", blob)
	}
}

// TestExtractEntry_BoundToServerConflictsAreRefusedByName — у проверки один
// источник идентификатора (authz_options.proto, поле bound_to_server): вместе с
// from_request_field / object_type_from_request_field, без object_type, на
// освобождённом или scope_filtered методе — отказ с именем метода.
func TestExtractEntry_BoundToServerConflictsAreRefusedByName(t *testing.T) {
	const fqn = "corelib.notify.InternalNotificationFeedService/Claim"
	cases := []struct {
		name string
		opts *descriptorpb.MethodOptions
		want string
	}{
		{"with_from_request_field", boundOpts(&authzv1.ScopeExtractor{
			ObjectType: "notification_feed", FromRequestField: "feed_id", BoundToServer: true}), "from_request_field"},
		{"with_object_type_from_request_field", boundOpts(&authzv1.ScopeExtractor{
			ObjectType: "notification_feed", ObjectTypeFromRequestField: "kind", BoundToServer: true}), "object_type_from_request_field"},
		{"without_object_type", boundOpts(&authzv1.ScopeExtractor{BoundToServer: true}), "object_type"},
		{"exempt", func() *descriptorpb.MethodOptions {
			o := &descriptorpb.MethodOptions{}
			proto.SetExtension(o, authzv1.E_Permission, ExemptSentinel)
			proto.SetExtension(o, authzv1.E_ExemptReason, "public")
			proto.SetExtension(o, authzv1.E_ScopeExtractor, &authzv1.ScopeExtractor{
				ObjectType: "notification_feed", BoundToServer: true})
			return o
		}(), "bound_to_server"},
		{"scope_filtered", func() *descriptorpb.MethodOptions {
			o := &descriptorpb.MethodOptions{}
			proto.SetExtension(o, authzv1.E_Permission, "platform.notification_feed.claim")
			proto.SetExtension(o, authzv1.E_ScopeFiltered, true)
			proto.SetExtension(o, authzv1.E_ScopeExtractor, &authzv1.ScopeExtractor{BoundToServer: true})
			return o
		}(), "scope_extractor"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, warn := extractEntry(fqn, c.opts)
			if warn == "" {
				t.Fatalf("сочетание %s принято молча", c.name)
			}
			if !strings.Contains(warn, fqn) || !strings.Contains(warn, c.want) {
				t.Fatalf("отказ обязан назвать метод и %q, а назвал: %s", c.want, warn)
			}
		})
	}
}
