package myctl

import (
	"encoding/json"
	"time"

	"github.com/cxr29/log"
	"github.com/cxr29/tiny"
	"github.com/cxr29/tiny/alog"
	v1 "github.com/erda-project/mysql-operator/api/v1"
	"github.com/erda-project/mysql-operator/pkg/mylet"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func (ctl *Myctl) Run() {
	tiny.Group("/api/addons/myctl", ctl.Route)

	mylet.Serve()

	for _, g := range ctl.M {
		g.ExitChan <- struct{}{}
	}
}

func (ctl *Myctl) Route(r *tiny.Router) {
	r.Group("/probe", func(r *tiny.Router) {
		r.Use(alog.Off)
		r.GET("/startup", func(ctx *tiny.Context) {
			if ctl.StartupProbe {
				ctx.WriteString("startup")
			} else {
				ctx.ServiceUnavailable()
			}
		})
		r.GET("/liveness", func(ctx *tiny.Context) {
			if ctl.LivenessProbe {
				ctx.WriteString("liveness")
			} else {
				ctx.ServiceUnavailable()
			}
		})
		r.GET("/readiness", func(ctx *tiny.Context) {
			if ctl.ReadinessProbe {
				ctx.WriteString("readiness")
			} else {
				ctx.ServiceUnavailable()
			}
		})
	})

	r.Group("/<ns>", func(r *tiny.Router) {
		r.Use(mylet.PushToken, ctl.PushMysqlGroup)
		r.GET("/mysql", ctl._Mysql)
		r.POST("/report", ctl._Report)
	})
}

func (ctl *Myctl) PullMysqlGroup(ctx *tiny.Context) *MysqlGroup {
	if v, ok := ctx.Values["MysqlGroup"]; ok {
		return v.(*MysqlGroup)
	}
	return nil
}

func (ctl *Myctl) PushMysqlGroup(ctx *tiny.Context) {
	t := mylet.PullToken(ctx)
	k := types.NamespacedName{
		Name:      t.GroupName,
		Namespace: ctx.Param("ns"),
	}

	g, ok := ctl.M[k]
	if !ok {
		ctx.WriteErrorf("%s not found", k.String())
		return
	}

	g.Lock()
	defer g.Unlock()

	if mylet.GroupToken(g.Mysql) != t.GroupToken {
		ctx.WriteError("token forbidden")
		return
	}

	if t.Id < 0 || t.Id >= g.Spec.Size() {
		ctx.WriteError("token id out of range")
		return
	}

	ctx.SetValue("MysqlGroup", g)
}

func (ctl *Myctl) _Mysql(ctx *tiny.Context) {
	g := ctl.PullMysqlGroup(ctx)

	g.Lock()
	defer g.Unlock()

	ctx.WriteData(g.Mysql)
}

func (ctl *Myctl) _Report(ctx *tiny.Context) {
	t := mylet.PullToken(ctx)

	var v mylet.MysqlReport
	if err := ctx.DecodeJSON(&v); err != nil {
		ctx.WriteError("illegal body")
		return
	}

	if t.Name != v.Name {
		ctx.WriteError("name inconsistent")
		return
	}

	g := ctl.PullMysqlGroup(ctx)
	n := g.Spec.Size()

	now := time.Now()
	states := make(map[int]*mylet.MysqlState, n)
	for i, b := range v.States {
		s := new(mylet.MysqlState)

		err := json.Unmarshal([]byte(b), s)
		if err != nil {
			ctx.WriteErrorf("state[%d] illegal", i)
			return
		}

		_, ok := states[s.ToId]
		if ok || s.FromId != t.Id {
			ctx.WriteErrorf("state[%d] invalid", i)
			return
		}

		if s.ToId < 0 || s.ToId >= n {
			continue
		}

		states[s.ToId] = s
	}

	for _, s := range states {
		s.YellowTime = now
		g.States[s.StateKey] = s
		log.Infof("[_Report] 收到上报: FromId=%d ToId=%d ErrorCount=%d GreenTime=%v RedTime=%v LastError=%s", s.FromId, s.ToId, s.ErrorCount, s.GreenTime, s.RedTime, s.LastError)
	}

	sizeSpec := mylet.NewSizeSpec(g.Mysql)
	if sizeSpec != v.SizeSpec {
		log.Infoln(v.Name, "size spec out of sync")
	}

	ctx.WriteData(mylet.ReportResult{
		ReceiveTime: now,
		SizeSpec:    sizeSpec,
	})
}

func (g *MysqlGroup) Check() error {
	now := time.Now()
	for _, s := range g.States {
		s.YellowDuration = now.Sub(s.YellowTime)
	}

	change := 0
	nRed := 0
	nYellow := 0
	n := g.Spec.Size()
	for i := 0; i < n; i++ {
		red, yellow, green := g.Color(i)
		log.Infof("[Check] color统计: id=%d red=%d yellow=%d green=%d", i, red, yellow, green)
		c := v1.Green
		if red+yellow > green {
			c = v1.Red
			nRed++
		} else if red+yellow > 0 {
			c = v1.Yellow
			nYellow++
		}
		if g.Status.Solos[i].Status.Color != c {
			log.Infof("[Check] id=%d 状态变更: %s -> %s", i, g.Status.Solos[i].Status.Color, c)
			g.Status.Solos[i].Status.Color = c
			change++
		}
	}
	c := v1.Green
	if nRed > 0 {
		c = v1.Red
	} else if nYellow > 0 {
		c = v1.Yellow
	}
	if g.Status.Color != c {
		log.Infof("[Check] 集群 color 变更: %s -> %s", g.Status.Color, c)
		g.Status.Color = c
		change++
	}

	if change > 0 {
		g.C <- event.GenericEvent{Object: g.Mysql}
	}

	if g.Spec.PrimaryMode != v1.ModeClassic {
		return nil
	}

	primaryId := *g.Spec.PrimaryId
	red, yellow, green := g.Color(primaryId)
	if red+yellow > 0 {
		log.Infof("[Check] 主节点 color: id=%d red=%d yellow=%d green=%d", primaryId, red, yellow, green)
	}

	writeId := *g.Status.WriteId
	if primaryId != writeId {
		if red+yellow > green {
			g.Spec.PrimaryId = pointer.IntPtr(writeId)
			return nil
		} else {
			return g.SwitchPrimary(primaryId)
		}
	}

	if red+yellow > green {
		g.SwitchCount++
	} else {
		g.SwitchCount = 0
	}

	if g.SwitchCount < SwitchCount || !*g.Spec.AutoSwitch {
		return nil
	}

	newId := -1
	for id := primaryId - 1; id >= 0; id-- {
		red, yellow, green := g.Color(id)
		if green > red+yellow {
			newId = id
			break
		}
	}
	if newId == -1 {
		for id := primaryId + 1; id < n; id++ {
			red, yellow, green := g.Color(id)
			if green > red+yellow {
				newId = id
				break
			}
		}
	}
	if newId != -1 {
		log.Infof("[Check] 触发主切换: %d -> %d", primaryId, newId)
		g.SwitchPrimary(newId)
	}

	return nil
}
