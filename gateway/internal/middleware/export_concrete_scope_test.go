// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

package middleware

// IsConcreteResourceScope — тот же предикат, которым край решает, спрашивает ли
// строка каталога об ОБЪЕКТЕ, названном запросом (authz_util.go). Гейт
// подстановочных отношений (`permission_catalog_wildcard_relation_test.go`)
// судит им сужение строки, а не своей копией: копия разошлась бы с краем молча.
var IsConcreteResourceScope = isConcreteResourceScope
