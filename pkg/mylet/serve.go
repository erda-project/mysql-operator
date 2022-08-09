package mylet

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cxr29/log"
	"github.com/cxr29/tiny"
	"github.com/cxr29/tiny/alog"
	v1 "github.com/erda-project/mysql-operator/api/v1"
)

func Fetch(myctlAddr, soloName, groupToken string) (*Mylet, error) {
	if myctlAddr == "" {
		return nil, fmt.Errorf("myctl addr required")
	}

	i := strings.LastIndexByte(soloName, '-')
	if i < 1 {
		return nil, fmt.Errorf("invalid solo name: %s", soloName)
	}
	id, err := strconv.Atoi(soloName[i+1:])
	if err != nil || id < 0 {
		return nil, fmt.Errorf("invalid solo name: %s", soloName)
	}

	if groupToken == "" {
		return nil, fmt.Errorf("group token required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), Timeout5s)
	defer cancel()

	u := url.URL{
		Scheme: "http",
		Host:   myctlAddr,
		Path:   "/api/addons/myctl/" + v1.Namespace + "/mysql",
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Token", soloName+":"+strconv.FormatInt(RandId, 36)+"@"+groupToken)

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status code %d, body: %s", res.StatusCode, string(b))
	}

	var v struct {
		Data  *v1.Mysql
		Error interface{}
	}

	err = json.Unmarshal(b, &v)
	if err != nil {
		return nil, err
	}

	if v.Error != nil || v.Data == nil {
		return nil, fmt.Errorf("return error: %s", v.Error)
	}

	mylet := &Mylet{
		Mysql:      v.Data,
		MysqlSolo:  v.Data.Status.Solos[id],
		ExitChan:   make(chan struct{}, 1),
		SwitchChan: make(chan int, 1),
	}

	return mylet, nil
}

func (mylet *Mylet) Run() {
	err := mylet.Configure()
	log.ErrFatal(err, "Configure")

	dir := mylet.DataDir()
	empty, err := IsEmpty(dir)
	log.ErrFatal(err, "IsEmpty")
	if empty {
		if mylet.Spec.Id == 0 {
			err = mylet.Initialize()
			log.ErrFatal(err, "Initialize")
		} else {
			err = mylet.FetchAndPrepare()
			log.ErrFatal(err, "FetchAndPrepare")
		}
	}

	go func() {
		log.ErrError(mylet.Start(), "Start")
		syscall.Kill(syscall.Getpid(), syscall.SIGINT)
		time.Sleep(time.Second)
		log.Infoln("Exit")
		os.Exit(0)
	}()

	tiny.Group("/api/addons/mylet", mylet._Route)

	Serve()

	mylet.ExitChan <- struct{}{}
	time.Sleep(time.Second)
}

func (mylet *Mylet) _Route(r *tiny.Router) {
	//TODO
	r.GET("/post/start", func(ctx *tiny.Context) {
	})
	r.GET("/pre/stop", func(ctx *tiny.Context) {
		mylet.ExitChan <- struct{}{}
	})

	r.Group("/probe", func(r *tiny.Router) {
		r.Use(alog.Off)
		r.GET("/startup", func(ctx *tiny.Context) {
			if mylet.StartupProbe {
				ctx.WriteString("startup")
			} else {
				ctx.ServiceUnavailable()
			}
		})
		r.GET("/liveness", func(ctx *tiny.Context) {
			if mylet.LivenessProbe {
				ctx.WriteString("liveness")
			} else {
				ctx.ServiceUnavailable()
			}
		})
		r.GET("/readiness", func(ctx *tiny.Context) {
			if mylet.ReadinessProbe {
				ctx.WriteString("readiness")
			} else {
				ctx.ServiceUnavailable()
			}
		})
	})

	r.GET("/switch/primary/<id:int>", mylet._SwitchPrimary)
	r.GET("/download/backup", mylet._DownloadBackup)
	r.POST("/backup", mylet._Backup)

	r.Group("", func(r *tiny.Router) {
		r.Use(PushToken, mylet._ValidateToken)
		r.POST("/user-db", mylet._User_DB)
		r.POST("/run-sql", mylet._Run_SQL)
	})
}

func (mylet *Mylet) _ValidateToken(ctx *tiny.Context) {
	t := PullToken(ctx)
	if GroupToken(mylet.Mysql) != t.GroupToken {
		ctx.WriteError("token forbidden")
		return
	}
}
func (mylet *Mylet) _DownloadBackup(ctx *tiny.Context) {
	t, err := ParseToken(ctx.Request.Header.Get("Token"))
	if err != nil || mylet == nil || t.GroupToken != GroupToken(mylet.Mysql) {
		ctx.Forbidden()
		return
	}

	s, n := ctx.First("datetime")
	if n != 1 {
		ctx.BadRequest()
		return
	}

	var f string

	if s == "replication" {
		a, err := mylet.GetCompresses()
		log.ErrError(err, "get compresses")
		if err != nil {
			ctx.InternalServerError()
			return
		}

		i := len(a) - 1
		if i == -1 || time.Since(a[i]) > Day {
			a, err = mylet.GetBackups()
			log.ErrError(err, "get backups")
			if err != nil {
				ctx.InternalServerError()
				return
			}

			i = len(a) - 1
			if i == -1 || time.Since(a[i]) > Day {
				_, err = mylet.FullBackup()
				log.ErrError(err, "full backup")
				if err != nil {
					ctx.InternalServerError()
					return
				}
			}

			f, err = mylet.CompressLastBackup()
			log.ErrError(err, "compress last backup")
			if err != nil {
				ctx.InternalServerError()
				return
			}
		} else {
			f = mylet.GetBackupDir(a[i]) + CompressExt
		}
	} else {
		t, err := time.ParseInLocation("20060102.150405", s, time.Local)
		if err != nil {
			ctx.BadRequest()
			return
		}

		d := mylet.GetBackupDir(t)
		f = d + CompressExt

		fi, err := os.Stat(f)
		if os.IsNotExist(err) {
			fi, err = os.Stat(d)
			if os.IsNotExist(err) || !fi.IsDir() {
				ctx.NotFound()
				return
			}

			_, _, err = ReadBackupInfo(d)
			if err != nil {
				ctx.NotFound()
				return
			}

			f, err = mylet.CompressBackup(d)
			log.ErrError(err, "compress backup")
			if err != nil {
				ctx.InternalServerError()
				return
			}
		} else if fi.IsDir() {
			ctx.NotFound()
			return
		}
	}

	ctx.ContentDisposition(filepath.Base(f), "")
	ctx.ServeFile(f)
}

func (mylet *Mylet) _Backup(ctx *tiny.Context) {
	t, err := ParseToken(ctx.Request.Header.Get("Token"))
	if err != nil || mylet == nil || t.GroupToken != GroupToken(mylet.Mysql) {
		ctx.Forbidden()
		return
	}

	incremental, n := ctx.FirstBool("incremental")
	if n != 0 && n != 1 {
		ctx.BadRequest()
		return
	}
	compress, n := ctx.FirstBool("compress")
	if n != 0 && n != 1 {
		ctx.BadRequest()
		return
	}

	var bt time.Time
	var inc int

	if incremental {
		a, err := mylet.GetBackups()
		if err != nil {
			log.Errorln("get backups", err)
			ctx.WriteError(err)
			return
		}

		if i := len(a) - 1; i == -1 {
			bt, err = mylet.FullBackup()
			if err != nil {
				log.Errorln("full backup", err)
				ctx.WriteError(err)
				return
			}
		} else {
			bt = a[i]
		}

		inc, err = mylet.IncrementalBackup(bt)
		if err != nil {
			log.Errorln("incremental backup", filepath.Base(mylet.GetBackupDir(bt)), err)
			ctx.WriteError(err)
			return
		}
	} else {
		bt, err = mylet.FullBackup()
		if err != nil {
			log.Errorln("full backup", err)
			ctx.WriteError(err)
			return
		}
	}

	if compress {
		d := mylet.GetBackupDir(bt)
		_, err = mylet.CompressBackup(d)
		if err != nil {
			log.Errorln("compress backup", filepath.Base(d), err)
			ctx.WriteError(err)
			return
		}
	}

	ctx.WriteData(tiny.M{
		"BackupTime":  bt.Format(DatetimeLayout),
		"Incremental": inc,
		"Compress":    compress,
	})
}

func (mylet *Mylet) _SwitchPrimary(ctx *tiny.Context) {
	t, err := ParseToken(ctx.Request.Header.Get("Token"))
	if err != nil || mylet == nil || t.GroupToken != GroupToken(mylet.Mysql) || !t.Myctl {
		ctx.Forbidden()
		return
	}

	newId, ok := ctx.ParamInt("id")
	if !ok || newId < 0 || newId >= mylet.Mysql.Spec.Size() {
		ctx.BadRequest()
		return
	}

	if newId != *mylet.Mysql.Spec.PrimaryId {
		mylet.SwitchChan <- newId
	}
	ctx.WriteData(newId)
}
