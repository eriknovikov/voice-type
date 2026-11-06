package main

import (
	"fmt"
	desktop "github.com/eriknovikov/voice-type/desktop"
	"log"
	"os"
)

var version = "dev"

func main() {
	command, err := desktop.ParseCommand(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, desktop.CommandUsage)
		os.Exit(2)
	}
	if command == desktop.CommandHelp {
		fmt.Println(desktop.CommandUsage)
		return
	}
	if command == desktop.CommandRemoveShortcuts {
		if err := desktop.RemoveGNOMEShortcuts(); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := desktop.Run(version, command); err != nil {
		log.Fatal(err)
	}
}
