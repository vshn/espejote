package main

import (
	"io"
	"net/http"
	"os"

	_ "github.com/stealthrocket/net/http"
)

func main() {
	resp, err := http.Get("https://vshn.github.io/espejote-test/crumb.html")
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	defer io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		panic("Expected status code 200 OK, got " + resp.Status)
	}

	io.Copy(os.Stdout, resp.Body)
}
