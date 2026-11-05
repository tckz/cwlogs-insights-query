package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/samber/lo"
)

var version string

type subcommand struct {
	name string
	desc string
	run  func(ctx context.Context, args []string) error
}

var subcommands = []subcommand{
	{name: "run", desc: "execute a query, then wait and output results as JSON lines", run: cmdRun},
	{name: "put-definition", desc: "create or update a saved query definition from yaml", run: cmdPutDefinition},
	{name: "diff-definition", desc: "show diff between yaml and the saved query definition", run: cmdDiffDefinition},
	{name: "version", desc: "show version", run: cmdVersion},
}

// exitError carries an explicit exit code. err may be nil when nothing should be logged.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err == nil {
		return fmt.Sprintf("exit %d", e.code)
	}
	return e.err.Error()
}

func (e *exitError) Unwrap() error { return e.err }

func progName() string {
	return filepath.Base(os.Args[0])
}

func usage(w io.Writer) {
	fmt.Fprintf(w, "Usage: %s <command> [flags]\n\nCommands:\n", progName())
	for _, c := range subcommands {
		fmt.Fprintf(w, "  %-16s %s\n", c.name, c.desc)
	}
	fmt.Fprintf(w, "\nRun '%s <command> -h' for details of each command.\n", progName())
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: %s %s [flags]\n\nFlags:\n", progName(), name)
		fs.PrintDefaults()
	}
	return fs
}

func loadAWSConfig(ctx context.Context) (aws.Config, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(os.Getenv("AWS_REGION")))
	if err != nil {
		return aws.Config{}, fmt.Errorf("config.LoadDefaultConfig: %w", err)
	}
	return cfg, nil
}

func cmdVersion(_ context.Context, _ []string) error {
	if version != "" {
		fmt.Println(version)
	} else if info, ok := debug.ReadBuildInfo(); ok {
		fmt.Println(info.Main.Version)
	}
	return nil
}

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}

	name := os.Args[1]
	switch name {
	case "-h", "--help", "help":
		usage(os.Stdout)
		return
	case "-version", "--version":
		name = "version"
	}

	cmd, ok := lo.Find(subcommands, func(c subcommand) bool { return c.name == name })
	if !ok {
		fmt.Fprintf(os.Stderr, "*** unknown command: %s\n\n", name)
		usage(os.Stderr)
		os.Exit(2)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	err := cmd.run(ctx, os.Args[2:])
	cancel()
	if err == nil {
		return
	}

	var ee *exitError
	if errors.As(err, &ee) {
		if ee.err != nil {
			log.Printf("*** %v", ee.err)
		}
		os.Exit(ee.code)
	}
	log.Fatalf("*** %v", err)
}
