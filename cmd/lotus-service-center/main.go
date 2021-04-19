package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	lcli "github.com/filecoin-project/lotus/cli"
	cliutil "github.com/filecoin-project/lotus/cli/util"
	"github.com/filecoin-project/lotus/lib/lotuslog"
	"github.com/gorilla/mux"
	"github.com/gwaylib/errors"
	logging "github.com/ipfs/go-log/v2"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"
)

var (
	nodeApi    api.SrvCenterAPI
	nodeCloser jsonrpc.ClientCloser
	nodeCCtx   *cli.Context
	nodeSync   sync.Mutex
	rpcUrl     string
)

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
		return nil, errors.As(err)
	}

	nodeApi = nApi
	nodeCloser = closer
	return nodeApi, nil
}

var log = logging.Logger("main")

func main() {
	lotuslog.SetupLogLevels()

	local := []*cli.Command{
		testCmd,
		runCmd,
		infoCmd,
	}

	app := &cli.App{
		Name:     "lotus-service-center",
		Usage:    "lotus remote service center ",
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
	Usage: "Start lotus server center",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "listen",
			Usage: "host address and port the server center will listen on",
			Value: "0.0.0.0:3452",
		},
	},
	Action: func(cctx *cli.Context) error {
		log.Info("Starting lotus server center")

		ctx := lcli.ReqContext(cctx)
		address := cctx.String("listen")
		netip := os.Getenv("NETIP")
		if len(address) == 0 {
			address = netip + "3452"
		}

		mux := mux.NewRouter()

		sc := &rpcServer{
			listen:   address,
			services: map[string][]api.ServiceDescInfo{},
			exist:    map[string]bool{},
			updateAt: map[string]int64{},
		}

		rpc := jsonrpc.NewServer()
		rpc.Register("Filecoin", sc)
		mux.Handle("/rpc/v0", rpc)

		srv := &http.Server{
			Handler: mux,
			BaseContext: func(listner net.Listener) context.Context {
				return ctx
			},
		}

		nl, err := net.Listen("tcp", address)
		if err != nil {
			return err
		}
		log.Infof("start server at: %s", address)
		return srv.Serve(nl)
	},
}

var infoCmd = &cli.Command{
	Name:  "info",
	Usage: "statistic service center info",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "service-center-address",
			Usage: "service-center-address",
			Value: "",
		},
	},
	Action: func(cctx *cli.Context) error {
		nodeCCtx = cctx
		ctx := lcli.ReqContext(cctx)

		if !cctx.IsSet("service-center-address") {
			return xerrors.New("need server center address")
		}

		addr := cctx.String("service-center-address")
		urlInfo := strings.Split(addr, ":")
		if len(urlInfo) != 2 {
			return errors.New("error url fomat")
		}

		rpcUrl = fmt.Sprintf("http://%s:%s/rpc/v0", urlInfo[0], urlInfo[1])
		napi, err := GetNodeApi()
		if err != nil {
			return err
		}

		info, err := napi.StatisticInfo(ctx)
		if err != nil {
			return err
		}

		fmt.Printf("\nthe Nnumber of Services:  %d\n", len(info.UpdatedAt))

		fmt.Printf("\nidle service info:\n")
		for key, val := range info.IdleNum {
			fmt.Printf("\t %s:  %d\n", key, val)
		}

		fmt.Printf("\nservice update info:\n")
		for key, val := range info.UpdatedAt {
			tm := time.Unix(val, 0)
			fmt.Printf("\t%s %s\n", key, tm.Format("2006-01-02 15:04:05"))
		}
		fmt.Println()
		return nil
	},
}
