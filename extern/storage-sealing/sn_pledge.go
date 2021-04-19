package sealing

import (
	"context"
	"errors"
	"sync"
	"time"

	sectorstorage "github.com/filecoin-project/lotus/extern/sector-storage"
	"github.com/filecoin-project/lotus/extern/sector-storage/ffiwrapper"
	"golang.org/x/xerrors"
)

var (
	pledgeExit    = make(chan bool, 1)
	pledgeRunning = false
	pledgeSync    = sync.Mutex{}
)

var (
	taskConsumed = make(chan int, 1)
)

func (m *Sealing) addConsumedTask() {
	select {
	case taskConsumed <- 1:
	default:
		//igonre
	}
}

// RunPledgeSector start pledging garbage data into sectors
func (m *Sealing) RunPledgeSector() error {
	pledgeSync.Lock()
	defer pledgeSync.Unlock()
	if pledgeRunning == true {
		return errors.New("In Runing")
	}

	pledgeRunning = true

	log.Infof("Pledge garbage start")

	sb := m.sealer.(*sectorstorage.Manager).Prover.(*ffiwrapper.Sealer)
	sb.SetPledgeListener(func(t ffiwrapper.WorkerTask) {
		m.addConsumedTask()
	})

	m.addConsumedTask() //for init

	gcTimer := time.NewTicker(10 * time.Minute)

	go func() {
		defer func() {
			pledgeRunning = false
			sb.SetPledgeListener(nil)
			gcTimer.Stop()
			log.Infof("Pledge daemon exited.")

			if err := recover(); err != nil {
				log.Error(xerrors.Errorf("Pledge daemon  not exit by normal, goto auto restart: %s", err))
				m.RunPledgeSector()
			}
		}()

		for {
			pledgeRunning = true
			select {
			case <-pledgeExit:
				return
			case <-gcTimer.C:
				m.addConsumedTask()
			case <-taskConsumed:
				states := sb.GetPledgeWait()
				//not accurrate, if missing the ConsummedTask event, it should republish in gcTime
				if states > 0 {
					continue
				}

				go func() {
					if _, err := m.PledgeSector(context.TODO()); err != nil {
						log.Errorf("%+v", err)
					}

					// if err happend, need to control the times.
					time.Sleep(time.Second)

					//fast to generage one pledge event
					m.addConsumedTask()
				}()

			}
		}
	}()
	return nil
}

func (m *Sealing) StatusPledgeSector() (int, error) {
	pledgeSync.Lock()
	defer pledgeSync.Unlock()
	if !pledgeRunning {
		return 0, nil
	}
	return 1, nil
}

func (m *Sealing) ExitPledgeSector() error {
	pledgeSync.Lock()
	defer pledgeSync.Unlock()
	if !pledgeRunning {
		return xerrors.New("Not in Running")
	}

	if len(pledgeExit) > 0 {
		return xerrors.New("Exiting")
	}

	pledgeExit <- true
	log.Infof("Pledge garbage exit")
	return nil
}
