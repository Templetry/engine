// Command templetry is the CLI for the Templetry engine.
package main

import (
	"fmt"
	"os"
)

const version = "0.0.1-dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("templetry", version)
		return
	}
	fmt.Println("templetry — project scaffolding for every platform, delivered to any forge")
	fmt.Println("Engine skeleton; commands arrive with Phase 1. See https://github.com/Templetry/wiki")
}
