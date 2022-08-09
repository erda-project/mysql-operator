package mylet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/cxr29/log"
	v1 "github.com/erda-project/mysql-operator/api/v1"
	"k8s.io/utils/pointer"
)

type SizeSpec struct {
	PrimaryMode string
	Primaries   int
	Replicas    int
	PrimaryId   int
	AutoSwitch  bool
}

type MysqlReport struct {
	Name string
	SizeSpec
	States []json.RawMessage
	Hang   int
}
type ReportResult struct {
	ReceiveTime time.Time
	SizeSpec
}

type StateKey struct {
	FromId int
	ToId   int
}
type MysqlState struct {
	StateKey

	GreenTime     time.Time
	GreenDuration time.Duration

	YellowTime     time.Time
	YellowDuration time.Duration

	RedTime     time.Time
	RedDuration time.Duration

	LastError  string //TODO max length
	ErrorCount int
}

func (let *Mylet) SendReport(ctx context.Context, r *MysqlReport) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}

	u := url.URL{
		Scheme: "http",
		Host:   let.Mysql.Spec.MyctlAddr,
		Path:   "/api/addons/myctl/" + v1.Namespace + "/report",
	}

	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(b))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Token", SoloToken(let.Mysql, let.Spec.Name))

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	b, err = io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("status code %d, body: %s", res.StatusCode, string(b))
	}

	var v struct {
		Error interface{}
		Data  ReportResult
	}
	err = json.Unmarshal(b, &v)
	if err != nil {
		return err
	}

	if v.Error != nil {
		return fmt.Errorf("return error: %s", v.Error)
	}

	return let.Reload(v.Data.SizeSpec)
}

func (let *Mylet) Reload(ss SizeSpec) error {
	let.Mysql.Spec.AutoSwitch = pointer.Bool(ss.AutoSwitch)

	if *let.Mysql.Spec.Replicas != ss.Replicas {
		log.Infoln("change replicas", *let.Mysql.Spec.Replicas, "to", ss.Replicas)

		s := let.MysqlSolo
		let.Mysql.Spec.Replicas = pointer.Int(ss.Replicas)

		err := let.Mysql.Validate()
		log.ErrError(err, "validate")
		if err != nil || s.Spec.Id >= let.Mysql.Spec.Size() {
			log.Infoln("reload replicas failed, restart")
			let.ExitChan <- struct{}{}
			return err
		}

		let.MysqlSolo = let.Mysql.Status.Solos[s.Spec.Id]
		if let.IsReplica() && *s.Spec.SourceId != *let.Spec.SourceId {
			err = let.StopReplica()
			log.ErrError(err, "stop replica")
			if err == nil {
				err = let.SetupReplica()
				log.ErrError(err, "setup replica")
			}
			if err != nil {
				log.Infoln("reload replica failed, restart")
				let.ExitChan <- struct{}{}
				return err
			}
		}
	}

	if let.Mysql.Spec.PrimaryMode != ss.PrimaryMode ||
		let.Mysql.Spec.Primaries != ss.Primaries {
		log.Infoln("primary mode or/and primaries changed, restart")
		let.ExitChan <- struct{}{} //TODO sts restart
		return nil
	}

	if *let.Mysql.Spec.PrimaryId != ss.PrimaryId {
		err := let.ChangePrimary(ss.PrimaryId)
		log.ErrError(err, "change primary")
		if err != nil {
			log.Infoln("change primary failed, restart")
			let.ExitChan <- struct{}{}
			return err
		}
	}

	return nil
}

func NewSizeSpec(mysql *v1.Mysql) SizeSpec {
	return SizeSpec{
		PrimaryMode: mysql.Spec.PrimaryMode,
		Primaries:   mysql.Spec.Primaries,
		Replicas:    *mysql.Spec.Replicas,
		PrimaryId:   *mysql.Spec.PrimaryId,
		AutoSwitch:  *mysql.Spec.AutoSwitch,
	}
}
