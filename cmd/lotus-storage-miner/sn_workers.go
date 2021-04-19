package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/filecoin-project/lotus/api"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/gwaylib/errors"
	"github.com/urfave/cli/v2"
)

var snWorkerCmd = &cli.Command{
	Name:  "sn-worker",
	Usage: "Manage worker",
	Subcommands: []*cli.Command{
		statusSNWorkerCmd,
		listSNWorkerCmd,
		getSNWorkerCmd,
		searchSNWorkerCmd,
		gcSNWorkerCmd,
		enableSNWorkerCmd,
		disableSNWorkerCmd,
		pauseSNSealCmd,
		unpauseSNSealCmd,
	},
}

func printWorkerStat(ctx context.Context, nodeApi api.StorageMiner) error {
	wstat, err := nodeApi.WorkerStatus(ctx)
	if err != nil {
		return err
	}

	fmt.Printf("Worker use:\n")
	fmt.Printf("\tPauseSeal : %t \n", wstat.PauseSeal != 0)
	fmt.Printf("\tSealWorker: %d / %d (locked: %d)\n", wstat.SealWorkerUsing, wstat.SealWorkerTotal, wstat.SealWorkerLocked)
	fmt.Printf("\tCommit2Srv: %d / %d\n", wstat.Commit2SrvUsed, wstat.Commit2SrvTotal)
	fmt.Printf("\tWnPoStSrv : %d / %d\n", wstat.WnPoStSrvUsed, wstat.WnPoStSrvTotal)
	fmt.Printf("\tWdPoStSrv : %d / %d\n", wstat.WdPoStSrvUsed, wstat.WdPoStSrvTotal)
	fmt.Printf("\tAllRemotes: all:%d, online:%d, offline:%d, disabled: %d\n", wstat.WorkerOnlines+wstat.WorkerOfflines+wstat.WorkerDisabled, wstat.WorkerOnlines, wstat.WorkerOfflines, wstat.WorkerDisabled)

	fmt.Printf("Queues:\n")
	fmt.Printf("\tAddPiece: %d\n", wstat.PledgeWait)
	fmt.Printf("\tPreCommit1: %d\n", wstat.PreCommit1Wait)
	fmt.Printf("\tPreCommit2: %d\n", wstat.PreCommit2Wait)
	fmt.Printf("\tCommit: %d\n", wstat.CommitWait)
	fmt.Printf("\tFinalize: %d\n", wstat.FinalizeWait)
	fmt.Printf("\tUnseal: %d\n", wstat.UnsealWait)
	return nil
}

var statusSNWorkerCmd = &cli.Command{
	Name:    "status",
	Aliases: []string{"info"},
	Usage:   "workers status",
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		return printWorkerStat(ctx, nodeApi)
	},
}
var listSNWorkerCmd = &cli.Command{
	Name:  "list",
	Usage: "list worker status",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "online",
			Usage: "show the online worker",
			Value: true,
		},
		&cli.BoolFlag{
			Name:  "offline",
			Usage: "show the offline worker",
			Value: false,
		},
		&cli.BoolFlag{
			Name:  "disabled",
			Usage: "show the disabled worker",
			Value: false,
		},
		&cli.BoolFlag{
			Name:  "service",
			Usage: "show the service worker",
			Value: true,
		},
	},
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		infos, err := nodeApi.WorkerStatusAll(ctx)
		if err != nil {
			return errors.As(err)
		}
		showDisabled := cctx.Bool("disabled")
		showOnline := cctx.Bool("online")
		showOffline := cctx.Bool("offline")
		showService := cctx.Bool("service")
		for _, info := range infos {
			if info.Disable && !showDisabled {
				continue
			}
			if info.Online && !showOnline {
				continue
			}
			if !info.Online && !showOffline {
				continue
			}
			if info.Srv && !showService {
				continue
			}
			fmt.Println(info.String())
		}
		return nil
	},
}
var getSNWorkerCmd = &cli.Command{
	Name:      "get",
	Usage:     "get worker detail",
	ArgsUsage: "worker id",
	Action: func(cctx *cli.Context) error {
		args := cctx.Args()
		workerId := args.First()
		if len(workerId) == 0 {
			return errors.New("need input workid")
		}
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		info, err := nodeApi.WorkerInfo(ctx, workerId)
		if err != nil {
			return err
		}
		output, err := json.MarshalIndent(info, "", "	")
		if err != nil {
			return err
		}
		fmt.Println(string(output))
		return nil
	},
}
var searchSNWorkerCmd = &cli.Command{
	Name:      "search",
	Usage:     "search worker with ip",
	ArgsUsage: "worker ip",
	Action: func(cctx *cli.Context) error {
		args := cctx.Args()
		workerId := args.First()
		if len(workerId) == 0 {
			return errors.New("need input workid")
		}
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		infos, err := nodeApi.WorkerSearch(ctx, workerId)
		if err != nil {
			return err
		}
		output, err := json.MarshalIndent(infos, "", "	")
		if err != nil {
			return err
		}
		fmt.Println(string(output))
		return nil
	},
}
var gcSNWorkerCmd = &cli.Command{
	Name:      "gc",
	Usage:     "gc the tasks who state is more than 200",
	ArgsUsage: "workid/all",
	Action: func(cctx *cli.Context) error {
		args := cctx.Args()
		workerId := args.First()
		if len(workerId) == 0 {
			return errors.New("need input workid/all")
		}
		if workerId == "all" {
			workerId = ""
		}
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		gcTasks, err := nodeApi.WorkerGcLock(ctx, workerId)
		if err != nil {
			return err
		}
		for _, task := range gcTasks {
			fmt.Printf("gc : %s\n", task)
		}
		fmt.Println("gc done")
		return nil
	},
}
var enableSNWorkerCmd = &cli.Command{
	Name:      "enable",
	Usage:     "Enable a work node to start allocating",
	ArgsUsage: "worker id",
	Action: func(cctx *cli.Context) error {
		args := cctx.Args()
		workerId := args.First()
		if len(workerId) == 0 {
			return errors.New("need input worker id")
		}
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		return nodeApi.WorkerDisable(ctx, workerId, false)
	},
}
var disableSNWorkerCmd = &cli.Command{
	Name:      "disable",
	Usage:     "Disable a work node to stop allocating OR start allocating",
	ArgsUsage: "worker id",
	Action: func(cctx *cli.Context) error {
		args := cctx.Args()
		workerId := args.First()
		if len(workerId) == 0 {
			return errors.New("need input worker id")
		}
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		return nodeApi.WorkerDisable(ctx, workerId, true)
	},
}
var pauseSNSealCmd = &cli.Command{
	Name:  "pause",
	Usage: "pause seal for all worker, so it can control the base fee.",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name: "really-do-it",
		},
	},
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		if !cctx.Bool("really-do-it") {
			fmt.Println("need input really-do-it for confirm")
			return nil
		}
		ctx := lcli.ReqContext(cctx)
		if err := nodeApi.PauseSeal(ctx, 1); err != nil {
			return err
		}
		return printWorkerStat(ctx, nodeApi)
	},
}
var unpauseSNSealCmd = &cli.Command{
	Name:  "unpause",
	Usage: "unpause seal for all worker, so it can control the base fee.",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name: "really-do-it",
		},
	},
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		if !cctx.Bool("really-do-it") {
			fmt.Println("need input really-do-it for confirm")
			return nil
		}
		ctx := lcli.ReqContext(cctx)
		if err := nodeApi.PauseSeal(ctx, 0); err != nil {
			return err
		}
		return printWorkerStat(ctx, nodeApi)
	},
}
