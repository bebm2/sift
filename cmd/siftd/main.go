// Command siftd runs the local Sift control-plane daemon.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/miaoxiaoyong/sift/internal/config"
	"github.com/miaoxiaoyong/sift/internal/controlplane"
)

func main() {
	home, err := config.ResolveHome()
	if err != nil {
		fatal(err)
	}
	if _, err := config.Load(home, time.Now()); err != nil {
		fatal(err)
	}
	s, err := controlplane.Start(home)
	if err != nil {
		fatal(err)
	}
	defer s.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := s.Serve(ctx); err != nil {
		fatal(err)
	}
}
func fatal(err error) { fmt.Fprintln(os.Stderr, "siftd:", err); os.Exit(1) }
