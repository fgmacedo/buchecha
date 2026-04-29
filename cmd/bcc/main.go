package main

import (
	"fmt"
	"os"

	"github.com/fgmacedo/buchecha/internal/cli"
)

func main() {
	err := cli.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if cli.ExitCode == 0 {
			cli.ExitCode = 1
		}
	}
	os.Exit(cli.ExitCode)
}
