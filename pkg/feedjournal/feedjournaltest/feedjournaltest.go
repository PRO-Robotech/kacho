// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: Apache-2.0

// Package feedjournaltest — двойник службы доступа для проб видимости строки
// сигнала ленты в потоке подписки модуля (kacho#2918, NTF-3, SA-3042-01).
//
// # Что двойник воспроизводит
//
// Ровно то свойство модели прав, на котором стоит предмет: у типа
// `notification_feed` объявлено ОДНО отношение — `reader`, и держит его
// `service:notify` (`type notification_feed { define reader: [service] }`,
// fga_model.fga службы доступа). Вопрос с отношением, которого у типа нет,
// служба доступа отказом не считает: план вопроса не строится, ответ —
// «неизвестно», и `AuthorizeService/BatchCheck` отвечает ошибкой на ВСЮ
// партию. Двойник делает то же — `UNAVAILABLE` на партию, назвав тип и
// отношение, — иначе проба зеленела бы на модуле, чей сужатель спрашивает
// о ленте чужое отношение и рвёт поток подписки.
//
// Вопросы о прочих типах двойник отвергает «нет» без ошибки: предмет проб —
// лента, а не ресурсные виды модуля.
package feedjournaltest

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/url"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/PRO-Robotech/corelib/authz"
	"github.com/PRO-Robotech/corelib/grpcsrv"
	"github.com/PRO-Robotech/corelib/notify/feed"
	iamv1 "github.com/PRO-Robotech/kaname/pkg/api/kaname/cloud/iam/v1"
)

// NotifySubject — субъект модели, держащий отношение `reader` на ленте.
const NotifySubject = "service:notify"

// notifySAN — SPIFFE-идентификатор, который звено опознания служб переводит в
// имя `notify`. Значение синтетическое и о домене доверия установки ничего не
// утверждает: домен `.invalid` не резолвится by construction (RFC 2606), а
// живёт идентификатор только в таблице звена этой пробы.
var notifySAN = (&url.URL{Scheme: "spiffe", Host: "feedjournaltest.invalid", Path: "/sa/notify"}).String()

// feedRelations — отношения типа ленты в модели службы доступа.
var feedRelations = map[string]bool{"reader": true}

// Kaname — соединение с двойником службы доступа: годится всякому, кто строит
// сужатель поверх `grpc.ClientConnInterface` (narrowiam).
func Kaname() grpc.ClientConnInterface { return kanameConn{} }

type kanameConn struct{}

func (kanameConn) Invoke(_ context.Context, method string, args, reply any, _ ...grpc.CallOption) error {
	if method != iamv1.AuthorizeService_BatchCheck_FullMethodName {
		return status.Errorf(codes.Unimplemented, "двойник службы доступа: метод %s не воспроизводится", method)
	}
	in, ok := args.(*iamv1.BatchAuthorizeCheckRequest)
	if !ok {
		return status.Errorf(codes.Internal, "двойник службы доступа: запрос %T", args)
	}
	out, ok := reply.(*iamv1.BatchAuthorizeCheckResponse)
	if !ok {
		return status.Errorf(codes.Internal, "двойник службы доступа: ответ %T", reply)
	}
	for _, c := range in.GetChecks() {
		allowed := false
		if c.GetResource().GetType() == string(feed.FeedObjectType) {
			rel := c.GetRequiredRelation()
			if !feedRelations[rel] {
				return status.Errorf(codes.Unavailable,
					"у типа %q нет отношения %q — ответ «неизвестно» на всю партию", feed.FeedObjectType, rel)
			}
			allowed = c.GetSubject() == NotifySubject
		}
		out.Responses = append(out.Responses, &iamv1.AuthorizeCheckResponse{Allowed: allowed})
	}
	return nil
}

func (kanameConn) NewStream(context.Context, *grpc.StreamDesc, string, ...grpc.CallOption) (grpc.ClientStream, error) {
	return nil, status.Error(codes.Unimplemented, "двойник службы доступа: потоков нет")
}

// NotifyCaller — контекст вызова от службы `notify`, опознанной звеном
// опознания служб по сертификату пира, — тем же путём, каким её опознаёт
// внутренний слушатель модуля.
func NotifyCaller(t testing.TB) context.Context {
	t.Helper()
	const method = "/corelib.subscription.InternalSubscriptionService/Subscribe"
	id, err := grpcsrv.NewServiceIdentity([]string{method}, map[string]grpcsrv.ServiceName{notifySAN: "notify"})
	if err != nil {
		t.Fatalf("ФИКСТУРА: звено опознания служб: %v", err)
	}
	u, err := url.Parse(notifySAN)
	if err != nil {
		t.Fatalf("ФИКСТУРА: SAN: %v", err)
	}
	leaf := &x509.Certificate{URIs: []*url.URL{u}}
	in := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credentials.TLSInfo{
		State: tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{leaf}}},
	}})
	var out context.Context
	if _, err := id.Unary()(in, nil, &grpc.UnaryServerInfo{FullMethod: method},
		func(c context.Context, _ any) (any, error) { out = c; return nil, nil }); err != nil {
		t.Fatalf("ФИКСТУРА: звено опознания служб отказало: %v", err)
	}
	if c, ok := authz.CallerSubject(out); !ok || c.Subject() != NotifySubject {
		t.Fatalf("ФИКСТУРА: звено не опознало службу notify (%+v, %v)", c, ok)
	}
	return out
}
