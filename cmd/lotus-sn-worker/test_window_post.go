package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"math"
	"time"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper/basicfs"
	"github.com/filecoin-project/specs-storage/storage"
	"github.com/gwaylib/errors"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"
	"golang.org/x/xerrors"
)

var testCmd = &cli.Command{
	Name:  "test",
	Usage: "test command",
	Subcommands: []*cli.Command{
		testWdPoStCmd,
		testWnPostCmd,
	},
}

var testWdPoStCmd = &cli.Command{
	Name:  "wdpost",
	Usage: "testing wdpost， need MinerAPI  and DaemonAPI",
	Flags: []cli.Flag{
		&cli.IntFlag{
			Name:  "index",
			Value: 0,
			Usage: "Window PoSt deadline index",
		},
		&cli.BoolFlag{
			Name:  "check-sealed",
			Value: false,
			Usage: "check and relink the sealed file",
		},
		&cli.BoolFlag{
			Name:  "mount",
			Value: false,
			Usage: "mount storage node from miner",
		},
		&cli.BoolFlag{
			Name:  "do-wdpost",
			Value: true,
			Usage: "running window post, false only check sectors files",
		},
	},
	Action: func(cctx *cli.Context) error {
		minerRepo, err := homedir.Expand(cctx.String("miner-repo"))
		if err != nil {
			return err
		}

		//获取daemon api
		fullApi, closer, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return errors.As(err)
		}
		defer closer()

		//获取minerapi
		minerApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return errors.As(err)
		}
		defer closer()

		ctx, cancel := context.WithCancel(lcli.ReqContext(cctx))
		defer cancel()

		act, err := minerApi.ActorAddress(ctx)
		if err != nil {
			return err
		}

		ssize, err := minerApi.ActorSectorSize(ctx, act)
		if err != nil {
			return err
		}
		log.Infof("Running ActorSize:%s", ssize.ShortString())

		minerSealer, err := ffiwrapper.New(ffiwrapper.RemoteCfg{}, &basicfs.Provider{
			Root: minerRepo,
		})

		if err != nil {
			return err
		}

		// mount storage from miner
		if cctx.Bool("mount") {
			rs := &rpcServer{
				sb:           minerSealer,
				storageCache: map[int64]database.StorageInfo{},
			}
			if err := rs.loadMinerStorage(ctx, minerApi); err != nil {
				return errors.As(err)
			}
		}

		log.Info("get chain head")
		ts, err := fullApi.ChainHead(ctx)
		if err != nil {
			return err
		}

		deadline, err := fullApi.StateMinerProvingDeadline(ctx, act, ts.Key())
		if err != nil {
			return err
		}

		index := cctx.Uint64("index")

		log.Info("get miner partitions")
		partitions, err := fullApi.StateMinerPartitions(ctx, act, index, ts.Key())
		if err != nil {
			return errors.As(err)
		}
		if len(partitions) == 0 {
			fmt.Println("No partitions")
			return nil
		}

		buf := new(bytes.Buffer)
		if err := act.MarshalCBOR(buf); err != nil {
			return errors.As(err)
		}

		log.Info("get randomness")
		randEpoch := deadline.Challenge - abi.ChainEpoch((deadline.Index-index)*60)
		rand, err := fullApi.ChainGetRandomnessFromBeacon(ctx, ts.Key(), crypto.DomainSeparationTag_WindowedPoStChallengeSeed, randEpoch, buf.Bytes())
		if err != nil {
			return errors.As(err)
		}

		log.Info("get mid")
		mid, err := address.IDFromAddress(act)
		if err != nil {
			return errors.As(err)
		}
		log.Info("create sinfos")
		var sinfos []storage.ProofSectorInfo
		var sectors = []storage.SectorRef{}
		for partIdx, partition := range partitions {
			pSector := partition.AllSectors
			liveCount, err := pSector.Count()
			if err != nil {
				return errors.As(err)
			}
			sset, err := fullApi.StateMinerSectors(ctx, act, &pSector, ts.Key())
			if err != nil {
				return errors.As(err, partIdx)
			}
			fmt.Printf("partition:%d,sectors:%d, sset:%d\n", partIdx, liveCount, len(sset))
			for _, sector := range sset {
				id := abi.SectorID{
					Miner:  abi.ActorID(mid),
					Number: sector.SectorNumber,
				}
				sFile, err := minerApi.SnSectorFile(ctx, storage.SectorName(id))
				if err != nil {
					return errors.As(err)
				}
				sRef := storage.SectorRef{
					ID:         id,
					ProofType:  sector.SealProof,
					SectorFile: *sFile,
				}
				sectors = append(sectors, sRef)
				sinfos = append(sinfos, storage.ProofSectorInfo{
					SectorRef: sRef,
					SealedCID: sector.SealedCID,
				})
			}
		}
		fmt.Println("Start CheckProvable")
		start := time.Now()
		all, _, bad, err := ffiwrapper.CheckProvable(ctx, sectors, nil, 6*time.Second)
		if err != nil {
			return errors.As(err)
		}

		toProvInfo := []storage.ProofSectorInfo{}
		for _, val := range all {
			errStr := "nil"
			if err := errors.ParseError(val.Err); err != nil {
				errStr = err.Code()
			} else {
				for i, _ := range sectors {
					if sectors[i].SectorId == val.Sector.SectorId {
						toProvInfo = append(toProvInfo, sinfos[i])
						break
					}
				}
			}
			fmt.Printf("%s,%d,%s,%+v\n", val.Sector.SectorId, val.Used, val.Used.String(), errStr)
		}
		fmt.Printf("used:%s,all:%d, bad:%d,toProve:%d\n", time.Now().Sub(start).String(), len(all), len(bad), len(toProvInfo))
		//	for _, val := range toProvInfo {
		//		fmt.Println(val.SectorNumber)
		//	}
		if !cctx.Bool("do-wdpost") {
			return nil
		}

		if _, _, err := minerSealer.GenerateWindowPoSt(ctx, abi.ActorID(mid), toProvInfo, abi.PoStRandomness(rand)); err != nil {
			return errors.As(err)
		}
		return nil
	},
}

var testWnPostCmd = &cli.Command{
	Name:  "wnpost",
	Usage: "testing wnpost",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "mount",
			Value: false,
			Usage: "mount storage node from miner",
		},
		&cli.BoolFlag{
			Name:  "do-wnpost",
			Value: true,
			Usage: "running window post, false only check sectors files",
		},
	},

	Action: func(cctx *cli.Context) error {
		minerRepo, err := homedir.Expand(cctx.String("miner-repo"))
		if err != nil {
			return err
		}
		fullApi, closer, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return errors.As(err)
		}
		defer closer()
		minerApi, closer, err := lcli.GetStorageMinerAPI(cctx)
		if err != nil {
			return errors.As(err)
		}
		defer closer()

		ctx, cancel := context.WithCancel(lcli.ReqContext(cctx))
		defer cancel()

		act, err := minerApi.ActorAddress(ctx)
		if err != nil {
			return err
		}

		minerSealer, err := ffiwrapper.New(ffiwrapper.RemoteCfg{}, &basicfs.Provider{
			Root: minerRepo,
		})
		if err != nil {
			return err
		}

		// mount storage from miner
		if cctx.Bool("mount") {
			rs := &rpcServer{
				sb:           minerSealer,
				storageCache: map[int64]database.StorageInfo{},
			}
			if err := rs.loadMinerStorage(ctx, minerApi); err != nil {
				return errors.As(err)
			}
		}

		var sector abi.SectorNumber = math.MaxUint64

		deadlines, err := fullApi.StateMinerDeadlines(ctx, act, types.EmptyTSK)
		if err != nil {
			return xerrors.Errorf("getting deadlines: %w", err)
		}
	out:
		for dlIdx := range deadlines {
			partitions, err := fullApi.StateMinerPartitions(ctx, act, uint64(dlIdx), types.EmptyTSK)
			if err != nil {
				return xerrors.Errorf("getting partitions for deadline %d: %w", dlIdx, err)
			}
			for _, partition := range partitions {
				b, err := partition.ActiveSectors.First()
				if err == bitfield.ErrNoBitsSet {
					continue
				}
				if err != nil {
					return err
				}
				sector = abi.SectorNumber(b)
				break out
			}
		}

		if sector == math.MaxUint64 {
			log.Info("skipping winning PoSt, no sectors")
			return nil
		}

		log.Infow("starting winning PoSt", "sector", sector)
		start := time.Now()

		var r abi.PoStRandomness = make([]byte, abi.RandomnessLength)
		_, _ = rand.Read(r)

		si, err := fullApi.StateSectorGetInfo(ctx, act, sector, types.EmptyTSK)
		if err != nil {
			return xerrors.Errorf("getting sector info: %w", err)
		}
		mid, err := address.IDFromAddress(act)
		if err != nil {
			return errors.As(err)
		}

		id := abi.SectorID{Miner: abi.ActorID(mid), Number: sector}
		sFile, err := minerApi.SnSectorFile(ctx, storage.SectorName(id))
		if err != nil {
			return errors.As(err)
		}
		rSector := storage.SectorRef{
			ID:         id,
			ProofType:  si.SealProof,
			SectorFile: *sFile,
		}

		if !cctx.Bool("do-wnpost") {
			return nil
		}

		_, err = minerSealer.GenerateWinningPoSt(ctx, abi.ActorID(mid), []storage.ProofSectorInfo{
			{
				SectorRef: rSector,
				SealedCID: si.SealedCID,
			},
		}, r)
		if err != nil {
			return xerrors.Errorf("failed to compute proof: %w", err)
		}
		log.Infow("winning PoSt successful", "took", time.Now().Sub(start))
		return nil
	},
}
