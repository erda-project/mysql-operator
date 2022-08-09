package v1

import (
	"net"
	"strconv"
	"strings"
)

func (s MysqlSolo) GroupReplicationLocalAddress() string {
	return net.JoinHostPort(s.Spec.Host, strconv.Itoa(s.Spec.GroupPort))
}

func (r *Mysql) GroupReplicationGroupSeeds() string {
	a := make([]string, r.Spec.Primaries)
	for i := range a {
		a[i] = r.Status.Solos[i].GroupReplicationLocalAddress()
	}
	return strings.Join(a, ",")
}
