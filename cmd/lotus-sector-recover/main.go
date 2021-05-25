package main

import (
	"os"
	"sync"
	"time"

	"github.com/filecoin-project/go-jsonrpc"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	lcli "github.com/filecoin-project/lotus/cli"
	logging "github.com/ipfs/go-log/v2"
	"github.com/urfave/cli/v2"
)

var log = logging.Logger("lotus-sector-backup")

var (
	minerApi   api.StorageMiner
	nodeCloser jsonrpc.ClientCloser
	nodeCCtx   *cli.Context
	nodeSync   sync.Mutex
)

func closeNodeApi() {
	if nodeCloser != nil {
		nodeCloser()
	}
	minerApi = nil
	nodeCloser = nil
}

func ReleaseNodeApi(shutdown bool) {
	nodeSync.Lock()
	defer nodeSync.Unlock()
	time.Sleep(3e9)

	if minerApi == nil {
		return
	}

	if shutdown {
		closeNodeApi()
		return
	}

	ctx := lcli.ReqContext(nodeCCtx)

	// try reconnection
	_, err := minerApi.Version(ctx)
	if err != nil {
		closeNodeApi()
		return
	}
}

func GetNodeApi() (api.StorageMiner, error) {
	nodeSync.Lock()
	defer nodeSync.Unlock()

	if minerApi != nil {
		return minerApi, nil
	}

	nApi, closer, err := lcli.GetStorageMinerAPI(nodeCCtx)
	if err != nil {
		closeNodeApi()
		return nil, err
	}
	minerApi = nApi
	nodeCloser = closer

	return minerApi, nil
}

func main() {
	logging.SetLogLevel("*", "INFO")

	log.Info("Starting lotus-sector-backup")

	app := &cli.App{
		Name:    "lotus-sector-backup",
		Usage:   "repair sector faults",
		Version: build.UserVersion(),
		Commands: []*cli.Command{
			SectorBackCmd,
			ControlCmd,
		},
	}
	app.Setup()
	if err := app.Run(os.Args); err != nil {
		log.Warnf("%+v", err)
		return
	}
}
