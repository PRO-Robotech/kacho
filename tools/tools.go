// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

//go:build tools

// Package tools пинит версии code-generation плагинов через go.mod.
//
// Зачем. Плагины protoc-gen-* ОПРЕДЕЛЯЮТ содержимое pkg/api. Если ставить их
// `go install …@latest`, версия у разработчика и в CI разъезжается по времени, а
// гейт `generate-diff` начинает мигать: локально стабы одни, в CI — другие, и никто
// не менял ни строчки .proto. Отладка такого «фантомного диффа» стоит дороже, чем
// сама фича.
//
// Здесь плагины импортируются как обычные зависимости → `go mod tidy` фиксирует их
// версии в go.mod/go.sum. CI ставит их БЕЗ @latest:
//
//	go install google.golang.org/protobuf/cmd/protoc-gen-go
//
// — и получает ровно ту версию, что записана в go.mod. Local == CI по построению, а не
// по договорённости. Версии плагинов при этом совпадают с рантайм-библиотеками, против
// которых собирается код (protobuf v1.36.11, grpc-gateway v2.29.0) — рассинхрон
// «стабы сгенерены новее рантайма» тоже исключён.
//
// Build-tag `tools` не даёт пакету попасть в обычную сборку.
package tools

import (
	_ "github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway"
	_ "google.golang.org/grpc/cmd/protoc-gen-go-grpc"
	_ "google.golang.org/protobuf/cmd/protoc-gen-go"
)
