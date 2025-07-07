package main

import (
	"os"

	v1 "github.com/erda-project/mysql-operator/api/v1"
	"github.com/erda-project/mysql-operator/pkg/mylet"
	log "github.com/sirupsen/logrus"
)

func main() {
	soloName := v1.PodName
	if soloName == "" {
		soloName, _ = os.Hostname()
	}
	myctlAddr := os.Getenv("MYCTL_ADDR")
	groupToken := os.Getenv("GROUP_TOKEN")

	mylet, err := mylet.Fetch(myctlAddr, soloName, groupToken)
	if err != nil {
		log.Fatal("Fetch", err)
	}

	mylet.Run()
}
