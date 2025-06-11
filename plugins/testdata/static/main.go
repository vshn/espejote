package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stdout, "Stdout args:", os.Args[1:])
	fmt.Fprintln(os.Stderr, "Stderr args:", os.Args[1:])
}
