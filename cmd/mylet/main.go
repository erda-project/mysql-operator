package main

import (
	"os"

	"github.com/cxr29/log"
	v1 "github.com/erda-project/mysql-operator/api/v1"
	"github.com/erda-project/mysql-operator/pkg/mylet"
)

func main() {
	soloName := v1.PodName
	if soloName == "" {
		soloName, _ = os.Hostname()
	}
	myctlAddr := os.Getenv("MYCTL_ADDR")
	groupToken := os.Getenv("GROUP_TOKEN")

	mylet, err := mylet.Fetch(myctlAddr, soloName, groupToken)
	log.ErrFatal(err, "Fetch")

	mylet.Run()
}
