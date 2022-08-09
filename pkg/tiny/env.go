package tiny

import "strings"

var env string

func Env() string {
	return env
}

func SetEnv(s string) {
	env = strings.ToLower(s)
}

func Prod() bool {
	switch env {
	case "prod", "production":
		return true
	}
	return false
}

func Test() bool {
	switch env {
	case "test", "testing":
		return true
	}
	return false
}

func Dev() bool {
	switch env {
	case "dev", "development":
		return true
	}
	return false
}
