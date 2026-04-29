package main

import (
	"fmt"
	"os"

	"github.com/fgmacedo/buchecha/cmd"
)

func main() {
	err := cmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if cmd.ExitCode == 0 {
			cmd.ExitCode = 1
		}
	}
	os.Exit(cmd.ExitCode)
}
