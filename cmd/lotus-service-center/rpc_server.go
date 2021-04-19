package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/filecoin-project/lotus/api"
)

var (
	ErrNotFound = errors.New("service not found")
)

type rpcServer struct {
	listen string

	lk sync.Mutex

	services map[string][]api.ServiceDescInfo
	exist    map[string]bool
	updateAt map[string]int64
}

func (r *rpcServer) Version(context.Context) (api.Version, error) {
	return 0, nil
}

func (r *rpcServer) RegisterService(ctx context.Context, s api.ServiceDescInfo) error {
	r.lk.Lock()
	defer r.lk.Unlock()
	r.updateAt[s.Key()] = time.Now().Unix()

	exist, ok := r.exist[s.Key()]
	if !ok {
		log.Infof("the first to register servie: %v", s)
	}
	if exist {
		return nil
	}
	ss, ok := r.services[s.Kind]
	if !ok {
		ss = []api.ServiceDescInfo{}
	}
	ss = append(ss, s)
	r.services[s.Kind] = ss
	r.exist[s.Key()] = true
	return nil
}

func (r *rpcServer) GetService(ctx context.Context, kind string) (api.ServiceDescInfo, error) {
	r.lk.Lock()
	defer r.lk.Unlock()

	ss, ok := r.services[kind]
	if !ok || len(ss) == 0 {
		log.Info("service not found, %s", kind)
		return api.ServiceDescInfo{}, ErrNotFound
	}

	res := ss[0]

	r.services[kind] = ss[1:]
	r.exist[res.Key()] = false

	return res, nil

}

func (r *rpcServer) StatisticInfo(ctx context.Context) (api.StatisticInfo, error) {
	r.lk.Lock()
	defer r.lk.Unlock()

	res := api.StatisticInfo{UpdatedAt: map[string]int64{}, IdleNum: map[string]int{}}

	for key, val := range r.services {
		res.IdleNum[key] = len(val)
	}

	for key, val := range r.updateAt {
		res.UpdatedAt[key] = val
	}
	return res, nil
}

var _ api.SrvCenterAPI = &rpcServer{}
