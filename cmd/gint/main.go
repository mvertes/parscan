package main

import (
	"log"
	"os"

	"github.com/gnolang/parscan/lang/golang"
	"github.com/gnolang/parscan/vm0"
)

func main() {
	log.SetFlags(log.Lshortfile)
	buf, err := os.ReadFile("/dev/stdin")
	if err != nil {
		log.Fatal(err)
	}
	if err := runSrc(string(buf)); err != nil {
		log.Fatal(err)
	}
}

func runSrc(src string) error {
	i := vm0.New(golang.GoParser)
	nodes, err := i.Parse(src)
	if err != nil {
		return err
	}
	i.Adot(nodes, os.Getenv("DOT"))
	for _, n := range nodes {
		if _, err := i.Run(n, ""); err != nil {
			return err
		}
	}
	return nil
}
