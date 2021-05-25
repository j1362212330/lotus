package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/filecoin-project/go-address"

	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/blockstore"

	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	lcli "github.com/filecoin-project/lotus/cli"
	"github.com/filecoin-project/lotus/extern/sector-storage/database"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper/basicfs"
	"github.com/filecoin-project/specs-storage/storage"
	"github.com/mitchellh/go-homedir"
	"github.com/urfave/cli/v2"
)

type P1Out struct {
	PreCommit1Out storage.PreCommit1Out
	SectorNumber  abi.SectorNumber
	ProofType     abi.RegisteredSealProof
}

var aplk sync.Mutex
var SectorBackCmd = &cli.Command{
	Name:  "sector_back",
	Usage: "Repair errors throught sector backup",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "storage_path",
			Usage: "speific file storage path",
			Value: "",
		},
		&cli.IntFlag{
			Name:  "parallel_count",
			Usage: "speific parrell_count",
			Value: 2,
		},
		&cli.BoolFlag{
			Name:  "is_push",
			Usage: "push : true  , not push : false ",
			Value: false,
		},
	},
	Action: func(cctx *cli.Context) error {
		nodeCCtx = cctx
		//存储路径
		path := cctx.String("storage_path")
		if len(path) == 0 {
			return fmt.Errorf("not specific path")
		}
		workerRepo, err := homedir.Expand(path)
		if err != nil {
			return err
		}
		log.Info("workerRepo : ", workerRepo)
		//获取 api
		minerApi, err := GetNodeApi()
		if err != nil {
			log.Error("getting miner api : ", err)
			return err
		}
		defer ReleaseNodeApi(true)

		fullApi, acloser, err := lcli.GetFullNodeAPI(cctx)
		if err != nil {
			return err
		}
		defer acloser()

		ctx, cancel := context.WithCancel(lcli.ReqContext(cctx))
		defer cancel()

		stor := store.ActorStore(ctx, blockstore.NewAPIBlockstore(fullApi))
		//获取 actor
		log.Infof("getting miner actor")
		maddr, err := minerApi.ActorAddress(ctx)
		if err != nil {
			return err
		}

		mid, err := address.IDFromAddress(maddr)
		if err != nil {
			return err
		}
		log.Infof("actor ID : %d", mid)
		mact, err := fullApi.StateGetActor(ctx, maddr, types.EmptyTSK)
		if err != nil {
			return err
		}

		mas, err := miner.Load(stor, mact)
		if err != nil {
			return err
		}
		//创建日志
		file, err := os.Create(fmt.Sprintf("sectorlog_%d_%s.txt", mid, time.Now().Format("2006-01-02T15:04:05")))
		if err != nil {
			return fmt.Errorf("creat log error")
		}
		defer file.Close()

		//创建在p2出错时记录p1输出的日志

		p1OutLog, err := os.Create("p1OutLog.log")
		if err != nil {
			return fmt.Errorf("creat log error")
		}
		defer p1OutLog.Close()

		workerSealer, err := ffiwrapper.New(ffiwrapper.RemoteCfg{}, &basicfs.Provider{
			Root: workerRepo,
		})
		if err != nil {
			return err
		}
		//并行数量
		parallelCount := cctx.Int("parallel_count")
		gothread := make(chan struct{}, parallelCount)
		for i := 0; i < parallelCount; i++ {
			gothread <- struct{}{}
		}
		var pushNoticeCh = make(chan storage.SectorRef, parallelCount)
		defer close(pushNoticeCh)
		var p1OutCh = make(chan P1Out, parallelCount)
		defer close(p1OutCh)
		sealProcess := func(status api.SectorInfo) {
			defer func() {
				gothread <- struct{}{}
			}()
			sid := storage.SectorRef{
				ID: abi.SectorID{
					Miner:  abi.ActorID(mid),
					Number: status.SectorID,
				},
				ProofType: status.SealProof,
			}
			sectorSize, err := status.SealProof.SectorSize()
			if err != nil {
				log.Error("sectorSize error : ", sectorSize)
				p1OutCh <- P1Out{}
				return
			}
			log.Info("ProofType : ", status.SealProof, "sectorSize : ", sectorSize)
			size := abi.PaddedPieceSize(sectorSize).Unpadded()
			//addpiece
			var pieceInfo []abi.PieceInfo
			for i := 0; i < 3; i++ {
				aplk.Lock()
				pieceInfo, err = workerSealer.PledgeSector(ctx, sid, nil, size)
				if err != nil {
					log.Error("AddPiece error : ", err.Error())
					ReleaseNodeApi(false)
				ApFlag:
					_, e := GetNodeApi()
					if e != nil {
						log.Warn("addpiece error : ", err.Error())
						time.Sleep(time.Second * 3)
						goto ApFlag
					}
					aplk.Unlock()
					continue
				}
				aplk.Unlock()
				break
			}
			if err != nil {
				p1OutCh <- P1Out{}
				return
			}
			log.Info("pieceInfo : ", pieceInfo)
			//p1
			var preCommit1Out storage.PreCommit1Out
			for i := 0; i < 3; i++ {
				preCommit1Out, err = workerSealer.SealPreCommit1(ctx, sid, status.Ticket.Value, pieceInfo)
				if err != nil {
					log.Error("SealPreCommit1 error : ", err.Error())
					ReleaseNodeApi(false)
				P1Flag:
					_, e := GetNodeApi()
					if e != nil {
						log.Warn("p1 error : ", err.Error())
						time.Sleep(time.Second * 3)
						goto P1Flag
					}
					continue
				}
				break
			}
			if err != nil {
				p1OutCh <- P1Out{}
				return
			}
			log.Info("preCommit1Out : ", preCommit1Out)
			p1OutCh <- P1Out{
				PreCommit1Out: preCommit1Out,
				ProofType:     status.SealProof,
				SectorNumber:  status.SectorID,
			}
		}

		isPush := cctx.Bool("is_push")
		go func() {
			for {
				select {
				case p1Out, ok := <-p1OutCh:
					if !ok {
						log.Info("p1Out closed , p2 exit...")
						return
					}
					sid := storage.SectorRef{
						ID: abi.SectorID{
							Miner:  abi.ActorID(mid),
							Number: p1Out.SectorNumber,
						},
						ProofType: p1Out.ProofType,
					}
					if len(p1Out.PreCommit1Out) == 0 {
						log.Error("error ...")
						continue
					}
					//p2
					log.Info("start SealPreCommit2")
					for i := 0; i < 3; i++ {
						_, err = workerSealer.SealPreCommit2(ctx, sid, p1Out.PreCommit1Out)
						if err != nil {
							log.Error("SealPreCommit2 error : ", err.Error())
							ReleaseNodeApi(false)
						P2Flag:
							_, e := GetNodeApi()
							if e != nil {
								log.Warn("p2 error : ", err.Error())
								time.Sleep(time.Second * 3)
								goto P2Flag
							}
							continue
						}
						break
					}
					if err != nil {
						b, err := json.Marshal(p1Out)
						if err == nil {
							p1OutLog.Write(b)
						}
						continue
					}
					err = workerSealer.FinalizeSector(ctx, sid, nil)
					if err != nil {
						log.Error("FinalizeSector error : ", err.Error())
						continue
					}
					if isPush {
						pushNoticeCh <- sid
					}
				}
			}
		}()
		go func() {
			for {
				select {
				case sid, ok := <-pushNoticeCh:
					if !ok {
						log.Info("pushNoticeCh closed , push exit...")
						return
					}
					for i := 0; i < 3; i++ {
						err = PushCache(ctx, workerSealer, sid, minerApi)
						if err != nil {
							log.Error("PushCache error : ", err.Error())
							ReleaseNodeApi(false)
						PushFlag:
							_, e := GetNodeApi()
							if e != nil {
								log.Warn("PushCache error : ", err.Error())
								time.Sleep(time.Second * 3)
								goto PushFlag
							}
							continue
						}
						break
					}
				}
			}
		}()

		http.HandleFunc("/p1", func(w http.ResponseWriter, r *http.Request) {
			P1File := r.URL.Query().Get("path")
			dIndexStr := r.URL.Query().Get("dIndex")
			var dIndex uint64 = 48
			if len(dIndexStr) > 0 {
				dIndex, err = strconv.ParseUint(dIndexStr, 10, 64)
				if err != nil {
					log.Error("dIndex ParseUint error : ", err.Error())
					dIndex = 48
				}
			}
			log.Info("P1File : ", P1File, "deadline : ", dIndex)
			sectorNumber := GetSectorNumber(P1File, dIndex, mas)
			go func() {
				for _, number := range sectorNumber {
					status, err := minerApi.SectorsStatus(ctx, abi.SectorNumber(number), true)
					if err != nil {
						log.Error("sector status error : ", number)
						continue
					}
					<-gothread
					file.WriteString(fmt.Sprintf("%d", number) + "\n")

					go sealProcess(status)
				}
			}()
		})
		http.HandleFunc("/p2", func(w http.ResponseWriter, r *http.Request) {
			P2File := r.URL.Query().Get("path")
			log.Info("start read : ", P2File)
			b, err := ioutil.ReadFile(P2File)
			ress := []P1Out{}
			if err == nil && len(b) > 0 {
				err = json.Unmarshal(b, &ress)
				if err == nil {
					go func() {
						for _, res := range ress {
							log.Info("http p2 ...")
							p1OutCh <- res
						}
					}()
				}
			} else {
				log.Error("P2File Unmarshal error : ", err.Error())
			}
		})

		http.HandleFunc("/push", func(w http.ResponseWriter, r *http.Request) {
			pushFile := r.URL.Query().Get("path")
			log.Info("start read : ", pushFile)
			b, err := ioutil.ReadFile(pushFile)
			ress := []P1Out{}
			if err == nil && len(b) > 0 {
				err = json.Unmarshal(b, &ress)
				if err == nil {
					go func() {
						for _, res := range ress {
							sid := storage.SectorRef{
								ID: abi.SectorID{
									Miner:  abi.ActorID(mid),
									Number: res.SectorNumber,
								},
								ProofType: res.ProofType,
							}
							log.Info("http push ...")
							pushNoticeCh <- sid
						}
					}()
				}
			} else {
				log.Error("pushFile Unmarshal error : ", err.Error())
			}
		})
		go http.ListenAndServe(":9999", nil)
		for {
			select {
			case <-ctx.Done():
				log.Info("PROCESS END")
				return nil
			}
		}
	},
}

func PushCache(ctx context.Context, workerSealer *ffiwrapper.Sealer, sid storage.SectorRef, minerApi api.StorageMiner) error {
	// err := workerSealer.FinalizeSector(ctx, sid, nil)
	// if err != nil {
	// 	log.Error("FinalizeSector error : ", err.Error())
	// 	return err
	// }
	err := Push(ctx, workerSealer, sid, minerApi)
	if err != nil {
		log.Error("push error : ", err.Error())
		return err
	}
	RemoveCache(workerSealer, sid)
	return nil
}

func Push(ctx context.Context, workerSealer *ffiwrapper.Sealer, sid storage.SectorRef, minerApi api.StorageMiner) error {
	sectorName := storage.SectorName(sid.ID)
	sealed := workerSealer.SectorPath("sealed", sectorName)
	log.Info("sealed path : ", sealed)
	cache := workerSealer.SectorPath("cache", sectorName)
	log.Info("cache path : ", cache)
	info, err := minerApi.SnSectorGetState(ctx, sectorName)
	if err != nil {
		log.Error(" SnSectorGetState error : ", err.Error())
		return err
	}
	if info.State != 200 || info.StorageId <= 0 {
		log.Warn("sector terminate or no allocate storage")
		return nil
	}
	storageInfo, err := minerApi.GetSNStorage(ctx, info.StorageId)
	if err != nil {
		log.Error(" GetSNStorage error : ", err.Error())
		return err
	}
	mountDir := filepath.Join(workerSealer.RepoPath(), "push", sectorName)
	if err := os.MkdirAll(mountDir, 0755); err != nil {
		return err
	}

	if err := database.Mount(
		storageInfo.MountType,
		storageInfo.MountTransfUri,
		mountDir,
		storageInfo.MountOpt,
	); err != nil {
		log.Error("Mount error : ", err)
		return err
	}
	defer Umount(mountDir)
	toSealedPath := filepath.Join(mountDir, "sealed", sectorName)
	log.Info("toSealedPath : ", toSealedPath)
	if err := CopyFile(ctx, sealed, toSealedPath); err != nil {
		log.Error("Copy sealed file error : ", err.Error())
		return err
	}
	toCachePath := filepath.Join(mountDir, "cache", sectorName)
	log.Info("toCachePath : ", toCachePath)
	if err := CopyFile(ctx, cache+"/", toCachePath+"/"); err != nil {
		log.Error("Copy cache file error : ", err.Error())
		return err
	}
	return nil
}

func Umount(mountDir string) error {
	if _, err := database.Umount(mountDir); err != nil {
		log.Error("Umount error : ", err.Error())
		return err
	}
	log.Infof("Remove mount point:%s", mountDir)
	if err := os.RemoveAll(mountDir); err != nil {
		return err
	}
	return nil
}

func RemoveCache(workerSealer *ffiwrapper.Sealer, sid storage.SectorRef) error {
	sectorName := storage.SectorName(sid.ID)
	if err := os.RemoveAll(workerSealer.SectorPath("sealed", sectorName)); err != nil {
		log.Error("remove sealed error : ", err.Error())
		return err
	}
	if err := os.RemoveAll(workerSealer.SectorPath("cache", sectorName)); err != nil {
		log.Error("remove cache error : ", err.Error())
		return err
	}
	if err := os.RemoveAll(workerSealer.SectorPath("unsealed", sectorName)); err != nil {
		log.Error("remove unsealed error : ", err.Error())
		return err
	}
	return nil
}

func GetSectorNumber(filePath string, dIndex uint64, mas miner.State) []uint64 {
	sectorNumber := []uint64{}
	if dIndex >= 0 && dIndex <= 47 {
		err := mas.ForEachDeadline(func(dlIdx uint64, dl miner.Deadline) error {
			if dlIdx != dIndex {
				return nil
			}
			return dl.ForEachPartition(func(partIdx uint64, part miner.Partition) error {
				faults, err := part.FaultySectors()
				if err != nil {
					return err
				}
				return faults.ForEach(func(num uint64) error {
					sectorNumber = append(sectorNumber, num)
					return nil
				})
			})
		})
		if err != nil {
			log.Error(err.Error())
			return nil
		}
	}
	if len(filePath) > 0 {
		sectorNumbers, err := ioutil.ReadFile(filePath)
		if err != nil {
			return nil
		}
		sectorNumberStr := strings.Split(string(sectorNumbers), "\n")
		for _, v := range sectorNumberStr {
			sn := strings.TrimSpace(v)
			if len(sn) == 0 {
				continue
			}
			log.Info("number : ", v)
			num, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				log.Error("sector number parser error :", num, err.Error())
				continue
			}
			sectorNumber = append(sectorNumber, num)
		}
	}
	return sectorNumber
}
