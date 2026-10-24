// Command scdeken is the sc-deken helper: it installs SuperCollider plugins
// and quarks, and is called by SuperCollider through "scdeken run".
package main

import (
	"os"

	"github.com/sc-deken/sc-deken/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
}
