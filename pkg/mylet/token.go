package mylet

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	v1 "github.com/erda-project/mysql-operator/api/v1"
	"github.com/cxr29/tiny"
)

var RandId = time.Now().UnixNano() // 启动时间、冲突检测

type Token struct {
	Name       string
	GroupName  string
	Id         int
	Myctl      bool
	RandId     int
	GroupToken string
}

func PullToken(ctx *tiny.Context) *Token {
	if v, ok := ctx.Values["Token"]; ok {
		return v.(*Token)
	}
	return nil
}

func PushToken(ctx *tiny.Context) {
	s := ctx.Request.Header.Get("Token")
	t, err := ParseToken(s)
	if err != nil {
		ctx.WriteError(err.Error())
		return
	}
	ctx.SetValue("Token", &t)
}

func ParseToken(s string) (t Token, err error) {
	i := strings.IndexByte(s, ':')
	j := strings.IndexByte(s, '@')
	if i < 1 || j < 1 || j <= i {
		err = fmt.Errorf("invalid token")
		return
	}
	t.Name = s[:i]
	randId, err := strconv.ParseInt(s[i+1:j], 36, 64)
	if err != nil {
		err = fmt.Errorf("invalid rand id")
		return
	}
	t.RandId = int(randId)
	t.GroupToken = s[j+1:]

	i = strings.LastIndexByte(t.Name, '-')
	if i < 1 {
		err = fmt.Errorf("invalid name")
		return
	}
	t.GroupName = t.Name[:i]
	s = t.Name[i+1:]
	t.Myctl = s == "myctl"
	if !t.Myctl {
		t.Id, err = strconv.Atoi(s)
	}
	return
}

func SoloToken(mysql *v1.Mysql, name string) string {
	return name + ":" + strconv.FormatInt(RandId, 36) + "@" + GroupToken(mysql)
}
func GroupToken(mysql *v1.Mysql) string {
	return hex.EncodeToString(sha256.New().Sum([]byte(mysql.Spec.LocalUsername + ":" + mysql.Spec.LocalPassword + "@" + mysql.Name)))
}
