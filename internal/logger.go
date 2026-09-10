package internal

import (
	"log"

	datboxcore "github.com/datb0x/datbox-core"
)

var Logger *datboxcore.Logger

func init() {
	logger, err := datboxcore.NewLogger("discord")
	if err != nil {
		log.Fatalf("Failed to create discord logger: %v", err)
	}
	Logger = logger
}
