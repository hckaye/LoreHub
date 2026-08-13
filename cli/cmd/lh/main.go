package main

import "github.com/lorehub/lorehub/cli/internal/cmdutil"

var version = "dev"

func main() {
	cmdutil.Execute(version)
}
