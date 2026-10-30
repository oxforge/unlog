package main

import (
	// Zone data for --tz on systems without a zone database (Windows,
	// minimal containers).
	_ "time/tzdata"

	"github.com/oxforge/unlog/internal/cmd"
)

func main() {
	cmd.Execute()
}
