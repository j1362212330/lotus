package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/urfave/cli/v2"

	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/gwaylib/errors"
)

var pledgeSectorCmd = &cli.Command{
	Name:  "pledge-sector",
	Usage: "Pledge sector",
	Subcommands: []*cli.Command{
		startPledgeSectorCmd,
		statusPledgeSectorCmd,
		stopPledgeSectorCmd,
	},
}

var snSectorCmd = &cli.Command{
	Name:  "sn-sector",
	Usage: "command for sn-sector",
	Subcommands: []*cli.Command{
		getSnSectorStateCmd,
		setSnSectorStateCmd,
		checkSnSectorCmd,
	},
}

var startPledgeSectorCmd = &cli.Command{
	Name:  "start",
	Usage: "start the pledge daemon",
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)

		return nodeApi.RunPledgeSector(ctx)
	},
}

var statusPledgeSectorCmd = &cli.Command{
	Name:  "status",
	Usage: "the pledge daemon status",
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)

		if status, err := nodeApi.StatusPledgeSector(ctx); err != nil {
			return errors.As(err)
		} else if status != 0 {
			fmt.Println("Running")
		} else {
			fmt.Println("Not Running")
		}
		return nil
	},
}
var stopPledgeSectorCmd = &cli.Command{
	Name:  "stop",
	Usage: "stop the pledge daemon",
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)

		return nodeApi.StopPledgeSector(ctx)
	},
}

var getSnSectorStateCmd = &cli.Command{
	Name:  "get",
	Usage: "get the sector info by sector id(s-t0xxx-x)",
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)

		sid := cctx.Args().First()
		if len(sid) == 0 {
			return errors.New("need input sector-id(s-t0xxxx-xxx")
		}
		info, err := nodeApi.SnSectorGetState(ctx, sid)
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
var setSnSectorStateCmd = &cli.Command{
	Name:  "set-state",
	Usage: "will set the sector state",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "sector-id",
			Usage: "sector id which want to set",
		},
		&cli.BoolFlag{
			Name:  "force",
			Usage: "force to release the working task",
		},
		&cli.BoolFlag{
			Name:  "reset",
			Usage: "reset the state, or it will be added, default is added",
			Value: false,
		},
		&cli.IntFlag{
			Name:  "state",
			Usage: "state which want to set",
		},
		&cli.StringFlag{
			Name:  "memo",
			Usage: "memo for state udpate",
		},
	},
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)

		sid := cctx.String("sector-id")
		if len(sid) == 0 {
			return errors.New("need input sector-id(s-t0xxxx-xxx")
		}
		memo := cctx.String("memo")
		if len(memo) == 0 {
			return errors.New("need input memo")
		}
		if _, err := nodeApi.SnSectorSetState(ctx, sid, memo, cctx.Int("state"), cctx.Bool("force"), cctx.Bool("reset")); err != nil {
			return err
		}
		info, err := nodeApi.SnSectorGetState(ctx, sid)
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
var checkSnSectorCmd = &cli.Command{
	Name:  "check",
	Usage: "checking provable of the sector",
	Flags: []cli.Flag{
		&cli.Int64Flag{
			Name:  "timeout",
			Usage: "the unit is second",
			Value: 6,
		},
	},
	Action: func(cctx *cli.Context) error {
		nodeApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return err
		}
		defer closer()
		ctx := lcli.ReqContext(cctx)
		sid := cctx.Args().First()
		if len(sid) == 0 {
			return errors.New("need input sector-id(s-t0xxxx-xxx")
		}
		used, err := nodeApi.SnSectorCheck(ctx, sid, time.Duration(cctx.Int64("timeout"))*time.Second)
		if err != nil {
			return err
		}
		fmt.Printf("used:%s\n", used)
		return nil
	},
}
