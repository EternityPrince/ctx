package main

import (
	"fmt"
	"os"

	"github.com/vladimirkasterin/ctx/internal/app"
	"github.com/vladimirkasterin/ctx/internal/cli"
)

func main() {
	command, err := cli.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	if err := app.Run(command, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
