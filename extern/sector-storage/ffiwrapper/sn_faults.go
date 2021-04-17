package ffiwrapper

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	ffi "github.com/filecoin-project/filecoin-ffi"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/extern/sector-storage/storiface"
	miner0 "github.com/filecoin-project/specs-actors/actors/builtin/miner"
	"github.com/filecoin-project/specs-actors/actors/runtime/proof"
	"github.com/filecoin-project/specs-storage/storage"
	"github.com/gwaylib/errors"
)

type ProvableStat struct {
	Sector storage.SectorRef
	Used   time.Duration
	Err    error
}

type ProvableStatArr []ProvableStat

func (g ProvableStatArr) Len() int {
	return len(g)
}
func (g ProvableStatArr) Swap(i, j int) {
	g[i], g[j] = g[j], g[i]
}
func (g ProvableStatArr) Less(i, j int) bool {
	return g[i].Used < g[j].Used
}

// CheckProvable returns unprovable sectors
func CheckProvable(ctx context.Context, sectors []storage.SectorRef, rg storiface.RGetter, timeout time.Duration) ([]ProvableStat, []ProvableStat, []ProvableStat, error) {

	var good = []ProvableStat{}
	var goodLk = sync.Mutex{}
	var appendGood = func(sid ProvableStat) {
		goodLk.Lock()
		defer goodLk.Unlock()
		good = append(good, sid)
	}

	var bad = []ProvableStat{}
	var badLk = sync.Mutex{}
	var appendBad = func(sid ProvableStat) {
		badLk.Lock()
		defer badLk.Unlock()
		bad = append(bad, sid)
	}
	log.Infof("Manager.CheckProvable in, len:%d", len(sectors))
	defer log.Infof("Manager.CheckProvable out, good:%d,bad:%d", len(good), len(bad))

	var all = ProvableStatArr{}
	var allLk = sync.Mutex{}
	var appendAll = func(good ProvableStat) {
		allLk.Lock()
		defer allLk.Unlock()
		all = append(all, good)
	}

	checkBad := func(ctx context.Context, sector storage.SectorRef, rg storiface.RGetter, timeout time.Duration) error {
		if len(sector.StorageRepo) == 0 {
			return errors.New("StorageRepo not found").As(sector)
		}
		sid, err := sector.SectorID()
		if err != nil {
			return errors.As(err)
		}
		lp := storiface.SectorPaths{
			ID:       sid,
			Unsealed: sector.UnsealedFile(),
			Sealed:   sector.SealedFile(),
			Cache:    sector.CachePath(),
		}

		if lp.Sealed == "" || lp.Cache == "" {
			return errors.New("CheckProvable Sector FAULT: cache an/or sealed paths not found").As(sector, lp.Sealed, lp.Cache)
		}

		ssize, err := sector.ProofType.SectorSize()
		if err != nil {
			return errors.As(err)
		}
		toCheck := map[string]int64{
			lp.Sealed:                        int64(ssize),
			filepath.Join(lp.Cache, "p_aux"): 0,
		}
		//filepath.Join(lp.Cache, "t_aux"): 0, // no check for fake
		if _, ok := miner0.SupportedProofTypes[abi.RegisteredSealProof_StackedDrg2KiBV1]; !ok {
			toCheck[filepath.Join(lp.Cache, "t_aux")] = 0
		}

		addCachePathsForSectorSize(toCheck, lp.Cache, ssize)

		for p, sz := range toCheck {
			// checking data
			checkCtx, checkCancel := context.WithTimeout(ctx, timeout)
			defer checkCancel()
			checkDone := make(chan error, 1)
			go func() {
				st, err := os.Stat(p)
				if err != nil {
					checkDone <- errors.As(err, p)
					return
				}

				if sz != 0 {
					if st.Size() < sz {
						checkDone <- errors.New("CheckProvable Sector FAULT: sector file is wrong size").As(p, st.Size())
						return
					}
				}
				checkDone <- nil
				return
			}()

			select {
			case <-checkCtx.Done():
				return errors.New("context timeout").As(p)
			case <-ctx.Done():
				return errors.New("context canceled").As(p)
			case err := <-checkDone:
				if err != nil {
					return errors.As(err, p)
				}
				// continue
			}
		}
		if rg != nil {
			wpp, err := sector.ProofType.RegisteredWindowPoStProof()
			if err != nil {
				return err
			}

			var pr abi.PoStRandomness = make([]byte, abi.RandomnessLength)
			_, _ = rand.Read(pr)
			pr[31] &= 0x3f

			ch, err := ffi.GeneratePoStFallbackSectorChallenges(wpp, sector.ID.Miner, pr, []abi.SectorNumber{
				sector.ID.Number,
			})
			if err != nil {
				return errors.As(err)
			}

			commr, err := rg(ctx, sector.ID)
			if err != nil {
				return errors.As(err)
			}

			_, err = ffi.GenerateSingleVanillaProof(ffi.PrivateSectorInfo{
				SectorInfo: proof.SectorInfo{
					SealProof:    sector.ProofType,
					SectorNumber: sector.ID.Number,
					SealedCID:    commr,
				},
				CacheDirPath:     lp.Cache,
				PoStProofType:    wpp,
				SealedSectorPath: lp.Sealed,
			}, ch.Challenges[sector.ID.Number])
			if err != nil {
				return errors.As(err)
			}
		}

		return nil
	}
	// limit the gorouting to checking the bad, the sectors would be so a lots.
	// limit low donw to 256 cause by 'runtime/cgo: pthread_create failed: Resource temporarily unavailable' panic
	routines := make(chan bool, 256)
	done := make(chan bool, len(sectors))
	for _, sector := range sectors {
		go func(s storage.SectorRef) {
			// limit the concurrency
			routines <- true
			defer func() {
				// thread end
				done <- true
				<-routines
			}()
			start := time.Now()
			err := checkBad(ctx, s, rg, timeout)
			used := time.Now().Sub(start)

			pState := ProvableStat{Sector: s, Used: used, Err: err}
			appendAll(pState)
			if err != nil {
				appendBad(pState)
			} else {
				appendGood(pState)
			}

		}(sector)
	}
	for waits := len(sectors); waits > 0; waits-- {
		<-done
	}

	sort.Sort(all)
	return all, good, bad, nil
}

func addCachePathsForSectorSize(chk map[string]int64, cacheDir string, ssize abi.SectorSize) {
	switch ssize {
	case 2 << 10:
		fallthrough
	case 8 << 20:
		fallthrough
	case 512 << 20:
		chk[filepath.Join(cacheDir, "sc-02-data-tree-r-last.dat")] = 0
	case 32 << 30:
		// chk[filepath.Join(cacheDir, fmt.Sprintf("sc-02-data-tree-r-last-%d.dat", 0))] = 4586976 // just check one file cause it use a lots of time to check every files.
		for i := 0; i < 8; i++ {
			//chk[filepath.Join(cacheDir, fmt.Sprintf("sc-02-data-tree-r-last-%d.dat", i))] = 4586976
			chk[filepath.Join(cacheDir, fmt.Sprintf("sc-02-data-tree-r-last-%d.dat", i))] = 0
		}
	case 64 << 30:
		//chk[filepath.Join(cacheDir, fmt.Sprintf("sc-02-data-tree-r-last-%d.dat", 0))] = 4586976
		for i := 0; i < 16; i++ {
			chk[filepath.Join(cacheDir, fmt.Sprintf("sc-02-data-tree-r-last-%d.dat", i))] = 0
		}
	default:
		log.Warnf("not checking cache files of %s sectors for faults", ssize)
	}
}
