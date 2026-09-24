// klog-ls is a language server for klog, the plain-text time tracking format.
// It uses klog's own parser, so it reports exactly what the klog CLI reports.
package main

import (
	"fmt"
	"log"
	"os"
	"time"
)

// version is set at build time: -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() {
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--version", "-v":
			fmt.Println("klog-ls", version)
			return
		case "--stdio":
			// Communicating over stdio is the only mode, but some clients pass it.
		default:
			fmt.Fprintf(os.Stderr, "klog-ls: unknown argument %q\n", arg)
			os.Exit(2)
		}
	}

	log.SetFlags(0)
	log.SetPrefix("klog-ls: ")
	s := newServer(os.Stdin, os.Stdout, time.Now)
	if err := s.run(); err != nil {
		log.Fatal(err)
	}
	os.Exit(s.exitCode())
}
