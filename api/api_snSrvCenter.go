package api

import (
	"context"
	"fmt"
)

const (
	C2 = "SealCommit2"
)

type ServiceDescInfo struct {
	Kind string
	Host string
	Port int
}

type StatisticInfo struct {
	UpdatedAt map[string]int64
	IdleNum   map[string]int
}

func (s ServiceDescInfo) Key() string {
	return fmt.Sprintf("%s:%s:%d", s.Kind, s.Host, s.Port)
}

type SrvCenterAPI interface {
	Version(context.Context) (Version, error) //perm:admin

	RegisterService(context.Context, ServiceDescInfo) error      //perm:admin
	GetService(context.Context, string) (ServiceDescInfo, error) //perm:admin
	StatisticInfo(context.Context) (StatisticInfo, error)        //perm:admin
}
