package myctl

import (
	"context"
	"time"

	"github.com/cxr29/log"
	v1 "github.com/erda-project/mysql-operator/api/v1"
	"github.com/erda-project/mysql-operator/pkg/mylet"
	"k8s.io/utils/pointer"
)

type MysqlGroup struct {
	*Myctl
	*v1.Mysql
	States      map[mylet.StateKey]*mylet.MysqlState
	SwitchCount int
	SwitchTime  time.Time
	Running     bool
	ExitChan    chan struct{}
}

func (g *MysqlGroup) Start() {
	g.Lock()
	if g.Running {
		g.Unlock()
		return
	} else {
		g.Running = true
	}
	g.Unlock()

	hang := 0 //TODO collect hang
	timer := time.NewTicker(mylet.Timeout5s)
	defer timer.Stop()

	for {
		select {
		case <-g.ExitChan:
			g.Running = false
			log.Infoln("start exit")
			return
		case <-timer.C:
			g.Lock()
			if hang > 0 {
				log.Errorln("hang", hang)
			}
			hang++
			g.Unlock()

			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), mylet.Timeout5s)
				defer cancel()

				now := time.Now()
				m := mylet.CrossCheck(ctx, g.Mysql)

				g.Lock()
				defer g.Unlock()
				defer g.Check()

				for id, err := range m {
					sk := mylet.StateKey{
						FromId: -1,
						ToId:   id,
					}

					v, ok := g.States[sk]
					if !ok {
						v = &mylet.MysqlState{
							StateKey: sk,
						}
						g.States[sk] = v
					}

					if err == nil {
						v.ErrorCount = 0
						v.GreenTime = now
					} else {
						if v.ErrorCount%10 == 1 {
							log.Errorln("from myctl to", g.SoloName(v.ToId), err)
						}
						v.ErrorCount++
						v.RedTime = now
						v.LastError = err.Error()
					}

					// Monotonic
					v.GreenDuration = now.Sub(v.GreenTime)
					v.RedDuration = now.Sub(v.RedTime)

					v.YellowTime = now //
				}

				hang--
				g.Status.Hang = hang
			}()
		}
	}
}

func (g *MysqlGroup) Diff(mysql *v1.Mysql) error {
	changed := 0

	// Reload changes
	if g.Spec.PrimaryMode != mysql.Spec.PrimaryMode ||
		g.Spec.Primaries != mysql.Spec.Primaries ||
		*g.Spec.Replicas != *mysql.Spec.Replicas ||
		*g.Spec.PrimaryId != *mysql.Spec.PrimaryId ||
		*g.Spec.AutoSwitch != *mysql.Spec.AutoSwitch {
		changed++

		g.Spec.PrimaryMode = mysql.Spec.PrimaryMode
		g.Spec.Primaries = mysql.Spec.Primaries
		g.Spec.PrimaryId = pointer.Int(*mysql.Spec.PrimaryId)
		g.Spec.Replicas = pointer.Int(*mysql.Spec.Replicas)
		g.Spec.AutoSwitch = pointer.Bool(*mysql.Spec.AutoSwitch)

		primaryId := *g.Spec.PrimaryId
		red, yellow, green := g.Color(primaryId)
		writeId := *g.Status.WriteId
		if primaryId != writeId {
			if red+yellow > green {
				log.Infoln("can not change primary", writeId, "to", primaryId)
				g.Spec.PrimaryId = pointer.Int(writeId)
			} else {
				log.Infoln("manual change primary", writeId, "to", primaryId)
				g.Status.WriteId = pointer.Int(primaryId)
			}
		}

		n := g.Spec.Size()
		for k := range g.States {
			if !v1.Between(k.FromId, -1, n-1) || !v1.Between(k.ToId, 0, n-1) {
				delete(g.States, k)
			}
		}
	}

	// Restart changes
	if g.Spec.EnableExporter != mysql.Spec.EnableExporter ||
		g.Spec.ExporterPort != mysql.Spec.ExporterPort ||
		!EqStrings(g.Spec.ExporterFlags, mysql.Spec.ExporterFlags) ||
		g.Spec.ExporterImage != mysql.Spec.ExporterImage ||
		g.Spec.ExporterUsername != mysql.Spec.ExporterUsername ||
		g.Spec.ExporterPassword != mysql.Spec.ExporterPassword {
		changed++

		g.Spec.EnableExporter = mysql.Spec.EnableExporter
		g.Spec.ExporterPort = mysql.Spec.ExporterPort
		g.Spec.ExporterFlags = mysql.Spec.ExporterFlags
		g.Spec.ExporterImage = mysql.Spec.ExporterImage
		g.Spec.ExporterUsername = mysql.Spec.ExporterUsername
		g.Spec.ExporterPassword = mysql.Spec.ExporterPassword
	}

	//TODO other changes

	if changed > 0 {
		if err := g.Validate(); err != nil {
			return err
		}
	}

	return nil
}

func EqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	} else {
		for i, s := range a {
			if s != b[i] {
				return false
			}
		}
		return true
	}
}
