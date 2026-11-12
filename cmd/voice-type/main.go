package main

import (
	"fmt"
	"log"
	"os"

	vt "github.com/eriknovikov/voice-type"
)

var version = "dev"

func main() {
	command, err := vt.ParseCommand(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, vt.CommandUsage)
		os.Exit(2)
	}
	if command == vt.CommandVersion {
		fmt.Println(version)
		return
	}
	if command == vt.CommandHelp {
		fmt.Println(vt.CommandUsage)
		return
	}
	if command == vt.CommandDoctor {
		os.Exit(vt.RunDoctor(version, os.Stdout))
	}
	if command == vt.CommandUpdate {
		os.Exit(vt.RunUpdate(version, os.Stdout, os.Stderr))
	}
	if command == vt.CommandRemoveShortcuts {
		if err := vt.RemoveGNOMEShortcuts(); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := vt.Run(version, command); err != nil {
		log.Fatal(err)
	}
}
