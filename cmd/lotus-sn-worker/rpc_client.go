package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-jsonrpc/auth"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/api/client"
	lcli "github.com/filecoin-project/lotus/cli"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/specs-storage/storage"
	"github.com/gwaylib/errors"
)

var (
	srvCenerURL     string
	srvCenterApi    api.SrvCenterAPI
	srvCenterLk     sync.Mutex
	srvCenterCloser jsonrpc.ClientCloser
)

func closeSrvCenter() {
	if srvCenterCloser != nil {
		srvCenterCloser()
	}

	srvCenterApi = nil
	srvCenterCloser = nil
}

func RealseSrvCenter(shutdown bool) {
	srvCenterLk.Lock()
	defer srvCenterLk.Unlock()
	time.Sleep(3e9)

	if nodeApi == nil {
		return
	}

	if shutdown {
		closeNodeApi()
		return
	}

	ctx := lcli.ReqContext(nodeCCtx)
	_, err := srvCenterApi.Version(ctx)
	if err != nil {
		closeNodeApi()
	}
}

func GetSrvCenterApi() (api.SrvCenterAPI, error) {
	srvCenterLk.Lock()
	defer srvCenterLk.Unlock()

	if srvCenterApi != nil {
		return srvCenterApi, nil
	}
	napi, closer, err := cliutil.GetSrvCenterAPI(nodeCCtx, srvCenerURL)
	if err != nil {
		closeSrvCenter()
		return nil, errors.As(err)
	}

	srvCenterApi = napi
	srvCenterCloser = closer
	return srvCenterApi, nil
}

type rpcClient struct {
	api.WorkerSnAPI
	closer jsonrpc.ClientCloser
}

func (r *rpcClient) Close() error {
	r.closer()
	return nil
}

func ConnectSnWorker(ctx context.Context, fa api.Common, url string) (*rpcClient, error) {
	token, err := fa.AuthNew(ctx, []auth.Permission{"admin"})
	if err != nil {
		return nil, errors.New("creating auth token for remote connection").As(err)
	}

	headers := http.Header{}
	headers.Add("Authorization", "Bearer "+string(token))
	urlInfo := strings.Split(url, ":")
	if len(urlInfo) != 2 {
		panic("error url format")
	}
	rpcUrl := fmt.Sprintf("ws://%s:%s/rpc/v0", urlInfo[0], urlInfo[1])

	wapi, closer, err := client.NewWorkerSnRPC(ctx, rpcUrl, headers)
	if err != nil {
		return nil, errors.New("creating jsonrpc client").As(err)
	}

	return &rpcClient{wapi, closer}, nil
}

func CallCommit2Service2(ctx context.Context, task ffiwrapper.WorkerTask, c1out storage.Commit1Out) (storage.Proof, error) {
	log.Infof("DEBUG: CallCommit2Service2 in , %v", task.SectorID)
	defer log.Infof("DEBUG: CallCommit2Service2 out, %v", task.SectorID)
	napi, err := GetSrvCenterApi()
	if err != nil {
		return nil, errors.As(err)
	}

	var service api.ServiceDescInfo
	retry := 0

	for {
		service, err = napi.GetService(ctx, api.C2)
		if err == nil || retry > 5 {
			break
		}
		if err != nil {
			retry++
			time.Sleep(time.Minute)
		}
	}

	if err != nil {
		return nil, errors.As(err)
	}
	log.Infof("DEBUG: CallCommit2Service2  get remote c2 service, %v", service)
	headers := http.Header{}
	rpcUrl := fmt.Sprintf("http://%s:%d/rpc/v0", service.Host, service.Port)
	sapi, closer, err := client.NewServerSnRPC(ctx, rpcUrl, headers)
	if err != nil {
		return nil, errors.As(err)
	}
	defer closer()
	return sapi.SealCommit2(ctx, storage.SectorRef{ID: task.SectorID, ProofType: task.ProofType}, c1out)
}
