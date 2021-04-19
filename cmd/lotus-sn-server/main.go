package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/gorilla/mux"
	"github.com/mitchellh/go-homedir"

	"os"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	lcli "github.com/filecoin-project/lotus/cli"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper/basicfs"
	"github.com/filecoin-project/lotus/lib/lotuslog"
	logging "github.com/ipfs/go-log/v2"
	"github.com/urfave/cli/v2"
)

const (
	ss2KiB   = 2 << 10
	ss8MiB   = 8 << 20
	ss512MiB = 512 << 20
	ss32GiB  = 32 << 30
	ss64GiB  = 64 << 30
)

var (
	nodeApi    api.SrvCenterAPI
	nodeCloser jsonrpc.ClientCloser
	nodeCCtx   *cli.Context
	nodeSync   sync.Mutex
	rpcUrl     string
)

var log = logging.Logger("main")

func closeNodeApi() {
	if nodeCloser != nil {
		nodeCloser()
	}

	nodeApi = nil
	nodeCloser = nil

}

func RealseNodeApi(shutdown bool) {
	nodeSync.Lock()
	defer nodeSync.Unlock()
	time.Sleep(3e9)

	if nodeApi == nil {
		return
	}

	if shutdown {
		closeNodeApi()
		return
	}

	ctx := lcli.ReqContext(nodeCCtx)
	_, err := nodeApi.Version(ctx)
	if err != nil {
		closeNodeApi()
	}
}

func GetNodeApi() (api.SrvCenterAPI, error) {
	nodeSync.Lock()
	defer nodeSync.Unlock()

	if nodeApi != nil {
		return nodeApi, nil
	}

	nApi, closer, err := cliutil.GetSrvCenterAPI(nodeCCtx, rpcUrl)
	if err != nil {
		closeNodeApi()
		return nil, err
	}

	nodeApi = nApi
	nodeCloser = closer
	return nodeApi, nil
}

func main() {
	lotuslog.SetupLogLevels()

	local := []*cli.Command{
		runCmd,
	}

	app := cli.App{
		Name:     "lotus-sn-server",
		Usage:    "sn server provide some service",
		Version:  build.UserVersion(),
		Commands: local,
	}
	app.Setup()

	if err := app.Run(os.Args); err != nil {
		log.Warnf("%+v", err)
		return
	}
}

var runCmd = &cli.Command{
	Name:  "run",
	Usage: "Start sn server",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "listen",
			Value: "",
		},
		&cli.StringFlag{
			Name:  "service-center-address",
			Usage: "host and port the service center",
		},
		&cli.BoolFlag{
			Name:  "32G",
			Usage: "allow to product 32G proof",
			Value: false,
		},
		&cli.BoolFlag{
			Name:  "64G",
			Usage: "allow to product 64G proof",
			Value: false,
		},
		&cli.BoolFlag{
			Name:  "2K",
			Usage: "allow to product 2k proof",
			Value: false,
		},
	},
	Action: func(cctx *cli.Context) error {
		log.Info("Starting sn server")
		ctx := lcli.ReqContext(cctx)
		nodeCCtx = cctx
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		listen := cctx.String("listen")
		if len(listen) == 0 {
			listen = os.Getenv("NETIP") + ":3299"
		}

		urlInfo := strings.Split(listen, ":")
		if len(urlInfo) != 2 {
			return errors.New("error listen address format")
		}
		port, err := strconv.ParseInt(urlInfo[1], 10, 64)
		if err != nil {
			return err
		}
		service := api.ServiceDescInfo{
			Kind: api.C2,
			Host: urlInfo[0],
			Port: int(port),
		}

		if !cctx.IsSet("service-center-address") {
			return errors.New("need service center address")
		}
		address := cctx.String("service-center-address")
		urlinfo := strings.Split(address, ":")
		if len(urlinfo) != 2 {
			return errors.New("error service center address format")
		}
		rpcUrl = fmt.Sprintf("http://%s:%s/rpc/v0", urlinfo[0], urlinfo[1])

		workerdir, err := homedir.Expand("~/.lotusworker")
		if err != nil {
			return err
		}
		sealer, err := ffiwrapper.New(ffiwrapper.RemoteCfg{}, &basicfs.Provider{
			Root: workerdir,
		})
		if err != nil {
			return err
		}

		sns := &rpcServer{
			busy: 0,
			sb:   sealer,
		}

		if cctx.IsSet("32G") {
			sns.checkParam(ctx, abi.SectorSize(ss32GiB))
		}

		if cctx.IsSet("64G") {
			sns.checkParam(ctx, abi.SectorSize(ss64GiB))
		}

		if cctx.IsSet("2K") {
			sns.checkParam(ctx, abi.SectorSize(ss2KiB))
		}

		rpcs := jsonrpc.NewServer()
		rpcs.Register("Filecoin", sns)

		mux := mux.NewRouter()
		mux.Handle("/rpc/v0", rpcs)

		srv := &http.Server{
			Handler: mux,
			BaseContext: func(listner net.Listener) context.Context {
				return ctx
			},
		}

		go func() {
			log.Infof("lotusn sn server start to register service, %v", service)
			defer func() {
				log.Error("lotus sn server stoped, not to register service")
				cancel()
			}()

			tiker := time.NewTicker(time.Minute)
			for {
				select {
				case <-ctx.Done():
					log.Info("lotus sn server exit")
					return
				case <-tiker.C:
					if atomic.LoadInt32(&sns.busy) == 0 {
						napi, err := GetNodeApi()
						if err != nil {
							log.Info(err)
							RealseNodeApi(false)
						}
						if err := napi.RegisterService(ctx, service); err != nil {
							log.Info(err)
						}
					}
				}
			}
		}()

		nl, err := net.Listen("tcp", listen)
		if err != nil {
			return err
		}
		log.Infof("start server, %s", listen)
		return srv.Serve(nl)
	},
}
