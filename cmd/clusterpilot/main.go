package main

import (
	"fmt"
	"os"

	"github.com/deepak-muley/k8s-cluster-pilot/internal/cli"
)

func main() {
	if err := cli.Execute(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "clusterpilot: %v\n", err)
		os.Exit(1)
	}
}
