package miner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/filecoin-project/lotus/api/v1api"

	proof2 "github.com/filecoin-project/specs-actors/v2/actors/runtime/proof"

	"github.com/filecoin-project/lotus/chain/gen/slashfilter"

	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/crypto"
	lru "github.com/hashicorp/golang-lru"

	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/build"
	"github.com/filecoin-project/lotus/chain/gen"
	"github.com/filecoin-project/lotus/chain/store"
	"github.com/filecoin-project/lotus/chain/types"
	"github.com/filecoin-project/lotus/journal"

	logging "github.com/ipfs/go-log/v2"
	"go.opencensus.io/trace"
	"golang.org/x/xerrors"

	"github.com/gwaylib/errors"
)

var log = logging.Logger("miner")

// Journal event types.
const (
	evtTypeBlockMined = iota
)

// waitFunc is expected to pace block mining at the configured network rate.
//
// baseTime is the timestamp of the mining base, i.e. the timestamp
// of the tipset we're planning to construct upon.
//
// Upon each mining loop iteration, the returned callback is called reporting
// whether we mined a block in this round or not.
type waitFunc func(ctx context.Context, baseTime uint64) (func(bool, abi.ChainEpoch, error), abi.ChainEpoch, error)

func randTimeOffset(width time.Duration) time.Duration {
	buf := make([]byte, 8)
	rand.Reader.Read(buf) //nolint:errcheck
	val := time.Duration(binary.BigEndian.Uint64(buf) % uint64(width))

	return val - (width / 2)
}

// NewMiner instantiates a miner with a concrete WinningPoStProver and a miner
// address (which can be different from the worker's address).
func NewMiner(api v1api.FullNode, epp gen.WinningPoStProver, addr address.Address, sf *slashfilter.SlashFilter, j journal.Journal) *Miner {
	arc, err := lru.NewARC(10000)
	if err != nil {
		panic(err)
	}

	return &Miner{
		api:     api,
		epp:     epp,
		address: addr,
		waitFunc: func(ctx context.Context, baseTime uint64) (func(bool, abi.ChainEpoch, error), abi.ChainEpoch, error) {
			// wait around for half the block time in case other parents come in
			//
			// if we're mining a block in the past via catch-up/rush mining,
			// such as when recovering from a network halt, this sleep will be
			// for a negative duration, and therefore **will return
			// immediately**.
			//
			// the result is that we WILL NOT wait, therefore fast-forwarding
			// and thus healing the chain by backfilling it with null rounds
			// rapidly.
			deadline := baseTime + build.PropagationDelaySecs
			baseT := time.Unix(int64(deadline), 0)

			baseT = baseT.Add(randTimeOffset(time.Second))

			build.Clock.Sleep(build.Clock.Until(baseT))

			return func(bool, abi.ChainEpoch, error) {}, 0, nil
		},

		sf:                sf,
		minedBlockHeights: arc,
		evtTypes: [...]journal.EventType{
			evtTypeBlockMined: j.RegisterEventType("miner", "block_mined"),
		},
		journal: j,
	}
}

// Miner encapsulates the mining processes of the system.
//
// Refer to the godocs on mineOne and mine methods for more detail.
type Miner struct {
	api v1api.FullNode

	epp gen.WinningPoStProver

	lk       sync.Mutex
	address  address.Address
	stop     chan struct{}
	stopping chan struct{}

	waitFunc waitFunc

	// lastWork holds the last MiningBase we built upon.
	lastWork *MiningBase

	sf *slashfilter.SlashFilter
	// minedBlockHeights is a safeguard that caches the last heights we mined.
	// It is consulted before publishing a newly mined block, for a sanity check
	// intended to avoid slashings in case of a bug.
	minedBlockHeights *lru.ARCCache

	evtTypes [1]journal.EventType
	journal  journal.Journal
}

// Address returns the address of the miner.
func (m *Miner) Address() address.Address {
	m.lk.Lock()
	defer m.lk.Unlock()

	return m.address
}

// Start starts the mining operation. It spawns a goroutine and returns
// immediately. Start is not idempotent.
func (m *Miner) Start(_ context.Context) error {
	m.lk.Lock()
	defer m.lk.Unlock()
	if m.stop != nil {
		return fmt.Errorf("miner already started")
	}
	m.stop = make(chan struct{})
	go m.mine(context.TODO())
	return nil
}

// Stop stops the mining operation. It is not idempotent, and multiple adjacent
// calls to Stop will fail.
func (m *Miner) Stop(ctx context.Context) error {
	m.lk.Lock()

	m.stopping = make(chan struct{})
	stopping := m.stopping
	close(m.stop)

	m.lk.Unlock()

	select {
	case <-stopping:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Miner) niceSleep(d time.Duration) bool {
	select {
	case <-build.Clock.After(d):
		return true
	case <-m.stop:
		log.Infow("received interrupt while trying to sleep in mining cycle")
		return false
	}
}
func nextRoundTime(base *MiningBase) time.Time {
	return time.Unix(int64(base.TipSet.MinTimestamp())+int64(build.BlockDelaySecs)*int64((base.NullRounds+1)), 0)
}
func (m *Miner) mine(ctx context.Context) {
	ctx, span := trace.StartSpan(ctx, "/mine")
	defer span.End()

	go m.doWinPoStWarmup(ctx)

	var lastBase MiningBase
	var nextRound time.Time

	for {
		select {
		case <-m.stop:
			stopping := m.stopping
			m.stop = nil
			m.stopping = nil
			close(stopping)
			return

		default:
		}
	miningBegin:

		oldbase := lastBase
		prebase, err := m.GetBestMiningCandidate(ctx)
		if err != nil {
			log.Errorf("failed to get best mining candidate: %s", err)
			m.niceSleep(time.Second * 1)
			continue
		}
		miningHeight := prebase.TipSet.Height() + 1
		if lastBase.TipSet != nil && lastBase.TipSet.Height()+lastBase.NullRounds > prebase.TipSet.Height() {
			miningHeight = lastBase.TipSet.Height() + lastBase.NullRounds + 1
		}
		// just wait for the beacon entry to become available before we select our final mining base
		_, err = m.api.BeaconGetEntry(ctx, miningHeight)
		if err != nil {
			log.Errorf("failed getting beacon entry: %s", err)
			m.niceSleep(time.Second * 1)
			continue
		}
		if !prebase.TipSet.Equals(lastBase.TipSet) {
			base := prebase
			// cause by net delay, skiping for a late tipset in begining of genesis node.
			now := time.Now()
			delay := (time.Duration(build.PropagationDelaySecs*3) * time.Second) - now.Sub(nextRound)
			log.Infof("Waiting PropagationDelay time: %s", delay)
			if delay > 0 && now.Sub(nextRound) > 0 {
				time.Sleep(delay + (time.Second / 2)) // offset 500ms
				// update the parent weight
				base, err = m.GetBestMiningCandidate(ctx)
				if err != nil {
					log.Errorf("failed to get best mining candidate: %s", err)
					continue
				}
			}

			log.Infof("Update %s base height to: %d", m.address, base.TipSet.Height())

			nextRound = nextRoundTime(base)
			lastBase = *base
		} else {
			now := time.Now()
			// if the base was dead, make the nullRound++ step by round actually change.
			// and in current round, checking the base by every 1 second until pass or round out.
			if lastBase.TipSet == nil || (now.Unix() < nextRound.Unix()+int64(2*build.PropagationDelaySecs)) {
				time.Sleep(1e9)
				continue
			} else if now.Unix() > (nextRound.Unix() + int64(build.BlockDelaySecs)) {
				// slowly down to mining a null round.
				time.Sleep(time.Duration(build.PropagationDelaySecs) * 1e9)
			}

		syncLoop:
			syncing := false
			state, err := m.api.SyncState(ctx)
			if err == nil {
				var maxCurHeight, maxTargetHeight abi.ChainEpoch
				for _, ss := range state.ActiveSyncs {
					if ss.Target == nil {
						continue
					}
					if ss.Height > maxCurHeight {
						maxCurHeight = ss.Height
					}
					if ss.Target.Height() > maxTargetHeight {
						maxTargetHeight = ss.Target.Height()
					}
				}
				// maybe > 1?
				if maxTargetHeight > 0 && maxTargetHeight-maxCurHeight > 0 {
					syncing = true
					log.Infof("Waiting chain sync, current:%d, target:%d", maxCurHeight, maxTargetHeight)
					time.Sleep(5e9)
					goto syncLoop
				}
				if syncing {
					goto miningBegin
				}
			}

			log.Infof("BestMiningCandidate from the previous(%d) round: %s (nulls:%d)", lastBase.TipSet.Height(), lastBase.TipSet.Cids(), lastBase.NullRounds)
			lastBase.NullRounds++
			nextRound = nextRoundTime(&lastBase)
		}

		log.Infof("Trying mineOne")
		b, err := m.mineOne(ctx, &oldbase, &lastBase)
		if err != nil {
			log.Errorf("mining block failed: %+v", err)
			m.niceSleep(time.Second)
			//onDone(false, 0, err)
			continue
		}
		//lastBase = *base

		//var h abi.ChainEpoch
		//if b != nil {
		//	h = b.Header.Height
		//}
		//onDone(b != nil, h, nil)

		if b != nil {
			m.journal.RecordEvent(m.evtTypes[evtTypeBlockMined], func() interface{} {
				return map[string]interface{}{
					"parents":   lastBase.TipSet.Cids(),
					"nulls":     lastBase.NullRounds,
					"epoch":     b.Header.Height,
					"timestamp": b.Header.Timestamp,
					"cid":       b.Header.Cid(),
				}
			})

			btime := time.Unix(int64(b.Header.Timestamp), 0)
			now := build.Clock.Now()
			switch {
			case btime == now:
				// block timestamp is perfectly aligned with time.
			case btime.After(now):
				if !m.niceSleep(build.Clock.Until(btime)) {
					log.Warnf("received interrupt while waiting to broadcast block, will shutdown after block is sent out")
					build.Clock.Sleep(build.Clock.Until(btime))
				}
			default:
				log.Warnw("mined block in the past",
					"block-time", btime, "time", build.Clock.Now(), "difference", build.Clock.Since(btime))
			}

			if err := m.sf.MinedBlock(b.Header, lastBase.TipSet.Height()+lastBase.NullRounds); err != nil {
				log.Errorf("<!!> SLASH FILTER ERROR: %s", err)
				continue
			}

			blkKey := fmt.Sprintf("%d", b.Header.Height)
			if _, ok := m.minedBlockHeights.Get(blkKey); ok {
				log.Warnw("Created a block at the same height as another block we've created", "height", b.Header.Height, "miner", b.Header.Miner, "parents", b.Header.Parents)
				// in case for nullRound, it will happend the same block.
				continue
			}

			m.minedBlockHeights.Add(blkKey, true)

			if err := m.api.SyncSubmitBlock(ctx, b); err != nil {
				log.Errorf("failed to submit newly mined block: %+v", err)
			}
		} else {
			// Wait until the next epoch, plus the propagation delay, so a new tipset
			// has enough time to form.
			//
			// See:  https://github.com/filecoin-project/lotus/issues/1845
			//nextRound := time.Unix(int64(base.TipSet.MinTimestamp()+build.BlockDelaySecs*uint64(base.NullRounds))+int64(build.PropagationDelaySecs), 0)

			log.Info("mine next round at:", nextRound.Format(time.RFC3339))

			select {
			case <-build.Clock.After(build.Clock.Until(nextRound)):
			case <-m.stop:
				stopping := m.stopping
				m.stop = nil
				m.stopping = nil
				close(stopping)
				return
			}
		}
	}
}

// MiningBase is the tipset on top of which we plan to construct our next block.
// Refer to godocs on GetBestMiningCandidate.
type MiningBase struct {
	TipSet     *types.TipSet
	NullRounds abi.ChainEpoch
}

// GetBestMiningCandidate implements the fork choice rule from a miner's
// perspective.
//
// It obtains the current chain head (HEAD), and compares it to the last tipset
// we selected as our mining base (LAST). If HEAD's weight is larger than
// LAST's weight, it selects HEAD to build on. Else, it selects LAST.
func (m *Miner) GetBestMiningCandidate(ctx context.Context) (*MiningBase, error) {
	m.lk.Lock()
	defer m.lk.Unlock()

	bts, err := m.api.ChainHead(ctx)
	if err != nil {
		return nil, err
	}

	if m.lastWork != nil {
		if m.lastWork.TipSet.Equals(bts) {
			return m.lastWork, nil
		}

		btsw, err := m.api.ChainTipSetWeight(ctx, bts.Key())
		if err != nil {
			m.lastWork = nil
			return nil, err
		}
		ltsw, err := m.api.ChainTipSetWeight(ctx, m.lastWork.TipSet.Key())
		if err != nil {
			m.lastWork = nil
			return nil, err
		}

		if types.BigCmp(btsw, ltsw) <= 0 {
			return m.lastWork, nil
		}
	}

	m.lastWork = &MiningBase{TipSet: bts}
	return m.lastWork, nil
}

// mineOne attempts to mine a single block, and does so synchronously, if and
// only if we are eligible to mine.
//
// {hint/landmark}: This method coordinates all the steps involved in mining a
// block, including the condition of whether mine or not at all depending on
// whether we win the round or not.
//
// This method does the following:
//
//  1.
func (m *Miner) mineOne(ctx context.Context, oldbase, base *MiningBase) (*types.BlockMsg, error) {
	log.Debugw("attempting to mine a block", "tipset", types.LogCids(base.TipSet.Cids()))
	start := build.Clock.Now()

	round := base.TipSet.Height() + base.NullRounds + 1

	mbi, err := m.api.MinerGetBaseInfo(ctx, m.address, round, base.TipSet.Key())
	if err != nil {
		return nil, xerrors.Errorf("failed to get mining base info: %w", err)
	}
	if mbi == nil {
		log.Info("No mbi")
		return nil, nil
	}
	if !mbi.EligibleForMining {
		// slashed or just have no power yet
		log.Info("No EligibleForMining")
		return nil, nil
	}

	tMBI := build.Clock.Now()

	beaconPrev := mbi.PrevBeaconEntry

	tDrand := build.Clock.Now()
	bvals := mbi.BeaconEntries

	tPowercheck := build.Clock.Now()

	preHeight := abi.ChainEpoch(0)
	if oldbase.TipSet != nil {
		preHeight = oldbase.TipSet.Height()
	}
	log.Infof(
		"Time delta between now and our mining, base(%d|%d):%ds (nulls: %d), power(%s/%s)",
		preHeight, base.TipSet.Height(),
		uint64(build.Clock.Now().Unix())-base.TipSet.MinTimestamp(), base.NullRounds,
		mbi.MinerPower, mbi.NetworkPower,
	)

	rbase := beaconPrev
	if len(bvals) > 0 {
		rbase = bvals[len(bvals)-1]
	}

	ticket, err := m.computeTicket(ctx, &rbase, base, mbi)
	if err != nil {
		return nil, xerrors.Errorf("scratching ticket failed: %w", err)
	}

	winner, err := gen.IsRoundWinner(ctx, base.TipSet, round, m.address, rbase, mbi, m.api)
	if err != nil {
		return nil, xerrors.Errorf("failed to check if we win next round: %w", err)
	}

	if winner == nil {
		log.Info("Not Win")
		return nil, nil
	}

	tTicket := build.Clock.Now()

	buf := new(bytes.Buffer)
	if err := m.address.MarshalCBOR(buf); err != nil {
		return nil, xerrors.Errorf("failed to marshal miner address: %w", err)
	}

	rand, err := store.DrawRandomness(rbase.Data, crypto.DomainSeparationTag_WinningPoStChallengeSeed, round, buf.Bytes())
	if err != nil {
		return nil, xerrors.Errorf("failed to get randomness for winning post: %w", err)
	}

	prand := abi.PoStRandomness(rand)

	tSeed := build.Clock.Now()

	postProof, err := m.epp.ComputeProof(ctx, mbi.Sectors, prand)
	if err != nil {
		log.Warn(errors.As(err, mbi.Sectors, prand))
		return nil, xerrors.Errorf("failed to compute winning post proof: %w", err)
	}

	tProof := build.Clock.Now()

	// get pending messages early,
	msgs, err := m.api.MpoolSelect(context.TODO(), base.TipSet.Key(), ticket.Quality())
	if err != nil {
		return nil, xerrors.Errorf("failed to select messages for block: %w", err)
	}

	tPending := build.Clock.Now()

	// TODO: winning post proof
	b, err := m.createBlock(base, m.address, ticket, winner, bvals, postProof, msgs)
	if err != nil {
		return nil, xerrors.Errorf("failed to create block: %w", err)
	}

	tCreateBlock := build.Clock.Now()
	dur := tCreateBlock.Sub(start)
	parentMiners := make([]address.Address, len(base.TipSet.Blocks()))
	for i, header := range base.TipSet.Blocks() {
		parentMiners[i] = header.Miner
	}
	log.Infow("mined new block",
		"cid", b.Cid(),
		"height", b.Header.Height,
		"weight", b.Header.ParentWeight,
		"rounds", base.NullRounds,
		"took", dur,
		"miner", b.Header.Miner,
		"parents", parentMiners,
		"submit", time.Unix(int64(base.TipSet.MinTimestamp()+(uint64(base.NullRounds)+1)*build.BlockDelaySecs), 0).Format(time.RFC3339))
	if dur > time.Second*time.Duration(build.BlockDelaySecs) {
		log.Warnw("CAUTION: block production took longer than the block delay. Your computer may not be fast enough to keep up",
			"tMinerBaseInfo ", tMBI.Sub(start),
			"tDrand ", tDrand.Sub(tMBI),
			"tPowercheck ", tPowercheck.Sub(tDrand),
			"tTicket ", tTicket.Sub(tPowercheck),
			"tSeed ", tSeed.Sub(tTicket),
			"tProof ", tProof.Sub(tSeed),
			"tPending ", tPending.Sub(tProof),
			"tCreateBlock ", tCreateBlock.Sub(tPending))
	}

	return b, nil
}

func (m *Miner) computeTicket(ctx context.Context, brand *types.BeaconEntry, base *MiningBase, mbi *api.MiningBaseInfo) (*types.Ticket, error) {
	buf := new(bytes.Buffer)
	if err := m.address.MarshalCBOR(buf); err != nil {
		return nil, xerrors.Errorf("failed to marshal address to cbor: %w", err)
	}

	round := base.TipSet.Height() + base.NullRounds + 1
	if round > build.UpgradeSmokeHeight {
		buf.Write(base.TipSet.MinTicket().VRFProof)
	}

	input, err := store.DrawRandomness(brand.Data, crypto.DomainSeparationTag_TicketProduction, round-build.TicketRandomnessLookback, buf.Bytes())
	if err != nil {
		return nil, err
	}

	vrfOut, err := gen.ComputeVRF(ctx, m.api.WalletSign, mbi.WorkerKey, input)
	if err != nil {
		return nil, err
	}

	return &types.Ticket{
		VRFProof: vrfOut,
	}, nil
}

func (m *Miner) createBlock(base *MiningBase, addr address.Address, ticket *types.Ticket,
	eproof *types.ElectionProof, bvals []types.BeaconEntry, wpostProof []proof2.PoStProof, msgs []*types.SignedMessage) (*types.BlockMsg, error) {
	uts := base.TipSet.MinTimestamp() + build.BlockDelaySecs*(uint64(base.NullRounds)+1)

	nheight := base.TipSet.Height() + base.NullRounds + 1

	// why even return this? that api call could just submit it for us
	return m.api.MinerCreateBlock(context.TODO(), &api.BlockTemplate{
		Miner:            addr,
		Parents:          base.TipSet.Key(),
		Ticket:           ticket,
		Eproof:           eproof,
		BeaconValues:     bvals,
		Messages:         msgs,
		Epoch:            nheight,
		Timestamp:        uts,
		WinningPoStProof: wpostProof,
	})
}
