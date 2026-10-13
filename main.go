package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Kiran-B/git-pull-all-repos/internal/app"
	"github.com/Kiran-B/git-pull-all-repos/internal/cli"
)

var version = "dev"

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	opts, err := cli.Parse(os.Args[1:], home)
	if errors.Is(err, cli.ErrHelp) {
		fmt.Fprint(os.Stdout, cli.Usage())
		os.Exit(0)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-pull-all-repos: %v\n\n%s", err, cli.Usage())
		os.Exit(2)
	}
	if opts.ShowVersion {
		fmt.Printf("git-pull-all-repos %s\n", version)
		os.Exit(0)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := app.Run(ctx, opts, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
