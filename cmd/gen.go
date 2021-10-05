package main

import (
	"fmt"
	"github.com/gospodinzerkalo/gocodegen/parser"
	"os"
)

func main() {
	p, err := parser.NewParser(os.Getenv("GOFILE"))
	if err != nil {
		fmt.Println(err)
		return
	}

	if err := p.Parse(); err != nil {
		fmt.Println(err)
		return
	}
}
