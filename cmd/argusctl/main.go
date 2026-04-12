package main

import (
	"fmt"
	"os"

	"github.com/btopcu/argus/cmd/argusctl/cmd"
)

var (
	version   = "dev"
	gitSHA    = "unknown"
	buildTime = "unknown"
)

func main() {
	cmd.Version = version
	cmd.GitSHA = gitSHA
	cmd.BuildTime = buildTime

	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
