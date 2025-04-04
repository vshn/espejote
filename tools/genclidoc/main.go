package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra/doc"
	"github.com/vshn/espejote/cmd"
)

const shellESC = "\x1b"

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Usage: genclidoc <path>")
		os.Exit(1)
	}
	path := os.Args[1]

	fmt.Fprintln(os.Stderr, "Generating CLI docs in", path)

	os.RemoveAll(path)
	if err := os.MkdirAll(path, 0755); err != nil {
		panic(err)
	}

	if err := doc.GenMarkdownTree(cmd.RootCmd, path); err != nil {
		panic(err)
	}

	files, err := filepath.Glob(filepath.Join(path, "*"))
	if err != nil {
		panic(err)
	}

	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			panic(err)
		}
		f, err := os.OpenFile(file, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
		if err != nil {
			panic(err)
		}

		for k, v := range replacements {
			raw = bytes.ReplaceAll(raw, []byte(k), []byte(v))
		}

		if bytes.Contains(raw, []byte(shellESC)) {
			fmt.Fprintf(os.Stderr, "ERROR: Unreplaced shell escape character in %s. Update the replacements.\n", file)
			os.Exit(1)
		}

		if _, err := f.Write(raw); err != nil {
			panic(err)
		}

		if err := f.Close(); err != nil {
			panic(err)
		}
	}

	fmt.Fprintln(os.Stderr, "CLI doc files generated in", path)
}

var replacements = map[string]string{
	shellESC + "[33;1m": "<span style=\"color:yellow\">",
	shellESC + "[44;1m": "<span style=\"background-color:blue\">",
	shellESC + "[1m":    "<span style=\"font-weight:bold\">",

	shellESC + "[0;22m": "</span>",
	shellESC + "[22m":   "</span>",
}
