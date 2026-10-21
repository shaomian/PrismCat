//go:build !windows

package main

import (
	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/server"
)

func platformRun(srv *server.Server, _ *config.Config, _ bool) error {
	return srv.Start()
}
