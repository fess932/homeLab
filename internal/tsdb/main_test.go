package tsdb

import (
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestMain(m *testing.M) {
	switch strings.TrimSuffix(filepath.Base(os.Args[0]), filepath.Ext(os.Args[0])) {
	case "vm-exit":
		os.Exit(1)
	case "vm-wait":
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		os.Exit(0)
	}
	os.Exit(m.Run())
}
