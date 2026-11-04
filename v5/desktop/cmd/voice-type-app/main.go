package main

import (
	desktop "github.com/eriknovikov/voice-type/desktop"
	"log"
)

var version = "dev"

func main() {
	if err := desktop.Run(version); err != nil {
		log.Fatal(err)
	}
}
