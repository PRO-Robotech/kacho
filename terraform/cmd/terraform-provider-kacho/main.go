// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Двоичный файл Terraform-провайдера Kachō.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/PRO-Robotech/kacho/terraform/internal/provider"
)

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "запустить с поддержкой отладчика")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New, providerserver.ServeOpts{
		Address: "registry.terraform.io/PRO-Robotech/kacho",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
