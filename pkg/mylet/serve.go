package mylet

import (
	"context"
	"database/sql"
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

	"github.com/cxr29/tiny"
	"github.com/cxr29/tiny/alog"
	v1 "github.com/erda-project/mysql-operator/api/v1"
	log "github.com/sirupsen/logrus"
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

	// 调试：打印获取到的 spec 和 status
	log.Infof("[Fetch] Got Spec: %+v", v.Data.Spec)
	log.Infof("[Fetch] Got Status: %+v", v.Data.Status)

	mysqlSolo := v.Data.Status.Solos[id]

	mylet := &Mylet{
		Mysql:      v.Data,
		MysqlSolo:  v.Data.Status.Solos[id],
		ExitChan:   make(chan struct{}, 1),
		SwitchChan: make(chan int, 1),
		Spec:       &mysqlSolo.Spec,
		hangCount:  0, // Initialize hangCount
	}

	restartLimitStr := os.Getenv("MYSQL_RESTART_LIMIT")
	if restartLimit, err := strconv.Atoi(restartLimitStr); err == nil && restartLimit > 0 {
		mylet.RestartLimit = restartLimit
	} else {
		// default restart limit
		mylet.RestartLimit = 5
	}

	return mylet, nil
}

// reapZombiesLoop 定期回收所有 defunct（僵尸）子进程，防止进程表堆积僵尸进程。
// 这是极端情况下的兜底措施，正常情况下 Go runtime 会自动 wait 掉子进程。
func reapZombiesLoop() {
	for {
		var ws syscall.WaitStatus
		var ru syscall.Rusage
		// -1 表示回收任意子进程，WNOHANG 表示非阻塞
		pid, err := syscall.Wait4(-1, &ws, syscall.WNOHANG, &ru)
		for pid > 0 {
			log.Warnf("reaped zombie process: pid=%d, status=%v", pid, ws)
			pid, err = syscall.Wait4(-1, &ws, syscall.WNOHANG, &ru)
		}
		// ECHILD 表示当前没有可回收的子进程
		if err != nil && err != syscall.ECHILD {
			log.Warnf("wait4 error: %v", err)
		}
		// 每 5 秒检查一次
		time.Sleep(5 * time.Second)
	}
}

// CollectLocalStatus 采集本地 MySQL 实例的状态，组装为 MysqlSoloStatus
// 注意：id 只在启动/Fetch 阶段解析一次，后续直接用 mylet.Spec.Id
func (mylet *Mylet) CollectLocalStatus() v1.MysqlSoloStatus {
	status := v1.MysqlSoloStatus{}
	id := mylet.Spec.Id // id 直接取自 Spec
	dsn := fmt.Sprintf("%s:%s%d@tcp(localhost:%d)/mysql",
		mylet.Mysql.Spec.LocalUsername, mylet.Mysql.Spec.LocalPassword, id, mylet.Spec.Port)
	db, err := sql.Open("mysql", dsn)
	if err == nil {
		defer db.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		err = db.PingContext(ctx)
		if err == nil {
			status.Color = v1.Green
			mylet.hangCount = 0 // 探测成功，重置 hang
		} else {
			status.Color = v1.Red
			mylet.hangCount++
		}
	} else {
		status.Color = v1.Red
		mylet.hangCount++
	}
	// 不再设置 hang 字段
	log.Infof("[CollectLocalStatus] id=%d, dsn=%s, color=%s", id, dsn, status.Color)
	return status
}

// 采集本地状态并组装上报结构体，id 直接用 mylet.Spec.Id
func (mylet *Mylet) CollectReport() *MysqlReport {
	localStatus := mylet.CollectLocalStatus()
	id := mylet.Spec.Id // id 直接取自 Spec
	state := struct {
		FromId int    `json:"fromId"`
		ToId   int    `json:"toId"`
		Color  string `json:"color"`
	}{
		FromId: id,
		ToId:   id,
		Color:  localStatus.Color,
	}
	stateJson, _ := json.Marshal(state)
	mr := &MysqlReport{
		Name:     mylet.Spec.Name,
		SizeSpec: NewSizeSpec(mylet.Mysql),
		States:   []json.RawMessage{stateJson},
	}
	log.Infof("[CollectReport] Name=%s, id=%d, color=%s, SizeSpec=%+v", mr.Name, id, localStatus.Color, mr.SizeSpec)
	return mr
}

func (mylet *Mylet) Run() {
	// 启动僵尸进程回收协程，防止 mysqld 被 kill -9 后变成 defunct
	go reapZombiesLoop()
	log.Info("reapZombiesLoop started")

	err := mylet.Configure()
	if err != nil {
		log.Fatal("Configure", err)
	}

	dir := mylet.DataDir()
	empty, err := IsEmpty(dir)
	if err != nil {
		log.Fatal("IsEmpty", err)
	}
	if empty {
		if mylet.Spec.Id == 0 {
			err = mylet.Initialize()
			if err != nil {
				log.Fatal("Initialize", err)
			}
		} else {
			err = mylet.FetchAndPrepare()
			if err != nil {
				log.Fatal("FetchAndPrepare", err)
			}
		}
	}

	// 定时上报 goroutine
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-mylet.ExitChan:
				return
			case <-ticker.C:
				report := mylet.CollectReport()
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				result, err := mylet.SendReport(ctx, report)
				cancel()
				if err != nil {
					log.Errorf("SendReport failed: %v", err)
					continue
				}
				if mylet.NeedReload(result.SizeSpec) {
					if err := mylet.Reload(result.SizeSpec); err != nil {
						log.Errorf("Reload failed: %v", err)
					}
				}
			}
		}
	}()

	go func() {
		err = mylet.Start()
		if err != nil {
			log.Error("Start", err)
		}
		syscall.Kill(syscall.Getpid(), syscall.SIGINT)
		time.Sleep(time.Second)
		log.Info("Exit")
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
		if err != nil {
			log.Error("get compresses", err)
			ctx.InternalServerError()
			return
		}

		i := len(a) - 1
		if i == -1 || time.Since(a[i]) > Day {
			a, err = mylet.GetBackups()
			if err != nil {
				log.Error("get backups", err)
				ctx.InternalServerError()
				return
			}

			i = len(a) - 1
			if i == -1 || time.Since(a[i]) > Day {
				_, err = mylet.FullBackup()
				if err != nil {
					log.Error("full backup", err)
					ctx.InternalServerError()
					return
				}
			}

			f, err = mylet.CompressLastBackup()
			if err != nil {
				log.Error("compress last backup", err)
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
			if err != nil {
				log.Error("compress backup", err)
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
			log.Error("get backups", err)
			ctx.WriteError(err)
			return
		}

		if i := len(a) - 1; i == -1 {
			bt, err = mylet.FullBackup()
			if err != nil {
				log.Error("full backup", err)
				ctx.WriteError(err)
				return
			}
		} else {
			bt = a[i]
		}

		inc, err = mylet.IncrementalBackup(bt)
		if err != nil {
			log.Error("incremental backup", filepath.Base(mylet.GetBackupDir(bt)), err)
			ctx.WriteError(err)
			return
		}
	} else {
		bt, err = mylet.FullBackup()
		if err != nil {
			log.Error("full backup", err)
			ctx.WriteError(err)
			return
		}
	}

	if compress {
		d := mylet.GetBackupDir(bt)
		_, err = mylet.CompressBackup(d)
		if err != nil {
			log.Error("compress backup", filepath.Base(d), err)
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
