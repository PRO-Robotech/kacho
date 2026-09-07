// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Package authzfilter — словарь kacho-storage для общего сужателя списков
// (`pkg/listnarrow`): типы объектов модели прав, аудит-строки действий и предикат
// членства страницы.
//
// Механики сужения здесь БОЛЬШЕ НЕТ. Она жила в четырёх почти дословных копиях с
// побайтово одинаковыми бюджетами и различием только в словаре ресурсов — и
// разошлась ровно там, где расхождение не видно: в полярности ответа безымянному
// вызывающему и в том, что означает «модель не провязана». Копии сведены в один дом;
// здесь остаётся то, что у сервиса действительно СВОЁ.
package authzfilter

import (
	"time"

	"google.golang.org/grpc"

	"github.com/PRO-Robotech/kacho/pkg/listnarrow"
	"github.com/PRO-Robotech/kacho/pkg/listnarrow/narrowiam"
)

// Narrower — сужатель списочной страницы kacho-storage.
type Narrower = listnarrow.Narrower

// Config — посадка сужателя без предиката: предикат у сервиса ОДИН и объявлен
// PageRelations, поэтому композиционный корень его не выбирает и не может ошибиться.
type Config struct {
	// Timeout — срок одного запроса к kaname.
	Timeout time.Duration
	// CacheTTL — окно жизни положительного вердикта.
	CacheTTL time.Duration
	// CacheMaxEntries — предел размера окна.
	CacheMaxEntries int
	// SoftPassOnPeerFailure — задокументированный мягкий проход на отказе соседа.
	SoftPassOnPeerFailure bool
	// Breakglass — аварийный режим: страница отдаётся несуженной. Каждое
	// срабатывание считается и называется (см. listnarrow.Counts).
	Breakglass bool
}

// New собирает сужатель поверх соединения с kaname. conn == nil означает, что
// спросить негде: без аварийного режима такой сужатель ОТКАЗЫВАЕТ.
func New(conn grpc.ClientConnInterface, cfg Config) *Narrower {
	var cli listnarrow.AuthorizeClient
	if conn != nil {
		cli = narrowiam.New(conn)
	}
	return listnarrow.New(cli, listnarrow.Config{
		Relations:             PageRelations,
		Timeout:               cfg.Timeout,
		CacheTTL:              cfg.CacheTTL,
		CacheMaxEntries:       cfg.CacheMaxEntries,
		SoftPassOnPeerFailure: cfg.SoftPassOnPeerFailure,
		Breakglass:            cfg.Breakglass,
	})
}
