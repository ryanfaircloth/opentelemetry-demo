// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Docker healthcheck binary: a plain TCP dial against the HTTP health port.
// This service's distroless image has no shell or HTTP client tooling, so a
// tiny static binary is used instead of the CMD-SHELL checks other services
// use.
package main

import (
	"net"
	"os"
	"time"
)

func main() {
	port := os.Getenv("PRODUCT_CATALOG_HEALTH_PORT")
	if port == "" {
		port = "8081"
	}

	conn, err := net.DialTimeout("tcp", "localhost:"+port, 2*time.Second)
	if err != nil {
		os.Exit(1)
	}
	conn.Close()
}
