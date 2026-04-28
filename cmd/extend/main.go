package main

import (
	"os"

	"github.com/extend-hq/extend-cli/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
