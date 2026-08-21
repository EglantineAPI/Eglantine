package commands

import (
	"fmt"
	"sort"
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/world"
)

// playerSource reports the player behind a command source, if it is one.
func playerSource(src cmd.Source) (*player.Player, bool) {
	p, ok := src.(*player.Player)
	return p, ok
}

// sourceName is what a command source is called in chat.
func sourceName(src cmd.Source) string {
	if n, ok := src.(interface{ Name() string }); ok {
		return n.Name()
	}
	return "Server"
}

// sayCommand is /say, broadcasting to everyone.
type sayCommand struct {
	Message cmd.Varargs `cmd:"message"`
}

func (sayCommand) Allow(src cmd.Source) bool { return operator(src) }

func (c sayCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	msg := strings.TrimSpace(string(c.Message))
	if msg == "" {
		o.Error("Say what?")
		return
	}
	_, _ = chat.Global.WriteString(fmt.Sprintf("[%s] %s", sourceName(src), msg))
}

// meCommand is /me, describing an action in the third person.
type meCommand struct {
	Action cmd.Varargs `cmd:"action"`
}

func (c meCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	action := strings.TrimSpace(string(c.Action))
	if action == "" {
		o.Error("Do what?")
		return
	}
	_, _ = chat.Global.WriteString(fmt.Sprintf("* %s %s", sourceName(src), action))
}

// tellCommand is /tell, also reachable as /msg and /w.
type tellCommand struct {
	Targets []cmd.Target `cmd:"player"`
	Message cmd.Varargs  `cmd:"message"`
}

func (c tellCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	msg := strings.TrimSpace(string(c.Message))
	if msg == "" {
		o.Error("Say what?")
		return
	}
	from := sourceName(src)
	sent := 0
	for _, t := range c.Targets {
		p, ok := t.(*player.Player)
		if !ok {
			continue
		}
		p.Messagef("%s whispers to you: %s", from, msg)
		sent++
	}
	if sent == 0 {
		o.Error("No player matched.")
		return
	}
	o.Printf("You whisper to %d player(s): %s", sent, msg)
}

// listCommand is /list, showing who is online.
type listCommand struct{}

func (listCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	if srv == nil {
		o.Error("The player list is unavailable.")
		return
	}
	count, max := srv.PlayerCount(), srv.MaxPlayerCount()
	if tx == nil {
		// Without a transaction the individual players cannot be read, but the
		// count is kept on the server itself.
		o.Printf("There are %d/%d players online.", count, max)
		return
	}
	var names []string
	for p := range srv.Players(tx) {
		names = append(names, p.Name())
	}
	o.Printf("There are %d/%d players online: %s", count, max, strings.Join(names, ", "))
}

// kickCommand is /kick.
type kickCommand struct {
	Targets []cmd.Target              `cmd:"player"`
	Reason  cmd.Optional[cmd.Varargs] `cmd:"reason"`
}

func (kickCommand) Allow(src cmd.Source) bool { return operator(src) }

func (c kickCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	reason := strings.TrimSpace(string(c.Reason.LoadOr("Kicked by an operator")))
	for _, t := range c.Targets {
		p, ok := t.(*player.Player)
		if !ok {
			continue
		}
		name := p.Name()
		p.Disconnect(reason)
		o.Printf("Kicked %s: %s", name, reason)
	}
}

// transferCommand is /transfer, sending a player to another server.
type transferCommand struct {
	Targets []cmd.Target `cmd:"player"`
	Address string       `cmd:"address"`
}

func (transferCommand) Allow(src cmd.Source) bool { return operator(src) }

func (c transferCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	for _, t := range c.Targets {
		p, ok := t.(*player.Player)
		if !ok {
			continue
		}
		if err := p.Transfer(c.Address); err != nil {
			o.Errorf("Could not transfer %s: %v", p.Name(), err)
			continue
		}
		o.Printf("Transferred %s to %s.", p.Name(), c.Address)
	}
}

// stopCommand is /stop, shutting the server down.
type stopCommand struct{}

func (stopCommand) Allow(src cmd.Source) bool { return operator(src) }

func (stopCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	if stopServer == nil {
		o.Error("This server cannot be stopped from a command.")
		return
	}
	o.Print("Stopping the server.")
	// Shutting down closes every world, which cannot happen from inside a
	// world transaction, so it is handed off.
	go stopServer()
}

// helpCommand is /help, listing what the source may run.
type helpCommand struct{}

func (helpCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	unique := map[string]cmd.Command{}
	for _, command := range cmd.Commands() {
		unique[command.Name()] = command
	}
	names := make([]string, 0, len(unique))
	for name, command := range unique {
		// Runnables reports only the overloads this source may run, so an
		// empty result means the command is hidden from them, as in vanilla.
		if len(command.Runnables(src)) == 0 {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	o.Printf("Commands (%d):", len(names))
	for _, name := range names {
		o.Printf("  /%s - %s", name, unique[name].Description())
	}
}
