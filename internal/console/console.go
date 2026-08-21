// Package console runs a command terminal on standard input.
package console

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/go-gl/mathgl/mgl64"
)

// Console is a cmd.Source that reads commands from standard input and prints
// their output to standard output. It is always allowed to run operator
// commands: reaching it already means having the server process.
type Console struct {
	log  *slog.Logger
	out  io.Writer
	stop func()

	// once guards stop, so a second "stop" while shutdown is under way does not
	// start a second shutdown.
	once sync.Once
}

// New creates a Console. stop is called when the operator asks the server to
// shut down, and when standard input reaches end of file.
func New(log *slog.Logger, stop func()) *Console {
	return &Console{log: log, out: os.Stdout, stop: stop}
}

// Position implements cmd.Target. The console is not in a world, so it sits at
// the origin; commands that need a real position must be given a player.
func (*Console) Position() mgl64.Vec3 { return mgl64.Vec3{} }

// Name implements cmd.NamedTarget.
func (*Console) Name() string { return "Console" }

// SendCommandOutput implements cmd.Source, printing what a command produced.
func (c *Console) SendCommandOutput(o *cmd.Output) {
	for _, e := range o.Errors() {
		fmt.Fprintf(c.out, "! %s\n", e.Error())
	}
	for _, m := range o.Messages() {
		fmt.Fprintf(c.out, "  %s\n", m.String())
	}
}

// Run reads commands until standard input is exhausted. It blocks, so callers
// normally run it in its own goroutine.
//
// Reaching end of file triggers the stop callback. That covers the operator
// pressing Ctrl-D, and it also covers a container started without an attached
// terminal, where stdin is closed immediately and a reader loop would otherwise
// spin.
func (c *Console) Run() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		c.Execute(line)
	}
	if err := scanner.Err(); err != nil {
		c.log.Error("Console input failed.", "error", err)
	}
	c.shutdown()
}

// Execute runs a single command line, with or without a leading slash.
func (c *Console) Execute(line string) {
	line = strings.TrimPrefix(strings.TrimSpace(line), "/")
	if line == "" {
		return
	}
	name, args, _ := strings.Cut(line, " ")

	switch strings.ToLower(name) {
	case "quit", "exit":
		fmt.Fprintln(c.out, "Stopping the server...")
		c.shutdown()
		return
	}
	// help and stop are ordinary registered commands, so they fall through to
	// the dispatch below and there is only one implementation of each.

	command, ok := cmd.ByAlias(strings.ToLower(name))
	if !ok {
		fmt.Fprintf(c.out, "! Unknown command %q. Type help for the list.\n", name)
		return
	}
	// The console belongs to no world, so the transaction is nil. Dragonfly
	// documents this and Runnables are expected to handle it.
	command.Execute(args, c, nil)
}

func (c *Console) shutdown() {
	c.once.Do(func() {
		if c.stop != nil {
			c.stop()
		}
	})
}
