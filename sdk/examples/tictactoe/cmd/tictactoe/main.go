// Command tictactoe is one program that's both a terminal game and a
// Concord plugin: launched by Concord (which sets CONCORD_* variables) it
// hosts tables in the plugin's channels; run from a terminal it plays
// locally, two players or against the computer.
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"

	"github.com/JMThomas00/Concord/sdk/examples/tictactoe"
	"github.com/JMThomas00/Concord/sdk/plugin"
	"github.com/JMThomas00/Concord/sdk/table"
)

func main() {
	cfg, underConcord := plugin.ConfigFromEnv()
	if !underConcord {
		if err := table.RunLocal(tictactoe.Rules, nil); err != nil {
			log.Fatal(err)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err := plugin.Run(ctx, cfg, table.New(tictactoe.Rules).Handler())
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}
