package main

import (
	desktop "github.com/eriknovikov/voice-type/desktop"
	"log"
	"os"
)

var version = "dev"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--remove-shortcuts" {
		if err := desktop.RemoveGNOMEShortcuts(); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "--shortcut-action" {
		if err := desktop.RunShortcutAction(os.Args[2]); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := desktop.Run(version); err != nil {
		log.Fatal(err)
	}
}
