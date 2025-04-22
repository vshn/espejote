package main

import (
	"log"
	"os"

	"github.com/vshn/espejote/cmd"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: %s (bash|fish|zsh)", os.Args[0])
	}

	var err error
	switch os.Args[1] {
	case "bash":
		err = cmd.RootCmd.GenBashCompletionV2(os.Stdout, true)

	case "fish":
		err = cmd.RootCmd.GenFishCompletion(os.Stdout, true)

	case "zsh":
		err = cmd.RootCmd.GenZshCompletion(os.Stdout)

	default:
		log.Fatalf("unsupported shell %q", os.Args[1])
	}

	if err != nil {
		log.Fatal(err)
	}
}
